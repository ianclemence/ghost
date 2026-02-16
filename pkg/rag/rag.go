package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
)

type Store struct {
	db       *db.DB
	provider providers.EmbeddingProvider
}

type SearchResult struct {
	Content    string  `json:"content"`
	Score      float32 `json:"score"`
	Source     string  `json:"source"`
	CreatedAt  time.Time
}

func NewStore(database *db.DB, provider providers.EmbeddingProvider) *Store {
	return &Store{
		db:       database,
		provider: provider,
	}
}

// Ingest chunks text and stores embeddings
func (s *Store) Ingest(ctx context.Context, content string, source string) error {
	// Simple chunking by paragraphs or max length
	// For now, just split by newlines or use the whole text if short
	chunks := splitText(content, 500) // 500 chars approx

	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}

		embedding, err := s.provider.Embed(ctx, chunk)
		if err != nil {
			return fmt.Errorf("failed to embed chunk: %w", err)
		}

		embeddingJSON, err := json.Marshal(embedding)
		if err != nil {
			return fmt.Errorf("failed to marshal embedding: %w", err)
		}

		id := uuid.New().String()
		_, err = s.db.Exec(`
			INSERT INTO memory_chunks (id, content, embedding, source, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, id, chunk, embeddingJSON, source, time.Now())

		if err != nil {
			return fmt.Errorf("failed to store chunk: %w", err)
		}
	}

	return nil
}

// Retrieve finds relevant chunks
func (s *Store) Retrieve(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	queryEmbedding, err := s.provider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// Fetch all chunks (naive implementation for small scale)
	// For larger scale, use an index or vector extension
	rows, err := s.db.Query(`SELECT content, embedding, source, created_at FROM memory_chunks`)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chunks: %w", err)
	}
	defer rows.Close()

	var results []SearchResult

	for rows.Next() {
		var content string
		var embeddingJSON []byte
		var source string
		var createdAt time.Time

		if err := rows.Scan(&content, &embeddingJSON, &source, &createdAt); err != nil {
			continue
		}

		var embedding []float32
		if err := json.Unmarshal(embeddingJSON, &embedding); err != nil {
			continue
		}

		score := cosineSimilarity(queryEmbedding, embedding)
		if score > 0.7 { // Threshold
			results = append(results, SearchResult{
				Content:   content,
				Score:     score,
				Source:    source,
				CreatedAt: createdAt,
			})
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

func splitText(text string, chunkSize int) []string {
	if len(text) <= chunkSize {
		return []string{text}
	}
	
	var chunks []string
	var currentChunk strings.Builder
	
	words := strings.Fields(text)
	for _, word := range words {
		if currentChunk.Len()+len(word)+1 > chunkSize {
			chunks = append(chunks, currentChunk.String())
			currentChunk.Reset()
		}
		if currentChunk.Len() > 0 {
			currentChunk.WriteString(" ")
		}
		currentChunk.WriteString(word)
	}
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}
	
	return chunks
}
