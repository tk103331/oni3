package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/plugins"
	"gopkg.in/yaml.v3"
)

func loadAgentHookFixture(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	previous := config.GetConfig()
	if previous == nil {
		previous = &config.Config{}
	}
	data, err := yaml.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	restore := filepath.Join(dir, "restore.yml")
	if err := os.WriteFile(restore, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := config.LoadConfig(restore); err != nil {
			t.Error(err)
		}
	})
	source := `export const hooks={
 before_agent(event){if(event.prompt==="block-before") throw new Error("before observed:"+event.agent)},
 after_agent(event){if(event.prompt==="block-after") throw new Error("after observed:"+event.result)}
};`
	if err := os.WriteFile(filepath.Join(dir, "hook.js"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "fixture.yml")
	if err := os.WriteFile(filename, []byte("plugins:\n  audit:\n    path: ./hook.js\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadConfig(filename); err != nil {
		t.Fatal(err)
	}
}

func TestPluginAgentHooksForEveryChatMode(t *testing.T) {
	loadAgentHookFixture(t)
	for _, mode := range []string{"chat", "callback-nil", "callback", "stream"} {
		for _, phase := range []string{"before", "after"} {
			t.Run(mode+"/"+phase, func(t *testing.T) {
				r := newReasoningAgent(t, []*schema.Message{schema.AssistantMessage("done", nil)})
				prompt := "block-" + phase
				var err error
				switch mode {
				case "chat":
					_, err = r.Chat(context.Background(), nil, prompt)
				case "callback-nil":
					_, err = r.ChatWithCallback(context.Background(), nil, prompt, nil)
				case "callback":
					_, err = r.ChatWithCallback(context.Background(), nil, prompt, func(any) {})
				case "stream":
					err = r.ChatStream(context.Background(), nil, prompt, nil, nil)
				}
				want := "before observed:test"
				if phase == "after" {
					want = "after observed:done"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("wanted %q, got %v", want, err)
				}
			})
		}
	}
}

func TestPluginToolHooksSeeArgumentsResultAndError(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "hook.js")
	if err := os.WriteFile(filename, []byte(`export const hooks={
 before_tool(event){if(event.agent!=="parent" || event.tool!=="probe" || event.arguments!=="{}") throw new Error("bad start")},
 after_tool(event){throw new Error("observed:"+event.result+":"+(event.error??"ok"))}
}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := plugins.Load(context.Background(), "audit", plugins.Config{Path: filename})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{LoadedPlugins: []*plugins.Plugin{p}}
	for _, fail := range []bool{false, true} {
		wrapped := pluginHookTool{InvokableTool: &eventTool{fail: fail}, cfg: cfg, agent: "parent", name: "probe"}
		_, err := wrapped.InvokableRun(context.Background(), "{}")
		want := "observed:probe ok:ok"
		if fail {
			want = "observed::probe exploded"
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("wanted %q, got %v", want, err)
		}
	}
}
