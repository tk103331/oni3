package plugins

import (
	"context"
	"strings"
	"testing"
)

func TestProviderOnlyPlugin(t *testing.T) {
	p := loadSource(t, `export const providers={cloud:{base_url:"https://example.invalid/v1",models_url:"catalog"}};`)
	if len(p.Providers) != 1 || !strings.Contains(string(p.Providers["cloud"]), "catalog") {
		t.Fatalf("provider manifest: %v", p.Providers)
	}
}

func TestInvalidProviderExportNames(t *testing.T) {
	for _, source := range []string{
		`export const providers={"bad.name":{}}`,
		`export const providers={"":{}}`,
		`export const providers={"` + strings.Repeat("a", 60) + `":{}}`,
	} {
		if _, err := Load(context.Background(), "demo", Config{Path: writeModule(t, source)}); err == nil || !strings.Contains(err.Error(), "invalid providers name") {
			t.Fatalf("expected invalid provider name, got %v", err)
		}
	}
}
