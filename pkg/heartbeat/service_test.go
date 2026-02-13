package heartbeat

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestHeartbeatStartStop(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) { return "ok", nil }
	hs := NewHeartbeatService(dir, handler, 1, true)

	if err := hs.Start(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !hs.IsRunning() {
		t.Fatal("expected service to be running")
	}

	hs.Stop()
	time.Sleep(50 * time.Millisecond) // let goroutine exit
	if hs.IsRunning() {
		t.Fatal("expected service to be stopped")
	}
}

func TestHeartbeatDisabledDoesNotStart(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) { return "ok", nil }
	hs := NewHeartbeatService(dir, handler, 1, false)

	if err := hs.Start(); err == nil {
		t.Fatal("expected error when starting disabled service")
	}
}

func TestHeartbeatCallsHandler(t *testing.T) {
	dir := t.TempDir()
	var called atomic.Int32
	handler := func(prompt string) (string, error) {
		called.Add(1)
		return "ok", nil
	}

	hs := NewHeartbeatService(dir, handler, 1, true)
	hs.interval = 50 * time.Millisecond
	if err := hs.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Call checkHeartbeat directly
	hs.checkHeartbeat()
	hs.checkHeartbeat()

	hs.Stop()

	count := called.Load()
	if count < 2 {
		t.Errorf("expected handler to be called at least 2 times, got %d", count)
	}
}

func TestHeartbeatNoOverlap(t *testing.T) {
	dir := t.TempDir()
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	handler := func(prompt string) (string, error) {
		current := concurrentCount.Add(1)
		for {
			old := maxConcurrent.Load()
			if current <= old {
				break
			}
			if maxConcurrent.CompareAndSwap(old, current) {
				break
			}
		}
		// Simulate slow handler
		time.Sleep(300 * time.Millisecond)
		concurrentCount.Add(-1)
		return "ok", nil
	}

	// 100ms interval with 300ms handler = should overlap without protection
	hs := NewHeartbeatService(dir, handler, 1, true)
	hs.interval = 100 * time.Millisecond // Override interval

	hs.Start()
	time.Sleep(800 * time.Millisecond)
	hs.Stop()

	max := maxConcurrent.Load()
	if max > 1 {
		t.Errorf("concurrent heartbeats detected: max=%d (overlap bug!)", max)
	}
}

func TestHeartbeatHandlerError(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) {
		return "", os.ErrPermission
	}

	hs := NewHeartbeatService(dir, handler, 1, true)
	hs.interval = 50 * time.Millisecond
	if err := hs.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	hs.checkHeartbeat()
	hs.Stop()
	// Shouldn't crash
}

// --- Note CRUD Tests ---

func TestAddAndListNotes(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) { return "ok", nil }
	hs := NewHeartbeatService(dir, handler, 1, true)

	note, err := hs.AddNote("Check stock market", "reminder")
	if err != nil {
		t.Fatalf("AddNote failed: %v", err)
	}
	if note.Content != "Check stock market" {
		t.Errorf("expected content 'Check stock market', got '%s'", note.Content)
	}
	if note.Category != "reminder" {
		t.Errorf("expected category 'reminder', got '%s'", note.Category)
	}
	if note.ID == "" {
		t.Error("expected non-empty ID")
	}

	notes := hs.ListNotes(true)
	if len(notes) != 1 {
		t.Fatalf("expected 1 note, got %d", len(notes))
	}
}

func TestAddNoteDefaultCategory(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) { return "ok", nil }
	hs := NewHeartbeatService(dir, handler, 1, true)

	note, err := hs.AddNote("test content", "")
	if err != nil {
		t.Fatalf("AddNote failed: %v", err)
	}
	if note.Category != "note" {
		t.Errorf("expected default category 'note', got '%s'", note.Category)
	}
}

func TestRemoveNote(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) { return "ok", nil }
	hs := NewHeartbeatService(dir, handler, 1, true)

	note, _ := hs.AddNote("to remove", "task")
	if !hs.RemoveNote(note.ID) {
		t.Error("expected RemoveNote to return true")
	}

	notes := hs.ListNotes(true)
	if len(notes) != 0 {
		t.Errorf("expected 0 notes after remove, got %d", len(notes))
	}

	if hs.RemoveNote("nonexistent") {
		t.Error("expected RemoveNote to return false for nonexistent")
	}
}

func TestEnableDisableNote(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) { return "ok", nil }
	hs := NewHeartbeatService(dir, handler, 1, true)

	note, _ := hs.AddNote("toggle me", "note")

	disabled := hs.EnableNote(note.ID, false)
	if disabled == nil || disabled.Enabled {
		t.Error("expected note to be disabled")
	}

	// ListNotes without disabled
	enabledNotes := hs.ListNotes(false)
	if len(enabledNotes) != 0 {
		t.Errorf("expected 0 enabled notes, got %d", len(enabledNotes))
	}

	// ListNotes with disabled
	allNotes := hs.ListNotes(true)
	if len(allNotes) != 1 {
		t.Errorf("expected 1 total note, got %d", len(allNotes))
	}

	enabled := hs.EnableNote(note.ID, true)
	if enabled == nil || !enabled.Enabled {
		t.Error("expected note to be enabled")
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) { return "ok", nil }

	// Create and add notes
	hs1 := NewHeartbeatService(dir, handler, 1, true)
	hs1.AddNote("persist me", "task")
	hs1.AddNote("and me too", "reminder")

	// Create new service instance pointing to same dir
	hs2 := NewHeartbeatService(dir, handler, 1, true)
	notes := hs2.ListNotes(true)
	if len(notes) != 2 {
		t.Fatalf("expected 2 persisted notes, got %d", len(notes))
	}
	if notes[0].Content != "persist me" {
		t.Errorf("expected first note 'persist me', got '%s'", notes[0].Content)
	}
}

func TestBuildPromptWithNotes(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) { return "ok", nil }
	hs := NewHeartbeatService(dir, handler, 1, true)

	hs.AddNote("Check stocks daily", "reminder")
	hs.AddNote("Monitor server health", "task")

	prompt := hs.buildPrompt()
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	// Should contain the note contents
	if !contains(prompt, "Check stocks daily") {
		t.Error("prompt should contain 'Check stocks daily'")
	}
	if !contains(prompt, "Monitor server health") {
		t.Error("prompt should contain 'Monitor server health'")
	}
}

func TestMigrateFromHeartbeatMD(t *testing.T) {
	dir := t.TempDir()
	handler := func(prompt string) (string, error) { return "ok", nil }

	// Create old-style HEARTBEAT.md
	memDir := filepath.Join(dir, "memory")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "HEARTBEAT.md"), []byte("# Old heartbeat content\nCheck VN stocks"), 0644)

	// New service should migrate
	hs := NewHeartbeatService(dir, handler, 1, true)
	notes := hs.ListNotes(true)
	if len(notes) != 1 {
		t.Fatalf("expected 1 migrated note, got %d", len(notes))
	}
	if notes[0].Category != "migrated" {
		t.Errorf("expected category 'migrated', got '%s'", notes[0].Category)
	}
	if !contains(notes[0].Content, "Old heartbeat content") {
		t.Error("migrated note should contain old file content")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
