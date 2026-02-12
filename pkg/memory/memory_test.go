package memory

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []float32
		expected float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
		},
		{
			name:     "similar vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 4},
			expected: 0.99,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
		},
		{
			name:     "different length",
			a:        []float32{1, 2},
			b:        []float32{1, 2, 3},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CosineSimilarity(tt.a, tt.b)
			if math.Abs(result-tt.expected) > 0.02 {
				t.Errorf("CosineSimilarity(%v, %v) = %f, want ~%f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMemoryStore_AddAndSearch(t *testing.T) {
	// Create temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_memory.db")

	store, err := NewMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Add some memories with embeddings
	memories := []MemoryItem{
		{
			UserID:    "user1",
			Content:   "User likes black coffee",
			Category:  CategoryPreference,
			Embedding: []float32{0.8, 0.2, 0.1, 0.0},
			Score:     0.8,
		},
		{
			UserID:    "user1",
			Content:   "User is a Go developer",
			Category:  CategoryFact,
			Embedding: []float32{0.1, 0.9, 0.1, 0.0},
			Score:     0.9,
		},
		{
			UserID:    "user1",
			Content:   "User lives in Vietnam",
			Category:  CategoryFact,
			Embedding: []float32{0.1, 0.1, 0.9, 0.0},
			Score:     0.7,
		},
		{
			UserID:    "user2",
			Content:   "Another user's memory",
			Category:  CategoryFact,
			Embedding: []float32{0.5, 0.5, 0.5, 0.0},
			Score:     0.5,
		},
	}

	for _, m := range memories {
		if err := store.Add(&m); err != nil {
			t.Fatalf("Failed to add memory: %v", err)
		}
	}

	// Search for coffee-related memories
	queryEmb := []float32{0.9, 0.1, 0.0, 0.0} // similar to coffee
	results, err := store.Search(queryEmb, "user1", 2, 0.0)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Expected search results, got none")
	}

	// The coffee memory should be most similar
	if results[0].Item.Content != "User likes black coffee" {
		t.Errorf("Expected coffee memory first, got: %s", results[0].Item.Content)
	}

	// Should not return user2's memories
	for _, r := range results {
		if r.Item.UserID != "user1" {
			t.Errorf("Got memory for wrong user: %s", r.Item.UserID)
		}
	}
}

func TestMemoryStore_Update(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_memory.db")

	store, err := NewMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	item := &MemoryItem{
		UserID:    "user1",
		Content:   "User likes tea",
		Category:  CategoryPreference,
		Embedding: []float32{0.5, 0.5, 0.0},
		Score:     0.6,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Failed to add: %v", err)
	}

	// Update content
	newEmb := []float32{0.6, 0.6, 0.1}
	if err := store.Update(item.ID, "User likes green tea specifically", newEmb); err != nil {
		t.Fatalf("Failed to update: %v", err)
	}

	// Verify update
	items, _ := store.GetByUser("user1")
	if len(items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(items))
	}
	if items[0].Content != "User likes green tea specifically" {
		t.Errorf("Content not updated: %s", items[0].Content)
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_memory.db")

	store, err := NewMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	item := &MemoryItem{
		UserID:    "user1",
		Content:   "Temporary fact",
		Category:  CategoryContext,
		Embedding: []float32{0.5, 0.5},
		Score:     0.3,
	}
	if err := store.Add(item); err != nil {
		t.Fatalf("Failed to add: %v", err)
	}

	// Delete
	if err := store.Delete(item.ID); err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Should no longer appear in results
	items, _ := store.GetByUser("user1")
	if len(items) != 0 {
		t.Errorf("Expected 0 items after delete, got %d", len(items))
	}

	// Should not appear in search
	results, _ := store.Search([]float32{0.5, 0.5}, "user1", 5, 0.0)
	if len(results) != 0 {
		t.Errorf("Expected 0 search results after delete, got %d", len(results))
	}
}

func TestMemoryStore_Prune(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_memory.db")

	store, err := NewMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// Add 5 memories with varying scores
	for i := 0; i < 5; i++ {
		item := &MemoryItem{
			UserID:    "user1",
			Content:   "Memory " + string(rune('A'+i)),
			Category:  CategoryFact,
			Embedding: []float32{float32(i) * 0.2, 0.5},
			Score:     float64(i) * 0.2,
		}
		store.Add(item)
	}

	// Prune to keep only 3
	deleted, err := store.Prune("user1", 3)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	if deleted != 2 {
		t.Errorf("Expected 2 deleted, got %d", deleted)
	}

	remaining, _ := store.GetByUser("user1")
	if len(remaining) != 3 {
		t.Errorf("Expected 3 remaining, got %d", len(remaining))
	}
}

func TestMemoryStore_GetStats(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_memory.db")

	store, err := NewMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	store.Add(&MemoryItem{UserID: "user1", Content: "pref1", Category: CategoryPreference, Embedding: []float32{0.1}, Score: 0.5})
	store.Add(&MemoryItem{UserID: "user1", Content: "pref2", Category: CategoryPreference, Embedding: []float32{0.2}, Score: 0.5})
	store.Add(&MemoryItem{UserID: "user1", Content: "fact1", Category: CategoryFact, Embedding: []float32{0.3}, Score: 0.5})

	stats, err := store.GetStats("user1")
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.TotalCount != 3 {
		t.Errorf("Expected total 3, got %d", stats.TotalCount)
	}
	if stats.Categories[CategoryPreference] != 2 {
		t.Errorf("Expected 2 preferences, got %d", stats.Categories[CategoryPreference])
	}
	if stats.Categories[CategoryFact] != 1 {
		t.Errorf("Expected 1 fact, got %d", stats.Categories[CategoryFact])
	}
}

func TestEmbeddingEncoding(t *testing.T) {
	original := []float32{0.1, 0.2, 0.3, -0.5, 1.0, 0.0}

	encoded := encodeEmbedding(original)
	decoded := decodeEmbedding(encoded)

	if len(decoded) != len(original) {
		t.Fatalf("Length mismatch: %d vs %d", len(decoded), len(original))
	}

	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("Mismatch at index %d: %f vs %f", i, decoded[i], original[i])
		}
	}
}

func TestEmbeddingEncoding_Empty(t *testing.T) {
	if encoded := encodeEmbedding(nil); encoded != nil {
		t.Error("Expected nil for nil input")
	}
	if decoded := decodeEmbedding(nil); decoded != nil {
		t.Error("Expected nil for nil input")
	}
}

func TestMemoryStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_persist.db")

	// Create and add memory
	store1, err := NewMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	store1.Add(&MemoryItem{
		UserID:    "user1",
		Content:   "Persisted memory",
		Category:  CategoryFact,
		Embedding: []float32{0.5, 0.5},
		Score:     0.8,
	})
	store1.Close()

	// Reopen and verify
	store2, err := NewMemoryStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to reopen store: %v", err)
	}
	defer store2.Close()

	items, _ := store2.GetByUser("user1")
	if len(items) != 1 {
		t.Fatalf("Expected 1 persisted item, got %d", len(items))
	}
	if items[0].Content != "Persisted memory" {
		t.Errorf("Content mismatch: %s", items[0].Content)
	}

	// Verify file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file should exist")
	}
}
