package memory

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/ntminh611/mclaw/pkg/config"
	"github.com/ntminh611/mclaw/pkg/providers"
)

// MemoryEngine orchestrates the entire memory pipeline:
// Extract facts → Embed → Search similar → Consolidate → Store
type MemoryEngine struct {
	store        *MemoryStore
	embedder     *Embedder
	extractor    *Extractor
	consolidator *Consolidator
	cfg          config.MemoryConfig
	processing   sync.Map // tracks in-flight processing per user
}

// NewMemoryEngine initializes all memory components.
func NewMemoryEngine(cfg *config.Config, provider providers.LLMProvider) (*MemoryEngine, error) {
	memCfg := cfg.Memory
	if !memCfg.Enabled {
		return nil, nil
	}

	// Resolve database path
	dataDir := filepath.Dir(cfg.WorkspacePath())
	dbPath := filepath.Join(dataDir, "memory.db")

	store, err := NewMemoryStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory store: %w", err)
	}

	// Resolve Gemini API key: memory.api_key → providers.gemini.api_key
	embedAPIKey := memCfg.APIKey
	if embedAPIKey == "" {
		embedAPIKey = cfg.Providers.Gemini.APIKey
	}
	if embedAPIKey == "" {
		store.Close()
		return nil, fmt.Errorf("no Gemini API key for memory embedding (set memory.api_key or providers.gemini.api_key)")
	}

	embedder := NewEmbedder(embedAPIKey, memCfg.APIBase)

	// Use agent's LLM for extraction and consolidation
	extractModel := memCfg.ExtractModel
	if extractModel == "" {
		extractModel = cfg.Agents.Defaults.Model
	}

	extractor := NewExtractor(provider, extractModel)
	consolidator := NewConsolidator(provider, extractModel)

	// Apply defaults
	if memCfg.TopK <= 0 {
		memCfg.TopK = 5
	}
	if memCfg.MinScore <= 0 {
		memCfg.MinScore = 0.3
	}
	if memCfg.MaxMemories <= 0 {
		memCfg.MaxMemories = 1000
	}

	engine := &MemoryEngine{
		store:        store,
		embedder:     embedder,
		extractor:    extractor,
		consolidator: consolidator,
		cfg:          memCfg,
	}

	log.Printf("[memory] Engine initialized (embedding=gemini/%s, topK=%d, minScore=%.2f)",
		geminiEmbedModel, memCfg.TopK, memCfg.MinScore)

	return engine, nil
}

// RecallMemories searches for relevant memories based on a query.
// This is called BEFORE the LLM response to inject context.
func (e *MemoryEngine) RecallMemories(ctx context.Context, userID, query string, topK int) ([]SearchResult, error) {
	if topK <= 0 {
		topK = e.cfg.TopK
	}

	// Embed the query
	queryEmb, err := e.embedder.Embed(ctx, query)
	if err != nil {
		log.Printf("[memory] Failed to embed query: %v", err)
		return nil, err
	}

	// Search for similar memories
	results, err := e.store.Search(queryEmb, userID, topK, e.cfg.MinScore)
	if err != nil {
		log.Printf("[memory] Search failed: %v", err)
		return nil, err
	}

	if len(results) > 0 {
		log.Printf("[memory] Recalled %d memories for user %s (query: %s)",
			len(results), userID, truncate(query, 50))
	}

	return results, nil
}

// ProcessConversation extracts facts from a conversation and stores them.
// This runs AFTER the LLM response, asynchronously.
func (e *MemoryEngine) ProcessConversation(ctx context.Context, userID string, messages []providers.Message) {
	// Prevent concurrent processing for the same user
	if _, loaded := e.processing.LoadOrStore(userID, true); loaded {
		return
	}
	defer e.processing.Delete(userID)

	// Use a separate context with timeout for background processing
	processCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Step 1: Extract facts
	facts, err := e.extractor.Extract(processCtx, messages)
	if err != nil {
		log.Printf("[memory] Extraction failed for user %s: %v", userID, err)
		return
	}

	if len(facts) == 0 {
		return
	}

	log.Printf("[memory] Processing %d extracted facts for user %s", len(facts), userID)

	// Step 2: For each fact, embed → search similar → consolidate → store
	for _, fact := range facts {
		if err := e.processFact(processCtx, userID, fact); err != nil {
			log.Printf("[memory] Failed to process fact '%s': %v", truncate(fact.Content, 50), err)
		}
	}

	// Step 3: Prune if over limit
	if _, err := e.store.Prune(userID, e.cfg.MaxMemories); err != nil {
		log.Printf("[memory] Prune failed for user %s: %v", userID, err)
	}
}

// processFact handles a single extracted fact through the consolidation pipeline.
func (e *MemoryEngine) processFact(ctx context.Context, userID string, fact ExtractedFact) error {
	// Embed the fact
	embedding, err := e.embedder.Embed(ctx, fact.Content)
	if err != nil {
		return fmt.Errorf("embedding failed: %w", err)
	}

	// Search for similar existing memories
	similar, err := e.store.Search(embedding, userID, 3, 0.5) // higher threshold for consolidation
	if err != nil {
		return fmt.Errorf("similarity search failed: %w", err)
	}

	// Consolidate with existing memories
	result, err := e.consolidator.Consolidate(ctx, fact.Content, similar)
	if err != nil {
		return fmt.Errorf("consolidation failed: %w", err)
	}

	// Execute the action
	switch result.Action {
	case ActionAdd:
		item := &MemoryItem{
			UserID:    userID,
			Content:   fact.Content,
			Category:  fact.Category,
			Embedding: embedding,
			Score:     fact.Importance,
		}
		return e.store.Add(item)

	case ActionUpdate:
		if result.TargetID == "" || result.MergedContent == "" {
			// Fallback to ADD if target/content missing
			item := &MemoryItem{
				UserID:    userID,
				Content:   fact.Content,
				Category:  fact.Category,
				Embedding: embedding,
				Score:     fact.Importance,
			}
			return e.store.Add(item)
		}
		// Re-embed the merged content
		newEmb, err := e.embedder.Embed(ctx, result.MergedContent)
		if err != nil {
			return fmt.Errorf("re-embedding failed: %w", err)
		}
		return e.store.Update(result.TargetID, result.MergedContent, newEmb)

	case ActionDelete:
		if result.TargetID != "" {
			return e.store.Delete(result.TargetID)
		}

	case ActionNoop:
		log.Printf("[memory] NOOP: %s (%s)", truncate(fact.Content, 50), result.Reason)
	}

	return nil
}

// GetStats returns memory statistics for a user.
func (e *MemoryEngine) GetStats(userID string) (*MemoryStats, error) {
	return e.store.GetStats(userID)
}

// Close shuts down the memory engine.
func (e *MemoryEngine) Close() error {
	if e.store != nil {
		return e.store.Close()
	}
	return nil
}
