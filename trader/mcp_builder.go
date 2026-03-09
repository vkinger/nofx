// Package trader: 根据 store.AIModel 构建 mcp.AIClient，供多 Agent 独立模型配置使用
package trader

import (
	"nofx/mcp"
	"nofx/store"
)

// BuildMCPClientFromAIModel 根据 AI 模型配置构建 MCP 客户端；用于分析师/风控官等独立模型
func BuildMCPClientFromAIModel(aiModel *store.AIModel) mcp.AIClient {
	if aiModel == nil {
		return nil
	}
	apiKey := string(aiModel.APIKey)
	url := aiModel.CustomAPIURL
	model := aiModel.CustomModelName
	provider := aiModel.Provider

	var client mcp.AIClient
	switch provider {
	case "claude":
		client = mcp.NewClaudeClient()
	case "kimi":
		client = mcp.NewKimiClient()
	case "gemini":
		client = mcp.NewGeminiClient()
	case "grok":
		client = mcp.NewGrokClient()
	case "openai":
		client = mcp.NewOpenAIClient()
	case "qwen":
		client = mcp.NewQwenClient()
	case "custom":
		client = mcp.New()
	default:
		client = mcp.NewDeepSeekClient()
	}
	client.SetAPIKey(apiKey, url, model)
	return client
}

// AnalystMaxTokens 分析师请求的 max_completion_tokens（推理+JSON 需更多空间，避免 finish_reason: length 截断）
const AnalystMaxTokens = 4096

// BuildMCPClientFromAIModelForAnalyst 构建分析师专用 MCP 客户端，并设置更高 MaxTokens 以容纳 reasoning_content + JSON
func BuildMCPClientFromAIModelForAnalyst(aiModel *store.AIModel) mcp.AIClient {
	client := BuildMCPClientFromAIModel(aiModel)
	if client != nil {
		client.SetMaxTokens(AnalystMaxTokens)
	}
	return client
}
