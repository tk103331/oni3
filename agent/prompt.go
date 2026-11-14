package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/tools"
)

// systemPrompt adds the delegation catalog at request time, including after
// agent/skill switches, without modifying the configured system instructions.
func (r *ReactAgent) systemPrompt(cfg *config.Config) string {
	if r.config == nil {
		return ""
	}
	var sections []string
	if strings.TrimSpace(r.config.System) != "" {
		sections = append(sections, r.config.System)
	}
	if cfg == nil {
		return strings.Join(sections, "\n\n")
	}

	for _, toolName := range r.config.Tools {
		tl, ok := cfg.Tools[toolName]
		if !ok || tl.Type != tools.SubagentToolType {
			continue
		}
		var allowed map[string]bool
		if v, ok := tl.Config["agents"]; ok && v.IsArray() {
			allowed = make(map[string]bool)
			for _, entry := range v.Array() {
				if name := strings.TrimSpace(entry.String()); name != "" {
					allowed[name] = true
				}
			}
		}
		var names []string
		for name := range cfg.Agents {
			if len(allowed) == 0 || allowed[name] {
				names = append(names, name)
			}
		}
		sort.Strings(names)

		var catalog strings.Builder
		fmt.Fprintf(&catalog, "Subagent tool %q:\n", toolName)
		catalog.WriteString("Run a configured agent by passing its exact name in `agent` and the task in `prompt`. ")
		catalog.WriteString("Do not combine `agent` with `system`.\n")
		if len(names) > 0 {
			catalog.WriteString("Available configured agents (name: description):\n")
			for _, name := range names {
				description := strings.TrimSpace(cfg.Agents[name].Description)
				if description == "" {
					description = "No description provided."
				}
				fmt.Fprintf(&catalog, "- %q: %q\n", name, description)
			}
		} else {
			catalog.WriteString("No configured agents are available through this tool.\n")
		}

		allowTemp := true
		if v, ok := tl.Config["allow_temp"]; ok {
			allowTemp = v.Bool()
		}
		if allowTemp {
			catalog.WriteString("You may also create a temporary agent by passing `system` and `prompt`, with optional `model` and `tools`; omit `agent`. ")
			catalog.WriteString("Only `prompt` is required for a default temporary agent. When omitted, the model and tools inherit from the current agent, subject to this tool's allowlists. ")
			catalog.WriteString("The child has its own conversation: include the necessary context in `prompt`.")
		} else {
			catalog.WriteString("Temporary agents are disabled for this tool; use one of the configured agents above.")
		}
		sections = append(sections, strings.TrimSpace(catalog.String()))
	}
	return strings.Join(sections, "\n\n")
}
