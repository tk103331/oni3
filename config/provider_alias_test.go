package config

import (
	"os"
	"reflect"
	"testing"
)

func TestImportedModelCanGainConfiguredAliasOnReload(t *testing.T) {
	const source = "providers:\n  cloud:\n    type: openai\n"
	filename := providerConfigFile(t, source)
	cfg, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddProviderModels("cloud", []string{"remote"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(source+"models:\n  tuned:\n    provider: cloud\n    model: remote\n    temperature: 0.6\n"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Models) != 1 || loaded.Models["tuned"].Temperature != 0.6 {
		t.Fatalf("imported model duplicated a configured alias: %+v", loaded.Models)
	}
	if names, err := loaded.AddProviderModels("cloud", []string{"remote"}); err != nil || !reflect.DeepEqual(names, []string{"tuned"}) {
		t.Fatalf("alias was not reusable: %v, %v", names, err)
	}
}
