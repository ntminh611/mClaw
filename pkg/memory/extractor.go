package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/ntminh611/mclaw/pkg/providers"
)

// Extractor extracts salient facts from conversations using an LLM.
type Extractor struct {
	provider providers.LLMProvider
	model    string
}

// NewExtractor creates a fact extractor using the given LLM provider.
func NewExtractor(provider providers.LLMProvider, model string) *Extractor {
	return &Extractor{
		provider: provider,
		model:    model,
	}
}

const extractPrompt = `You are a memory extraction system. Analyze the conversation below and extract important, reusable facts about the user.

RULES:
- Extract ONLY personal, reusable information (preferences, habits, facts about the user, instructions they've given)
- Do NOT extract ephemeral information (what time it is, current task progress, greetings)
- Do NOT extract information about the AI assistant itself
- Each fact should be a short, atomic statement
- Maximum 5 facts per conversation turn
- Assign a category: "preference" (likes/dislikes), "fact" (personal info), "context" (background/situation), "instruction" (how the user wants things done)
- Assign importance 0.0-1.0 (1.0 = critical personal info, 0.5 = useful context, 0.1 = minor detail)

RESPOND WITH ONLY A JSON ARRAY. No explanation, no markdown, no code blocks.
If no facts to extract, respond with: []

Example output:
[{"content":"User prefers dark mode in all applications","category":"preference","importance":0.7},{"content":"User is a Go developer based in Vietnam","category":"fact","importance":0.8}]

CONVERSATION:
`

// Extract analyzes a conversation and returns extracted facts.
func (e *Extractor) Extract(ctx context.Context, messages []providers.Message) ([]ExtractedFact, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	// Build conversation text from messages
	var conv strings.Builder
	for _, m := range messages {
		if m.Role == "user" || m.Role == "assistant" {
			conv.WriteString(fmt.Sprintf("%s: %s\n", m.Role, m.Content))
		}
	}

	prompt := extractPrompt + conv.String()

	response, err := e.provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: prompt},
	}, nil, e.model, map[string]interface{}{
		"max_tokens":  1024,
		"temperature": 0.0, // deterministic extraction
	})
	if err != nil {
		return nil, fmt.Errorf("extraction LLM call failed: %w", err)
	}

	// Parse JSON response
	content := strings.TrimSpace(response.Content)

	// Strip markdown code blocks if present
	content = stripCodeBlock(content)

	var facts []ExtractedFact
	content = repairJSONArray(content)
	if err := json.Unmarshal([]byte(content), &facts); err != nil {
		log.Printf("[memory] Failed to parse extraction response: %v (raw: %s)", err, truncate(content, 200))
		return nil, nil // non-fatal: just skip this extraction
	}

	// Validate and filter
	validFacts := make([]ExtractedFact, 0, len(facts))
	for _, f := range facts {
		if f.Content == "" {
			continue
		}
		// Clamp importance
		if f.Importance < 0 {
			f.Importance = 0
		}
		if f.Importance > 1 {
			f.Importance = 1
		}
		// Default category
		if f.Category == "" {
			f.Category = CategoryFact
		}
		validFacts = append(validFacts, f)
	}

	if len(validFacts) > 5 {
		validFacts = validFacts[:5]
	}

	log.Printf("[memory] Extracted %d facts from conversation", len(validFacts))
	return validFacts, nil
}

// stripCodeBlock removes markdown code block wrappers from a string.
func stripCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	// Remove ```json ... ``` or ``` ... ```
	if strings.HasPrefix(s, "```") {
		lines := strings.SplitN(s, "\n", 2)
		if len(lines) > 1 {
			s = lines[1]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

// repairJSONArray attempts to fix truncated JSON arrays by closing at the last complete element.
func repairJSONArray(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") {
		return s
	}

	// Already valid JSON
	var test json.RawMessage
	if json.Unmarshal([]byte(s), &test) == nil {
		return s
	}

	// Find the last complete object (ending with "}")
	lastComplete := strings.LastIndex(s, "}")
	if lastComplete > 0 {
		repaired := s[:lastComplete+1] + "]"
		if json.Unmarshal([]byte(repaired), &test) == nil {
			return repaired
		}
	}

	// Fallback: empty array
	return "[]"
}

// repairJSONObject attempts to fix truncated JSON objects by closing with missing braces.
func repairJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return s
	}

	var test json.RawMessage
	if json.Unmarshal([]byte(s), &test) == nil {
		return s
	}

	// Try adding closing brace(s)
	for _, suffix := range []string{"\"}", "\"\"}", "}", "}}"} {
		repaired := s + suffix
		if json.Unmarshal([]byte(repaired), &test) == nil {
			return repaired
		}
	}

	// Try truncating at last complete key-value pair
	lastQuote := strings.LastIndex(s, "\"")
	if lastQuote > 0 {
		// Find the comma or opening brace before the last incomplete field
		for i := lastQuote; i >= 0; i-- {
			if s[i] == ',' {
				repaired := s[:i] + "}"
				if json.Unmarshal([]byte(repaired), &test) == nil {
					return repaired
				}
			}
		}
	}

	return s
}
