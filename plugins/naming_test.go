package plugins

import (
	"context"
	"testing"
)

func TestPluginNamesUseLowercaseIdentifiers(t *testing.T) {
	filename := writeModule(t, `export const models = {fast: {provider: "local", model: "remote/Model:Q4"}}`)
	for _, namespace := range []string{"Demo", "demo-name", "_demo", "123demo"} {
		if _, err := Load(context.Background(), namespace, Config{Path: filename}); err == nil {
			t.Errorf("accepted plugin namespace %q", namespace)
		}
	}
	for _, source := range []string{
		`export const tools = {"bad-name": {description: "x", execute() {return 1}}}`,
		`export const agents = {CamelCase: {}}`,
		`export const models = {"bad-name": {}}`,
		`export const providers = {CamelCase: {}}`,
		`export const themes = {"bad-name": {}}`,
	} {
		if _, err := Load(context.Background(), "demo", Config{Path: writeModule(t, source)}); err == nil {
			t.Errorf("accepted noncanonical local name: %s", source)
		}
	}
}
