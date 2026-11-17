package agent

import (
	"strings"
	"testing"

	"github.com/tk103331/oni3/config"
)

func TestApplySkillPrependsInstructionsAndAddsToolsOnce(t *testing.T) {
	ac := config.Agent{System: "Agent instructions", Tools: []string{"shell", "file_system"}}
	ApplySkill(&ac, config.Skill{
		Name:   "coder",
		System: "Skill instructions",
		Tools:  []string{"shell", "grep", "grep", " "},
	})

	if ac.System != "Skill instructions\n\nAgent instructions" {
		t.Fatalf("system = %q", ac.System)
	}
	got := strings.Join(ac.Tools, ",")
	if got != "shell,file_system,grep" {
		t.Fatalf("tools = %q", got)
	}
	if ac.ActiveSkill != "coder" {
		t.Fatalf("active skill = %q", ac.ActiveSkill)
	}

	// An agent without instructions receives the skill's body alone.
	empty := config.Agent{}
	ApplySkill(&empty, config.Skill{Name: "x", System: "Only skill text"})
	if empty.System != "Only skill text" {
		t.Fatalf("system = %q", empty.System)
	}
}

func TestSkillsCatalogIsProgressive(t *testing.T) {
	cfg := &config.Config{Skills: map[string]config.Skill{
		"code-review": {
			Name:        "code-review",
			Description: "Review changes for correctness",
			System:      "Read references/checklist.md first.",
			Path:        "/skills/code-review/SKILL.md",
			Dir:         "/skills/code-review",
		},
		"active": {
			Name:        "active",
			Description: "Already inlined",
			System:      "Inlined instructions",
		},
		"no-body": {
			Name:        "no-body",
			Description: "Selection-only preset",
		},
		"undescribed": {
			Name:   "undescribed",
			System: "Triage incidents. Ask for the timeline.",
		},
	}}

	catalog := skillsCatalog(cfg, "active")
	if catalog == "" {
		t.Fatal("catalog empty")
	}
	for _, want := range []string{
		"Skills:",
		"Read the document of a skill before using it",
		"- code-review: Review changes for correctness (SKILL.md: /skills/code-review/SKILL.md)",
		"- undescribed: Triage incidents",
	} {
		if !strings.Contains(catalog, want) {
			t.Fatalf("catalog missing %q:\n%s", want, catalog)
		}
	}
	// Only name and description go in the prompt: the body stays on disk.
	if strings.Contains(catalog, "references/checklist.md") {
		t.Fatalf("catalog inlined the instructions:\n%s", catalog)
	}
	// The active skill is already merged; a skill without instructions
	// cannot be read on demand.
	if strings.Contains(catalog, "active") || strings.Contains(catalog, "no-body") {
		t.Fatalf("catalog lists unusable skills:\n%s", catalog)
	}

	if got := skillsCatalog(&config.Config{}, ""); got != "" {
		t.Fatalf("catalog for a config without skills = %q", got)
	}
	if got := skillsCatalog(cfg, "active"); strings.Contains(got, "Already inlined") {
		t.Fatalf("active skill body leaked into the catalog:\n%s", got)
	}
}

func TestSkillsCatalogInSystemPrompt(t *testing.T) {
	cfg := &config.Config{Skills: map[string]config.Skill{
		"code-review": {
			Name:        "code-review",
			Description: "Review changes for correctness",
			System:      "Full instructions",
			Path:        "/skills/code-review/SKILL.md",
		},
	}}
	r := NewReactAgent("parent", &config.Agent{System: "Agent instructions"})
	prompt := r.systemPrompt(cfg)

	if !strings.Contains(prompt, "Agent instructions\n\nSkills:") {
		t.Fatalf("catalog not appended after the agent instructions:\n%s", prompt)
	}
	if !strings.Contains(prompt, "/skills/code-review/SKILL.md") {
		t.Fatalf("document location missing:\n%s", prompt)
	}

	// An active skill contributes its body and disappears from the catalog.
	ac := config.Agent{System: "Agent instructions"}
	ApplySkill(&ac, cfg.Skills["code-review"])
	active := NewReactAgent("parent", &ac).systemPrompt(cfg)
	if !strings.Contains(active, "Full instructions\n\nAgent instructions") {
		t.Fatalf("active skill body missing:\n%s", active)
	}
	if strings.Contains(active, "Skills:") {
		t.Fatalf("active skill still advertised:\n%s", active)
	}
}
