// MClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 MClaw contributors

package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ntminh611/mclaw/pkg/config"
	"github.com/ntminh611/mclaw/pkg/logger"
)

type HTTPProvider struct {
	apiKey        string
	apiBase       string
	modelOverride string
	httpClient    *http.Client
}

func NewHTTPProvider(apiKey, apiBase, modelOverride string) *HTTPProvider {
	return &HTTPProvider{
		apiKey:        apiKey,
		apiBase:       apiBase,
		modelOverride: modelOverride,
		httpClient: &http.Client{
			Timeout: 600 * time.Second,
		},
	}
}

func (p *HTTPProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p.apiBase == "" {
		return nil, fmt.Errorf("API base not configured")
	}

	// Use modelOverride if set (prefix stripped for non-OpenRouter providers)
	actualModel := model
	if p.modelOverride != "" {
		actualModel = p.modelOverride
	}

	requestBody := map[string]interface{}{
		"model":    actualModel,
		"messages": messages,
		"stream":   true,
	}

	if len(tools) > 0 {
		requestBody["tools"] = tools
		requestBody["tool_choice"] = "auto"
	}

	if maxTokens, ok := options["max_tokens"].(int); ok {
		requestBody["max_tokens"] = maxTokens
	}

	if temperature, ok := options["temperature"].(float64); ok {
		requestBody["temperature"] = temperature
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	logger.InfoC("llm", fmt.Sprintf("POST %s/chat/completions (model=%s, messages=%d, stream=true)", p.apiBase, actualModel, len(messages)))

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		authHeader := "Bearer " + p.apiKey
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &RateLimitError{StatusCode: 429, Body: string(body)}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	// Check if response is actually streamed
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") && !strings.Contains(contentType, "text/plain") {
		// Non-streamed response, parse normally
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		logger.InfoC("llm", fmt.Sprintf("Non-streamed response (%d bytes)", len(body)))
		return p.parseResponse(body)
	}

	return p.parseStreamResponse(resp.Body)
}

func (p *HTTPProvider) parseStreamResponse(body io.Reader) (*LLMResponse, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var contentBuilder strings.Builder
	var thinkingBuilder strings.Builder
	var finishReason string
	thinkingDone := false

	// Tool call accumulation by index
	type partialToolCall struct {
		ID           string
		Type         string
		Name         string
		ArgsJSON     strings.Builder
		ExtraContent map[string]interface{}
	}
	toolCallMap := make(map[int]*partialToolCall)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
					ToolCalls        []struct {
						Index        int                    `json:"index"`
						ID           string                 `json:"id"`
						Type         string                 `json:"type"`
						ExtraContent map[string]interface{} `json:"extra_content"`
						Function     *struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta

		// Handle thinking/reasoning content
		thinking := delta.ReasoningContent
		if thinking == "" {
			thinking = delta.Reasoning
		}
		if thinking != "" {
			if !thinkingDone {
				if thinkingBuilder.Len() == 0 {
					logger.InfoC("thinking", "💭 Model is thinking...")
				}
				thinkingBuilder.WriteString(thinking)
				// Log thinking progress periodically (every ~200 chars)
				if thinkingBuilder.Len()%200 < len(thinking) {
					// Show last snippet of thinking
					full := thinkingBuilder.String()
					snippet := full
					if len(snippet) > 100 {
						snippet = "..." + snippet[len(snippet)-100:]
					}
					logger.DebugC("thinking", snippet)
				}
			}
		}

		// Handle regular content
		if delta.Content != "" {
			if !thinkingDone && thinkingBuilder.Len() > 0 {
				thinkingDone = true
				logger.InfoC("thinking", fmt.Sprintf("✅ Thinking complete (%d chars)", thinkingBuilder.Len()))
			}
			contentBuilder.WriteString(delta.Content)
		}

		// Handle tool calls
		for _, tc := range delta.ToolCalls {
			ptc, ok := toolCallMap[tc.Index]
			if !ok {
				ptc = &partialToolCall{}
				toolCallMap[tc.Index] = ptc
			}
			if tc.ID != "" {
				ptc.ID = tc.ID
			}
			if tc.Type != "" {
				ptc.Type = tc.Type
			}
			if tc.ExtraContent != nil {
				ptc.ExtraContent = tc.ExtraContent
			}
			if tc.Function != nil {
				if tc.Function.Name != "" {
					ptc.Name = tc.Function.Name
				}
				ptc.ArgsJSON.WriteString(tc.Function.Arguments)
			}
		}

		if chunk.Choices[0].FinishReason != nil {
			finishReason = *chunk.Choices[0].FinishReason
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream reading error: %w", err)
	}

	// Build tool calls
	toolCalls := make([]ToolCall, 0, len(toolCallMap))
	for i := 0; i < len(toolCallMap); i++ {
		ptc, ok := toolCallMap[i]
		if !ok {
			continue
		}
		arguments := make(map[string]interface{})
		argsStr := ptc.ArgsJSON.String()
		if argsStr != "" {
			if err := json.Unmarshal([]byte(argsStr), &arguments); err != nil {
				arguments["raw"] = argsStr
			}
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:           ptc.ID,
			Name:         ptc.Name,
			Arguments:    arguments,
			ExtraContent: ptc.ExtraContent,
		})
	}

	content := contentBuilder.String()
	thinking := thinkingBuilder.String()

	logger.InfoC("llm", fmt.Sprintf("Stream complete: content=%d chars, thinking=%d chars, tools=%d",
		len(content), len(thinking), len(toolCalls)))

	return &LLMResponse{
		Content:      content,
		Thinking:     thinking,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
	}, nil
}

