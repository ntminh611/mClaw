package memory

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// MemoryStore handles persistent storage of memories using SQLite.
type MemoryStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewMemoryStore creates or opens a SQLite database for memory storage.
func NewMemoryStore(dbPath string) (*MemoryStore, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open memory database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite works best with single writer
	db.SetMaxIdleConns(1)

	store := &MemoryStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate memory database: %w", err)
	}

	log.Printf("[memory] Store initialized at %s", dbPath)
	return store, nil
}

// migrate creates the memories table if it doesn't exist.
func (s *MemoryStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS memories (
		id          TEXT PRIMARY KEY,
		user_id     TEXT NOT NULL,
		content     TEXT NOT NULL,
		category    TEXT NOT NULL DEFAULT 'fact',
		embedding   BLOB,
		score       REAL NOT NULL DEFAULT 0.5,
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		access_cnt  INTEGER NOT NULL DEFAULT 0,
		deleted     INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_memories_user ON memories(user_id, deleted);
	CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(user_id, category, deleted);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Add inserts a new memory item into the store.
func (s *MemoryStore) Add(item *MemoryItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if item.ID == "" {
		item.ID = uuid.New().String()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	item.UpdatedAt = time.Now()

	embBlob := encodeEmbedding(item.Embedding)

	_, err := s.db.Exec(
		`INSERT INTO memories (id, user_id, content, category, embedding, score, created_at, updated_at, access_cnt)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.UserID, item.Content, item.Category, embBlob,
		item.Score, item.CreatedAt, item.UpdatedAt, item.AccessCnt,
	)
	if err != nil {
		return fmt.Errorf("failed to add memory: %w", err)
	}

	log.Printf("[memory] Added: [%s] %s (user=%s, score=%.2f)", item.Category, truncate(item.Content, 60), item.UserID, item.Score)
	return nil
}

// Update modifies an existing memory's content and embedding.
func (s *MemoryStore) Update(id, content string, embedding []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	embBlob := encodeEmbedding(embedding)
	result, err := s.db.Exec(
		`UPDATE memories SET content = ?, embedding = ?, updated_at = ? WHERE id = ? AND deleted = 0`,
		content, embBlob, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("failed to update memory: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("memory not found: %s", id)
	}

	log.Printf("[memory] Updated: %s → %s", id[:8], truncate(content, 60))
	return nil
}

// Delete soft-deletes a memory by ID.
func (s *MemoryStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`UPDATE memories SET deleted = 1, updated_at = ? WHERE id = ?`, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to delete memory: %w", err)
	}

	log.Printf("[memory] Deleted: %s", id[:8])
	return nil
}

// Search finds the top-K most similar memories for a given query embedding.
func (s *MemoryStore) Search(queryEmbedding []float32, userID string, topK int, minScore float64) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, user_id, content, category, embedding, score, created_at, updated_at, access_cnt
		 FROM memories WHERE user_id = ? AND deleted = 0 AND embedding IS NOT NULL`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query memories: %w", err)
	}
	defer rows.Close()

	var results []SearchResult

	for rows.Next() {
		var item MemoryItem
		var embBlob []byte

		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Content, &item.Category,
			&embBlob, &item.Score, &item.CreatedAt, &item.UpdatedAt, &item.AccessCnt,
		); err != nil {
			continue
		}

		item.Embedding = decodeEmbedding(embBlob)

		similarity := CosineSimilarity(queryEmbedding, item.Embedding)
		if similarity >= minScore {
			results = append(results, SearchResult{
				Item:       item,
				Similarity: similarity,
			})
		}
	}

	// Sort by similarity descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	// Limit to topK
	if len(results) > topK {
		results = results[:topK]
	}

	// Increment access count for returned memories
	for _, r := range results {
		go func(id string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.db.Exec(`UPDATE memories SET access_cnt = access_cnt + 1 WHERE id = ?`, id)
		}(r.Item.ID)
	}

	return results, nil
}

// GetByUser returns all active memories for a user.
func (s *MemoryStore) GetByUser(userID string) ([]MemoryItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, user_id, content, category, score, created_at, updated_at, access_cnt
		 FROM memories WHERE user_id = ? AND deleted = 0
		 ORDER BY updated_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get memories: %w", err)
	}
	defer rows.Close()

	var items []MemoryItem
	for rows.Next() {
		var item MemoryItem
		if err := rows.Scan(&item.ID, &item.UserID, &item.Content, &item.Category,
			&item.Score, &item.CreatedAt, &item.UpdatedAt, &item.AccessCnt); err != nil {
			continue
		}
		items = append(items, item)
	}

	return items, nil
}

// GetStats returns memory statistics for a user.
func (s *MemoryStore) GetStats(userID string) (*MemoryStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &MemoryStats{
		UserID:     userID,
		Categories: make(map[string]int),
	}

	rows, err := s.db.Query(
		`SELECT category, COUNT(*) FROM memories WHERE user_id = ? AND deleted = 0 GROUP BY category`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var cat string
		var count int
		if err := rows.Scan(&cat, &count); err != nil {
			continue
		}
		stats.Categories[cat] = count
		stats.TotalCount += count
	}

	return stats, nil
}

// Prune removes the lowest-value memories when a user exceeds maxItems.
func (s *MemoryStore) Prune(userID string, maxItems int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Count current memories
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM memories WHERE user_id = ? AND deleted = 0`, userID,
	).Scan(&count)
	if err != nil {
		return 0, err
	}

	if count <= maxItems {
		return 0, nil
	}

	// Delete lowest-value memories (score * log(access_cnt+1))
	toDelete := count - maxItems
	result, err := s.db.Exec(
		`UPDATE memories SET deleted = 1, updated_at = ?
		 WHERE id IN (
			SELECT id FROM memories
			WHERE user_id = ? AND deleted = 0
			ORDER BY (score * (1 + 0.1 * access_cnt)) ASC
			LIMIT ?
		 )`,
		time.Now(), userID, toDelete,
	)
	if err != nil {
		return 0, err
	}

	deleted, _ := result.RowsAffected()
	log.Printf("[memory] Pruned %d low-value memories for user %s", deleted, userID)
	return int(deleted), nil
}

// Close closes the database connection.
func (s *MemoryStore) Close() error {
	return s.db.Close()
}

// --- Encoding helpers ---

// encodeEmbedding converts a float32 slice to a byte slice for BLOB storage.
func encodeEmbedding(emb []float32) []byte {
	if len(emb) == 0 {
		return nil
	}
	buf := make([]byte, len(emb)*4)
	for i, v := range emb {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// decodeEmbedding converts a byte slice back to a float32 slice.
func decodeEmbedding(data []byte) []float32 {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}
	emb := make([]float32, len(data)/4)
	for i := range emb {
		emb[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return emb
}

// truncate safely truncates a string for logging.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
