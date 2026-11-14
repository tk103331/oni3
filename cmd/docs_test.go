package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk103331/oni3/config"
	"github.com/tk103331/oni3/tools"
	"gopkg.in/yaml.v3"
)

func TestReadmeConfigurationUsesCanonicalNames(t *testing.T) {
	for _, filename := range []string{"README.md", "README_CN.md"} {
		t.Run(filename, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", filename))
			if err != nil {
				t.Fatal(err)
			}
			var example string
			for _, block := range strings.Split(string(data), "```yaml\n")[1:] {
				body := strings.SplitN(block, "```", 2)[0]
				if strings.Contains(body, "\nproviders:\n") && strings.Contains(body, "\nmcp_servers:\n") && strings.Contains(body, "\nagents:\n") {
					example = body
					break
				}
			}
			if example == "" {
				t.Fatal("complete configuration example missing")
			}
			decoder := yaml.NewDecoder(bytes.NewBufferString(example))
			decoder.KnownFields(true)
			var cfg config.Config
			if err := decoder.Decode(&cfg); err != nil {
				t.Fatal(err)
			}
			for name, definition := range cfg.Tools {
				instance, err := tools.CreateTool(name, definition)
				if err != nil {
					t.Fatalf("documented tool %s: %v", name, err)
				}
				info, err := instance.Info(context.Background())
				if err != nil || info.Name != name {
					t.Fatalf("documented tool identity %s: %+v / %v", name, info, err)
				}
			}
			for name, definition := range cfg.Agents {
				if _, exists := cfg.Models[definition.Model]; !exists {
					t.Fatalf("agent %s references unknown model %s", name, definition.Model)
				}
				for _, toolName := range definition.Tools {
					if _, exists := cfg.Tools[toolName]; !exists {
						t.Fatalf("agent %s references unknown tool %s", name, toolName)
					}
				}
			}
		})
	}
}
