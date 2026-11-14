// Package plugins embeds JavaScript tools using the pure-Go moejs runtime.
package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/tk103331/oni3/logger"
)

const (
	defaultTimeout = 30 * time.Second
	defaultMemory  = 64 << 20
	maxSourceBytes = 1 << 20
	maxResultBytes = 1 << 20
)

var (
	validName  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	validColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// Config describes one ES module. Paths are resolved relative to config.yml.
type Config struct {
	Path          string         `yaml:"path"`
	Timeout       int            `yaml:"timeout,omitempty"`         // seconds; default 30
	MemoryLimitMB int            `yaml:"memory_limit_mb,omitempty"` // default 64
	Config        map[string]any `yaml:"config,omitempty"`
}

type definition struct {
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Plugin contains the extensions exported by one ES module. Agents and models
// use the same fields as YAML configuration; registration is owned by config.
type Plugin struct {
	Tools     []*Tool
	Agents    map[string]json.RawMessage
	Models    map[string]json.RawMessage
	Providers map[string]json.RawMessage
	Themes    map[string]Theme

	name    string
	module  *moejs.Module
	config  string // immutable JSON snapshot, copied into each runtime
	timeout time.Duration
	memory  int64
	hooks   map[string]moejs.Hook
}

// ThemeVariant defines foreground colors for a light or dark terminal.
type ThemeVariant struct {
	Primary string `json:"primary"`
	Accent  string `json:"accent"`
	Success string `json:"success"`
	Warning string `json:"warning"`
	Error   string `json:"error"`
	User    string `json:"user"`
	Muted   string `json:"muted"`
}

// Theme has matching light and dark variants.
type Theme struct {
	Description string       `json:"description"`
	Light       ThemeVariant `json:"light"`
	Dark        ThemeVariant `json:"dark"`
}

var hookNames = []string{"before_agent", "after_agent", "before_tool", "after_tool"}

// Event is the immutable JSON snapshot sent to lifecycle hooks. Hooks observe
// events; their return value does not replace prompts, arguments or results.
type Event struct {
	Type      string `json:"type"`
	Agent     string `json:"agent,omitempty"`
	Model     string `json:"model,omitempty"`
	Tool      string `json:"tool,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result"`
	Error     string `json:"error,omitempty"`
}

// HasHook reports whether this plugin subscribes to an event.
func (p *Plugin) HasHook(name string) bool { _, ok := p.hooks[name]; return ok }

// Emit runs a hook with its own runtime and the plugin's deadline and budget.
func (p *Plugin) Emit(ctx context.Context, event Event) error {
	hook, ok := p.hooks[event.Type]
	if !ok {
		return nil
	}
	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	args, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.invocationError(parentCtx, p.invoke(ctx, hook, string(args), "", nil))
}

func (p *Plugin) invocationError(parent context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// A plugin's own deadline is an ordinary tool failure. Only the parent
		// deadline should cancel the entire agent turn.
		return fmt.Errorf("execution exceeded plugin timeout (%s)", p.timeout)
	}
	return err
}

// Name returns the configured plugin namespace.
func (p *Plugin) Name() string { return p.name }

// Tool is one named tool exported by a JavaScript plugin. A compiled module is
// shared, but each call gets its own runtime, globals and plugin configuration.
type Tool struct {
	plugin *Plugin
	local  string
	info   *schema.ToolInfo
	hook   moejs.Hook
}

// Load compiles a plugin and reads its exported `tools` object. Tool names are
// namespaced as <plugin>__<tool> so multiple plugins can use the same local name.
func Load(ctx context.Context, name string, cfg Config) (*Plugin, error) {
	if !validName.MatchString(name) {
		return nil, fmt.Errorf("invalid plugin name %q: use letters, numbers, underscores or hyphens", name)
	}
	if strings.TrimSpace(cfg.Path) == "" {
		return nil, fmt.Errorf("plugin %q: path is required", name)
	}
	if cfg.Timeout < 0 || cfg.MemoryLimitMB < 0 {
		return nil, fmt.Errorf("plugin %q: timeout and memory_limit_mb must be positive", name)
	}
	p := &Plugin{name: name, timeout: defaultTimeout, memory: defaultMemory, hooks: make(map[string]moejs.Hook)}
	if cfg.Timeout > 0 {
		if int64(cfg.Timeout) > int64((1<<63-1)/time.Second) {
			return nil, fmt.Errorf("plugin %q: timeout is too large", name)
		}
		p.timeout = time.Duration(cfg.Timeout) * time.Second
	}
	if cfg.MemoryLimitMB > 0 {
		if int64(cfg.MemoryLimitMB) > (1<<63-1)>>20 {
			return nil, fmt.Errorf("plugin %q: memory_limit_mb is too large", name)
		}
		p.memory = int64(cfg.MemoryLimitMB) << 20
	}
	data, err := json.Marshal(cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("plugin %q config: %w", name, err)
	}
	p.config = string(data)
	if cfg.Config == nil {
		p.config = "{}"
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	p.module, err = compileModule(ctx, cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: %w", name, err)
	}

	var definitions map[string]definition
	err = p.withRuntime(ctx, func(rt *moejs.Runtime) error {
		for _, section := range []struct {
			name   string
			target any
		}{
			{"tools", &definitions}, {"agents", &p.Agents}, {"models", &p.Models}, {"providers", &p.Providers}, {"themes", &p.Themes},
		} {
			if exported, ok := rt.Export(section.name); ok {
				if err := rt.UnmarshalWith(exported, section.target, moejs.DecodeOptions{MaxBytes: maxResultBytes}); err != nil {
					return fmt.Errorf("invalid %s manifest: %w", section.name, err)
				}
			}
		}
		if hooks, ok := rt.Export("hooks"); ok {
			if !hooks.IsObject() || hooks.AsObject().ClassName() != "Object" {
				return fmt.Errorf("hooks must be an object of event functions")
			}
			for _, key := range hooks.AsObject().OwnEnumerableStringKeys() {
				name := key.String().GoString()
				if !slices.Contains(hookNames, name) {
					return fmt.Errorf("unknown hook event %q", name)
				}
				hook, err := p.module.Hook("hooks", name)
				if err != nil {
					return err
				}
				if ok, err := rt.Has(hook); err != nil || !ok {
					return fmt.Errorf("hook %q must be a function", name)
				}
				p.hooks[name] = hook
			}
		}
		if len(definitions)+len(p.Agents)+len(p.Models)+len(p.Providers)+len(p.Themes)+len(p.hooks) == 0 {
			return fmt.Errorf("module must export tools, hooks, themes, agents, models or providers")
		}
		for local := range definitions {
			hook, err := p.module.Hook("tools", local, "execute")
			if err != nil {
				return err
			}
			if ok, err := rt.Has(hook); err != nil || !ok {
				return fmt.Errorf("tool %q must define an execute function", local)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("plugin %q: %w", name, err)
	}

	names := make([]string, 0, len(definitions))
	for local := range definitions {
		names = append(names, local)
	}
	sort.Strings(names)
	result := make([]*Tool, 0, len(names))
	for _, local := range names {
		fullName := name + "__" + local
		if !validName.MatchString(local) || len(fullName) > 64 {
			return nil, fmt.Errorf("plugin %q: invalid tool name %q (namespaced names must be at most 64 characters)", name, local)
		}
		def := definitions[local]
		if strings.TrimSpace(def.Description) == "" {
			return nil, fmt.Errorf("plugin %q: tool %q requires a description", name, local)
		}
		params := &jsonschema.Schema{Type: "object"}
		if len(def.Parameters) > 0 {
			if err := json.Unmarshal(def.Parameters, params); err != nil {
				return nil, fmt.Errorf("plugin %q tool %q parameters: %w", name, local, err)
			}
		}
		if params.Type != "object" {
			return nil, fmt.Errorf("plugin %q tool %q: parameters must be an object JSON Schema", name, local)
		}
		hook, err := p.module.Hook("tools", local, "execute")
		if err != nil {
			return nil, err
		}
		result = append(result, &Tool{plugin: p, local: local, hook: hook, info: &schema.ToolInfo{
			Name: fullName, Desc: def.Description, ParamsOneOf: schema.NewParamsOneOfByJSONSchema(params),
		}})
	}
	for section, names := range map[string][]string{"agents": mapNames(p.Agents), "models": mapNames(p.Models), "providers": mapNames(p.Providers), "themes": mapNames(p.Themes)} {
		for _, local := range names {
			if !validName.MatchString(local) || len(name+"__"+local) > 64 {
				return nil, fmt.Errorf("plugin %q: invalid %s name %q", name, section, local)
			}
		}
	}
	for local, theme := range p.Themes {
		for _, variant := range []ThemeVariant{theme.Light, theme.Dark} {
			for _, color := range []string{variant.Primary, variant.Accent, variant.Success, variant.Warning, variant.Error, variant.User, variant.Muted} {
				if !validColor.MatchString(color) {
					return nil, fmt.Errorf("plugin %q theme %q: all light/dark colors must be #RRGGBB", name, local)
				}
			}
		}
	}
	p.Tools = result
	return p, nil
}

func mapNames[V any](items map[string]V) []string {
	names := make([]string, 0, len(items))
	for name := range items {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (t *Tool) Info(context.Context) (*schema.ToolInfo, error) { return t.info, nil }

// InvokableRun passes JSON arguments and {plugin, tool, config} to execute.
// Strings are returned as text; other JSON values are serialized as JSON.
func (t *Tool) InvokableRun(ctx context.Context, arguments string, _ ...tool.Option) (string, error) {
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	if len(arguments) > maxResultBytes {
		return "", fmt.Errorf("plugin tool %q: arguments exceed 1 MiB", t.info.Name)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &object); err != nil || object == nil {
		return "", fmt.Errorf("plugin tool %q: arguments must be a JSON object", t.info.Name)
	}
	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, t.plugin.timeout)
	defer cancel()
	var output string
	err := t.plugin.invocationError(parentCtx, t.plugin.invoke(ctx, t.hook, arguments, t.local, &output))
	if err != nil {
		return "", fmt.Errorf("plugin tool %q: %w", t.info.Name, err)
	}
	return output, nil
}

func (p *Plugin) invoke(ctx context.Context, hook moejs.Hook, arguments, local string, output *string) error {
	var value moejs.Value
	removeRoots := func() {}
	defer func() { removeRoots() }()
	return p.withRuntime(ctx, func(rt *moejs.Runtime) error {
		removeRoots = rt.AddLiveRoots(func(add func(moejs.Value)) { add(value) })
		arg, err := rt.ParseJSONString(arguments)
		if err != nil {
			return err
		}
		data, err := json.Marshal(map[string]any{"plugin": p.name, "tool": local, "config": json.RawMessage(p.config)})
		if err != nil {
			return err
		}
		env, err := rt.ParseJSON(data)
		if err != nil {
			return err
		}
		value, err = rt.Call(hook, arg, env)
		return err
	}, func(rt *moejs.Runtime) error {
		if state, settled, ok := moejs.PromiseResult(value); ok {
			switch state {
			case moejs.PromisePending:
				return fmt.Errorf("function returned an unsettled promise")
			case moejs.PromiseRejected:
				return &moejs.Exception{Value: settled}
			default:
				value = settled
			}
		}
		if output == nil {
			return nil
		}
		limit := moejs.DecodeOptions{MaxBytes: maxResultBytes}
		if value.IsString() {
			return rt.UnmarshalWith(value, output, limit)
		}
		var raw json.RawMessage
		if err := rt.UnmarshalWith(value, &raw, limit); err != nil {
			return fmt.Errorf("execute must return text or a JSON value: %w", err)
		}
		*output = string(raw)
		return nil
	})
}

// withRuntime runs module initialization, execution and result conversion under
// the same deadline. Only Interrupt crosses goroutines; the VM stays on this one.
func (p *Plugin) withRuntime(ctx context.Context, run func(*moejs.Runtime) error, finish ...func(*moejs.Runtime) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: p.memory})
	loop, err := eventloop.New(rt, eventloop.Options{})
	if err != nil {
		return err
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
	log := moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		parts := make([]string, len(args))
		for i, arg := range args {
			parts[i] = arg.String()
		}
		logger.Info("PLUGIN:"+p.name, strings.Join(parts, " "))
		return moejs.Undefined(), nil
	})
	if err := rt.SetGlobal("console", map[string]any{"log": log, "info": log, "warn": log, "error": log}); err != nil {
		return err
	}
	err = loop.Run(func(rt *moejs.Runtime) error {
		if err := rt.Load(p.module); err != nil {
			return err
		}
		return run(rt)
	})
	if err == nil {
		for _, fn := range finish {
			if err = fn(rt); err != nil {
				break
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// compileModule resolves static relative imports inside the plugin directory.
// The compiled graph no longer needs open files when tools execute.
func compileModule(ctx context.Context, filename string) (*moejs.Module, error) {
	root, err := os.OpenRoot(filepath.Dir(filename))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	cache := make(map[string]*moejs.Module)
	var compile func(string) (*moejs.Module, error)
	compile = func(name string) (*moejs.Module, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if module := cache[name]; module != nil {
			return module, nil
		}
		if len(cache) >= 128 {
			return nil, fmt.Errorf("plugin exceeds 128 modules")
		}
		file, err := root.Open(filepath.FromSlash(name))
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(file, maxSourceBytes+1))
		file.Close()
		if err != nil {
			return nil, err
		}
		if len(data) > maxSourceBytes {
			return nil, fmt.Errorf("module %q exceeds 1 MiB", name)
		}
		module, err := moejs.CompileWith(name, string(data), moejs.CompileOptions{Context: ctx})
		if err == nil {
			cache[name] = module
		}
		return module, err
	}
	entry, err := compile(filepath.Base(filename))
	if err != nil {
		return nil, err
	}
	return moejs.Link(entry, func(referrer moejs.Referrer, specifier string) (*moejs.Module, error) {
		if !strings.HasPrefix(specifier, "./") && !strings.HasPrefix(specifier, "../") {
			return nil, fmt.Errorf("only relative imports inside the plugin directory are supported")
		}
		name := path.Join(path.Dir(referrer.Name()), specifier)
		if !fs.ValidPath(name) || strings.Contains(name, "\\") {
			return nil, fmt.Errorf("import %q is outside the plugin directory", specifier)
		}
		return compile(name)
	})
}
