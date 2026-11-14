package models

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tk103331/oni3/config"
)

func TestDiscoverModelsAuthenticatedAndSorted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request: method=%s path=%s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"data":[{"id":"z"},{"id":"a","name":"Alpha"},{"id":"z"}]}`)
	}))
	defer server.Close()
	got, err := DiscoverModels(context.Background(), config.Provider{Type: "openai", BaseURL: server.URL + "/v1", APIKey: "secret"})
	want := []DiscoveredModel{{ID: "a", Name: "Alpha"}, {ID: "z", Name: "z"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
}

func TestDiscoverModelsCustomEndpointAndMappings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/list" || r.URL.Query().Get("version") != "2" || r.Header.Get("X-Custom-Key") != "Token secret" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected custom request path=%s", r.URL.Path)
		}
		io.WriteString(w, `{"response":{"entries":[{"metadata":{"code":"namespace/id","title":"Display"}}]}}`)
	}))
	defer server.Close()
	got, err := DiscoverModels(context.Background(), config.Provider{
		BaseURL: server.URL + "/v1", ModelsURL: "/list?version=2", APIKey: "secret",
		ModelsPath: "response.entries", ModelIDPath: "metadata.code", ModelNamePath: "metadata.title",
		ModelHeaders: map[string]string{"X-Custom-Key": "Token {{api_key}}", "Authorization": ""},
	})
	if err != nil || !reflect.DeepEqual(got, []DiscoveredModel{{ID: "namespace/id", Name: "Display"}}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDiscoverModelsProviderHeadersAndShapes(t *testing.T) {
	for _, tc := range []struct{ kind, path, header, value, body, id, name string }{
		{"ollama", "/api/tags", "Authorization", "", `{"models":[{"name":"llama:latest","model":"llama:latest"}]}`, "llama:latest", "llama:latest"},
		{"gemini", "/v1beta/models", "x-goog-api-key", "secret", `{"models":[{"name":"models/gemini-test","displayName":"Gemini Test"}]}`, "gemini-test", "Gemini Test"},
		{"anthropic", "/v1/models", "x-api-key", "secret", `{"data":[{"id":"test","display_name":"Test"}]}`, "test", "Test"},
		{"openai", "/models", "Authorization", "Bearer secret", `["remote/id"]`, "remote/id", "remote/id"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.Header.Get(tc.header) != tc.value {
					t.Errorf("unexpected path or provider auth: %s", r.URL.Path)
				}
				if tc.kind == "anthropic" && (r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "") {
					t.Error("incorrect Anthropic headers")
				}
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			base := server.URL
			if tc.kind == "anthropic" {
				base += "/v1"
			} else if tc.kind == "gemini" {
				base += "/v1beta"
			}
			key := "secret"
			if tc.kind == "ollama" {
				key = ""
			}
			got, err := DiscoverModels(context.Background(), config.Provider{Type: tc.kind, BaseURL: base, APIKey: key})
			if err != nil || !reflect.DeepEqual(got, []DiscoveredModel{{ID: tc.id, Name: tc.name}}) {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestDiscoverModelsErrorsAreSafe(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"unauthorized", `echo secret Bearer secret`, 401},
		{"server", `secret`, 500},
		{"JSON", `secret`, 200},
		{"shape", `{}`, 200},
		{"null", `{"data":null}`, 200},
		{"missing ID", `{"data":[{}]}`, 200},
		{"empty ID", `[""]`, 200},
		{"control ID", `["secret\u001b[0m"]`, 200},
		{"whitespace ID", `["some model"]`, 200},
		{"numeric ID", `{"models":[{"id":42}]}`, 200},
		{"large", strings.Repeat("x", maxDiscoveryBytes+1), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			_, err := DiscoverModels(context.Background(), config.Provider{BaseURL: server.URL, APIKey: "secret"})
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected safe error, got %v", err)
			}
		})
	}
	for _, provider := range []config.Provider{
		{BaseURL: "http://user:secret@example.invalid"},
		{BaseURL: "http://example.invalid", ModelsURL: "http://user:secret@example.invalid"},
		{BaseURL: "http://[secret"},
		{Type: "secret"},
	} {
		_, err := DiscoverModels(context.Background(), provider)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("expected safe URL/type error, got %v", err)
		}
	}
}

func TestDiscoverModelsCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := DiscoverModels(ctx, config.Provider{BaseURL: server.URL})
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery ignored cancellation")
	}
	ctx, cancel = context.WithTimeout(context.Background(), -time.Second)
	defer cancel()
	if _, err := DiscoverModels(ctx, config.Provider{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
}

func TestDiscoverModelsRedirectsDoNotForwardCredentials(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		io.WriteString(w, `["leaked"]`)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			http.Redirect(w, r, target.URL+"/secret", http.StatusFound)
		case "/same/models":
			http.Redirect(w, r, "/actual", http.StatusTemporaryRedirect)
		case "/actual":
			if r.Header.Get("X-Custom-Key") != "secret" {
				t.Error("same-origin redirect lost custom header")
			}
			io.WriteString(w, `["ok"]`)
		}
	}))
	defer server.Close()
	provider := config.Provider{BaseURL: server.URL, APIKey: "secret", ModelHeaders: map[string]string{"X-Custom-Key": "{{api_key}}"}}
	if _, err := DiscoverModels(context.Background(), provider); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected safe blocked redirect error, got %v", err)
	}
	if targetRequests.Load() != 0 {
		t.Fatal("cross-origin redirect was contacted")
	}
	provider.BaseURL += "/same"
	if got, err := DiscoverModels(context.Background(), provider); err != nil || len(got) != 1 || got[0].ID != "ok" {
		t.Fatalf("same-origin redirect: %+v, %v", got, err)
	}
}

func TestDiscoveryEndpointResolution(t *testing.T) {
	for _, tc := range []struct{ base, models, want string }{
		{"https://example.com/compatible/v1", "", "https://example.com/compatible/v1/models"},
		{"https://example.com/compatible/v1/", "catalog", "https://example.com/compatible/v1/catalog"},
		{"https://example.com/compatible/v1", "/catalog", "https://example.com/catalog"},
		{"https://example.com/v1", "https://other.example/list", "https://other.example/list"},
	} {
		got, _, err := discoveryEndpoint(config.Provider{BaseURL: tc.base, ModelsURL: tc.models})
		if err != nil || got.String() != tc.want {
			t.Fatalf("resolution: %v, %v; want %s", got, err, tc.want)
		}
	}
}
