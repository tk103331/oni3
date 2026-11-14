package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
)

const maxJavaScriptBytes = 1 << 20

type javaScriptTool struct {
	info    *schema.ToolInfo
	timeout time.Duration
	memory  int64
}

// NewJavaScriptTool executes an async function body in a fresh moejs runtime.
// No filesystem, network, process or module loader APIs are installed.
func NewJavaScriptTool(name string, cfg config.Tool) (tool.InvokableTool, error) {
	seconds, memoryMB := int64(30), int64(64)
	for _, setting := range []struct {
		name   string
		value  *int64
		factor int64
	}{
		{"timeout", &seconds, int64(time.Second)},
		{"memory_limit_mb", &memoryMB, 1 << 20},
	} {
		if value, ok := cfg.Config[setting.name]; ok {
			var raw any
			if err := value.Parse(&raw); err != nil {
				return nil, fmt.Errorf("invalid javascript %s: %w", setting.name, err)
			}
			var n int64
			switch number := raw.(type) {
			case int:
				n = int64(number)
			case int64:
				n = number
			default:
				return nil, fmt.Errorf("javascript %s must be a nonnegative integer within range", setting.name)
			}
			if n < 0 || n > math.MaxInt64/setting.factor {
				return nil, fmt.Errorf("javascript %s must be a nonnegative integer within range", setting.name)
			}
			if n > 0 {
				*setting.value = n
			}
		}
	}
	desc := cfg.Description
	if desc == "" {
		desc = "Execute JavaScript using moejs for calculations and data processing, without Node.js. " +
			"code is an async function body: use return for the result and await for promises/timers. " +
			"Optional input data is available as input. Returns JSON with result (null if omitted) and logs from console.log/info/warn/error. " +
			"Each call is isolated; filesystem, network, processes and imports are unavailable."
	}
	return &javaScriptTool{
		timeout: time.Duration(seconds) * time.Second,
		memory:  memoryMB << 20,
		info: &schema.ToolInfo{
			Name: name,
			Desc: desc,
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"code":  {Type: schema.String, Required: true, Desc: "JavaScript async function body. Use return to provide a result; await and console.log are supported."},
				"input": {Type: schema.Object, Desc: "Optional JSON object available to the script as input; defaults to {}."},
			}),
		},
	}, nil
}

func (t *javaScriptTool) Info(context.Context) (*schema.ToolInfo, error) { return t.info, nil }

func (t *javaScriptTool) InvokableRun(ctx context.Context, arguments string, _ ...tool.Option) (string, error) {
	if len(arguments) > 2*maxJavaScriptBytes {
		return "", fmt.Errorf("javascript arguments exceed 2 MiB")
	}
	var in struct {
		Code  string          `json:"code"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal([]byte(arguments), &in); err != nil {
		return "", fmt.Errorf("invalid javascript arguments: %w", err)
	}
	if strings.TrimSpace(in.Code) == "" {
		return "", fmt.Errorf("javascript code is required")
	}
	if len(in.Code) > maxJavaScriptBytes || len(in.Input) > maxJavaScriptBytes {
		return "", fmt.Errorf("javascript code and input each have a 1 MiB limit")
	}
	if len(in.Input) == 0 {
		in.Input = json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(in.Input, &object); err != nil || object == nil {
		return "", fmt.Errorf("javascript input must be a JSON object")
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	output, err := t.execute(ctx, in.Code, in.Input)
	if parent.Err() != nil {
		return "", parent.Err()
	}
	if ctx.Err() != nil {
		// A tool-local timeout must not cancel the whole agent turn.
		return "", fmt.Errorf("javascript execution exceeded timeout (%s)", t.timeout)
	}
	if err != nil {
		return "", fmt.Errorf("javascript execution failed: %w", err)
	}
	return output, nil
}

func (t *javaScriptTool) execute(ctx context.Context, code string, input json.RawMessage) (string, error) {
	script, err := moejs.CompileFunction("javascript", "input", code, moejs.FunctionOptions{Async: true, Context: ctx})
	if err != nil {
		return "", err
	}
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: t.memory})
	loop, err := eventloop.New(rt, eventloop.Options{})
	if err != nil {
		return "", err
	}
	defer loop.Terminate()
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		loop.Interrupt(ctx.Err())
		close(interrupted)
	})
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()

	logs := make([]string, 0)
	logBytes := 0
	log := moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		var line strings.Builder
		for i, arg := range args {
			var text string
			if arg.IsUndefined() {
				text = "undefined"
			} else if arg.IsString() {
				if err := rt.UnmarshalWith(arg, &text, moejs.DecodeOptions{MaxBytes: maxJavaScriptBytes}); err != nil {
					return moejs.Undefined(), err
				}
			} else {
				var raw json.RawMessage
				if err := rt.UnmarshalWith(arg, &raw, moejs.DecodeOptions{MaxBytes: maxJavaScriptBytes}); err != nil {
					return moejs.Undefined(), err
				}
				text = string(raw)
			}
			extra := len(text)
			if i > 0 {
				extra++
			}
			if logBytes+line.Len()+extra+1 > maxJavaScriptBytes {
				return moejs.Undefined(), fmt.Errorf("console output exceeds 1 MiB")
			}
			if i > 0 {
				line.WriteByte(' ')
			}
			line.WriteString(text)
		}
		if logBytes+line.Len()+1 > maxJavaScriptBytes {
			return moejs.Undefined(), fmt.Errorf("console output exceeds 1 MiB")
		}
		logBytes += line.Len() + 1
		logs = append(logs, line.String())
		return moejs.Undefined(), nil
	})
	if err := rt.SetGlobal("console", map[string]any{"log": log, "info": log, "warn": log, "error": log}); err != nil {
		return "", err
	}
	var value, fn, data moejs.Value
	removeRoots := rt.AddLiveRoots(func(add func(moejs.Value)) {
		add(value)
		add(fn)
		add(data)
	})
	defer removeRoots()
	err = loop.Run(func(rt *moejs.Runtime) error {
		fn, err = rt.RunScript(script)
		if err != nil {
			return err
		}
		data, err = rt.ParseJSON(input)
		if err != nil {
			return err
		}
		value, err = rt.CallFunction(fn, moejs.Undefined(), data)
		return err
	})
	if err != nil {
		return "", err
	}
	if state, settled, ok := moejs.PromiseResult(value); ok {
		switch state {
		case moejs.PromisePending:
			return "", fmt.Errorf("script returned an unsettled promise")
		case moejs.PromiseRejected:
			return "", &moejs.Exception{Value: settled}
		default:
			value = settled
		}
	}
	result := json.RawMessage(`null`)
	if !value.IsUndefined() {
		if err := rt.UnmarshalWith(value, &result, moejs.DecodeOptions{MaxBytes: maxJavaScriptBytes}); err != nil {
			return "", fmt.Errorf("result must be JSON-compatible and within 1 MiB: %w", err)
		}
	}
	encoded, err := json.Marshal(struct {
		Result json.RawMessage `json:"result"`
		Logs   []string        `json:"logs"`
	}{result, logs})
	if err != nil {
		return "", err
	}
	if len(encoded) > maxJavaScriptBytes {
		return "", fmt.Errorf("javascript output exceeds 1 MiB")
	}
	return string(encoded), nil
}
