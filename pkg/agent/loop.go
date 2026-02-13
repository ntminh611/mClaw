// MClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 MClaw contributors

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ntminh611/mclaw/pkg/bus"
	"github.com/ntminh611/mclaw/pkg/config"
	"github.com/ntminh611/mclaw/pkg/memory"
	"github.com/ntminh611/mclaw/pkg/providers"
	"github.com/ntminh611/mclaw/pkg/session"
	"github.com/ntminh611/mclaw/pkg/tools"
)

type AgentLoop struct {
	bus            *bus.MessageBus
	provider       providers.LLMProvider
	workspace      string
	model          string
	contextWindow  int
	maxIterations  int
	sessions       *session.SessionManager
	contextBuilder *ContextBuilder
	tools          *tools.ToolRegistry
	memory         *memory.MemoryEngine
	running        bool
	summarizing    sync.Map
}

func NewAgentLoop(cfg *config.Config, bus *bus.MessageBus, provider providers.LLMProvider) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)

	toolsRegistry := tools.NewToolRegistry()
	toolsRegistry.Register(&tools.ReadFileTool{})
	toolsRegistry.Register(&tools.WriteFileTool{})
	toolsRegistry.Register(&tools.ListDirTool{})
	toolsRegistry.Register(tools.NewExecTool(workspace))

	braveAPIKey := cfg.Tools.Web.Search.APIKey
	toolsRegistry.Register(tools.NewWebSearchTool(braveAPIKey, cfg.Tools.Web.Search.MaxResults))
	toolsRegistry.Register(tools.NewWebFetchTool(50000))
	toolsRegistry.Register(tools.NewBrowserTool(30 * time.Second))
	toolsRegistry.Register(tools.NewCronTool())
	toolsRegistry.Register(tools.NewHeartbeatTool())

	sessionsManager := session.NewSessionManager(filepath.Join(filepath.Dir(cfg.WorkspacePath()), "sessions"))

	// Initialize Mem0-lite memory engine
	var memEngine *memory.MemoryEngine
	if cfg.Memory.Enabled {
		var err error
		memEngine, err = memory.NewMemoryEngine(cfg, provider)
		if err != nil {
			log.Printf("[agent] Warning: Failed to initialize memory engine: %v", err)
		} else if memEngine != nil {
			log.Printf("[agent] Mem0-lite memory engine enabled")
		}
	}

	return &AgentLoop{
		bus:            bus,
		provider:       provider,
		workspace:      workspace,
		model:          cfg.Agents.Defaults.Model,
		contextWindow:  cfg.Agents.Defaults.MaxTokens,
		maxIterations:  cfg.Agents.Defaults.MaxToolIterations,
		sessions:       sessionsManager,
		contextBuilder: NewContextBuilder(workspace),
		tools:          toolsRegistry,
		memory:         memEngine,
		running:        false,
		summarizing:    sync.Map{},
	}
}

func (al *AgentLoop) GetSessionManager() *session.SessionManager {
	return al.sessions
}

func (al *AgentLoop) GetToolRegistry() *tools.ToolRegistry {
	return al.tools
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.running = true

	for al.running {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, ok := al.bus.ConsumeInbound(ctx)
			if !ok {
				continue
			}

			response, err := al.processMessage(ctx, msg)
			if err != nil {
				response = formatErrorForUser(err)
			}

			if response != "" {
				al.bus.PublishOutbound(bus.OutboundMessage{
					Channel: msg.Channel,
					ChatID:  msg.ChatID,
					Content: response,
				})
			}
		}
	}

	return nil
}

func (al *AgentLoop) Stop() {
	al.running = false
}

