package agent

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/ntminh611/mclaw/pkg/config"
	"github.com/ntminh611/mclaw/pkg/providers"
)

// ModelSwitcher manages automatic model fallback on 429 rate limit errors.
// When the primary model is rate-limited, it switches to fallback models.
// At the start of a new day (local time), it resets back to the primary model.
type ModelSwitcher struct {
	cfg             *config.Config
	primaryModel    string
	fallbackModels  []string
	currentModel    string
	currentProvider providers.LLMProvider
	rateLimitDay    int // day of year when rate limit was hit (-1 = not rate limited)
	mu              sync.RWMutex
}

// NewModelSwitcher creates a new ModelSwitcher with the given config and initial provider.
func NewModelSwitcher(cfg *config.Config, initialProvider providers.LLMProvider) *ModelSwitcher {
	return &ModelSwitcher{
		cfg:             cfg,
		primaryModel:    cfg.Agents.Defaults.Model,
		fallbackModels:  cfg.Agents.Defaults.FallbackModels,
		currentModel:    cfg.Agents.Defaults.Model,
		currentProvider: initialProvider,
		rateLimitDay:    -1,
	}
}

// CurrentModel returns the currently active model name.
func (ms *ModelSwitcher) CurrentModel() string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.currentModel
}

// CurrentProvider returns the currently active provider.
func (ms *ModelSwitcher) CurrentProvider() providers.LLMProvider {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.currentProvider
}

// Chat sends a chat request with automatic fallback on 429 errors.
// If the current model returns 429, it switches to the next fallback model
// and retries the request once.
func (ms *ModelSwitcher) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, options map[string]interface{}) (*providers.LLMResponse, error) {
	ms.maybeResetDaily()

	ms.mu.RLock()
	model := ms.currentModel
	provider := ms.currentProvider
	ms.mu.RUnlock()

	response, err := provider.Chat(ctx, messages, tools, model, options)
	if err == nil {
		return response, nil
	}

	// Check if it's a rate limit error
	if !providers.IsRateLimitError(err) {
		return nil, err
	}

	log.Printf("[model-switcher] Rate limit hit on model %s, attempting fallback...", model)

	// Try to switch to next model
	if !ms.switchToNext() {
		log.Printf("[model-switcher] No fallback models available, returning rate limit error")
		return nil, err
	}

	// Retry with new model
	ms.mu.RLock()
	newModel := ms.currentModel
	newProvider := ms.currentProvider
	ms.mu.RUnlock()

	log.Printf("[model-switcher] Retrying with fallback model: %s", newModel)
	return newProvider.Chat(ctx, messages, tools, newModel, options)
}

// switchToNext attempts to switch to the next available fallback model.
// Returns true if a switch was made, false if no fallback is available.
func (ms *ModelSwitcher) switchToNext() bool {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if len(ms.fallbackModels) == 0 {
		return false
	}

	// Find current model in the fallback list to determine next
	nextModel := ""
	if ms.currentModel == ms.primaryModel {
		// Switch from primary to first fallback
		nextModel = ms.fallbackModels[0]
	} else {
		// Find current position in fallback list and try next
		for i, m := range ms.fallbackModels {
			if m == ms.currentModel {
				if i+1 < len(ms.fallbackModels) {
					nextModel = ms.fallbackModels[i+1]
				}
				break
			}
		}
	}

	if nextModel == "" {
		return false
	}

	// Create provider for the new model
	provider, err := providers.CreateProviderForModel(ms.cfg, nextModel)
	if err != nil {
		log.Printf("[model-switcher] Failed to create provider for %s: %v", nextModel, err)
		return false
	}

	ms.currentModel = nextModel
	ms.currentProvider = provider
	ms.rateLimitDay = time.Now().YearDay()

	log.Printf("[model-switcher] ✅ Switched from rate-limited model to: %s", nextModel)
	return true
}

// maybeResetDaily checks if a new day has started since the last rate limit,
// and resets to the primary model if so.
func (ms *ModelSwitcher) maybeResetDaily() {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.rateLimitDay < 0 {
		return // not rate limited
	}

	today := time.Now().YearDay()
	if today == ms.rateLimitDay {
		return // same day, keep fallback
	}

	// New day — reset to primary
	if ms.currentModel == ms.primaryModel {
		ms.rateLimitDay = -1
		return
	}

	provider, err := providers.CreateProviderForModel(ms.cfg, ms.primaryModel)
	if err != nil {
		log.Printf("[model-switcher] Failed to reset to primary model %s: %v", ms.primaryModel, err)
		return
	}

	log.Printf("[model-switcher] 🔄 New day — resetting from %s back to primary model: %s", ms.currentModel, ms.primaryModel)
	ms.currentModel = ms.primaryModel
	ms.currentProvider = provider
	ms.rateLimitDay = -1
}
