package memory

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/ntminh611/mclaw/pkg/config"
	"github.com/ntminh611/mclaw/pkg/logger"
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
// providerGetter and modelGetter are used to dynamically resolve the current
// active provider and model (e.g. from ModelSwitcher for fallback support).
func NewMemoryEngine(cfg *config.Config, providerGetter func() providers.LLMProvider, modelGetter func() string) (*MemoryEngine, error) {
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

	// Determine provider/model for extraction and consolidation.
	// If extract_model is explicitly set, create a dedicated provider for it.
	// Otherwise, use the dynamic getters from ModelSwitcher for fallback support.
	var extractor *Extractor
	var consolidator *Consolidator

	if memCfg.ExtractModel != "" {
		// Dedicated model for memory operations — independent of agent's model
		dedProvider, err := providers.CreateProviderForModel(cfg, memCfg.ExtractModel)
		if err != nil {
			logger.WarnC("memory", fmt.Sprintf("Failed to create provider for extract_model %s, falling back to agent model: %v", memCfg.ExtractModel, err))
			extractor = NewExtractor(providerGetter, modelGetter)
			consolidator = NewConsolidator(providerGetter, modelGetter)
		} else {
			extractModel := memCfg.ExtractModel
			staticProvider := func() providers.LLMProvider { return dedProvider }
			staticModel := func() string { return extractModel }
			extractor = NewExtractor(staticProvider, staticModel)
			consolidator = NewConsolidator(staticProvider, staticModel)
			logger.InfoC("memory", fmt.Sprintf("Using dedicated extract_model: %s", extractModel))
		}
	} else {
		// Dynamic — follows ModelSwitcher (agent's current active model)
		extractor = NewExtractor(providerGetter, modelGetter)
		consolidator = NewConsolidator(providerGetter, modelGetter)
	}

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

	logger.InfoC("memory", fmt.Sprintf("Engine initialized (embedding=gemini/%s, topK=%d, minScore=%.2f)",
		geminiEmbedModel, memCfg.TopK, memCfg.MinScore))

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
		logger.WarnC("memory", fmt.Sprintf("Failed to embed query: %v", err))
		return nil, err
	}

	// Search for similar memories
	results, err := e.store.Search(queryEmb, userID, topK, e.cfg.MinScore)
	if err != nil {
		logger.WarnC("memory", fmt.Sprintf("Search failed: %v", err))
		return nil, err
	}

	if len(results) > 0 {
		logger.InfoC("memory", fmt.Sprintf("Recalled %d memories for user %s (query: %s)",
			len(results), userID, truncate(query, 50)))
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
		logger.WarnC("memory", fmt.Sprintf("Extraction failed for user %s: %v", userID, err))
		return
	}

	if len(facts) == 0 {
		return
	}

	logger.InfoC("memory", fmt.Sprintf("Processing %d extracted facts for user %s", len(facts), userID))

	// Step 2: For each fact, embed → search similar → consolidate → store
	for _, fact := range facts {
		if err := e.processFact(processCtx, userID, fact); err != nil {
			logger.WarnC("memory", fmt.Sprintf("Failed to process fact '%s': %v", truncate(fact.Content, 50), err))
		}
	}

	// Step 3: Prune if over limit
	if _, err := e.store.Prune(userID, e.cfg.MaxMemories); err != nil {
		logger.WarnC("memory", fmt.Sprintf("Prune failed for user %s: %v", userID, err))
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
		logger.InfoC("memory", fmt.Sprintf("NOOP: %s (%s)", truncate(fact.Content, 50), result.Reason))
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