func (al *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	msg := bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     "direct",
		Content:    content,
		SessionKey: sessionKey,
	}

	return al.processMessage(ctx, msg)
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Per-message timeout to prevent hanging
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Inject current chat context into CronTool for auto-delivery
	if cronTool, ok := al.tools.Get("cron"); ok {
		if ct, ok := cronTool.(*tools.CronTool); ok {
			ct.SetContext(msg.Channel, msg.ChatID)
		}
	}

	history := al.sessions.GetHistory(msg.SessionKey)
	summary := al.sessions.GetSummary(msg.SessionKey)

	// Recall relevant memories from Mem0-lite
	var memories []memory.SearchResult
	if al.memory != nil {
		recalled, err := al.memory.RecallMemories(ctx, msg.SenderID, msg.Content, 0)
		if err != nil {
			log.Printf("[agent] Memory recall failed: %v", err)
		} else {
			memories = recalled
		}
	}

	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		msg.Content,
		nil,
		memories,
	)

	iteration := 0
	var finalContent string
	consecutiveToolErrors := 0
	consecutiveToolOnly := 0
	const maxConsecutiveErrors = 3
	const maxConsecutiveToolOnly = 10

	for iteration < al.maxIterations {
		iteration++

		toolDefs := al.tools.GetDefinitions()
		providerToolDefs := make([]providers.ToolDefinition, 0, len(toolDefs))

		// If too many consecutive tool errors, stop providing tools to force a text response
		if consecutiveToolErrors >= maxConsecutiveErrors {
			log.Printf("[agent] Too many consecutive tool errors (%d), forcing text-only response", consecutiveToolErrors)
			providerToolDefs = nil
		} else {
			for _, td := range toolDefs {
				providerToolDefs = append(providerToolDefs, providers.ToolDefinition{
					Type: td["type"].(string),
					Function: providers.ToolFunctionDefinition{
						Name:        td["function"].(map[string]interface{})["name"].(string),
						Description: td["function"].(map[string]interface{})["description"].(string),
						Parameters:  td["function"].(map[string]interface{})["parameters"].(map[string]interface{}),
					},
				})
			}
		}

		log.Printf("[agent] Iteration %d: calling LLM (model=%s)...", iteration, al.model)
		llmStart := time.Now()

		response, err := al.provider.Chat(ctx, messages, providerToolDefs, al.model, map[string]interface{}{
			"max_tokens":  8192,
			"temperature": 0.7,
		})

		llmDuration := time.Since(llmStart)
		if err != nil {
			log.Printf("[agent] LLM call failed after %s: %v", llmDuration, err)
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		log.Printf("[agent] LLM responded in %s (content=%d chars, thinking=%d chars, tools=%d)",
			llmDuration, len(response.Content), len(response.Thinking), len(response.ToolCalls))

		// Send thinking content to user if available
		if response.Thinking != "" && msg.Channel != "cli" {
			thinkingPreview := response.Thinking
			if len(thinkingPreview) > 3500 {
				thinkingPreview = thinkingPreview[:3500] + "\n...（truncated）"
			}
			al.bus.PublishOutbound(bus.OutboundMessage{
				Channel: msg.Channel,
				ChatID:  msg.ChatID,
				Content: "💭 *Thinking:*\n\n" + thinkingPreview,
			})
		}

		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			break
		}

		// Track consecutive tool-only iterations (no text content produced)
		if response.Content == "" {
			consecutiveToolOnly++
		} else {
			consecutiveToolOnly = 0
		}

		// Safety: break if too many consecutive tool-only iterations
		if consecutiveToolOnly >= maxConsecutiveToolOnly {
			log.Printf("[agent] Breaking: %d consecutive tool-only iterations with no text content", consecutiveToolOnly)
			finalContent = response.Content
			if finalContent == "" {
				finalContent = "I've been working on your request but encountered difficulties. Could you try rephrasing or being more specific?"
			}
			break
		}

		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
		}

		for _, tc := range response.ToolCalls {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:           tc.ID,
				Type:         "function",
				ExtraContent: tc.ExtraContent,
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)

		allFailed := true
		for _, tc := range response.ToolCalls {
			log.Printf("[agent] Executing tool: %s", tc.Name)
			toolStart := time.Now()
			result, err := al.tools.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				log.Printf("[agent] Tool %s failed after %s: %v", tc.Name, time.Since(toolStart), err)
				result = fmt.Sprintf("Error: %v\n\nHint: If this is a path error, make sure to use absolute paths. Your workspace is at an absolute path, not a relative one.", err)
			} else {
				log.Printf("[agent] Tool %s completed in %s (result=%d chars)", tc.Name, time.Since(toolStart), len(result))
				allFailed = false
			}

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)
		}

		// Track consecutive all-failed tool iterations
		if allFailed {
			consecutiveToolErrors++
		} else {
			consecutiveToolErrors = 0
		}
	}

	if finalContent == "" {
		finalContent = "I've completed processing but have no response to give."
	}

	al.sessions.AddMessage(msg.SessionKey, "user", msg.Content)
	al.sessions.AddMessage(msg.SessionKey, "assistant", finalContent)

	// Async: Process conversation for memory extraction (Mem0-lite)
	if al.memory != nil {
		convMessages := []providers.Message{
			{Role: "user", Content: msg.Content},
			{Role: "assistant", Content: finalContent},
		}
		go al.memory.ProcessConversation(ctx, msg.SenderID, convMessages)
	}

	// Context compression logic
	newHistory := al.sessions.GetHistory(msg.SessionKey)

	// Token Awareness (Dynamic)
	// Trigger if history > 20 messages OR estimated tokens > 75% of context window
	tokenEstimate := al.estimateTokens(newHistory)
	threshold := al.contextWindow * 75 / 100

	if len(newHistory) > 20 || tokenEstimate > threshold {
		if _, loading := al.summarizing.LoadOrStore(msg.SessionKey, true); !loading {
			go func() {
				defer al.summarizing.Delete(msg.SessionKey)
				al.summarizeSession(msg.SessionKey)
			}()
		}
	}

	al.sessions.Save(al.sessions.GetOrCreate(msg.SessionKey))

	return finalContent, nil
}