func (p *HTTPProvider) parseResponse(body []byte) (*LLMResponse, error) {
	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID           string                 `json:"id"`
					Type         string                 `json:"type"`
					ExtraContent map[string]interface{} `json:"extra_content"`
					Function     *struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *UsageInfo `json:"usage"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(apiResponse.Choices) == 0 {
		return &LLMResponse{
			Content:      "",
			FinishReason: "stop",
		}, nil
	}

	choice := apiResponse.Choices[0]

	toolCalls := make([]ToolCall, 0, len(choice.Message.ToolCalls))
	for _, tc := range choice.Message.ToolCalls {
		arguments := make(map[string]interface{})
		name := ""

		// Handle OpenAI format with nested function object
		if tc.Type == "function" && tc.Function != nil {
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		} else if tc.Function != nil {
			// Legacy format without type field
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		}

		toolCalls = append(toolCalls, ToolCall{
			ID:           tc.ID,
			Name:         name,
			Arguments:    arguments,
			ExtraContent: tc.ExtraContent,
		})
	}

	return &LLMResponse{
		Content:      choice.Message.Content,
		ToolCalls:    toolCalls,
		FinishReason: choice.FinishReason,
		Usage:        apiResponse.Usage,
	}, nil
}

func (p *HTTPProvider) GetDefaultModel() string {
	return ""
}

func CreateProvider(cfg *config.Config) (LLMProvider, error) {
	return CreateProviderForModel(cfg, cfg.Agents.Defaults.Model)
}

func CreateProviderForModel(cfg *config.Config, model string) (LLMProvider, error) {
	var apiKey, apiBase string

	lowerModel := strings.ToLower(model)

	// stripPrefix removes provider routing prefix from model name
	// e.g. "openai/claude-opus-4" -> "claude-opus-4"
	stripPrefix := func(m string) string {
		prefixes := []string{"openai/", "anthropic/", "openrouter/", "meta-llama/", "deepseek/", "google/", "gemini/", "groq/"}
		for _, p := range prefixes {
			if strings.HasPrefix(m, p) {
				return strings.TrimPrefix(m, p)
			}
		}
		return m
	}

	var modelName string // the actual model name sent to the API

	switch {
	case strings.HasPrefix(model, "openai/"):
		// openai/ prefix: use OpenAI provider first (supports local gateways/proxies),
		// fall back to OpenRouter if OpenAI provider is not configured
		if cfg.Providers.OpenAI.APIKey != "" {
			apiKey = cfg.Providers.OpenAI.APIKey
			apiBase = cfg.Providers.OpenAI.APIBase
			if apiBase == "" {
				apiBase = "https://api.openai.com/v1"
			}
			modelName = stripPrefix(model) // strip prefix for direct provider
		} else {
			apiKey = cfg.Providers.OpenRouter.APIKey
			if cfg.Providers.OpenRouter.APIBase != "" {
				apiBase = cfg.Providers.OpenRouter.APIBase
			} else {
				apiBase = "https://openrouter.ai/api/v1"
			}
			// OpenRouter expects prefixed model names, keep as-is
		}

	case strings.HasPrefix(model, "openrouter/") || strings.HasPrefix(model, "anthropic/") || strings.HasPrefix(model, "meta-llama/") || strings.HasPrefix(model, "deepseek/") || strings.HasPrefix(model, "google/"):
		apiKey = cfg.Providers.OpenRouter.APIKey
		if cfg.Providers.OpenRouter.APIBase != "" {
			apiBase = cfg.Providers.OpenRouter.APIBase
		} else {
			apiBase = "https://openrouter.ai/api/v1"
		}
		// OpenRouter expects prefixed model names, keep as-is

	case strings.Contains(lowerModel, "claude"):
		// Note: Anthropic's native API uses a different format (x-api-key header,
		// non-OpenAI schema). For Claude, prefer using "anthropic/" prefix which
		// routes through OpenRouter (OpenAI-compatible). This case is a fallback
		// for users with a custom OpenAI-compatible Anthropic proxy.
		apiKey = cfg.Providers.Anthropic.APIKey
		apiBase = cfg.Providers.Anthropic.APIBase
		if apiBase == "" {
			// Fall back to OpenRouter if no custom Anthropic base is configured
			if cfg.Providers.OpenRouter.APIKey != "" {
				apiKey = cfg.Providers.OpenRouter.APIKey
				if cfg.Providers.OpenRouter.APIBase != "" {
					apiBase = cfg.Providers.OpenRouter.APIBase
				} else {
					apiBase = "https://openrouter.ai/api/v1"
				}
			} else {
				apiBase = "https://api.anthropic.com/v1"
			}
		}

	case strings.Contains(lowerModel, "gpt"):
		apiKey = cfg.Providers.OpenAI.APIKey
		apiBase = cfg.Providers.OpenAI.APIBase
		if apiBase == "" {
			apiBase = "https://api.openai.com/v1"
		}

	case strings.Contains(lowerModel, "gemini") || strings.HasPrefix(model, "gemini/"):
		apiKey = cfg.Providers.Gemini.APIKey
		apiBase = cfg.Providers.Gemini.APIBase
		if apiBase == "" {
			apiBase = "https://generativelanguage.googleapis.com/v1beta/openai"
		}
		modelName = stripPrefix(model)

	case strings.Contains(lowerModel, "glm") || strings.Contains(lowerModel, "zhipu") || strings.Contains(lowerModel, "zai"):
		apiKey = cfg.Providers.Zhipu.APIKey
		apiBase = cfg.Providers.Zhipu.APIBase
		if apiBase == "" {
			apiBase = "https://open.bigmodel.cn/api/paas/v4"
		}
		modelName = stripPrefix(model)

	case strings.Contains(lowerModel, "groq") || strings.HasPrefix(model, "groq/"):
		apiKey = cfg.Providers.Groq.APIKey
		apiBase = cfg.Providers.Groq.APIBase
		if apiBase == "" {
			apiBase = "https://api.groq.com/openai/v1"
		}
		modelName = stripPrefix(model)

	case cfg.Providers.VLLM.APIBase != "":
		apiKey = cfg.Providers.VLLM.APIKey
		apiBase = cfg.Providers.VLLM.APIBase

	default:
		if cfg.Providers.OpenRouter.APIKey != "" {
			apiKey = cfg.Providers.OpenRouter.APIKey
			if cfg.Providers.OpenRouter.APIBase != "" {
				apiBase = cfg.Providers.OpenRouter.APIBase
			} else {
				apiBase = "https://openrouter.ai/api/v1"
			}
		} else {
			return nil, fmt.Errorf("no API key configured for model: %s", model)
		}
	}

	if apiKey == "" && !strings.HasPrefix(model, "bedrock/") {
		return nil, fmt.Errorf("no API key configured for provider (model: %s)", model)
	}

	if apiBase == "" {
		return nil, fmt.Errorf("no API base configured for provider (model: %s)", model)
	}

	return NewHTTPProvider(apiKey, apiBase, modelName), nil
}
