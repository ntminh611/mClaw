package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const (
	geminiEmbedModel   = "text-embedding-004"
	geminiEmbedBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

// Embedder generates vector embeddings using Gemini text-embedding-004 (free).
type Embedder struct {
	apiKey  string
	apiBase string
	client  *http.Client
}

// NewEmbedder creates a new Gemini embedding client.
// apiBase can be empty to use the default Gemini endpoint.
func NewEmbedder(apiKey, apiBase string) *Embedder {
	if apiBase == "" {
		apiBase = geminiEmbedBaseURL
	}

	return &Embedder{
		apiKey:  apiKey,
		apiBase: apiBase,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Embed generates a vector embedding for a single text using Gemini text-embedding-004.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	url := fmt.Sprintf("%s/models/%s:embedContent?key=%s",
		e.apiBase, geminiEmbedModel, e.apiKey,
	)

	body := map[string]interface{}{
		"model": fmt.Sprintf("models/%s", geminiEmbedModel),
		"content": map[string]interface{}{
			"parts": []map[string]string{
				{"text": text},
			},
		},
	}

	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gemini embedding API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Embedding struct {
			Values []float32 `json:"values"`
		} `json:"embedding"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse embedding response: %w", err)
	}

	if len(result.Embedding.Values) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	log.Printf("[memory] Embedded text (%d chars) → %d dimensions", len(text), len(result.Embedding.Values))
	return result.Embedding.Values, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := e.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text %d: %w", i, err)
		}
		results[i] = emb
	}
	return results, nil
}
