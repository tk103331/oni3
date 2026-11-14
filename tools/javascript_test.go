package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/tk103331/oni3/config"
	"gopkg.in/yaml.v3"
)

func javascriptFixture(t *testing.T, settings string) tool.InvokableTool {
	t.Helper()
	var cfg config.Tool
	if err := yaml.Unmarshal([]byte("type: javascript\n"+settings), &cfg); err != nil {
		t.Fatal(err)
	}
	instance, err := CreateTool("js", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

func runJavaScript(ctx context.Context, instance tool.InvokableTool, code string, input any) (string, error) {
	args := map[string]any{"code": code}
	if input != nil {
		args["input"] = input
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return instance.InvokableRun(ctx, string(encoded))
}

func TestJavaScriptAsyncInputResultAndLogs(t *testing.T) {
	instance := javascriptFixture(t, "")
	info, err := instance.Info(context.Background())
	if err != nil || info.Name != "js" {
		t.Fatalf("tool info: %+v / %v", info, err)
	}
	out, err := runJavaScript(context.Background(), instance, `
		console.log("你好", input.values);
		await new Promise(resolve => setTimeout(resolve, 1));
		console.info("ready"); console.warn(false); console.error(null, undefined);
		return {sum: input.values.reduce((a, b) => a + b, 0), text: "完成🙂"};
	`, map[string]any{"values": []int{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Result struct {
			Sum  int    `json:"sum"`
			Text string `json:"text"`
		} `json:"result"`
		Logs []string `json:"logs"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Result.Sum != 6 || got.Result.Text != "完成🙂" || strings.Join(got.Logs, "|") != "你好 [1,2,3]|ready|false|null undefined" {
		t.Fatalf("unexpected output: %s", out)
	}
	out, err = runJavaScript(context.Background(), instance, `console.log(Object.keys(input).length);`, nil)
	if err != nil || out != `{"result":null,"logs":["0"]}` {
		t.Fatalf("console-only script: %s / %v", out, err)
	}
}

func TestJavaScriptErrorsAndOutputLimits(t *testing.T) {
	instance := javascriptFixture(t, "")
	for _, tc := range []struct{ name, code, want string }{
		{"syntax", "return (", "SyntaxError"},
		{"throw", `throw new Error("boom")`, "boom"},
		{"reject", `await Promise.reject(new Error("async failure"))`, "async failure"},
		{"unsettled", `return new Promise(() => {})`, "unsettled promise"},
		{"cyclic", `const a = {}; a.self = a; return a`, "JSON-compatible"},
		{"large result", `return "a".repeat(1048577)`, "1 MiB"},
		{"large logs", `console.log("a".repeat(600000)); console.log("b".repeat(600000));`, "console output"},
		{"many logs", `for (let i = 0; i < 1048577; i++) console.log();`, "console output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runJavaScript(context.Background(), instance, tc.code, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
	for _, args := range []string{`{`, `null`, `{}`, `{"code":123}`, `{"code":"return input", "input":[]}`, `{"code":"return input", "input":null}`} {
		if _, err := instance.InvokableRun(context.Background(), args); err == nil {
			t.Fatalf("accepted invalid arguments: %s", args)
		}
	}
	if _, err := runJavaScript(context.Background(), instance, strings.Repeat(" ", maxJavaScriptBytes)+"return 1", nil); err == nil {
		t.Fatal("accepted oversized code")
	}
}

func TestJavaScriptTimeoutCancellationAndMemory(t *testing.T) {
	instance := javascriptFixture(t, "")
	// Use a short local deadline while testing the same production execution path.
	instance.(*javaScriptTool).timeout = 30 * time.Millisecond
	for _, code := range []string{
		`while (true) {}`,
		`await new Promise(resolve => setTimeout(resolve, 10000))`,
		`return {get value() {while (true) {}}}`,
	} {
		_, err := runJavaScript(context.Background(), instance, code, nil)
		if err == nil || !strings.Contains(err.Error(), "timeout") || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("local deadline must be a recoverable failure: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(10*time.Millisecond, cancel)
	defer timer.Stop()
	_, err := runJavaScript(ctx, instance, `while (true) {}`, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation lost: %v", err)
	}
	limited := javascriptFixture(t, "config:\n  memory_limit_mb: 1\n  timeout: 2\n")
	_, err = runJavaScript(context.Background(), limited, `const items = []; for (let i = 0; i < 1000000; i++) items.push({i}); return items.length`, nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "memory") {
		t.Fatalf("memory budget not enforced: %v", err)
	}
}

func TestJavaScriptConcurrentCallsAreIsolated(t *testing.T) {
	instance := javascriptFixture(t, "")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := runJavaScript(context.Background(), instance, `globalThis.count = (globalThis.count || 0) + 1; await new Promise(r => setTimeout(r, 1)); return {count, APIs: [typeof process, typeof require, typeof fetch]}`, nil)
			if err != nil || out != `{"result":{"count":1,"APIs":["undefined","undefined","undefined"]},"logs":[]}` {
				t.Errorf("runtime isolation: %s / %v", out, err)
			}
		}()
	}
	wg.Wait()
}

func TestJavaScriptConfigValidation(t *testing.T) {
	for _, setting := range []string{"timeout: -1", "timeout: nope", "timeout: 0.5", "timeout: true", `timeout: "1"`, "timeout: 9223372036854775807", "memory_limit_mb: -1", "memory_limit_mb: 9223372036854775807"} {
		var cfg config.Tool
		if err := yaml.Unmarshal([]byte("type: javascript\nconfig:\n  "+setting), &cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := NewJavaScriptTool("js", cfg); err == nil {
			t.Fatalf("accepted invalid config: %s", setting)
		}
	}
}
