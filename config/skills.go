package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tk103331/oni3/internal/datadir"
	"github.com/tk103331/oni3/logger"
)

// skillDocumentName is the document read from an Agent Skills directory.
const skillDocumentName = "SKILL.md"

// maxSkillNameLength mirrors the Agent Skills limit (64 characters).
const maxSkillNameLength = 64

// skillFrontMatter is the optional YAML header of a skill markdown file. It
// follows the Agent Skills fields, plus oni3's `tools` extension.
type skillFrontMatter struct {
	Name         string          `yaml:"name,omitempty"`
	Description  string          `yaml:"description,omitempty"`
	License      string          `yaml:"license,omitempty"`
	AllowedTools allowedToolList `yaml:"allowed-tools,omitempty"`
	Metadata     map[string]any  `yaml:"metadata,omitempty"`
	Tools        []string        `yaml:"tools,omitempty"`
}

// allowedToolList accepts both front matter spellings: the space/comma
// separated string used by Agent Skills ("Bash(git:*) Read") and a YAML list.
type allowedToolList []string

func (a *allowedToolList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*a = strings.FieldsFunc(node.Value, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == ','
		})
	case yaml.SequenceNode:
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*a = list
	default:
		return fmt.Errorf("allowed-tools must be a string or a list, got %s", node.Tag)
	}
	return nil
}

// SkillDirectories returns the directories scanned for skills in priority
// order (on name clashes the first entry wins):
//
//	./.oni3/skills      project skills
//	~/.oni3/skills      personal skills
//	~/.agents/skills    skills shared with other agent tools
func SkillDirectories() []string {
	dirs := []string{filepath.Join(".oni3", "skills")}
	if data := datadir.Dir(); data != "" {
		dirs = append(dirs, filepath.Join(data, "skills"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".agents", "skills"))
	}
	return dirs
}

// ValidateSkillName rejects names that cannot be selected or referenced:
// empty names, names with path separators or whitespace, and names longer
// than the Agent Skills limit. Style deviations (upper case, underscores) are
// accepted for backward compatibility and reported separately.
func ValidateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name must not be empty")
	}
	if len(name) > maxSkillNameLength {
		return fmt.Errorf("skill name %q exceeds %d characters", name, maxSkillNameLength)
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r == ':' || r == '.' || r < ' ' || r == ' ' {
			return fmt.Errorf("skill name %q contains an unusable character %q", name, r)
		}
	}
	return nil
}

// canonicalSkillName reports whether a name already follows the Agent Skills
// convention: lower-case letters, digits and single hyphens.
func canonicalSkillName(name string) bool {
	if name == "" || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// parseSkillDocument parses one skill markdown document (SKILL.md or a legacy
// flat file) and returns the skill name together with the skill. The name is
// the front matter `name` when present, otherwise defaultName.
//
// The document may start with YAML front matter delimited by "---" lines:
//
//	---
//	name: code-review
//	description: Review code changes for correctness
//	license: MIT
//	allowed-tools: Read Grep
//	metadata:
//	  owner: platform
//	---
//	Start with correctness issues...
//
// Without front matter the whole body is the instructions and the description
// falls back to the first non-empty line.
func parseSkillDocument(defaultName, content string) (string, Skill, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")

	var meta skillFrontMatter
	body := content

	lines := strings.Split(content, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				front := strings.Join(lines[1:i], "\n")
				// Front matter is user content: an unparsable header leaves
				// the document without metadata instead of dropping the skill.
				_ = yaml.Unmarshal([]byte(front), &meta)
				body = strings.Join(lines[i+1:], "\n")
				break
			}
		}
	}

	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = defaultName
	}
	if err := ValidateSkillName(name); err != nil {
		return "", Skill{}, err
	}

	instructions := strings.TrimSpace(body)
	description := strings.TrimSpace(meta.Description)
	if description == "" {
		description = firstLine(instructions)
	}

	return name, Skill{
		Name:         name,
		Description:  description,
		System:       instructions,
		Tools:        meta.Tools,
		AllowedTools: meta.AllowedTools,
		License:      strings.TrimSpace(meta.License),
		Metadata:     meta.Metadata,
	}, nil
}

// firstLine returns the first non-empty line, truncated to a readable length.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#>*- "))
		if line != "" {
			runes := []rune(line)
			if len(runes) > 80 {
				return string(runes[:79]) + "…"
			}
			return line
		}
	}
	return ""
}

// loadSkillsFromDirs merges skills found in SkillDirectories into cfg.
// Files override config-defined skills with the same name; among directories
// the first entry of SkillDirectories wins. Directories are therefore loaded
// from lowest to highest priority.
func loadSkillsFromDirs(cfg *Config) {
	if cfg.Skills == nil {
		cfg.Skills = make(map[string]Skill)
	}
	// Config entries keep their map key as the identifier.
	for key, skill := range cfg.Skills {
		if strings.TrimSpace(skill.Name) == "" {
			skill.Name = key
			cfg.Skills[key] = skill
		}
	}
	dirs := SkillDirectories()
	for i := len(dirs) - 1; i >= 0; i-- {
		loadSkillsFromDir(cfg, dirs[i])
	}
}

// loadSkillsFromDir loads the skills of one directory. Both layouts are
// supported: the Agent Skills layout (<name>/SKILL.md, with bundled resources
// next to it) and legacy flat <name>.md documents. Directory skills win over
// a flat file with the same name in the same directory.
func loadSkillsFromDir(cfg *Config, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		loadSkillDocument(cfg, name, filepath.Join(dir, entry.Name()), "")
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path, ok := findSkillDocument(filepath.Join(dir, entry.Name()))
		if !ok {
			continue
		}
		loadSkillDocument(cfg, entry.Name(), path, filepath.Dir(path))
	}
}

// findSkillDocument returns the document inside a skill directory. The file
// name is matched case-insensitively so SKILL.md works everywhere.
func findSkillDocument(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), skillDocumentName) {
			return filepath.Join(dir, entry.Name()), true
		}
	}
	return "", false
}

// loadSkillDocument reads one document and registers the skill. dir is empty
// for legacy flat files, which have no bundled resources.
func loadSkillDocument(cfg *Config, defaultName, path, dir string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	name, skill, err := parseSkillDocument(defaultName, string(data))
	if err != nil {
		logSkillIssue(path, err)
		return
	}
	if !canonicalSkillName(name) {
		logSkillIssue(path, fmt.Errorf("name %q does not follow the Agent Skills convention (lower-case letters, digits and hyphens)", name))
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	skill.Path = path
	skill.Dir = dir
	cfg.Skills[name] = skill
}

// logSkillIssue reports a skipped or non-conforming skill file without making
// startup fail: a broken skill only removes that one entry. It goes to the log
// file, never to stderr, which a running TUI would render as frame garbage.
func logSkillIssue(path string, err error) {
	logger.Warn("SKILL", fmt.Sprintf("%s ignored: %v", path, err))
}
