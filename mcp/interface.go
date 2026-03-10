package mcp

import (
	"net/http"
	"time"
)

// AIClient public AI client interface (for external use)
type AIClient interface {
	SetAPIKey(apiKey string, customURL string, customModel string)
	SetTimeout(timeout time.Duration)
	SetMaxTokens(tokens int)       // Set max completion tokens (e.g. 4096 for analyst so reasoning + JSON fit)
	SetTemperature(temp float64)   // Set sampling temperature (0-2); lower for deterministic (compliance), higher for diversity (analyst)
	SetTopP(p float64)             // Set top_p nucleus sampling (0-1)
	SetPresencePenalty(p float64)  // Set presence penalty (-2 to 2)
	SetFrequencyPenalty(p float64)  // Set frequency penalty (-2 to 2)
	CallWithMessages(systemPrompt, userPrompt string) (string, error)
	CallWithRequest(req *Request) (string, error) // Builder pattern API (supports advanced features)
	SetJSONSchema(jsonSchema string)              // Set JSON Schema for structured output (if model supports it)
}

// clientHooks internal hook interface (for subclass to override specific steps)
// These methods are only used inside the package to implement dynamic dispatch
type clientHooks interface {
	// Hook methods that can be overridden by subclass

	call(systemPrompt, userPrompt string) (string, error)

	buildMCPRequestBody(systemPrompt, userPrompt string) map[string]any
	buildUrl() string
	buildRequest(url string, jsonData []byte) (*http.Request, error)
	setAuthHeader(reqHeaders http.Header)
	marshalRequestBody(requestBody map[string]any) ([]byte, error)
	parseMCPResponse(body []byte) (string, error)
	isRetryableError(err error) bool
}
