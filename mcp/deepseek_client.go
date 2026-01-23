package mcp

import (
	"encoding/json"
	"net/http"
)

const (
	ProviderDeepSeek       = "deepseek"
	DefaultDeepSeekBaseURL = "https://api.deepseek.com"
	DefaultDeepSeekModel   = "deepseek-chat"
)

type DeepSeekClient struct {
	*Client
}

// NewDeepSeekClient creates DeepSeek client (backward compatible)
//
// Deprecated: Recommend using NewDeepSeekClientWithOptions for better flexibility
func NewDeepSeekClient() AIClient {
	return NewDeepSeekClientWithOptions()
}

// NewDeepSeekClientWithOptions creates DeepSeek client (supports options pattern)
//
// Usage examples:
//   // Basic usage
//   client := mcp.NewDeepSeekClientWithOptions()
//
//   // Custom configuration
//   client := mcp.NewDeepSeekClientWithOptions(
//       mcp.WithAPIKey("sk-xxx"),
//       mcp.WithLogger(customLogger),
//       mcp.WithTimeout(60*time.Second),
//   )
func NewDeepSeekClientWithOptions(opts ...ClientOption) AIClient {
	// 1. Create DeepSeek preset options
	deepseekOpts := []ClientOption{
		WithProvider(ProviderDeepSeek),
		WithModel(DefaultDeepSeekModel),
		WithBaseURL(DefaultDeepSeekBaseURL),
	}

	// 2. Merge user options (user options have higher priority)
	allOpts := append(deepseekOpts, opts...)

	// 3. Create base client
	baseClient := NewClient(allOpts...).(*Client)

	// 4. Create DeepSeek client
	dsClient := &DeepSeekClient{
		Client: baseClient,
	}

	// 5. Set hooks to point to DeepSeekClient (implement dynamic dispatch)
	baseClient.hooks = dsClient

	return dsClient
}

func (dsClient *DeepSeekClient) SetAPIKey(apiKey string, customURL string, customModel string) {
	dsClient.APIKey = apiKey

	if len(apiKey) > 8 {
		dsClient.logger.Infof("🔧 [MCP] DeepSeek API Key: %s...%s", apiKey[:4], apiKey[len(apiKey)-4:])
	}
	if customURL != "" {
		dsClient.BaseURL = customURL
		dsClient.logger.Infof("🔧 [MCP] DeepSeek using custom BaseURL: %s", customURL)
	} else {
		dsClient.logger.Infof("🔧 [MCP] DeepSeek using default BaseURL: %s", dsClient.BaseURL)
	}
	if customModel != "" {
		dsClient.Model = customModel
		dsClient.logger.Infof("🔧 [MCP] DeepSeek using custom Model: %s", customModel)
	} else {
		dsClient.logger.Infof("🔧 [MCP] DeepSeek using default Model: %s", dsClient.Model)
	}
}

func (dsClient *DeepSeekClient) setAuthHeader(reqHeaders http.Header) {
	dsClient.Client.setAuthHeader(reqHeaders)
}

// buildMCPRequestBody DeepSeek uses OpenAI-compatible API, so JSON Schema format is the same as OpenAI
func (c *DeepSeekClient) buildMCPRequestBody(systemPrompt, userPrompt string) map[string]any {
	// Call base implementation
	requestBody := c.Client.buildMCPRequestBody(systemPrompt, userPrompt)

	// Add JSON Schema support if model supports it and schema is provided
	if c.JSONSchema != "" && checkModelSupportsJSONSchema(c.Provider, c.Model) {
		// Parse JSON Schema string to map
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(c.JSONSchema), &schemaMap); err == nil {
			// Validate that schemaMap is not empty
			if len(schemaMap) > 0 {
				// DeepSeek uses OpenAI-compatible format: response_format with json_schema
				requestBody["response_format"] = map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"name":        "trading_decision",
						"schema":      schemaMap,
						"strict":      true, // Enable strict mode for guaranteed schema compliance
						"description": "Trading decision output format",
					},
				}
				c.logger.Infof("🔧 [MCP DeepSeek] JSON Schema enabled for structured output")
			} else {
				c.logger.Warnf("⚠️ [MCP DeepSeek] JSON Schema is empty after parsing, skipping response_format")
			}
		} else {
			c.logger.Warnf("⚠️ [MCP DeepSeek] Failed to parse JSON Schema: %v, JSON Schema content (first 200 chars): %s", err, c.JSONSchema[:min(len(c.JSONSchema), 200)])
		}
	} else {
		// Log why JSON Schema is not being used
		if c.JSONSchema == "" {
			c.logger.Debugf("🔍 [MCP DeepSeek] JSON Schema is empty, not using structured output")
		} else if !checkModelSupportsJSONSchema(c.Provider, c.Model) {
			c.logger.Debugf("🔍 [MCP DeepSeek] Model %s/%s does not support JSON Schema API", c.Provider, c.Model)
		}
	}

	return requestBody
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
