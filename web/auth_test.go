package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/tk103331/oni3/config"
)

func authTestServer(t *testing.T, token string) *httptest.Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{
		DefaultModel: "base",
		Models:       map[string]config.Model{"base": {Provider: "openai", Model: "gpt-4"}},
		Agents:       map[string]config.Agent{"helper": {Model: "base"}},
	}
	app, err := New(cfg, Options{
		DefaultAgent:    "helper",
		WorkDir:         "/tmp/oni3",
		Version:         "test",
		Token:           token,
		EnableMCPServer: true,
		EnableA2A:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return server
}

func request(t *testing.T, client *http.Client, method, target string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestTokenAuthentication(t *testing.T) {
	server := authTestServer(t, "top-secret")

	t.Run("login page without token", func(t *testing.T) {
		resp := request(t, http.DefaultClient, http.MethodGet, server.URL+"/", nil)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), `id="token"`) || !strings.Contains(string(body), "需要访问令牌") {
			t.Fatalf("login page not served: %s", body)
		}
		if strings.Contains(string(body), `id="app"`) {
			t.Fatal("application page leaked without authentication")
		}
	})

	t.Run("api requires auth", func(t *testing.T) {
		resp := request(t, http.DefaultClient, http.MethodGet, server.URL+"/api/models", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if resp.Header.Get("WWW-Authenticate") == "" {
			t.Fatal("missing WWW-Authenticate header")
		}
	})

	t.Run("mcp requires auth", func(t *testing.T) {
		resp := request(t, http.DefaultClient, http.MethodPost, server.URL+"/mcp", strings.NewReader(`{}`))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("bearer token", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/models", nil)
		req.Header.Set("Authorization", "Bearer top-secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d (%s)", resp.StatusCode, body)
		}
	})

	t.Run("a2a bearer token", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/a2a",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ListTasks","params":{}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer top-secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d (%s)", resp.StatusCode, body)
		}
		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["result"] == nil || payload["error"] != nil {
			t.Fatalf("response = %+v", payload)
		}
	})

	t.Run("mcp bearer token", func(t *testing.T) {
		mcpClient, err := mcpclient.NewStreamableHttpClient(
			server.URL+"/mcp",
			transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer top-secret"}),
		)
		if err != nil {
			t.Fatalf("NewStreamableHttpClient: %v", err)
		}
		defer mcpClient.Close()
		ctx := context.Background()
		if err := mcpClient.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{Name: "auth-test", Version: "1.0.0"}
		if _, err := mcpClient.Initialize(ctx, initRequest); err != nil {
			t.Fatalf("Initialize: %v", err)
		}
	})

	t.Run("header token", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/models", nil)
		req.Header.Set("X-Oni3-Token", "top-secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("query token", func(t *testing.T) {
		resp := request(t, http.DefaultClient, http.MethodGet, server.URL+"/api/models?token=top-secret", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		resp := request(t, http.DefaultClient, http.MethodGet, server.URL+"/api/models?token=wrong", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	})

	t.Run("query token exchanges for cookie", func(t *testing.T) {
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp := request(t, client, http.MethodGet, server.URL+"/?token=top-secret", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if location := resp.Header.Get("Location"); location != "/" {
			t.Fatalf("location = %q", location)
		}
		cookies := resp.Cookies()
		if len(cookies) != 1 || cookies[0].Name != authCookieName || cookies[0].Value == "" {
			t.Fatalf("cookies = %+v", cookies)
		}
		if !cookies[0].HttpOnly {
			t.Fatal("auth cookie is not HttpOnly")
		}
	})

	t.Run("cookie session", func(t *testing.T) {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Jar: jar}
		resp := request(t, client, http.MethodGet, server.URL+"/?token=top-secret", nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `id="app"`) {
			t.Fatalf("app page: status=%d body=%s", resp.StatusCode, body)
		}

		resp = request(t, client, http.MethodGet, server.URL+"/api/models", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("cookie auth status = %d", resp.StatusCode)
		}

		loginPage := request(t, client, http.MethodGet, server.URL+"/login", nil)
		loginPage.Body.Close()
		if loginPage.StatusCode != http.StatusOK || loginPage.Request.URL.Path != "/" {
			t.Fatalf("authenticated /login did not redirect: %d %s", loginPage.StatusCode, loginPage.Request.URL.Path)
		}
	})
}

func TestTokenLoginAndLogout(t *testing.T) {
	server := authTestServer(t, "top-secret")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}

	bad := request(t, client, http.MethodPost, server.URL+"/api/login",
		strings.NewReader(`{"token":"wrong"}`))
	bad.Body.Close()
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d", bad.StatusCode)
	}

	good := request(t, client, http.MethodPost, server.URL+"/api/login",
		strings.NewReader(`{"token":"top-secret"}`))
	good.Body.Close()
	if good.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d", good.StatusCode)
	}

	auth := request(t, client, http.MethodGet, server.URL+"/api/auth", nil)
	auth.Body.Close()
	if auth.StatusCode != http.StatusOK {
		t.Fatalf("auth status = %d", auth.StatusCode)
	}

	logout := request(t, client, http.MethodPost, server.URL+"/api/logout", nil)
	logout.Body.Close()
	if logout.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", logout.StatusCode)
	}

	after := request(t, client, http.MethodGet, server.URL+"/api/models", nil)
	after.Body.Close()
	if after.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d", after.StatusCode)
	}
}

func TestAuthenticationDisabled(t *testing.T) {
	server := authTestServer(t, "")

	resp := request(t, http.DefaultClient, http.MethodGet, server.URL+"/login", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/" {
		t.Fatalf("/login did not redirect: %d %s", resp.StatusCode, resp.Request.URL.Path)
	}

	var boot map[string]any
	getJSON(t, server.URL+"/api/bootstrap", &boot)
	if enabled, _ := boot["auth_enabled"].(bool); enabled {
		t.Fatal("auth_enabled should be false")
	}
}
