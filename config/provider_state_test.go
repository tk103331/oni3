package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func providerConfigFile(t *testing.T, contents string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	filename := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(filename, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestProviderStateRoundTripPrivateAndSourceUntouched(t *testing.T) {
	const source = "# user comment\nproviders:\n  cloud:\n    type: openai\n    base_url: https://example.invalid/v1\nmodels:\n  tuned:\n    provider: cloud\n    model: existing\n    temperature: 0.7\n"
	filename := providerConfigFile(t, source)
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SourcePath != filename {
		t.Fatalf("source path: %s", cfg.SourcePath)
	}
	if err := cfg.SetProviderAPIKey("cloud", "secret"); err != nil {
		t.Fatal(err)
	}
	got, err := cfg.AddProviderModels("cloud", []string{"existing", "remote/model", "other", "remote/model"})
	want := []string{"tuned", "cloud/remote/model", "cloud/other"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("import: %v, %v", got, err)
	}
	if cfg.Models["tuned"].Temperature != 0.7 || len(cfg.Models) != 3 {
		t.Fatalf("alias tuning or dedup lost: %+v", cfg.Models)
	}
	statePath, err := cfg.providerStatePath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(statePath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("state mode: %v, %v", info, err)
	}
	beforeTime := info.ModTime()
	if err := cfg.SetProviderAPIKey("cloud", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddProviderModels("cloud", []string{"existing", "remote/model"}); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(statePath)
	if !info.ModTime().Equal(beforeTime) {
		t.Fatal("no-op operations rewrote state")
	}
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != source {
		t.Fatal("source YAML was rewritten")
	}
	loaded, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Providers["cloud"].APIKey != "secret" || !reflect.DeepEqual(loaded.Models, cfg.Models) {
		t.Fatal("saved key/models not restored")
	}
	entries, err := os.ReadDir(filepath.Dir(statePath))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary state file was left behind: %v, %v", entries, err)
	}
}

func TestProviderStateScopeAndConfiguredKeyPrecedence(t *testing.T) {
	const source = "providers:\n  cloud:\n    type: openai\n    base_url: https://first.invalid/v1\n"
	filename := providerConfigFile(t, source)
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProviderAPIKey("cloud", "saved"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddProviderModels("cloud", []string{"imported"}); err != nil {
		t.Fatal(err)
	}
	for _, modified := range []string{
		strings.ReplaceAll(source, "first.invalid", "second.invalid"),
		strings.ReplaceAll(source, "openai", "deepseek"),
		source + "    models_url: https://list.invalid/models\n",
	} {
		if err := os.WriteFile(filename, []byte(modified), 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadConfig(filename)
		if err != nil || loaded.Providers["cloud"].APIKey != "" || len(loaded.Models) != 0 {
			t.Fatalf("state crossed provider identity: %+v, %v", loaded, err)
		}
	}
	other := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(other, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(other)
	if err != nil || loaded.Providers["cloud"].APIKey != "" || len(loaded.Models) != 0 {
		t.Fatal("state crossed source paths")
	}
	if err := os.WriteFile(filename, []byte(source+"    api_key: configured\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadConfig(filename)
	if err != nil || loaded.Providers["cloud"].APIKey != "configured" || loaded.Models["cloud/imported"].Model != "imported" {
		t.Fatalf("configured key must win while restoring models: %+v, %v", loaded, err)
	}
}

func TestProviderModelBatchFailuresAreAtomic(t *testing.T) {
	filename := providerConfigFile(t, "providers:\n  cloud:\n    type: openai\nmodels:\n  cloud/collision:\n    provider: other\n    model: preserve\n")
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{"valid", "collision"}, {"valid", "bad\nID"}, {"valid", ""}, {"valid", "invalid model"}} {
		if _, err := cfg.AddProviderModels("cloud", ids); err == nil {
			t.Fatalf("expected failure for %q", ids)
		}
		if len(cfg.Models) != 1 || cfg.Models["cloud/collision"].Model != "preserve" {
			t.Fatal("failed batch mutated model map")
		}
	}
	statePath, _ := cfg.providerStatePath()
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatal("failed batches persisted state")
	}
	// Force a filesystem failure after validation without relying on uid/mode.
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".oni3"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddProviderModels("cloud", []string{"valid"}); err == nil || len(cfg.Models) != 1 {
		t.Fatal("persistence failure mutated models")
	}
	if err := cfg.SetProviderAPIKey("cloud", "secret"); err == nil || cfg.Providers["cloud"].APIKey != "" {
		t.Fatal("persistence failure mutated key")
	}
}

func TestProviderStateRestorePreservesConfiguredCollisions(t *testing.T) {
	const source = "providers:\n  cloud:\n    type: openai\n"
	filename := providerConfigFile(t, source)
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddProviderModels("cloud", []string{"remote"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(source+"models:\n  cloud/remote:\n    provider: other\n    model: configured\n    max_tokens: 100\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(filename)
	if err != nil || loaded.Models["cloud/remote"].Provider != "other" || loaded.Models["cloud/remote"].MaxTokens != 100 {
		t.Fatalf("configured collision overwritten: %+v, %v", loaded, err)
	}
}

func TestProviderStateInvalidInputAndCorruption(t *testing.T) {
	filename := providerConfigFile(t, "providers:\n  cloud:\n    type: openai\n")
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProviderAPIKey("missing", "secret"); err == nil {
		t.Fatal("unknown provider accepted")
	}
	for _, key := range []string{"", " \t", "secret\nheader"} {
		if err := cfg.SetProviderAPIKey("cloud", key); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("expected safe invalid-key error, got %v", err)
		}
	}
	if _, err := cfg.AddProviderModels("missing", []string{"model"}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	if err := cfg.SetProviderAPIKey("cloud", "secret"); err != nil {
		t.Fatal(err)
	}
	statePath, _ := cfg.providerStatePath()
	if err := os.WriteFile(statePath, []byte(`{"api_key":"secret"`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filename); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected safe corruption error, got %v", err)
	}
}
