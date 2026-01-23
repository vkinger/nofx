package mcp

import (
	"encoding/json"
	"net/http"
)

const (
	ProviderGemini       = "gemini"
	DefaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"
	DefaultGeminiModel   = "gemini-3-pro-preview"
)

type GeminiClient struct {
	*Client
}

// NewGeminiClient creates Gemini client (backward compatible)
func NewGeminiClient() AIClient {
	return NewGeminiClientWithOptions()
}

// NewGeminiClientWithOptions creates Gemini client (supports options pattern)
func NewGeminiClientWithOptions(opts ...ClientOption) AIClient {
	// 1. Create Gemini preset options
	geminiOpts := []ClientOption{
		WithProvider(ProviderGemini),
		WithModel(DefaultGeminiModel),
		WithBaseURL(DefaultGeminiBaseURL),
	}

	// 2. Merge user options (user options have higher priority)
	allOpts := append(geminiOpts, opts...)

	// 3. Create base client
	baseClient := NewClient(allOpts...).(*Client)

	// 4. Create Gemini client
	geminiClient := &GeminiClient{
		Client: baseClient,
	}

	// 5. Set hooks to point to GeminiClient (implement dynamic dispatch)
	baseClient.hooks = geminiClient

	return geminiClient
}

func (c *GeminiClient) SetAPIKey(apiKey string, customURL string, customModel string) {
	c.APIKey = apiKey

	if len(apiKey) > 8 {
		c.logger.Infof("🔧 [MCP] Gemini API Key: %s...%s", apiKey[:4], apiKey[len(apiKey)-4:])
	}
	if customURL != "" {
		c.BaseURL = customURL
		c.logger.Infof("🔧 [MCP] Gemini using custom BaseURL: %s", customURL)
	} else {
		c.logger.Infof("🔧 [MCP] Gemini using default BaseURL: %s", c.BaseURL)
	}
	if customModel != "" {
		c.Model = customModel
		c.logger.Infof("🔧 [MCP] Gemini using custom Model: %s", customModel)
	} else {
		c.logger.Infof("🔧 [MCP] Gemini using default Model: %s", c.Model)
	}
}

// Gemini OpenAI-compatible API uses standard Bearer auth
func (c *GeminiClient) setAuthHeader(reqHeaders http.Header) {
	c.Client.setAuthHeader(reqHeaders)
}

// buildMCPRequestBody Gemini uses OpenAI-compatible API, so JSON Schema format is the same as OpenAI
// Note: Gemini currently does not support JSON Schema, but this method is implemented for consistency
// and future compatibility. The checkModelSupportsJSONSchema check will prevent JSON Schema from
// being added if the model doesn't support it.
func (c *GeminiClient) buildMCPRequestBody(systemPrompt, userPrompt string) map[string]any {
	// Call base implementation
	requestBody := c.Client.buildMCPRequestBody(systemPrompt, userPrompt)

	// Add JSON Schema support if model supports it and schema is provided
	if c.JSONSchema != "" && checkModelSupportsJSONSchema(c.Provider, c.Model) {
		// Parse JSON Schema string to map
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(c.JSONSchema), &schemaMap); err == nil {
			// Validate that schemaMap is not empty
			if len(schemaMap) > 0 {
				// Gemini uses OpenAI-compatible format: response_format with json_schema
				requestBody["response_format"] = map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"name":        "trading_decision",
						"schema":      schemaMap,
						"strict":      true, // Enable strict mode for guaranteed schema compliance
						"description": "Trading decision output format",
					},
				}
				c.logger.Infof("🔧 [MCP Gemini] JSON Schema enabled for structured output")
			} else {
				c.logger.Warnf("⚠️ [MCP Gemini] JSON Schema is empty after parsing, skipping response_format")
			}
		} else {
			c.logger.Warnf("⚠️ [MCP Gemini] Failed to parse JSON Schema: %v, JSON Schema content (first 200 chars): %s", err, c.JSONSchema[:min(len(c.JSONSchema), 200)])
		}
	} else {
		// Log why JSON Schema is not being used
		if c.JSONSchema == "" {
			c.logger.Debugf("🔍 [MCP Gemini] JSON Schema is empty, not using structured output")
		} else if !checkModelSupportsJSONSchema(c.Provider, c.Model) {
			c.logger.Debugf("🔍 [MCP Gemini] Model %s/%s does not support JSON Schema API", c.Provider, c.Model)
		}
	}

	return requestBody
}
