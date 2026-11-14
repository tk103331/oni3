package tools

import (
	"context"

	"github.com/cloudwego/eino-ext/components/tool/googlesearch"
	"github.com/cloudwego/eino/components/tool"
	"github.com/tk103331/oni3/config"
)

// NewGoogleSearchTool creates Google search tool
func NewGoogleSearchTool(name string, cfg config.Tool) (tool.InvokableTool, error) {
	ctx := context.Background()

	// Create Google search configuration
	googleConfig := &googlesearch.Config{
		ToolName: name,
		ToolDesc: cfg.Description,
		Num:      5,       // Default return 5 results
		Lang:     "zh-CN", // Default Chinese
	}

	// Read required and optional parameters from configuration
	if apiKey, exists := cfg.Config["api_key"]; exists {
		googleConfig.APIKey = apiKey.String()
	}
	if searchEngineID, exists := cfg.Config["search_engine_id"]; exists {
		googleConfig.SearchEngineID = searchEngineID.String()
	}
	if baseURL, exists := cfg.Config["base_url"]; exists {
		googleConfig.BaseURL = baseURL.String()
	}
	if maxResults, exists := cfg.Config["max_results"]; exists {
		if val := maxResults.Int(); val > 0 {
			googleConfig.Num = val
		}
	}
	if language, exists := cfg.Config["language"]; exists {
		googleConfig.Lang = language.String()
	}

	// Create tool instance
	return googlesearch.NewTool(ctx, googleConfig)
}
