package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// StarterConfig is the configuration written on the first run. Oni3 already
// ships the common providers (OpenAI, Anthropic, Gemini, DeepSeek, Qwen, Kimi,
// Zhipu, MiniMax, Ollama and more), so this file only needs entries for custom
// endpoints. Models are imported from the provider at runtime and API keys are
// never stored here.
const StarterConfig = `# Oni3 configuration.
#
# This file was created on the first run. It can stay empty: the common
# providers are built in and appear in the provider picker (/provider), where
# an API key can be entered and the available models imported.
#
# Add entries here only for custom endpoints:
#
#   providers:
#     my-endpoint:
#       type: openai
#       base_url: https://example.com/v1
#       key_url: https://example.com/api-keys
#
# Models are imported from the provider and do not need to be listed here.
# The full reference lives at https://github.com/tk103331/oni3
`

// EnsureConfigFile creates a starter configuration file at path when none
// exists yet. It reports whether the file was created.
func EnsureConfigFile(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("configuration file path is empty")
	}
	info, err := os.Stat(path)
	switch {
	case err == nil:
		if info.IsDir() {
			return false, fmt.Errorf("configuration path is a directory: %s", path)
		}
		return false, nil
	case !os.IsNotExist(err):
		return false, fmt.Errorf("failed to inspect configuration file: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return false, fmt.Errorf("failed to create configuration directory: %w", err)
		}
	}
	// The file can hold an API key, so it stays readable by its owner only.
	if err := os.WriteFile(path, []byte(StarterConfig), 0600); err != nil {
		return false, fmt.Errorf("failed to write configuration file: %w", err)
	}
	return true, nil
}
