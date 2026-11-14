package config

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tk103331/oni3/plugins"
	"gopkg.in/yaml.v3"
)

// loadPlugins makes all extensions available before factories and command
// palettes inspect configuration. A failed load never replaces globalConfig.
func (c *Config) loadPlugins(ctx context.Context, configPath string) error {
	if len(c.Plugins) == 0 {
		return c.loadProviderState()
	}
	if c.Providers == nil {
		c.Providers = make(map[string]Provider)
	}
	providerOwners := make(map[string]string)
	if c.Tools == nil {
		c.Tools = make(map[string]Tool)
	}
	if c.Agents == nil {
		c.Agents = make(map[string]Agent)
	}
	if c.Models == nil {
		c.Models = make(map[string]Model)
	}
	c.PluginThemes = make(map[string]plugins.Theme)
	names := make([]string, 0, len(c.Plugins))
	for name := range c.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		settings := c.Plugins[name]
		if settings.Path != "" && !filepath.IsAbs(settings.Path) {
			settings.Path = filepath.Join(filepath.Dir(configPath), settings.Path)
		}
		p, err := plugins.Load(ctx, name, settings)
		if err != nil {
			return err
		}
		for _, t := range p.Tools {
			info, err := t.Info(ctx)
			if err != nil {
				return err
			}
			if _, exists := c.Tools[info.Name]; exists {
				return fmt.Errorf("plugin %q: tool %q already exists", name, info.Name)
			}
			c.Tools[info.Name] = Tool{Type: "plugin", Description: info.Desc, PluginTool: t}
		}
		for local, data := range p.Providers {
			fullName := name + "__" + local
			if owner, exists := providerOwners[fullName]; exists {
				return fmt.Errorf("plugin %q: provider %q already exported by plugin %q", name, fullName, owner)
			}
			var provider Provider
			if err := decodePluginDefinition(data, &provider); err != nil {
				return fmt.Errorf("plugin %q provider %q: %w", name, local, err)
			}
			if override, exists := c.Providers[fullName]; exists {
				if err := c.mergeProviderOverride(fullName, &provider, override); err != nil {
					return fmt.Errorf("plugin %q provider %q: %w", name, local, err)
				}
			}
			provider.Type = provider.EffectiveType()
			if err := ValidateProviderType(provider.Type); err != nil {
				return fmt.Errorf("plugin %q provider %q: %w", name, local, err)
			}
			c.Providers[fullName] = provider
			providerOwners[fullName] = name
		}
		for local, data := range p.Models {
			fullName := name + "__" + local
			if _, exists := c.Models[fullName]; exists {
				return fmt.Errorf("plugin %q: model %q already exists", name, fullName)
			}
			var model Model
			if err := decodePluginDefinition(data, &model); err != nil {
				return fmt.Errorf("plugin %q model %q: %w", name, local, err)
			}
			c.Models[fullName] = model
		}
		for local, data := range p.Agents {
			fullName := name + "__" + local
			if _, exists := c.Agents[fullName]; exists {
				return fmt.Errorf("plugin %q: agent %q already exists", name, fullName)
			}
			var agent Agent
			if err := decodePluginDefinition(data, &agent); err != nil {
				return fmt.Errorf("plugin %q agent %q: %w", name, local, err)
			}
			c.Agents[fullName] = agent
		}
		for local, theme := range p.Themes {
			fullName := name + "__" + local
			for existing := range c.PluginThemes {
				if strings.EqualFold(existing, fullName) {
					return fmt.Errorf("plugin %q: theme %q already exists", name, fullName)
				}
			}
			c.PluginThemes[fullName] = theme
		}
		c.LoadedPlugins = append(c.LoadedPlugins, p)
	}
	// Imported models must exist before validating plugin-agent references.
	if err := c.loadProviderState(); err != nil {
		return err
	}
	// Resolve references after loading every plugin, allowing cross-plugin use.
	for _, p := range c.LoadedPlugins {
		for local := range p.Models {
			name := p.Name() + "__" + local
			model := c.Models[name]
			if _, exists := c.Providers[model.Provider]; !exists || strings.TrimSpace(model.Model) == "" {
				return fmt.Errorf("plugin model %q needs a configured provider and a nonempty model", name)
			}
		}
		for local := range p.Agents {
			name := p.Name() + "__" + local
			agent := c.Agents[name]
			if agent.MaxSteps < 0 {
				return fmt.Errorf("plugin agent %q: max_steps cannot be negative", name)
			}
			if agent.Model != "" {
				if _, exists := c.Models[agent.Model]; !exists {
					return fmt.Errorf("plugin agent %q: unknown model %q", name, agent.Model)
				}
			}
			// Tools and MCP servers are resolved by the ordinary agent factory.
			// CLI defaults are installed after LoadConfig, so built-in tool names
			// need not already exist while plugin definitions are merged.
		}
	}
	return nil
}

func decodePluginDefinition(data []byte, target any) error {
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		return fmt.Errorf("definition must be an object")
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}

// HasPluginHook reports whether an event has subscribers.
func (c *Config) HasPluginHook(event string) bool {
	if c != nil {
		for _, p := range c.LoadedPlugins {
			if p.HasHook(event) {
				return true
			}
		}
	}
	return false
}

// EmitPluginEvent invokes subscribers in configured plugin-name order.
// A hook error is returned to the caller, so failures cannot silently disappear.
func (c *Config) EmitPluginEvent(ctx context.Context, event plugins.Event) error {
	if c == nil {
		return nil
	}
	for _, p := range c.LoadedPlugins {
		if err := p.Emit(ctx, event); err != nil {
			return fmt.Errorf("plugin %q hook %q: %w", p.Name(), event.Type, err)
		}
	}
	return nil
}
