package mcp

import (
	"encoding/json"
	"net/http"
)

const (
	ProviderOpenAI       = "openai"
	DefaultOpenAIBaseURL = "https://api.openai.com/v1"
	DefaultOpenAIModel   = "gpt-5.2"
)

type OpenAIClient struct {
	*Client
}

// NewOpenAIClient creates OpenAI client (backward compatible)
func NewOpenAIClient() AIClient {
	return NewOpenAIClientWithOptions()
}

// NewOpenAIClientWithOptions creates OpenAI client (supports options pattern)
func NewOpenAIClientWithOptions(opts ...ClientOption) AIClient {
	// 1. Create OpenAI preset options
	openaiOpts := []ClientOption{
		WithProvider(ProviderOpenAI),
		WithModel(DefaultOpenAIModel),
		WithBaseURL(DefaultOpenAIBaseURL),
	}

	// 2. Merge user options (user options have higher priority)
	allOpts := append(openaiOpts, opts...)

	// 3. Create base client
	baseClient := NewClient(allOpts...).(*Client)

	// 4. Create OpenAI client
	openaiClient := &OpenAIClient{
		Client: baseClient,
	}

	// 5. Set hooks to point to OpenAIClient (implement dynamic dispatch)
	baseClient.hooks = openaiClient

	return openaiClient
}

func (c *OpenAIClient) SetAPIKey(apiKey string, customURL string, customModel string) {
	c.APIKey = apiKey

	if len(apiKey) > 8 {
		c.logger.Infof("🔧 [MCP] OpenAI API Key: %s...%s", apiKey[:4], apiKey[len(apiKey)-4:])
	}
	if customURL != "" {
		c.BaseURL = customURL
		c.logger.Infof("🔧 [MCP] OpenAI using custom BaseURL: %s", customURL)
	} else {
		c.logger.Infof("🔧 [MCP] OpenAI using default BaseURL: %s", c.BaseURL)
	}
	if customModel != "" {
		c.Model = customModel
		c.logger.Infof("🔧 [MCP] OpenAI using custom Model: %s", customModel)
	} else {
		c.logger.Infof("🔧 [MCP] OpenAI using default Model: %s", c.Model)
	}
}

// OpenAI uses standard Bearer auth
func (c *OpenAIClient) setAuthHeader(reqHeaders http.Header) {
	c.Client.setAuthHeader(reqHeaders)
}

// buildMCPRequestBody OpenAI-specific request body with JSON Schema support
func (c *OpenAIClient) buildMCPRequestBody(systemPrompt, userPrompt string) map[string]any {
	// Call base implementation
	requestBody := c.Client.buildMCPRequestBody(systemPrompt, userPrompt)

	// Add JSON Schema support if model supports it and schema is provided
	if c.JSONSchema != "" && checkModelSupportsJSONSchema(c.Provider, c.Model) {
		// Parse JSON Schema string to map
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(c.JSONSchema), &schemaMap); err == nil {
			// Validate that schemaMap is not empty
			if len(schemaMap) > 0 {
				// OpenAI format: response_format with json_schema
				requestBody["response_format"] = map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"name":        "trading_decision",
						"schema":      schemaMap,
						"strict":      true, // Enable strict mode for guaranteed schema compliance
						"description": "Trading decision output format",
					},
				}
				c.logger.Infof("🔧 [MCP OpenAI] JSON Schema enabled for structured output")
			} else {
				c.logger.Warnf("⚠️ [MCP OpenAI] JSON Schema is empty after parsing, skipping response_format")
			}
		} else {
			c.logger.Warnf("⚠️ [MCP OpenAI] Failed to parse JSON Schema: %v, JSON Schema content (first 200 chars): %s", err, c.JSONSchema[:min(len(c.JSONSchema), 200)])
		}
	} else {
		// Log why JSON Schema is not being used
		if c.JSONSchema == "" {
			c.logger.Debugf("🔍 [MCP OpenAI] JSON Schema is empty, not using structured output")
		} else if !checkModelSupportsJSONSchema(c.Provider, c.Model) {
			c.logger.Debugf("🔍 [MCP OpenAI] Model %s/%s does not support JSON Schema API", c.Provider, c.Model)
		}
	}

	return requestBody
}
