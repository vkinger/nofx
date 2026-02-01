package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ProviderCustom = "custom"

	MCPClientTemperature = 0.5
)

var (
	DefaultTimeout = 240 * time.Second

	MaxRetryTimes = 10

	retryableErrors = []string{
		"EOF",
		"timeout",
		"connection reset",
		"connection refused",
		"temporary failure",
		"no such host",
		"stream error",   // HTTP/2 stream error
		"INTERNAL_ERROR", // Server internal error

		"stream error",
		"429",
		// ⭐ 配额 / Free tier（关键）
		"AllocationQuota",
		"FreeTier",
		"quota",
		"exhausted",
	}

	// TokenUsageCallback is called after each AI request with token usage info
	TokenUsageCallback func(usage TokenUsage)

	// JSONSchemaChecker is a callback function to check if a model supports JSON Schema
	// This allows external packages (like kernel) to provide the implementation
	// without creating circular dependencies
	// If not set, falls back to a simple default implementation
	JSONSchemaChecker func(provider, modelName string) bool
)

// checkModelSupportsJSONSchema 检查模型是否支持JSON Schema（API级别）
// 优先使用外部设置的回调函数（来自kernel包），如果没有设置则使用默认实现
func checkModelSupportsJSONSchema(provider, modelName string) bool {
	// 如果设置了外部回调函数，使用它（来自kernel包的完整实现）
	if JSONSchemaChecker != nil {
		return JSONSchemaChecker(provider, modelName)
	}

	// 默认实现（简化版，作为fallback）
	if provider == "" || modelName == "" {
		return false
	}

	providerLower := strings.ToLower(provider)
	modelNameLower := strings.ToLower(modelName)

	// OpenAI 模型检查
	if strings.Contains(providerLower, "openai") {
		// GPT-4o 系列
		if strings.Contains(modelNameLower, "gpt-4o") {
			return true
		}
		// GPT-4-turbo 系列
		if strings.Contains(modelNameLower, "gpt-4-turbo") {
			return true
		}
		// GPT-4o-mini
		if strings.Contains(modelNameLower, "gpt-4o-mini") {
			return true
		}
		// o1 系列
		if strings.Contains(modelNameLower, "o1") {
			return true
		}
		// o3 系列
		if strings.Contains(modelNameLower, "o3") {
			return true
		}
		// GPT-4 系列（2024年后版本）
		if strings.Contains(modelNameLower, "gpt-4") {
			// 排除旧版本
			if strings.Contains(modelNameLower, "gpt-4-0314") {
				return false
			}
			// 检查是否包含2024或2025
			if strings.Contains(modelNameLower, "2024") || strings.Contains(modelNameLower, "2025") {
				return true
			}
			// 其他 GPT-4 变体（假设支持）
			return true
		}
		// GPT-5 系列
		if strings.Contains(modelNameLower, "gpt-5") {
			return true
		}
	}

	// Claude 模型检查
	if strings.Contains(providerLower, "claude") {
		// Claude 3.x 系列不支持
		if strings.Contains(modelNameLower, "claude-3") {
			return false
		}
		// Claude 4.x 系列支持
		if strings.Contains(modelNameLower, "claude-4") || strings.Contains(modelNameLower, "claude-opus-4") || strings.Contains(modelNameLower, "claude-sonnet-4") {
			// 排除 Haiku
			if strings.Contains(modelNameLower, "haiku") {
				return false
			}
			return true
		}
		// Opus 4.x 或 Sonnet 4.x
		if strings.Contains(modelNameLower, "opus-4") || strings.Contains(modelNameLower, "sonnet-4") {
			if strings.Contains(modelNameLower, "haiku") {
				return false
			}
			return true
		}
	}

	return false
}

