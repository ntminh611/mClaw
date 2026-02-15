package agent

import (
	"fmt"
	"testing"

	"github.com/ntminh611/mclaw/pkg/config"
	"github.com/ntminh611/mclaw/pkg/providers"
)

func TestModelSwitcherInit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.Defaults.Model = "gemini/gemini-3-pro"
	cfg.Agents.Defaults.FallbackModels = []string{"gemini/gemini-2.0-flash"}
	cfg.Providers.Gemini.APIKey = "test-key"

	provider, _ := providers.CreateProviderForModel(cfg, cfg.Agents.Defaults.Model)
	ms := NewModelSwitcher(cfg, provider)

	if ms.CurrentModel() != "gemini/gemini-3-pro" {
		t.Errorf("expected primary model, got %s", ms.CurrentModel())
	}
}

func TestModelSwitcherNoFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.Defaults.Model = "gemini/gemini-3-pro"
	cfg.Agents.Defaults.FallbackModels = nil
	cfg.Providers.Gemini.APIKey = "test-key"

	provider, _ := providers.CreateProviderForModel(cfg, cfg.Agents.Defaults.Model)
	ms := NewModelSwitcher(cfg, provider)

	// switchToNext should return false when no fallbacks
	if ms.switchToNext() {
		t.Error("expected switchToNext to return false with no fallback models")
	}
}

func TestModelSwitcherSwitchToFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.Defaults.Model = "gemini/gemini-3-pro"
	cfg.Agents.Defaults.FallbackModels = []string{"gemini/gemini-2.0-flash", "gemini/gemini-2.0-flash-lite"}
	cfg.Providers.Gemini.APIKey = "test-key"

	provider, _ := providers.CreateProviderForModel(cfg, cfg.Agents.Defaults.Model)
	ms := NewModelSwitcher(cfg, provider)

	// Switch to first fallback
	if !ms.switchToNext() {
		t.Fatal("expected switchToNext to succeed")
	}
	if ms.CurrentModel() != "gemini/gemini-2.0-flash" {
		t.Errorf("expected gemini-2.0-flash, got %s", ms.CurrentModel())
	}

	// Switch to second fallback
	if !ms.switchToNext() {
		t.Fatal("expected switchToNext to succeed for second fallback")
	}
	if ms.CurrentModel() != "gemini/gemini-2.0-flash-lite" {
		t.Errorf("expected gemini-2.0-flash-lite, got %s", ms.CurrentModel())
	}

	// No more fallbacks
	if ms.switchToNext() {
		t.Error("expected switchToNext to return false when all fallbacks exhausted")
	}
}

func TestModelSwitcherDailyReset(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.Defaults.Model = "gemini/gemini-3-pro"
	cfg.Agents.Defaults.FallbackModels = []string{"gemini/gemini-2.0-flash"}
	cfg.Providers.Gemini.APIKey = "test-key"

	provider, _ := providers.CreateProviderForModel(cfg, cfg.Agents.Defaults.Model)
	ms := NewModelSwitcher(cfg, provider)

	// Switch to fallback
	ms.switchToNext()
	if ms.CurrentModel() != "gemini/gemini-2.0-flash" {
		t.Fatalf("expected fallback model, got %s", ms.CurrentModel())
	}

	// Simulate day change by setting rateLimitDay to yesterday
	ms.mu.Lock()
	ms.rateLimitDay = ms.rateLimitDay - 1
	if ms.rateLimitDay < 0 {
		ms.rateLimitDay = 364 // wrap around
	}
	ms.mu.Unlock()

	// maybeResetDaily should reset to primary
	ms.maybeResetDaily()
	if ms.CurrentModel() != "gemini/gemini-3-pro" {
		t.Errorf("expected reset to primary model, got %s", ms.CurrentModel())
	}
}

func TestRateLimitErrorDetection(t *testing.T) {
	err := &providers.RateLimitError{StatusCode: 429, Body: "quota exceeded"}

	if !providers.IsRateLimitError(err) {
		t.Error("expected IsRateLimitError to return true")
	}

	regularErr := fmt.Errorf("some other error")
	if providers.IsRateLimitError(regularErr) {
		t.Error("expected IsRateLimitError to return false for non-rate-limit error")
	}
}
