package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/ntminh611/mclaw/pkg/providers"
)

// Consolidator decides how to handle a new fact relative to existing memories.
type Consolidator struct {
	provider providers.LLMProvider
	model    string
}

// NewConsolidator creates a new memory consolidator.
func NewConsolidator(provider providers.LLMProvider, model string) *Consolidator {
	return &Consolidator{
		provider: provider,
		model:    model,
	}
}

const consolidatePrompt = `You are a memory consolidation system. Given a NEW FACT and a list of EXISTING MEMORIES, decide the best action.

ACTIONS:
- "ADD": The fact is genuinely new information not covered by any existing memory
- "UPDATE": The fact updates or extends an existing memory (specify target_id and provide merged_content)
- "DELETE": The fact contradicts an existing memory, making it obsolete (specify target_id)
- "NOOP": The fact is already known or too similar to an existing memory

RULES:
- Be conservative: prefer NOOP over ADD to avoid duplicates
- When updating, merge the old and new info into one coherent statement
- Only DELETE when the new fact directly contradicts an old one
- Always provide a brief reason

RESPOND WITH ONLY JSON. No explanation, no markdown.

Example:
{"action":"UPDATE","target_id":"abc-123","merged_content":"User prefers Vietnamese coffee, specifically black coffee without sugar","reason":"Extends existing coffee preference with new detail"}

NEW FACT: %s

EXISTING MEMORIES:
%s
`

// Consolidate determines the appropriate action for a new fact.
func (c *Consolidator) Consolidate(ctx context.Context, newFact string, existingMemories []SearchResult) (*ConsolidateResult, error) {
	if len(existingMemories) == 0 {
		// No existing memories to compare against — always ADD
		return &ConsolidateResult{
			Action: ActionAdd,
			Reason: "No existing memories to compare",
		}, nil
	}

	// Build existing memories list
	var memList strings.Builder
	for _, m := range existingMemories {
		memList.WriteString(fmt.Sprintf("- [ID: %s] [%s] %s (similarity: %.0f%%)\n",
			m.Item.ID, m.Item.Category, m.Item.Content, m.Similarity*100))
	}

	prompt := fmt.Sprintf(consolidatePrompt, newFact, memList.String())

	response, err := c.provider.Chat(ctx, []providers.Message{
		{Role: "user", Content: prompt},
	}, nil, c.model, map[string]interface{}{
		"max_tokens":  512,
		"temperature": 0.0,
	})
	if err != nil {
		return nil, fmt.Errorf("consolidation LLM call failed: %w", err)
	}

	content := strings.TrimSpace(response.Content)
	content = stripCodeBlock(content)

	var result ConsolidateResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		log.Printf("[memory] Failed to parse consolidation response: %v (raw: %s)", err, truncate(content, 200))
		// Default to ADD on parse failure
		return &ConsolidateResult{
			Action: ActionAdd,
			Reason: "Parse failure, defaulting to ADD",
		}, nil
	}

	// Validate action
	switch result.Action {
	case ActionAdd, ActionUpdate, ActionDelete, ActionNoop:
		// valid
	default:
		result.Action = ActionAdd
		result.Reason = "Unknown action, defaulting to ADD"
	}

	log.Printf("[memory] Consolidation: %s (target=%s, reason=%s)", result.Action, result.TargetID, result.Reason)
	return &result, nil
}
