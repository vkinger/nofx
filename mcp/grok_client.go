package mcp

import (
	"encoding/json"
	"net/http"
)

const (
	ProviderGrok       = "grok"
	DefaultGrokBaseURL = "https://api.x.ai/v1"
	DefaultGrokModel   = "grok-3-latest"
)

type GrokClient struct {
	*Client
}

// NewGrokClient creates Grok client (backward compatible)
func NewGrokClient() AIClient {
	return NewGrokClientWithOptions()
}

// NewGrokClientWithOptions creates Grok client (supports options pattern)
func NewGrokClientWithOptions(opts ...ClientOption) AIClient {
	// 1. Create Grok preset options
	grokOpts := []ClientOption{
		WithProvider(ProviderGrok),
		WithModel(DefaultGrokModel),
		WithBaseURL(DefaultGrokBaseURL),
	}

	// 2. Merge user options (user options have higher priority)
	allOpts := append(grokOpts, opts...)

	// 3. Create base client
	baseClient := NewClient(allOpts...).(*Client)

	// 4. Create Grok client
	grokClient := &GrokClient{
		Client: baseClient,
	}

	// 5. Set hooks to point to GrokClient (implement dynamic dispatch)
	baseClient.hooks = grokClient

	return grokClient
}

func (c *GrokClient) SetAPIKey(apiKey string, customURL string, customModel string) {
	c.APIKey = apiKey

	if len(apiKey) > 8 {
		c.logger.Infof("🔧 [MCP] Grok API Key: %s...%s", apiKey[:4], apiKey[len(apiKey)-4:])
	}
	if customURL != "" {
		c.BaseURL = customURL
		c.logger.Infof("🔧 [MCP] Grok using custom BaseURL: %s", customURL)
	} else {
		c.logger.Infof("🔧 [MCP] Grok using default BaseURL: %s", c.BaseURL)
	}
	if customModel != "" {
		c.Model = customModel
		c.logger.Infof("🔧 [MCP] Grok using custom Model: %s", customModel)
	} else {
		c.logger.Infof("🔧 [MCP] Grok using default Model: %s", c.Model)
	}
}

// Grok uses standard OpenAI-compatible API with Bearer auth
func (c *GrokClient) setAuthHeader(reqHeaders http.Header) {
	c.Client.setAuthHeader(reqHeaders)
}

// buildMCPRequestBody Grok uses OpenAI-compatible API, so JSON Schema format is the same as OpenAI
// Note: Grok currently does not support JSON Schema, but this method is implemented for consistency
// and future compatibility. The checkModelSupportsJSONSchema check will prevent JSON Schema from
// being added if the model doesn't support it.
func (c *GrokClient) buildMCPRequestBody(systemPrompt, userPrompt string) map[string]any {
	// Call base implementation
	requestBody := c.Client.buildMCPRequestBody(systemPrompt, userPrompt)

	// Add JSON Schema support if model supports it and schema is provided
	if c.JSONSchema != "" && checkModelSupportsJSONSchema(c.Provider, c.Model) {
		// Parse JSON Schema string to map
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(c.JSONSchema), &schemaMap); err == nil {
			// Validate that schemaMap is not empty
			if len(schemaMap) > 0 {
				// Grok uses OpenAI-compatible format: response_format with json_schema
				requestBody["response_format"] = map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"name":        "trading_decision",
						"schema":      schemaMap,
						"strict":      true, // Enable strict mode for guaranteed schema compliance
						"description": "Trading decision output format",
					},
				}
				c.logger.Infof("🔧 [MCP Grok] JSON Schema enabled for structured output")
			} else {
				c.logger.Warnf("⚠️ [MCP Grok] JSON Schema is empty after parsing, skipping response_format")
			}
		} else {
			c.logger.Warnf("⚠️ [MCP Grok] Failed to parse JSON Schema: %v, JSON Schema content (first 200 chars): %s", err, c.JSONSchema[:min(len(c.JSONSchema), 200)])
		}
	} else {
		// Log why JSON Schema is not being used
		if c.JSONSchema == "" {
			c.logger.Debugf("🔍 [MCP Grok] JSON Schema is empty, not using structured output")
		} else if !checkModelSupportsJSONSchema(c.Provider, c.Model) {
			c.logger.Debugf("🔍 [MCP Grok] Model %s/%s does not support JSON Schema API", c.Provider, c.Model)
		}
	}

	return requestBody
}
