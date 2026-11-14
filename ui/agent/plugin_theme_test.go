package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tk103331/oni3/config"
	"gopkg.in/yaml.v3"
)

func TestPluginThemeSelectableAndApplied(t *testing.T) {
	dir := t.TempDir()
	previous := config.GetConfig()
	if previous == nil {
		previous = &config.Config{}
	}
	data, err := yaml.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	restore := filepath.Join(dir, "restore.yml")
	if err := os.WriteFile(restore, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := config.LoadConfig(restore); err != nil {
			t.Error(err)
		}
	})
	if _, err := config.LoadConfig(filepath.Join("..", "..", "examples", "plugins", "demo.yml")); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(themeNames(), "demo__ocean") {
		t.Fatal("plugin theme missing from command candidates")
	}
	m := NewViewModel(nil)
	if err := m.ApplyTheme("DEMO__OCEAN", "light"); err != nil {
		t.Fatal(err)
	}
	if m.ThemeName() != "demo__ocean" || m.theme.Primary != "#0369A1" {
		t.Fatalf("light theme: %+v", m.theme)
	}
	if err := m.ApplyTheme("demo__ocean", "dark"); err != nil {
		t.Fatal(err)
	}
	if m.theme.Primary != "#7DD3FC" {
		t.Fatalf("dark theme: %+v", m.theme)
	}
}