// TokenUsage represents token usage from AI API response
type TokenUsage struct {
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Client AI API configuration
type Client struct {
	Provider   string
	APIKey     string
	BaseURL    string
	Model      string
	UseFullURL bool   // Whether to use full URL (without appending /chat/completions)
	MaxTokens  int    // Maximum tokens for AI response
	JSONSchema string // Optional JSON Schema for structured output (if model supports it)

	httpClient *http.Client
	logger     Logger  // Logger (replaceable)
	config     *Config // Config object (stores all configurations)

	// hooks are used to implement dynamic dispatch (polymorphism)
	// When DeepSeekClient embeds Client, hooks point to DeepSeekClient
	// This way methods called in call() are automatically dispatched to the overridden version in subclass
	hooks clientHooks
}

// New creates default client (backward compatible)
//
// Deprecated: Recommend using NewClient(...opts) for better flexibility
func New() AIClient {
	return NewClient()
}

// NewClient creates client (supports options pattern)
//
// Usage examples:
//
//	// Basic usage (backward compatible)
//	client := mcp.NewClient()
//
//	// Custom logger
//	client := mcp.NewClient(mcp.WithLogger(customLogger))
//
//	// Custom timeout
//	client := mcp.NewClient(mcp.WithTimeout(60*time.Second))
//
//	// Combine multiple options
//	client := mcp.NewClient(
//	    mcp.WithDeepSeekConfig("sk-xxx"),
//	    mcp.WithLogger(customLogger),
//	    mcp.WithTimeout(60*time.Second),
//	)
func NewClient(opts ...ClientOption) AIClient {
	// 1. Create default config
	cfg := DefaultConfig()

	// 2. Apply user options
	for _, opt := range opts {
		opt(cfg)
	}

	// 3. Create client instance
	client := &Client{
		Provider:   cfg.Provider,
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		Model:      cfg.Model,
		MaxTokens:  cfg.MaxTokens,
		UseFullURL: cfg.UseFullURL,
		httpClient: cfg.HTTPClient,
		logger:     cfg.Logger,
		config:     cfg,
	}

	// 4. Set default Provider (if not set)
	if client.Provider == "" {
		client.Provider = ProviderDeepSeek
		client.BaseURL = DefaultDeepSeekBaseURL
		client.Model = DefaultDeepSeekModel
	}

	// 5. Set hooks to point to self
	client.hooks = client

	return client
}

// SetCustomAPI sets custom OpenAI-compatible API
func (client *Client) SetAPIKey(apiKey, apiURL, customModel string) {
	client.Provider = ProviderCustom
	client.APIKey = apiKey

	// Check if URL ends with #, if so use full URL (without appending /chat/completions)
	if strings.HasSuffix(apiURL, "#") {
		client.BaseURL = strings.TrimSuffix(apiURL, "#")
		client.UseFullURL = true
	} else {
		client.BaseURL = apiURL
		client.UseFullURL = false
	}

	client.Model = customModel
}

func (client *Client) SetTimeout(timeout time.Duration) {
	client.httpClient.Timeout = timeout
}

// SetJSONSchema sets JSON Schema for structured output (if model supports it)
func (client *Client) SetJSONSchema(jsonSchema string) {
	client.JSONSchema = jsonSchema
	if jsonSchema != "" {
		client.logger.Infof("🔧 [MCP] JSON Schema set for structured output")
	}
}

// CallWithMessages template method - fixed retry flow (cannot be overridden)
func (client *Client) CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	if client.APIKey == "" {
		return "", fmt.Errorf("AI API key not set, please call SetAPIKey first")
	}

	// Fixed retry flow
	var lastErr error
	maxRetries := client.config.MaxRetries
	modelList := strings.Split(client.Model, ",")
	client.logger.Infof("✓ AI API candidate models: %v", modelList)
	originalModel := client.Model
	defer func() { client.Model = originalModel }()
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// model failover
		selectedModel := (attempt - 1) % len(modelList)
		client.Model = modelList[selectedModel]
		if attempt == 1 {
			client.logger.Infof("✓ AI API calling model[%d]: %s", selectedModel, client.Model)
		} else {
			client.logger.Warnf(
				"⚠️ AI API call failed, retrying (%d/%d) with model[%d]: %s",
				attempt, maxRetries, selectedModel, client.Model,
			)
		}
		// Call the fixed single-call flow
		result, err := client.hooks.call(systemPrompt, userPrompt)
		client.logger.Infof("✓ AI API calling response: %s", result)

		if err == nil {
			if attempt > 1 {
				client.logger.Infof("✓ AI API retry succeeded")
			}
			return result, nil
		}
		client.logger.Warnf(
			"⚠️ AI API call retry check: attempt=%d, err=%v, retryable=%v",
			attempt, err, client.hooks.isRetryableError(err),
		)

		lastErr = err
		// Check if error is retryable via hooks (supports custom retry strategy in subclass)
		if !client.hooks.isRetryableError(err) {
			return "", err
		}

		// Wait before retry
		if attempt < maxRetries {
			waitTime := client.config.RetryWaitBase * time.Duration(attempt)
			client.logger.Infof("⏳ Waiting %v before retry...", waitTime)
			time.Sleep(waitTime)
		}
	}

	return "", fmt.Errorf("still failed after %d retries: %w", maxRetries, lastErr)
}