func (al *AgentLoop) summarizeSession(sessionKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	history := al.sessions.GetHistory(sessionKey)
	summary := al.sessions.GetSummary(sessionKey)

	// Keep last 4 messages for continuity
	if len(history) <= 4 {
		return
	}

	toSummarize := history[:len(history)-4]

	// Oversized Message Guard (Dynamic)
	// Skip messages larger than 50% of context window to prevent summarizer overflow.
	maxMessageTokens := al.contextWindow / 2
	validMessages := make([]providers.Message, 0)
	omitted := false

	for _, m := range toSummarize {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		// Estimate tokens for this message
		msgTokens := len(m.Content) / 4
		if msgTokens > maxMessageTokens {
			omitted = true
			continue
		}
		validMessages = append(validMessages, m)
	}

	if len(validMessages) == 0 {
		return
	}

	// Multi-Part Summarization
	// Split into two parts if history is significant
	var finalSummary string
	if len(validMessages) > 10 {
		mid := len(validMessages) / 2
		part1 := validMessages[:mid]
		part2 := validMessages[mid:]

		s1, _ := al.summarizeBatch(ctx, part1, "")
		s2, _ := al.summarizeBatch(ctx, part2, "")

		// Merge them
		mergePrompt := fmt.Sprintf("Merge these two conversation summaries into one cohesive summary:\n\n1: %s\n\n2: %s", s1, s2)
		resp, err := al.provider.Chat(ctx, []providers.Message{{Role: "user", Content: mergePrompt}}, nil, al.model, map[string]interface{}{
			"max_tokens":  1024,
			"temperature": 0.3,
		})
		if err == nil {
			finalSummary = resp.Content
		} else {
			finalSummary = s1 + " " + s2
		}
	} else {
		finalSummary, _ = al.summarizeBatch(ctx, validMessages, summary)
	}

	if omitted && finalSummary != "" {
		finalSummary += "\n[Note: Some oversized messages were omitted from this summary for efficiency.]"
	}

	if finalSummary != "" {
		al.sessions.SetSummary(sessionKey, finalSummary)
		al.sessions.TruncateHistory(sessionKey, 4)
		al.sessions.Save(al.sessions.GetOrCreate(sessionKey))
	}
}

func (al *AgentLoop) summarizeBatch(ctx context.Context, batch []providers.Message, existingSummary string) (string, error) {
	prompt := "Provide a concise summary of this conversation segment, preserving core context and key points.\n"
	if existingSummary != "" {
		prompt += "Existing context: " + existingSummary + "\n"
	}
	prompt += "\nCONVERSATION:\n"
	for _, m := range batch {
		prompt += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
	}

	response, err := al.provider.Chat(ctx, []providers.Message{{Role: "user", Content: prompt}}, nil, al.model, map[string]interface{}{
		"max_tokens":  1024,
		"temperature": 0.3,
	})
	if err != nil {
		return "", err
	}
	return response.Content, nil
}

func (al *AgentLoop) estimateTokens(messages []providers.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4 // Simple heuristic: 4 chars per token
	}
	return total
}

func formatErrorForUser(err error) string {
	errStr := err.Error()

	switch {
	case strings.Contains(errStr, "429") || strings.Contains(errStr, "rate") || strings.Contains(errStr, "exhausted"):
		return "⚠️ API rate limit reached. Please wait a moment and try again."
	case strings.Contains(errStr, "context deadline exceeded") || strings.Contains(errStr, "timeout"):
		return "⏰ Request timed out. The AI took too long to respond. Please try a simpler question or try again."
	case strings.Contains(errStr, "401") || strings.Contains(errStr, "403") || strings.Contains(errStr, "PERMISSION"):
		return "🔑 API authentication error. Please check your API key configuration."
	case strings.Contains(errStr, "500") || strings.Contains(errStr, "502") || strings.Contains(errStr, "503"):
		return "🔧 AI service is temporarily unavailable. Please try again later."
	default:
		// Truncate long error messages
		if len(errStr) > 200 {
			errStr = errStr[:200] + "..."
		}
		return fmt.Sprintf("❌ Error: %s", errStr)
	}
}
