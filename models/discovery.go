package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tk103331/oni3/config"
)

const (
	maxDiscoveryBytes = 4 << 20
	discoveryTimeout  = 20 * time.Second
)

// DiscoveredModel is a provider's remote model ID and optional display name.
type DiscoveredModel struct {
	ID   string
	Name string
}

// DiscoverModels queries a provider's model-list endpoint without mutating config.
// ModelsURL paths without a leading slash append to BaseURL's path; paths with a
// leading slash replace it. Explicit absolute URLs are supported. Redirects may
// stay on the original origin only, including when custom auth headers are used.
func DiscoverModels(ctx context.Context, provider config.Provider) ([]DiscoveredModel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint, kind, err := discoveryEndpoint(provider)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create model discovery request")
	}
	req.Header.Set("Accept", "application/json")
	switch kind {
	case config.ProviderTypeAnthropic:
		req.Header.Set("anthropic-version", "2023-06-01")
		if provider.APIKey != "" {
			req.Header.Set("x-api-key", provider.APIKey)
		}
	case config.ProviderTypeGemini:
		if provider.APIKey != "" {
			req.Header.Set("x-goog-api-key", provider.APIKey)
		}
	default:
		if provider.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+provider.APIKey)
		}
	}
	for name, value := range provider.ModelHeaders {
		req.Header.Set(name, strings.ReplaceAll(value, "{{api_key}}", provider.APIKey))
	}
	client := &http.Client{
		Timeout: discoveryTimeout,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !sameOrigin(endpoint, next.URL) {
				return fmt.Errorf("model discovery redirect is not permitted")
			}
			return nil
		},
	}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// url.Error and transport/header errors can contain URLs or credentials.
		return nil, fmt.Errorf("model discovery request failed (connection or redirect)")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Never include server-provided text: many APIs echo auth details.
		return nil, fmt.Errorf("model discovery returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDiscoveryBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("cannot read model discovery response")
	}
	if len(body) > maxDiscoveryBytes {
		return nil, fmt.Errorf("model discovery response exceeds 4 MiB")
	}
	return parseDiscoveredModels(body, provider, kind)
}

func sameOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b) && b.User == nil
}

func discoveryEndpoint(provider config.Provider) (*url.URL, string, error) {
	kind := provider.EffectiveType()
	if err := config.ValidateProviderType(kind); err != nil {
		// Avoid echoing arbitrary configuration values into discovery errors.
		return nil, kind, fmt.Errorf("unsupported provider type for model discovery")
	}
	defaults := map[string]string{
		config.ProviderTypeOpenAI:    "https://api.openai.com/v1",
		config.ProviderTypeAnthropic: "https://api.anthropic.com/v1",
		config.ProviderTypeGemini:    "https://generativelanguage.googleapis.com/v1beta",
		config.ProviderTypeOllama:    "http://localhost:11434",
		config.ProviderTypeQwen:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
		config.ProviderTypeDeepSeek:  "https://api.deepseek.com",
		config.ProviderTypeArk:       "https://ark.cn-beijing.volces.com/api/v3",
		config.ProviderTypeQianfan:   "https://qianfan.baidubce.com/v2",
	}
	defaultBase := defaults[kind]
	base := provider.BaseURL
	if base == "" {
		base = defaultBase
	}
	baseURL, err := url.Parse(base)
	if err != nil || !validDiscoveryURL(baseURL) {
		return nil, kind, fmt.Errorf("model discovery requires a valid HTTP(S) base URL without user information or fragments")
	}
	reference := provider.ModelsURL
	if reference == "" {
		reference = "models"
		if kind == config.ProviderTypeOllama {
			reference = "api/tags"
		}
	}
	relative, err := url.Parse(reference)
	if err != nil {
		return nil, kind, fmt.Errorf("invalid model discovery URL")
	}
	// Treat BaseURL as a directory even when it has no trailing slash.
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/"
	if baseURL.RawPath != "" {
		baseURL.RawPath = strings.TrimRight(baseURL.RawPath, "/") + "/"
	}
	endpoint := baseURL.ResolveReference(relative)
	if !validDiscoveryURL(endpoint) {
		return nil, kind, fmt.Errorf("model discovery requires a valid HTTP(S) URL without user information or fragments")
	}
	return endpoint, kind, nil
}

func validDiscoveryURL(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func dotPath(value any, path string) (any, bool) {
	if path == "" {
		return value, true
	}
	for _, key := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func modelString(item any, paths ...string) string {
	for _, path := range paths {
		value, ok := dotPath(item, path)
		if text, isString := value.(string); ok && isString && text != "" {
			return text
		}
	}
	return ""
}

func parseDiscoveredModels(body []byte, provider config.Provider, kind string) ([]DiscoveredModel, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("model discovery returned invalid JSON")
	}
	var list any
	if provider.ModelsPath != "" {
		var ok bool
		list, ok = dotPath(root, provider.ModelsPath)
		if !ok {
			return nil, fmt.Errorf("model discovery response is missing the configured models path")
		}
	} else {
		switch value := root.(type) {
		case []any:
			list = value
		case map[string]any:
			if data, ok := value["data"]; ok {
				list = data
			} else {
				list = value["models"]
			}
		}
	}
	items, ok := list.([]any)
	if !ok {
		return nil, fmt.Errorf("model discovery response must contain a model array")
	}
	byID := make(map[string]DiscoveredModel)
	for _, item := range items {
		var id string
		if provider.ModelIDPath != "" {
			id = modelString(item, provider.ModelIDPath)
		} else if text, ok := item.(string); ok {
			id = text
		} else {
			id = modelString(item, "id", "model", "name")
		}
		if kind == config.ProviderTypeGemini && provider.ModelIDPath == "" {
			id = strings.TrimPrefix(id, "models/")
		}
		if !validDiscoveredID(id) {
			return nil, fmt.Errorf("model discovery returned an invalid or missing model ID")
		}
		name := ""
		if provider.ModelNamePath != "" {
			name = modelString(item, provider.ModelNamePath)
		} else {
			name = modelString(item, "displayName", "display_name", "name")
		}
		name = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, name))
		if name == "" {
			name = id
		}
		if _, exists := byID[id]; !exists {
			byID[id] = DiscoveredModel{ID: id, Name: name}
		}
	}
	result := make([]DiscoveredModel, 0, len(byID))
	for _, model := range byID {
		result = append(result, model)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func validDiscoveredID(id string) bool {
	if id == "" || len(id) > 1024 || !utf8.ValidString(id) || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
