package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillDocumentFrontMatter(t *testing.T) {
	content := `---
name: code-review
description: Review code changes for correctness
license: MIT
allowed-tools: Read Grep Write
metadata:
  owner: platform
tools: [shell, grep]
---
You are a strict reviewer.
Be concise.`

	name, skill, err := parseSkillDocument("fallback", content)
	if err != nil {
		t.Fatal(err)
	}
	if name != "code-review" || skill.Name != "code-review" {
		t.Fatalf("name = %q / %q", name, skill.Name)
	}
	if skill.Description != "Review code changes for correctness" {
		t.Fatalf("description = %q", skill.Description)
	}
	if len(skill.Tools) != 2 || skill.Tools[0] != "shell" {
		t.Fatalf("tools = %v", skill.Tools)
	}
	if got := strings.Join(skill.AllowedTools, ","); got != "Read,Grep,Write" {
		t.Fatalf("allowed-tools = %v", skill.AllowedTools)
	}
	if skill.License != "MIT" {
		t.Fatalf("license = %q", skill.License)
	}
	if skill.Metadata["owner"] != "platform" {
		t.Fatalf("metadata = %v", skill.Metadata)
	}
	if skill.System != "You are a strict reviewer.\nBe concise." {
		t.Fatalf("system = %q", skill.System)
	}
}

func TestParseSkillDocumentAllowedToolsList(t *testing.T) {
	content := `---
description: list form
allowed-tools:
  - Bash(git:*)
  - Read
---
Instructions.`

	_, skill, err := parseSkillDocument("list-form", content)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(skill.AllowedTools, ","); got != "Bash(git:*),Read" {
		t.Fatalf("allowed-tools = %v", skill.AllowedTools)
	}
}

func TestParseSkillDocumentPlain(t *testing.T) {
	content := "\n# Translator\n\nTranslate Chinese to English."

	name, skill, err := parseSkillDocument("translator", content)
	if err != nil {
		t.Fatal(err)
	}
	if name != "translator" {
		t.Fatalf("name = %q", name)
	}
	if skill.Description != "Translator" {
		t.Fatalf("description = %q", skill.Description)
	}
	if skill.System != "# Translator\n\nTranslate Chinese to English." {
		t.Fatalf("system = %q", skill.System)
	}
}

func TestParseSkillDocumentRejectsUnusableNames(t *testing.T) {
	for _, name := range []string{"", "has space", "with/slash", strings.Repeat("a", 65)} {
		if _, _, err := parseSkillDocument(name, "body"); err == nil {
			t.Fatalf("name %q accepted", name)
		}
	}
}

func TestLoadConfigMergesSkillFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Keep the project directory out of the way: the scan is relative to the
	// working directory, which is this package for the test binary.
	t.Chdir(t.TempDir())

	dataDir := filepath.Join(home, ".oni3", "skills")
	agentsDir := filepath.Join(home, ".agents", "skills")
	for _, dir := range []string{dataDir, agentsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Legacy flat documents.
	write(filepath.Join(dataDir, "alpha.md"), "Oni3 alpha prompt")
	write(filepath.Join(agentsDir, "beta.md"), "Agents beta prompt")
	write(filepath.Join(agentsDir, "shared.md"), "Agents shared prompt")
	// Agent Skills layout: directory + SKILL.md + bundled resource.
	write(filepath.Join(dataDir, "shared", "SKILL.md"), `---
description: Oni3 shared
---
Oni3 shared prompt`)
	write(filepath.Join(dataDir, "review", "SKILL.md"), `---
name: code-review
description: Review changes
---
Read references/checklist.md first.`)
	write(filepath.Join(dataDir, "review", "references", "checklist.md"), "checklist")

	cfgPath := filepath.Join(home, "config.yml")
	write(cfgPath, `models:
  m1:
    provider: p
    model: x
skills:
  fromconfig:
    description: from config
    system: config prompt
`)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Skills["alpha"].System != "Oni3 alpha prompt" {
		t.Fatalf("flat skill missing: %+v", cfg.Skills)
	}
	if cfg.Skills["beta"].System != "Agents beta prompt" {
		t.Fatalf("agents skill missing: %+v", cfg.Skills)
	}
	// The SKILL.md directory wins over a flat file of the same name, and
	// ~/.oni3/skills wins over ~/.agents/skills.
	if cfg.Skills["shared"].System != "Oni3 shared prompt" {
		t.Fatalf("directory skill should win: %+v", cfg.Skills["shared"])
	}
	// Config-defined skills are still merged.
	if cfg.Skills["fromconfig"].System != "config prompt" {
		t.Fatalf("config skill missing: %+v", cfg.Skills)
	}

	// The front matter name replaces the directory name, and the document
	// path plus resource directory are recorded for progressive disclosure.
	review, ok := cfg.Skills["code-review"]
	if !ok {
		t.Fatalf("front matter name ignored: %+v", cfg.Skills)
	}
	if _, stale := cfg.Skills["review"]; stale {
		t.Fatalf("directory name leaked as a second skill: %+v", cfg.Skills)
	}
	if !strings.HasSuffix(review.Path, filepath.Join("review", "SKILL.md")) {
		t.Fatalf("path = %q", review.Path)
	}
	if !strings.HasSuffix(review.Dir, "review") {
		t.Fatalf("dir = %q", review.Dir)
	}
	if review.Description != "Review changes" {
		t.Fatalf("description = %q", review.Description)
	}
}

func TestProjectSkillsTakePriority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(project, ".oni3", "skills", "style", "SKILL.md"), "Project style")
	write(filepath.Join(home, ".oni3", "skills", "style", "SKILL.md"), "Personal style")

	cfgPath := filepath.Join(home, "config.yml")
	write(cfgPath, "models:\n  m1:\n    provider: p\n    model: x\n")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Skills["style"].System != "Project style" {
		t.Fatalf("project skill did not win: %+v", cfg.Skills["style"])
	}
}

func TestSkillDirectoryRequiresUsableName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())

	dir := filepath.Join(home, ".oni3", "skills", "Bad Name", "")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(home, "config.yml")
	if err := os.WriteFile(cfgPath, []byte("models:\n  m1:\n    provider: p\n    model: x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Skills["Bad Name"]; ok {
		t.Fatalf("unusable name loaded: %+v", cfg.Skills)
	}
}
