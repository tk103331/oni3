package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// StarterConfig is the configuration written on the first run. It declares one
// provider so the interface can offer the API key prompt and import models
// right away. The API key itself is never stored in this file.
const StarterConfig = `# Oni3 configuration.
#
# This file was created on the first run. Add a provider below, then run
# /provider inside Oni3 to enter its API key and import the available models.
#
# Any OpenAI-compatible endpoint works:
#
#   providers:
#     deepseek:
#       type: deepseek
#       base_url: https://api.deepseek.com/v1
#     qwen:
#       type: qwen
#       base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
#     ollama:
#       type: ollama
#       base_url: http://localhost:11434/v1
#       api_key_optional: true
#
# Models are imported from the provider and do not need to be listed here.
# The full reference lives at https://github.com/tk103331/oni3

providers:
  openai:
    type: openai
    base_url: https://api.openai.com/v1
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
