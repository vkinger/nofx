package mcp

import (
	"encoding/json"
	"net/http"
)

const (
	ProviderKimi       = "kimi"
	DefaultKimiBaseURL = "https://api.moonshot.ai/v1" // Global endpoint (use api.moonshot.cn for China)
	DefaultKimiModel   = "moonshot-v1-auto"
)

type KimiClient struct {
	*Client
}

// NewKimiClient creates Kimi (Moonshot) client (backward compatible)
func NewKimiClient() AIClient {
	return NewKimiClientWithOptions()
}

// NewKimiClientWithOptions creates Kimi client (supports options pattern)
func NewKimiClientWithOptions(opts ...ClientOption) AIClient {
	// 1. Create Kimi preset options
	kimiOpts := []ClientOption{
		WithProvider(ProviderKimi),
		WithModel(DefaultKimiModel),
		WithBaseURL(DefaultKimiBaseURL),
	}

	// 2. Merge user options (user options have higher priority)
	allOpts := append(kimiOpts, opts...)

	// 3. Create base client
	baseClient := NewClient(allOpts...).(*Client)

	// 4. Create Kimi client
	kimiClient := &KimiClient{
		Client: baseClient,
	}

	// 5. Set hooks to point to KimiClient (implement dynamic dispatch)
	baseClient.hooks = kimiClient

	return kimiClient
}

func (c *KimiClient) SetAPIKey(apiKey string, customURL string, customModel string) {
	c.APIKey = apiKey

	if len(apiKey) > 8 {
		c.logger.Infof("🔧 [MCP] Kimi API Key: %s...%s", apiKey[:4], apiKey[len(apiKey)-4:])
	}
	if customURL != "" {
		c.BaseURL = customURL
		c.logger.Infof("🔧 [MCP] Kimi using custom BaseURL: %s", customURL)
	} else {
		c.logger.Infof("🔧 [MCP] Kimi using default BaseURL: %s", c.BaseURL)
	}
	if customModel != "" {
		c.Model = customModel
		c.logger.Infof("🔧 [MCP] Kimi using custom Model: %s", customModel)
	} else {
		c.logger.Infof("🔧 [MCP] Kimi using default Model: %s", c.Model)
	}
}

// Kimi uses standard OpenAI-compatible API, so we just use the base client methods
func (c *KimiClient) setAuthHeader(reqHeaders http.Header) {
	c.Client.setAuthHeader(reqHeaders)
}

// buildMCPRequestBody Kimi uses OpenAI-compatible API, so JSON Schema format is the same as OpenAI
func (c *KimiClient) buildMCPRequestBody(systemPrompt, userPrompt string) map[string]any {
	// Call base implementation
	requestBody := c.Client.buildMCPRequestBody(systemPrompt, userPrompt)

	// Add JSON Schema support if model supports it and schema is provided
	if c.JSONSchema != "" && checkModelSupportsJSONSchema(c.Provider, c.Model) {
		// Parse JSON Schema string to map
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(c.JSONSchema), &schemaMap); err == nil {
			// Validate that schemaMap is not empty
			if len(schemaMap) > 0 {
				// Kimi uses OpenAI-compatible format: response_format with json_schema
				requestBody["response_format"] = map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"name":        "trading_decision",
						"schema":      schemaMap,
						"strict":      true, // Enable strict mode for guaranteed schema compliance
						"description": "Trading decision output format",
					},
				}
				c.logger.Infof("🔧 [MCP Kimi] JSON Schema enabled for structured output")
			} else {
				c.logger.Warnf("⚠️ [MCP Kimi] JSON Schema is empty after parsing, skipping response_format")
			}
		} else {
			c.logger.Warnf("⚠️ [MCP Kimi] Failed to parse JSON Schema: %v, JSON Schema content (first 200 chars): %s", err, c.JSONSchema[:min(len(c.JSONSchema), 200)])
		}
	} else {
		// Log why JSON Schema is not being used
		if c.JSONSchema == "" {
			c.logger.Debugf("🔍 [MCP Kimi] JSON Schema is empty, not using structured output")
		} else if !checkModelSupportsJSONSchema(c.Provider, c.Model) {
			c.logger.Debugf("🔍 [MCP Kimi] Model %s/%s does not support JSON Schema API", c.Provider, c.Model)
		}
	}

	return requestBody
}
