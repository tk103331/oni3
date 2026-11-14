package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tk103331/oni3/config"
)

func TestRootCommandCreatesMissingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")

	previous := configPath
	configPath = path
	t.Cleanup(func() { configPath = previous })

	// A fresh install has no configuration file yet. The root command must
	// create a starter one instead of failing.
	if err := RootCmd.PersistentPreRunE(RootCmd, nil); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("starter configuration was not created: %v", err)
	}

	cfg := config.GetConfig()
	if cfg == nil {
		t.Fatal("starter configuration was not loaded")
	}
	if len(cfg.Providers) == 0 {
		t.Fatal("starter configuration declares no provider")
	}
}

func TestRootCommandKeepsExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	const content = "providers:\n  mine:\n    type: openai\n    base_url: https://example.com/v1\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	previous := configPath
	configPath = path
	t.Cleanup(func() { configPath = previous })

	if err := RootCmd.PersistentPreRunE(RootCmd, nil); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("existing configuration was changed:\n%s", data)
	}
}