func (client *Client) setAuthHeader(reqHeader http.Header) {
	reqHeader.Set("Authorization", fmt.Sprintf("Bearer %s", client.APIKey))
}

// estimateTokenCount 估算文本的token数量（mcp包内使用）
// 使用与decision包相同的算法，确保估算一致性
func estimateTokenCount(text string) int {
	if len(text) == 0 {
		return 0
	}

	// 计算中文字符数量
	chineseCharCount := 0
	totalRunes := utf8.RuneCountInString(text)

	for _, r := range text {
		// 中文字符Unicode范围：0x4E00-0x9FFF
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseCharCount++
		}
	}

	// 估算token数（与decision包保持一致）
	// 中文字符：1.5字符/1token，非中文字符：4字符/1token
	nonChineseCount := totalRunes - chineseCharCount
	estimatedTokens := int(float64(chineseCharCount)/1.5 + float64(nonChineseCount)/4.0)

	// 至少返回总字符数的1/3（保守估算）
	minTokens := totalRunes / 3
	if estimatedTokens < minTokens {
		estimatedTokens = minTokens
	}

	return estimatedTokens
}

func (client *Client) buildMCPRequestBody(systemPrompt, userPrompt string) map[string]any {
	// Build messages array
	messages := []map[string]string{}

	// If system prompt exists, add system message
	if systemPrompt != "" {
		messages = append(messages, map[string]string{
			"role":    "system",
			"content": systemPrompt,
		})
	}
	// Add user message
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": userPrompt,
	})

	// Build request body
	requestBody := map[string]interface{}{
		"model":       client.Model,
		"messages":    messages,
		"temperature": client.config.Temperature, // Use configured temperature
	}
	// OpenAI newer models use max_completion_tokens instead of max_tokens
	if client.Provider == ProviderOpenAI {
		requestBody["max_completion_tokens"] = client.MaxTokens
	} else {
		requestBody["max_tokens"] = client.MaxTokens
	}

	// Note: JSON Schema support is handled by specific client implementations (OpenAI/Claude)
	// They override buildMCPRequestBody to add response_format/output_format parameters

	return requestBody
}

// can be used to marshal the request body and can be overridden
func (client *Client) marshalRequestBody(requestBody map[string]any) ([]byte, error) {
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}
	return jsonData, nil
}

func (client *Client) parseMCPResponse(body []byte) (string, error) {
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("API returned empty response")
	}

	// Report token usage if callback is set
	if TokenUsageCallback != nil && result.Usage.TotalTokens > 0 {
		TokenUsageCallback(TokenUsage{
			Provider:         client.Provider,
			Model:            client.Model,
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		})
	}

	return result.Choices[0].Message.Content, nil
}

