package models

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
)

func TestFactorySendsFixedUserAgent(t *testing.T) {
	for _, kind := range []string{config.ProviderTypeOpenAI, config.ProviderTypeAnthropic} {
		t.Run(kind, func(t *testing.T) {
			var gotUA string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotUA = r.Header.Get("User-Agent")
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("x-api-key") != "" {
					io.WriteString(w, `{"id":"msg_test","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
					return
				}
				io.WriteString(w, `{"id":"chat_test","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()

			cfg := &config.Config{
				Providers: map[string]config.Provider{"cloud": {Type: kind, BaseURL: server.URL, APIKey: "k"}},
				Models:    map[string]config.Model{"m": {Provider: "cloud", Model: "m"}},
			}
			chatModel, err := NewFactory(cfg).CreateChatModel(context.Background(), "m")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := chatModel.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")}); err != nil {
				t.Fatal(err)
			}
			if gotUA != UserAgent {
				t.Fatalf("User-Agent = %q, want %q", gotUA, UserAgent)
			}
		})
	}
}

func TestFactoryBuildsGeminiClient(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.Provider{"g": {Type: config.ProviderTypeGemini, APIKey: "k"}},
		Models:    map[string]config.Model{"m": {Provider: "g", Model: "gemini-2.0-flash"}},
	}
	if _, err := NewFactory(cfg).CreateChatModel(context.Background(), "m"); err != nil {
		t.Fatalf("create gemini chat model: %v", err)
	}
}

func TestDiscoverySendsFixedUserAgent(t *testing.T) {
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()

	provider := config.Provider{Type: config.ProviderTypeOpenAI, BaseURL: server.URL, APIKey: "k"}
	if _, err := DiscoverModels(context.Background(), provider); err != nil {
		t.Fatal(err)
	}
	if gotUA != UserAgent {
		t.Fatalf("User-Agent = %q, want %q", gotUA, UserAgent)
	}
}
