package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeModule(t *testing.T, source string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "plugin.js")
	if err := os.WriteFile(filename, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func loadSource(t *testing.T, source string) *Plugin {
	t.Helper()
	p, err := Load(context.Background(), "demo", Config{Path: writeModule(t, source), Config: map[string]any{"greeting": "你好"}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestToolsArgumentsSchemaResultsAndIsolation(t *testing.T) {
	p := loadSource(t, `let count = 0;
export const tools = {
  hello: {
    description: "Greet someone",
    parameters: {type:"object", properties:{name:{type:"string"}}, required:["name"]},
    execute(args, context) { count++; return {text:context.config.greeting + args.name, count, plugin:context.plugin, tool:context.tool}; }
  },
  wait: {description:"Async greeting", async execute(args) {
    await new Promise(resolve => setTimeout(resolve, 5)); return "done:" + args.value;
  }}
};`)
	if len(p.Tools) != 2 {
		t.Fatalf("tools: %d", len(p.Tools))
	}
	info, err := p.Tools[0].Info(context.Background())
	if err != nil || info.Name != "demo__hello" {
		t.Fatalf("info: %+v, %v", info, err)
	}
	params, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || len(params.Required) != 1 || params.Required[0] != "name" {
		t.Fatalf("schema: %+v, %v", params, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := p.Tools[0].InvokableRun(context.Background(), `{"name":"世界"}`)
			if err != nil {
				t.Error(err)
				return
			}
			var decoded struct {
				Text         string
				Count        int
				Plugin, Tool string
			}
			if err := json.Unmarshal([]byte(result), &decoded); err != nil {
				t.Error(err)
				return
			}
			if decoded.Text != "你好世界" || decoded.Count != 1 || decoded.Plugin != "demo" || decoded.Tool != "hello" {
				t.Errorf("result: %+v", decoded)
			}
		}()
	}
	wg.Wait()
	result, err := p.Tools[1].InvokableRun(context.Background(), `{"value":"ok"}`)
	if err != nil || result != "done:ok" {
		t.Fatalf("async: %q, %v", result, err)
	}
}

func TestPluginMayOnlyExportOtherExtensions(t *testing.T) {
	p := loadSource(t, `export const agents = {helper:{description:"Help",system:"Answer"}};
export const models = {fast:{provider:"local", model:"test"}};
export const hooks = {async before_agent(event, context) {
 await new Promise(resolve => setTimeout(resolve, 1));
 if(event.prompt !== "hello" || context.config.greeting !== "你好") throw new Error("bad event");
}};`)
	if len(p.Tools) != 0 || len(p.Agents) != 1 || len(p.Models) != 1 || !p.HasHook("before_agent") {
		t.Fatalf("manifest: %+v", p)
	}
	if err := p.Emit(context.Background(), Event{Type: "before_agent", Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Emit(context.Background(), Event{Type: "before_agent", Prompt: "bad"}); err == nil || !strings.Contains(err.Error(), "bad event") {
		t.Fatalf("hook error: %v", err)
	}
	if err := p.Emit(context.Background(), Event{Type: "after_agent"}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidManifests(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`export const other = {};`, "module must export"},
		{`export const tools = {x:{description:"X"}}`, "execute function"},
		{`export const tools = {x:{execute(){}}}`, "description"},
		{`export const tools = {"a.b":{description:"X",execute(){}}}`, "invalid tool name"},
		{`export const tools = {x:{description:"X",parameters:{type:"array"},execute(){}}}`, "object JSON Schema"},
		{`export const hooks = {before_tool:"bad"}`, "must be a function"},
		{`export const themes = {x:{light:{primary:"bad"},dark:{}}}`, "#RRGGBB"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			_, err := Load(context.Background(), "demo", Config{Path: writeModule(t, tc.source)})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
		})
	}
}

func TestToolErrors(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`throw new Error("broken")`, "broken"},
		{`return Promise.reject(new Error("rejected"))`, "rejected"},
		{`return new Promise(() => {})`, "unsettled promise"},
		{`return "x".repeat(1100000)`, "too large"},
		{`return {toJSON(){throw new Error("decode failed")}}`, "decode failed"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			p := loadSource(t, `export const tools={x:{description:"X",execute(){`+tc.body+`}}}`)
			_, err := p.Tools[0].InvokableRun(context.Background(), `{}`)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
		})
	}
	p := loadSource(t, `export const tools={x:{description:"X",execute(args){return args}}}`)
	for _, args := range []string{`[]`, `null`, `false`, `{"broken"`} {
		if _, err := p.Tools[0].InvokableRun(context.Background(), args); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
}

func TestCancellationCoversInitializationExecutionAndConversion(t *testing.T) {
	for _, source := range []string{
		`while(true){}; export const tools={x:{description:"X",execute(){return 1}}}`,
		`export const tools={x:{description:"X",execute(){while(true){}}}}`,
		`export const tools={x:{description:"X",async execute(){await new Promise(r=>setTimeout(r,100000));return 1}}}`,
		`export const tools={x:{description:"X",execute(){return {get value(){while(true){}}}}}}`,
	} {
		t.Run(source, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			p, err := Load(ctx, "demo", Config{Path: writeModule(t, source)})
			if err == nil {
				_, err = p.Tools[0].InvokableRun(ctx, `{}`)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected context deadline, got %v", err)
			}
		})
	}
}

func TestPluginDeadlineAndMemoryBudget(t *testing.T) {
	p := loadSource(t, `export const tools={x:{description:"X",execute(){while(true){}}}}`)
	p.timeout = 20 * time.Millisecond
	_, err := p.Tools[0].InvokableRun(context.Background(), `{}`)
	if err == nil || !strings.Contains(err.Error(), "plugin timeout") || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("local timeout must be a recoverable tool failure: %v", err)
	}
	p = loadSource(t, `export const tools={x:{description:"X",execute(){return new Array(10000000).fill("x")}}}`)
	p.memory = 8 << 20
	_, err = p.Tools[0].InvokableRun(context.Background(), `{}`)
	if err == nil || !strings.Contains(err.Error(), "memory limit") {
		t.Fatalf("memory budget: %v", err)
	}
}

func TestRelativeImportsAndEscapeRejection(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "plugin.js")
	if err := os.WriteFile(filepath.Join(dir, "helper.js"), []byte(`export const value="imported"`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte(`import {value} from "./helper.js"; export const tools={x:{description:"X",execute(){return value}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(context.Background(), "demo", Config{Path: entry})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Tools[0].InvokableRun(context.Background(), `{}`)
	if err != nil || result != "imported" {
		t.Fatalf("import: %q %v", result, err)
	}
	for _, specifier := range []string{"../outside.js", "node:fs", "/tmp/module.js"} {
		if err := os.WriteFile(entry, []byte(`import "`+specifier+`"; export const tools={}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(context.Background(), "demo", Config{Path: entry}); err == nil {
			t.Fatalf("accepted %s", specifier)
		}
	}
}
