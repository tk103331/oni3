package agent

import (
	"strings"
	"testing"

	"github.com/tk103331/oni3/config"
	"gopkg.in/yaml.v3"
)

func TestSubagentCatalogInSystemPrompt(t *testing.T) {
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(`
agents:
  reviewer:
    description: 审查代码并指出正确性问题
    system: Private reviewer instructions
  coder:
    description: 实现功能并运行测试
    system: Private coder instructions
  legacy:
    system: Private legacy instructions
tools:
  delegate:
    type: subagent
`), &cfg); err != nil {
		t.Fatal(err)
	}
	r := NewReactAgent("parent", &config.Agent{System: "Original instructions", Tools: []string{"delegate"}})
	prompt := r.systemPrompt(&cfg)
	for _, want := range []string{
		"Original instructions\n\n", `Subagent tool "delegate"`,
		`"coder": "实现功能并运行测试"`, `"reviewer": "审查代码并指出正确性问题"`,
		`"legacy": "No description provided."`,
		"`agent`", "`prompt`", "temporary agent", "`system`", "`model`", "`tools`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Index(prompt, `- "coder"`) > strings.Index(prompt, `- "reviewer"`) {
		t.Fatalf("agent catalog is not sorted:\n%s", prompt)
	}
	if strings.Contains(prompt, "Private") {
		t.Fatalf("catalog exposed child system prompts:\n%s", prompt)
	}
	if r.config.System != "Original instructions" || r.systemPrompt(&cfg) != prompt {
		t.Fatal("building the prompt mutated config or duplicated the catalog")
	}
}

func TestSubagentCatalogRespectsToolConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		policy  string
		tools   []string
		present []string
		absent  []string
	}{
		{
			name: "allowlist", policy: "agents: [reviewer, missing]", tools: []string{"delegate"},
			present: []string{`- "reviewer"`, "temporary agent"}, absent: []string{`- "coder"`, `- "missing"`},
		},
		{
			name: "empty allowlist", policy: "agents: []", tools: []string{"delegate"},
			present: []string{`- "reviewer"`, `- "coder"`},
		},
		{
			name: "blank allowlist", policy: `agents: ["  "]`, tools: []string{"delegate"},
			present: []string{`- "reviewer"`, `- "coder"`},
		},
		{
			name: "temporary disabled", policy: "allow_temp: false", tools: []string{"delegate"},
			present: []string{"Temporary agents are disabled", `- "reviewer"`}, absent: []string{"You may also create"},
		},
		{
			name: "temporary only", policy: "agents: [missing]", tools: []string{"delegate"},
			present: []string{"No configured agents are available", "You may also create a temporary agent"},
			absent:  []string{`- "coder"`, `- "reviewer"`},
		},
		{
			name: "tool not enabled", policy: "{}", tools: []string{"file_system"},
			absent: []string{"Subagent tool", "temporary agent", "reviewer", "coder"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var policy map[string]config.Value
			if err := yaml.Unmarshal([]byte(tc.policy), &policy); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{
				Agents: map[string]config.Agent{"coder": {Description: "Write code"}, "reviewer": {Description: "Review code"}},
				Tools:  map[string]config.Tool{"delegate": {Type: "subagent", Config: policy}},
			}
			r := NewReactAgent("parent", &config.Agent{Tools: tc.tools})
			prompt := r.systemPrompt(cfg)
			for _, want := range tc.present {
				if !strings.Contains(prompt, want) {
					t.Fatalf("missing %q:\n%s", want, prompt)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(prompt, unwanted) {
					t.Fatalf("unexpected %q:\n%s", unwanted, prompt)
				}
			}
		})
	}
}

func TestSystemPromptWithoutGlobalConfig(t *testing.T) {
	r := NewReactAgent("parent", &config.Agent{System: "Original", Tools: []string{"subagent"}})
	if got := r.systemPrompt(nil); got != "Original" {
		t.Fatalf("missing global config changed system prompt: %q", got)
	}
}
