package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/tk103331/oni3/config"
	"gopkg.in/yaml.v3"
)

func providerTestApp(t *testing.T, provider config.Provider, haveModel bool) (*AgentApp, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	previous := config.GetConfig()
	if previous == nil {
		previous = &config.Config{}
	}
	before, err := yaml.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	restore := filepath.Join(dir, "restore.yml")
	if err := os.WriteFile(restore, before, 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := config.LoadConfig(restore); err != nil {
			t.Error(err)
		}
	})
	cfg := config.Config{Providers: map[string]config.Provider{"cloud": provider}, Agents: map[string]config.Agent{"OniThree": {System: "Help the user"}}}
	if haveModel {
		cfg.Models = map[string]config.Model{"old": {Provider: "cloud", Model: "old"}}
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "fixture.yml")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadConfig(filename); err != nil {
		t.Fatal(err)
	}
	app, err := NewAgentApp("OniThree")
	if err != nil {
		t.Fatal(err)
	}
	app.model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app, filename
}

func providerKeyCmd(t *testing.T, m *ViewModel, key tea.KeyMsg) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(key)
	return cmd
}

func receiveDiscovery(t *testing.T, m *ViewModel, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("discovery did not return an asynchronous command")
	}
	m.Update(cmd())
	if m.provider == nil || m.provider.stage != providerModels {
		t.Fatalf("expected model selection, got %+v", m.provider)
	}
}

func TestProviderKeyMaskedAndSingleImportSwitchesImmediately(t *testing.T) {
	const key = "test-private-key"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("missing configured API key")
		}
		fmt.Fprint(w, `{"data":[{"id":"alpha"},{"id":"beta"}]}`)
	}))
	defer server.Close()
	app, filename := providerTestApp(t, config.Provider{Type: "openai", BaseURL: server.URL}, false)
	m := app.model
	if app.agent != nil {
		t.Fatal("a provider-only configuration should await its first model")
	}
	if cmd := m.executeSlashCommand("/provider cloud"); cmd != nil {
		t.Fatal("request started before entering a key")
	}
	if m.provider == nil || m.provider.stage != providerKey {
		t.Fatal("missing API key prompt")
	}
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	if strings.Contains(m.View(), key) || m.input != "" {
		t.Fatal("API key exposed in rendered or chat input")
	}
	for _, message := range m.messages {
		if strings.Contains(message.Content, key) {
			t.Fatal("key entered transcript")
		}
	}
	cmd := providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if requests.Load() != 0 {
		t.Fatal("network request blocked the update goroutine")
	}
	receiveDiscovery(t, m, cmd)
	if config.GetConfig().Providers["cloud"].APIKey != key {
		t.Fatal("successful key was not saved")
	}
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // first model is the default selection
	if m.provider != nil {
		t.Fatal("successful import did not close selection")
	}
	if app.currentModelName() != "cloud/alpha" || app.agent == nil || !strings.Contains(m.subtitle, "cloud/alpha") {
		t.Fatalf("model did not switch: %s / %s", app.currentModelName(), m.subtitle)
	}
	if _, ok := config.GetConfig().Models["cloud/beta"]; ok {
		t.Fatal("single selection imported every model")
	}
	reloaded, err := config.LoadConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Providers["cloud"].APIKey != key || reloaded.Models["cloud/alpha"].Model != "alpha" {
		t.Fatal("provider state did not survive restart")
	}
	if requests.Load() != 1 {
		t.Fatalf("expected one models request, got %d", requests.Load())
	}
}

func TestProviderConfiguredKeyAndImportAllPreservesSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer configured" {
			t.Error("configured key not used")
		}
		fmt.Fprint(w, `{"data":[{"id":"alpha"},{"id":"beta"}]}`)
	}))
	defer server.Close()
	app, _ := providerTestApp(t, config.Provider{Type: "openai", BaseURL: server.URL, APIKey: "configured"}, true)
	m := app.model
	receiveDiscovery(t, m, m.executeSlashCommand("/provider cloud"))
	if m.provider.stage != providerModels {
		t.Fatal("unnecessary API key prompt")
	}
	m.provider.sel = 0
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if app.currentModelName() != "old" {
		t.Fatalf("import-all switched model to %q", app.currentModelName())
	}
	for _, name := range []string{"cloud/alpha", "cloud/beta"} {
		if _, ok := config.GetConfig().Models[name]; !ok {
			t.Fatalf("missing imported model %s", name)
		}
	}
	if m.provider != nil {
		t.Fatal("import all did not close modal")
	}
}