func (client *Client) buildUrl() string {
	if client.UseFullURL {
		return client.BaseURL
	}
	return fmt.Sprintf("%s/chat/completions", client.BaseURL)
}

func (client *Client) buildRequest(url string, jsonData []byte) (*http.Request, error) {
	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("fail to build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Set auth header via hooks (supports overriding in subclass)
	client.hooks.setAuthHeader(req.Header)

	return req, nil
}

// call single AI API call (fixed flow, cannot be overridden)
func (client *Client) call(systemPrompt, userPrompt string) (string, error) {
	// Print current AI configuration
	client.logger.Infof("📡 [%s] Request AI Server: BaseURL: %s", client.String(), client.BaseURL)
	client.logger.Debugf("[%s] UseFullURL: %v", client.String(), client.UseFullURL)
	if len(client.APIKey) > 8 {
		client.logger.Debugf("[%s]   API Key: %s...%s", client.String(), client.APIKey[:4], client.APIKey[len(client.APIKey)-4:])
	}

	// Estimate token count before sending request
	systemTokens := estimateTokenCount(systemPrompt)
	userTokens := estimateTokenCount(userPrompt)
	totalTokens := systemTokens + userTokens
	client.logger.Infof("📊 [MCP %s] Estimated input tokens: ~%d (system: ~%d, user: ~%d)",
		client.String(), totalTokens, systemTokens, userTokens)

	// Step 1: Build request body (via hooks for dynamic dispatch)
	requestBody := client.hooks.buildMCPRequestBody(systemPrompt, userPrompt)

	// Log max_tokens configuration
	tokenKey := "max_tokens"
	if client.Provider == ProviderOpenAI {
		tokenKey = "max_completion_tokens"
	}
	if maxTokensVal, ok := requestBody[tokenKey]; ok {
		client.logger.Infof("📊 [MCP %s] Max output tokens: %d", client.String(), maxTokensVal)
	}

	// Step 2: Serialize request body (via hooks for dynamic dispatch)
	jsonData, err := client.hooks.marshalRequestBody(requestBody)
	if err != nil {
		return "", err
	}

	// Step 3: Build URL (via hooks for dynamic dispatch)
	url := client.hooks.buildUrl()
	client.logger.Infof("📡 [MCP %s] Request URL: %s", client.String(), url)

	// Step 4: Create HTTP request (fixed logic)
	req, err := client.hooks.buildRequest(url, jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Step 5: Send HTTP request (fixed logic)
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Step 6: Read response body (fixed logic)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Step 7: Check HTTP status code (fixed logic)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned error (status %d): %s", resp.StatusCode, string(body))
	}

	// Step 8: Parse response (via hooks for dynamic dispatch)
	result, err := client.hooks.parseMCPResponse(body)
	if err != nil {
		return "", fmt.Errorf("fail to parse AI server response: %w", err)
	}

	return result, nil
}

func (client *Client) String() string {
	return fmt.Sprintf("[Provider: %s, Model: %s]",
		client.Provider, client.Model)
}

// isRetryableError determines if error is retryable (network errors, timeouts, etc.)
func (client *Client) isRetryableError(err error) bool {
	errStr := err.Error()
	// Network errors, timeouts, EOF, etc. can be retried
	for _, retryable := range client.config.RetryableErrors {
		if strings.Contains(errStr, retryable) {
			return true
		}
	}
	return false
}

// ============================================================
// Builder Pattern API (Advanced Features)
// ============================================================

// CallWithRequest calls AI API using Request object (supports advanced features)
//
// This method supports:
// - Multi-turn conversation history
// - Fine-grained parameter control (temperature, top_p, penalties, etc.)
// - Function Calling / Tools
// - Streaming response (future support)
//
// Usage example:
//
//	request := NewRequestBuilder().
//	    WithSystemPrompt("You are helpful").
//	    WithUserPrompt("Hello").
//	    WithTemperature(0.8).
//	    Build()
//	result, err := client.CallWithRequest(request)
func (client *Client) CallWithRequest(req *Request) (string, error) {
	if client.APIKey == "" {
		return "", fmt.Errorf("AI API key not set, please call SetAPIKey first")
	}

	// If Model is not set in Request, use Client's Model
	if req.Model == "" {
		req.Model = client.Model
	}

	// Fixed retry flow
	var lastErr error
	maxRetries := client.config.MaxRetries

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			client.logger.Warnf("⚠️  AI API call failed, retrying (%d/%d)...", attempt, maxRetries)
		}

		// Call single request
		result, err := client.callWithRequest(req)
		if err == nil {
			if attempt > 1 {
				client.logger.Infof("✓ AI API retry succeeded")
			}
			return result, nil
		}

		lastErr = err
		// Check if error is retryable
		if !client.hooks.isRetryableError(err) {
			return "", err
		}

		// Wait before retry
		if attempt < maxRetries {
			waitTime := client.config.RetryWaitBase * time.Duration(attempt)
			client.logger.Infof("⏳ Waiting %v before retry...", waitTime)
			time.Sleep(waitTime)
		}
	}

	return "", fmt.Errorf("still failed after %d retries: %w", maxRetries, lastErr)
}

