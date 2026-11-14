package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureConfigFileCreatesStarter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yml")

	created, err := EnsureConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected the starter file to be created")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != StarterConfig {
		t.Fatalf("unexpected file content:\n%s", data)
	}

	// The starter file must load, and it must offer a provider so the
	// interface can ask for an API key and import models.
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) == 0 {
		t.Fatal("starter configuration declares no provider")
	}
	if _, ok := cfg.Providers["openai"]; !ok {
		t.Fatalf("starter configuration providers: %v", cfg.Providers)
	}
}

func TestEnsureConfigFileKeepsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	const content = "providers:\n  mine:\n    type: openai\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	created, err := EnsureConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("an existing configuration file must not be recreated")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("existing file was changed:\n%s", data)
	}
}

func TestEnsureConfigFileErrors(t *testing.T) {
	if _, err := EnsureConfigFile(""); err == nil {
		t.Fatal("expected an error for an empty path")
	}

	dir := t.TempDir()
	if _, err := EnsureConfigFile(dir); err == nil {
		t.Fatal("expected an error when the path is a directory")
	}
}

func TestStarterConfigMentionsTheProviderCommand(t *testing.T) {
	if !strings.Contains(StarterConfig, "/provider") {
		t.Fatal("the starter configuration does not mention the /provider command")
	}
}
