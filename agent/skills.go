package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tk103331/oni3/config"
)

// ApplySkill folds a skill into an agent configuration: the skill's
// instructions go in front of the agent's system prompt and its extension
// tools are added without duplicating tools the agent already has.
//
// This is the eager half of the Agent Skills model: an explicitly activated
// skill (`/skill`, session config options) is inlined for the whole session.
// Skills that are only discovered stay out of the prompt body; see
// skillsCatalog.
func ApplySkill(ac *config.Agent, sk config.Skill) {
	if skillSystem := strings.TrimSpace(sk.System); skillSystem != "" {
		if strings.TrimSpace(ac.System) != "" {
			ac.System = skillSystem + "\n\n" + ac.System
		} else {
			ac.System = skillSystem
		}
	}

	seen := make(map[string]bool, len(ac.Tools))
	for _, name := range ac.Tools {
		seen[name] = true
	}
	for _, name := range sk.Tools {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		ac.Tools = append(ac.Tools, name)
		seen[name] = true
	}

	if sk.Name != "" {
		ac.ActiveSkill = sk.Name
	}
}

// skillsCatalog renders the discoverable skills for the system prompt,
// mirroring Anthropic's Agent Skills progressive disclosure: only the name,
// the description and the document location are loaded up front, so the model
// pays for a skill's full instructions only when one is relevant.
//
// The explicitly activated skill is left out: ApplySkill already inlined its
// instructions. Skills without instructions or without a document (for
// example a config.yml entry used only with /skill) are skipped.
func skillsCatalog(cfg *config.Config, active string) string {
	if cfg == nil || len(cfg.Skills) == 0 {
		return ""
	}

	names := make([]string, 0, len(cfg.Skills))
	for name := range cfg.Skills {
		if name == active {
			continue
		}
		skill := cfg.Skills[name]
		if strings.TrimSpace(skill.System) == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("Skills:\n")
	b.WriteString("Optional capability presets. Each entry gives a name, when to use it, and the markdown document with its full instructions. ")
	b.WriteString("Read the document of a skill before using it, follow it, and read the files bundled next to it when the instructions refer to them.\n")
	for _, name := range names {
		skill := cfg.Skills[name]
		description := strings.TrimSpace(skill.Description)
		if description == "" {
			description = firstSentence(skill.System)
		}
		fmt.Fprintf(&b, "- %s: %s", name, description)
		switch {
		case skill.Path != "":
			fmt.Fprintf(&b, " (SKILL.md: %s)", skill.Path)
		case skill.Dir != "":
			fmt.Fprintf(&b, " (directory: %s)", skill.Dir)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// firstSentence shortens instructions for a catalog entry that has no
// description: the first line is usually the skill's title.
func firstSentence(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#>*- "))
		if line == "" {
			continue
		}
		if idx := strings.IndexAny(line, ".!?"); idx > 0 {
			line = line[:idx]
		}
		runes := []rune(line)
		if len(runes) > 120 {
			return string(runes[:119]) + "…"
		}
		return line
	}
	return ""
}