// callWithRequest single AI API call (using Request object)
func (client *Client) callWithRequest(req *Request) (string, error) {
	// Print current AI configuration
	client.logger.Infof("📡 [%s] Request AI Server with Builder: BaseURL: %s", client.String(), client.BaseURL)
	client.logger.Debugf("[%s] Messages count: %d", client.String(), len(req.Messages))

	// Build request body (from Request object)
	requestBody := client.buildRequestBodyFromRequest(req)

	// Serialize request body
	jsonData, err := client.hooks.marshalRequestBody(requestBody)
	if err != nil {
		return "", err
	}

	// Build URL
	url := client.hooks.buildUrl()
	client.logger.Infof("📡 [MCP %s] Request URL: %s", client.String(), url)

	// Create HTTP request
	httpReq, err := client.hooks.buildRequest(url, jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Send HTTP request
	resp, err := client.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	result, err := client.hooks.parseMCPResponse(body)
	if err != nil {
		return "", fmt.Errorf("fail to parse AI server response: %w", err)
	}

	return result, nil
}

// buildRequestBodyFromRequest builds request body from Request object
func (client *Client) buildRequestBodyFromRequest(req *Request) map[string]any {
	// Convert Message to API format
	messages := make([]map[string]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	// Build basic request body
	requestBody := map[string]interface{}{
		"model":    req.Model,
		"messages": messages,
	}

	// Add optional parameters (only add non-nil parameters)
	if req.Temperature != nil {
		requestBody["temperature"] = *req.Temperature
	} else {
		// If not set in Request, use Client's configuration
		requestBody["temperature"] = client.config.Temperature
	}

	// OpenAI newer models use max_completion_tokens instead of max_tokens
	tokenKey := "max_tokens"
	if client.Provider == ProviderOpenAI {
		tokenKey = "max_completion_tokens"
	}
	if req.MaxTokens != nil {
		requestBody[tokenKey] = *req.MaxTokens
	} else {
		// If not set in Request, use Client's MaxTokens
		requestBody[tokenKey] = client.MaxTokens
	}

	if req.TopP != nil {
		requestBody["top_p"] = *req.TopP
	}

	if req.FrequencyPenalty != nil {
		requestBody["frequency_penalty"] = *req.FrequencyPenalty
	}

	if req.PresencePenalty != nil {
		requestBody["presence_penalty"] = *req.PresencePenalty
	}

	if len(req.Stop) > 0 {
		requestBody["stop"] = req.Stop
	}

	if len(req.Tools) > 0 {
		requestBody["tools"] = req.Tools
	}

	if req.ToolChoice != "" {
		requestBody["tool_choice"] = req.ToolChoice
	}

	if req.Stream {
		requestBody["stream"] = true
	}

	return requestBody
}
