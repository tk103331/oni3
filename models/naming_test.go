package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
)

func TestFactoryDefaultAndAnthropicPreserveRemoteModelID(t *testing.T) {
	const remoteID = "claude-remote-Original"
	for _, kind := range []string{"", config.ProviderTypeOpenAI, config.ProviderTypeAnthropic} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var body struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != remoteID {
					t.Errorf("remote model ID changed: %+v, %v", body, err)
				}
				w.Header().Set("Content-Type", "application/json")
				if kind == config.ProviderTypeAnthropic {
					if r.Header.Get("x-api-key") != "test-key" {
						t.Error("anthropic factory did not use the Anthropic implementation")
					}
					io.WriteString(w, `{"id":"msg_test","type":"message","role":"assistant","model":"claude-remote-Original","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
				} else {
					if r.Header.Get("Authorization") != "Bearer test-key" {
						t.Error("empty/openai factory type did not use the OpenAI implementation")
					}
					io.WriteString(w, `{"id":"chat_test","object":"chat.completion","created":1,"model":"claude-remote-Original","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
				}
			}))
			defer server.Close()
			cfg := &config.Config{
				Providers: map[string]config.Provider{"My_Cloud": {Type: kind, BaseURL: server.URL, APIKey: "test-key"}},
				Models:    map[string]config.Model{"My_Alias": {Provider: "My_Cloud", Model: remoteID, MaxTokens: 16}},
			}
			model, err := NewFactory(cfg).CreateChatModel(context.Background(), "My_Alias")
			if err != nil {
				t.Fatal(err)
			}
			message, err := model.Generate(context.Background(), []*schema.Message{schema.UserMessage("test")})
			if err != nil || message.Content != "ok" || requests.Load() != 1 {
				t.Fatalf("generate via canonical provider: %+v, %v; requests=%d", message, err, requests.Load())
			}
			if cfg.Providers["My_Cloud"].Type != kind || cfg.Models["My_Alias"].Model != remoteID {
				t.Fatal("factory mutated provider type, user alias, or remote model ID")
			}
		})
	}
}

func TestFactoryAndDiscoveryRejectNoncanonicalTypesBeforeRequests(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()
	for _, kind := range []string{"claude", "Claude", "ANTHROPIC", "OpenAI", "open_ai", " openai", "openai ", "gemini\n", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			provider := config.Provider{Type: kind, BaseURL: server.URL, APIKey: "test-key"}
			cfg := &config.Config{
				Providers: map[string]config.Provider{"cloud": provider},
				Models:    map[string]config.Model{"test": {Provider: "cloud", Model: "remote"}},
			}
			if _, err := NewFactory(cfg).CreateChatModel(context.Background(), "test"); err == nil || !strings.Contains(err.Error(), "unsupported provider type") {
				t.Errorf("factory accepted %q: %v", kind, err)
			}
			if _, err := DiscoverModels(context.Background(), provider); err == nil || !strings.Contains(err.Error(), "unsupported provider type") {
				t.Errorf("discovery accepted %q: %v", kind, err)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatal("invalid provider types made discovery requests")
	}
}

func TestDiscoveryHasEndpointForEveryCanonicalProvider(t *testing.T) {
	for kind, want := range map[string]string{
		config.ProviderTypeOpenAI:    "https://api.openai.com/v1/models",
		config.ProviderTypeAnthropic: "https://api.anthropic.com/v1/models",
		config.ProviderTypeGemini:    "https://generativelanguage.googleapis.com/v1beta/models",
		config.ProviderTypeQwen:      "https://dashscope.aliyuncs.com/compatible-mode/v1/models",
		config.ProviderTypeQianfan:   "https://qianfan.baidubce.com/v2/models",
		config.ProviderTypeArk:       "https://ark.cn-beijing.volces.com/api/v3/models",
		config.ProviderTypeDeepSeek:  "https://api.deepseek.com/models",
		config.ProviderTypeOllama:    "http://localhost:11434/api/tags",
	} {
		endpoint, gotKind, err := discoveryEndpoint(config.Provider{Type: kind})
		if err != nil || gotKind != kind || endpoint.String() != want {
			t.Errorf("canonical provider %q endpoint: %v, %q, %v", kind, endpoint, gotKind, err)
		}
	}
}
