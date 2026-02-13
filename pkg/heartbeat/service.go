package heartbeat

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// HeartbeatNote represents an individual heartbeat item
type HeartbeatNote struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	Category    string `json:"category"` // reminder, task, note, instruction
	Enabled     bool   `json:"enabled"`
	CreatedAtMS int64  `json:"createdAtMs"`
}

// HeartbeatStore is persisted as JSON
type HeartbeatStore struct {
	Version int             `json:"version"`
	Notes   []HeartbeatNote `json:"notes"`
}

type HeartbeatService struct {
	workspace   string
	storePath   string
	store       *HeartbeatStore
	onHeartbeat func(string) (string, error)
	interval    time.Duration
	enabled     bool
	mu          sync.RWMutex
	stopChan    chan struct{}
	processing  atomic.Bool
}

func NewHeartbeatService(workspace string, onHeartbeat func(string) (string, error), intervalS int, enabled bool) *HeartbeatService {
	storePath := filepath.Join(workspace, "memory", "heartbeat_notes.json")
	hs := &HeartbeatService{
		workspace:   workspace,
		storePath:   storePath,
		onHeartbeat: onHeartbeat,
		interval:    time.Duration(intervalS) * time.Second,
		enabled:     enabled,
		stopChan:    nil, // not started
	}
	hs.loadStore()
	return hs
}

func (hs *HeartbeatService) Start() error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.running() {
		return nil
	}

	if !hs.enabled {
		return fmt.Errorf("heartbeat service is disabled")
	}

	hs.stopChan = make(chan struct{})
	go hs.runLoop()

	return nil
}

func (hs *HeartbeatService) Stop() {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if !hs.running() {
		return
	}

	close(hs.stopChan)
}

func (hs *HeartbeatService) running() bool {
	if hs.stopChan == nil {
		return false
	}
	select {
	case <-hs.stopChan:
		return false
	default:
		return true
	}
}

func (hs *HeartbeatService) IsRunning() bool {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return hs.running()
}

func (hs *HeartbeatService) runLoop() {
	ticker := time.NewTicker(hs.interval)
	defer ticker.Stop()

	for {
		select {
		case <-hs.stopChan:
			return
		case <-ticker.C:
			hs.checkHeartbeat()
		}
	}
}

func (hs *HeartbeatService) checkHeartbeat() {
	hs.mu.RLock()
	if !hs.enabled || !hs.running() {
		hs.mu.RUnlock()
		return
	}
	hs.mu.RUnlock()

	if !hs.processing.CompareAndSwap(false, true) {
		log.Printf("[heartbeat] Skipping: previous heartbeat still processing")
		return
	}
	defer hs.processing.Store(false)

	prompt := hs.buildPrompt()
	log.Printf("[heartbeat] Running heartbeat check")

	if hs.onHeartbeat != nil {
		_, err := hs.onHeartbeat(prompt)
		if err != nil {
			hs.log(fmt.Sprintf("Heartbeat error: %v", err))
			log.Printf("[heartbeat] Error: %v", err)
		} else {
			hs.log("Heartbeat completed successfully")
			log.Printf("[heartbeat] Completed successfully")
		}
	}
}

func (hs *HeartbeatService) buildPrompt() string {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	now := time.Now().Format("2006-01-02 15:04")

	var notesList string
	enabledCount := 0
	for _, note := range hs.store.Notes {
		if note.Enabled {
			enabledCount++
			notesList += fmt.Sprintf("- [%s] %s\n", note.Category, note.Content)
		}
	}

	if enabledCount == 0 {
		notesList = "(no active notes)"
	}

	prompt := fmt.Sprintf(`# Heartbeat Check

Current time: %s
Active notes (%d):

%s

Check if there are any tasks you should act on based on the notes above.
Be proactive in identifying potential issues or improvements.
`, now, enabledCount, notesList)

	return prompt
}

// --- CRUD Methods ---

func (hs *HeartbeatService) AddNote(content, category string) (*HeartbeatNote, error) {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if category == "" {
		category = "note"
	}

	note := HeartbeatNote{
		ID:          fmt.Sprintf("%d", time.Now().UnixNano()),
		Content:     content,
		Category:    category,
		Enabled:     true,
		CreatedAtMS: time.Now().UnixMilli(),
	}

	hs.store.Notes = append(hs.store.Notes, note)
	if err := hs.saveStore(); err != nil {
		return nil, err
	}

	return &note, nil
}

func (hs *HeartbeatService) RemoveNote(noteID string) bool {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	before := len(hs.store.Notes)
	var notes []HeartbeatNote
	for _, note := range hs.store.Notes {
		if note.ID != noteID {
			notes = append(notes, note)
		}
	}
	hs.store.Notes = notes
	removed := len(hs.store.Notes) < before

	if removed {
		hs.saveStore()
	}

	return removed
}

func (hs *HeartbeatService) EnableNote(noteID string, enabled bool) *HeartbeatNote {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	for i := range hs.store.Notes {
		if hs.store.Notes[i].ID == noteID {
			hs.store.Notes[i].Enabled = enabled
			hs.saveStore()
			return &hs.store.Notes[i]
		}
	}

	return nil
}

func (hs *HeartbeatService) ListNotes(includeDisabled bool) []HeartbeatNote {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	if includeDisabled {
		result := make([]HeartbeatNote, len(hs.store.Notes))
		copy(result, hs.store.Notes)
		return result
	}

	var enabled []HeartbeatNote
	for _, note := range hs.store.Notes {
		if note.Enabled {
			enabled = append(enabled, note)
		}
	}
	return enabled
}

// --- Store Persistence ---

func (hs *HeartbeatService) loadStore() {
	hs.store = &HeartbeatStore{
		Version: 1,
		Notes:   []HeartbeatNote{},
	}

	data, err := os.ReadFile(hs.storePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[heartbeat] Error loading store: %v", err)
		}
		// Try to migrate from old HEARTBEAT.md
		hs.migrateFromFile()
		return
	}

	if err := json.Unmarshal(data, hs.store); err != nil {
		log.Printf("[heartbeat] Error parsing store: %v", err)
	}
}

func (hs *HeartbeatService) migrateFromFile() {
	oldFile := filepath.Join(hs.workspace, "memory", "HEARTBEAT.md")
	data, err := os.ReadFile(oldFile)
	if err != nil || len(data) == 0 {
		return
	}

	content := string(data)
	hs.store.Notes = append(hs.store.Notes, HeartbeatNote{
		ID:          fmt.Sprintf("%d", time.Now().UnixNano()),
		Content:     content,
		Category:    "migrated",
		Enabled:     true,
		CreatedAtMS: time.Now().UnixMilli(),
	})

	if err := hs.saveStore(); err == nil {
		log.Printf("[heartbeat] Migrated HEARTBEAT.md content to notes store")
	}
}

func (hs *HeartbeatService) saveStore() error {
	dir := filepath.Dir(hs.storePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(hs.store, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(hs.storePath, data, 0644)
}

func (hs *HeartbeatService) log(message string) {
	logFile := filepath.Join(hs.workspace, "memory", "heartbeat.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f.WriteString(fmt.Sprintf("[%s] %s\n", timestamp, message))
}
