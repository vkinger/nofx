package mcp

import (
	"encoding/json"
	"net/http"
)

const (
	ProviderQwen       = "qwen"
	DefaultQwenBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	DefaultQwenModel   = "qwen3-max"
)

type QwenClient struct {
	*Client
}

// NewQwenClient creates Qwen client (backward compatible)
//
// Deprecated: Recommend using NewQwenClientWithOptions for better flexibility
func NewQwenClient() AIClient {
	return NewQwenClientWithOptions()
}

// NewQwenClientWithOptions creates Qwen client (supports options pattern)
//
// Usage examples:
//
//	// Basic usage
//	client := mcp.NewQwenClientWithOptions()
//
//	// Custom configuration
//	client := mcp.NewQwenClientWithOptions(
//	    mcp.WithAPIKey("sk-xxx"),
//	    mcp.WithLogger(customLogger),
//	    mcp.WithTimeout(60*time.Second),
//	)
func NewQwenClientWithOptions(opts ...ClientOption) AIClient {
	// 1. Create Qwen preset options
	qwenOpts := []ClientOption{
		WithProvider(ProviderQwen),
		WithModel(DefaultQwenModel),
		WithBaseURL(DefaultQwenBaseURL),
	}

	// 2. Merge user options (user options have higher priority)
	allOpts := append(qwenOpts, opts...)

	// 3. Create base client
	baseClient := NewClient(allOpts...).(*Client)

	// 4. Create Qwen client
	qwenClient := &QwenClient{
		Client: baseClient,
	}

	// 5. Set hooks to point to QwenClient (implement dynamic dispatch)
	baseClient.hooks = qwenClient

	return qwenClient
}

func (qwenClient *QwenClient) SetAPIKey(apiKey string, customURL string, customModel string) {
	qwenClient.APIKey = apiKey

	if len(apiKey) > 8 {
		qwenClient.logger.Infof("🔧 [MCP] Qwen API Key: %s...%s", apiKey[:4], apiKey[len(apiKey)-4:])
	}
	if customURL != "" {
		qwenClient.BaseURL = customURL
		qwenClient.logger.Infof("🔧 [MCP] Qwen using custom BaseURL: %s", customURL)
	} else {
		qwenClient.logger.Infof("🔧 [MCP] Qwen using default BaseURL: %s", qwenClient.BaseURL)
	}
	if customModel != "" {
		qwenClient.Model = customModel
		qwenClient.logger.Infof("🔧 [MCP] Qwen using custom Model: %s", customModel)
	} else {
		qwenClient.logger.Infof("🔧 [MCP] Qwen using default Model: %s", qwenClient.Model)
	}
}

func (qwenClient *QwenClient) setAuthHeader(reqHeaders http.Header) {
	qwenClient.Client.setAuthHeader(reqHeaders)
}

// buildMCPRequestBody Qwen uses OpenAI-compatible API, so JSON Schema format is the same as OpenAI
func (c *QwenClient) buildMCPRequestBody(systemPrompt, userPrompt string) map[string]any {
	// Call base implementation
	requestBody := c.Client.buildMCPRequestBody(systemPrompt, userPrompt)

	// Add JSON Schema support if model supports it and schema is provided
	if c.JSONSchema != "" && checkModelSupportsJSONSchema(c.Provider, c.Model) {
		// Parse JSON Schema string to map
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(c.JSONSchema), &schemaMap); err == nil {
			// Validate that schemaMap is not empty
			if len(schemaMap) > 0 {
				// Qwen uses OpenAI-compatible format: response_format with json_schema
				// Note: Qwen only supports basic JSON Schema, so we don't include "strict" parameter
				requestBody["response_format"] = map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"name":        "trading_decision",
						"schema":      schemaMap,
						"description": "Trading decision output format",
					},
				}
				c.logger.Infof("🔧 [MCP Qwen] JSON Schema enabled for structured output (basic mode, no strict)")
			} else {
				c.logger.Warnf("⚠️ [MCP Qwen] JSON Schema is empty after parsing, skipping response_format")
			}
		} else {
			c.logger.Warnf("⚠️ [MCP Qwen] Failed to parse JSON Schema: %v, JSON Schema content (first 200 chars): %s", err, c.JSONSchema[:min(len(c.JSONSchema), 200)])
		}
	} else {
		// Log why JSON Schema is not being used
		if c.JSONSchema == "" {
			c.logger.Debugf("🔍 [MCP Qwen] JSON Schema is empty, not using structured output")
		} else if !checkModelSupportsJSONSchema(c.Provider, c.Model) {
			c.logger.Debugf("🔍 [MCP Qwen] Model %s/%s does not support JSON Schema API", c.Provider, c.Model)
		}
	}

	return requestBody
}
