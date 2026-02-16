// MClaw - Mem0-lite: Intelligent Memory Layer
// Native Go implementation inspired by mem0ai/mem0
// License: MIT

package memory

import (
	"math"
	"time"
)

// MemoryItem represents a single memory fact stored in the system.
type MemoryItem struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	Category  string    `json:"category"` // preference, fact, context, instruction
	Embedding []float32 `json:"-"`        // vector embedding (not serialized to JSON)
	Score     float64   `json:"score"`    // importance score (0-1)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	AccessCnt int       `json:"access_count"` // for auto-pruning
}

// SearchResult represents a memory search result with similarity score.
type SearchResult struct {
	Item       MemoryItem `json:"item"`
	Similarity float64    `json:"similarity"` // cosine similarity (0-1)
}

// ConsolidateAction defines what to do with a new fact vs existing memories.
type ConsolidateAction string

const (
	ActionAdd    ConsolidateAction = "ADD"
	ActionUpdate ConsolidateAction = "UPDATE"
	ActionDelete ConsolidateAction = "DELETE"
	ActionNoop   ConsolidateAction = "NOOP"
)

// ConsolidateResult is the outcome of the consolidation process.
type ConsolidateResult struct {
	Action        ConsolidateAction `json:"action"`
	TargetID      string            `json:"target_id,omitempty"`      // ID of existing memory to update/delete
	MergedContent string            `json:"merged_content,omitempty"` // merged content for UPDATE
	Reason        string            `json:"reason,omitempty"`         // explanation
}

// ExtractedFact represents a fact extracted from conversation by the LLM.
type ExtractedFact struct {
	Content    string  `json:"content"`
	Category   string  `json:"category"`   // preference, fact, context, instruction
	Importance float64 `json:"importance"` // 0-1
}

// MemoryStats holds statistics about a user's memories.
type MemoryStats struct {
	UserID     string         `json:"user_id"`
	TotalCount int            `json:"total_count"`
	Categories map[string]int `json:"categories"`
}

// Memory categories
const (
	CategoryPreference  = "preference"
	CategoryFact        = "fact"
	CategoryContext     = "context"
	CategoryInstruction = "instruction"
)

// CosineSimilarity computes the cosine similarity between two vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