func TestProviderFailedKeyCanBeReplacedWithoutLeakingOrSavingIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer valid" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "bad key")
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"alpha"}]}`)
	}))
	defer server.Close()
	app, _ := providerTestApp(t, config.Provider{Type: "openai", BaseURL: server.URL}, false)
	m := app.model
	m.executeSlashCommand("/provider cloud")
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("invalid")})
	cmd := providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(cmd())
	if m.provider.stage != providerFailed || config.GetConfig().Providers["cloud"].APIKey != "" {
		t.Fatal("failed key was saved or failure UI missing")
	}
	if strings.Contains(m.View(), "invalid") {
		t.Fatal("failed key exposed")
	}
	m.provider.sel = 1
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("valid")})
	receiveDiscovery(t, m, providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter}))
	if config.GetConfig().Providers["cloud"].APIKey != "valid" {
		t.Fatal("replacement key not saved")
	}
}

func TestProviderCancellationIgnoresLateResults(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	app, _ := providerTestApp(t, config.Provider{Type: "openai", BaseURL: server.URL, APIKey: "configured"}, true)
	m := app.model
	cmd := m.executeSlashCommand("/provider cloud")
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request never started")
	}
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	select {
	case msg := <-result:
		m.Update(msg)
	case <-time.After(2 * time.Second):
		t.Fatal("Esc did not cancel the request")
	}
	if m.provider != nil {
		t.Fatal("late result reopened the canceled flow")
	}
	if len(config.GetConfig().Models) != 1 {
		t.Fatal("canceled flow imported models")
	}
}

func TestPluginProvidersAvailableBeforeFirstModel(t *testing.T) {
	_, fixture := providerTestApp(t, config.Provider{Type: "openai"}, false)
	module, err := filepath.Abs(filepath.Join("..", "..", "examples", "plugins", "providers.js"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(map[string]any{"plugins": map[string]any{"catalog": map[string]any{"path": module}}, "agents": map[string]any{"OniThree": map[string]any{"system": "Help"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(fixture)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewAgentApp("OniThree")
	if err != nil {
		t.Fatal(err)
	}
	var candidates []string
	for _, command := range app.slashCommands() {
		if command.Name == "provider" {
			candidates = command.Values
		}
	}
	if strings.Join(candidates, ",") != "catalog__cloud,catalog__local" {
		t.Fatalf("provider candidates: %v", candidates)
	}
	if len(cfg.Models) != 0 {
		t.Fatal("providers unexpectedly registered models")
	}
	m := app.model
	m.executeSlashCommand("/provider catalog__cloud")
	if m.provider == nil || m.provider.stage != providerKey {
		t.Fatal("plugin provider did not prompt for missing key")
	}
	m.closeProvider()
	cmd := m.executeSlashCommand("/provider catalog__local")
	if cmd == nil || m.provider.stage != providerLoading {
		t.Fatal("optional-key provider did not prepare discovery")
	}
	m.closeProvider() // No real endpoint is contacted: the command is not executed.
}

func TestProviderFilteringFrameHeightAndSecretCancellation(t *testing.T) {
	m := NewViewModel(nil)
	cfg := &config.Config{Providers: map[string]config.Provider{"cloud": {Type: "openai"}, "local": {Type: "ollama"}}}
	m.openProvider(cfg, context.Background(), "", nil)
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("cloud")})
	if options := m.provider.options(m.input); len(options) != 1 || options[0].value != "cloud" {
		t.Fatalf("filter: %+v", options)
	}
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	secret := "test-key-" + strings.Repeat("a", 100)
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(secret)})
	for _, width := range []int{24, 40, 80} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 18})
		out := ansi.Strip(m.View())
		if strings.Contains(out, secret) || len(strings.Split(out, "\n")) > m.height {
			t.Fatalf("invalid masked frame at %d", width)
		}
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("wrapped row at %d: %q", width, line)
			}
		}
	}
	p := m.provider
	providerKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.provider != nil || len(p.key) != 0 || p.candidateKey != "" || m.input != "" {
		t.Fatal("canceled secret was retained")
	}
}
