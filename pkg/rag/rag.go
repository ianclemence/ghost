package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/philippgille/chromem-go"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
)

type Store struct {
	db         *db.DB
	provider   providers.EmbeddingProvider
	chromemDB  *chromem.DB
	collection *chromem.Collection
	config     config.RAGConfig
	mu         sync.RWMutex
	ready      bool
}

type SearchResult struct {
	Content   string    `json:"content"`
	Score     float32   `json:"score"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

func NewStore(database *db.DB, provider providers.EmbeddingProvider, cfg config.RAGConfig) *Store {
	// Initialize in-memory vector DB
	chromemDB := chromem.NewDB()
	// Create collection without an embedder since we provide embeddings manually
	collection, err := chromemDB.CreateCollection("memory", nil, nil)
	if err != nil {
		// Should not happen for in-memory DB
		logger.ErrorCF("rag", "Failed to create vector collection", map[string]interface{}{"error": err.Error()})
	}

	return &Store{
		db:         database,
		provider:   provider,
		chromemDB:  chromemDB,
		collection: collection,
		config:     cfg,
	}
}

// LoadIndex populates the vector index from the SQLite database
func (s *Store) LoadIndex(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.collection == nil {
		return fmt.Errorf("vector collection not initialized")
	}

	logger.InfoC("rag", "Loading RAG index from database...")
	start := time.Now()

	rows, err := s.db.Query(`SELECT id, embedding FROM memory_chunks`)
	if err != nil {
		return fmt.Errorf("failed to fetch chunks for indexing: %w", err)
	}
	defer rows.Close()

	count := 0
	// Batch load
	var ids []string
	var embeddings [][]float32

	for rows.Next() {
		var id string
		var embeddingJSON []byte

		if err := rows.Scan(&id, &embeddingJSON); err != nil {
			logger.ErrorCF("rag", "Failed to scan row during indexing", map[string]interface{}{"error": err.Error()})
			continue
		}

		var embedding []float32
		if err := json.Unmarshal(embeddingJSON, &embedding); err != nil {
			logger.ErrorCF("rag", "Failed to unmarshal embedding", map[string]interface{}{"id": id, "error": err.Error()})
			continue
		}

		ids = append(ids, id)
		embeddings = append(embeddings, embedding)
		count++
	}

	if len(ids) > 0 {
		// Add to vector index in one go
		if err := s.collection.Add(ctx, ids, embeddings, nil, nil); err != nil {
			return fmt.Errorf("failed to add batch to index: %w", err)
		}
	}

	s.ready = true
	logger.InfoCF("rag", "RAG index loaded", map[string]interface{}{
		"items":    count,
		"duration": time.Since(start).String(),
	})

	return nil
}

// Ingest chunks text and stores embeddings
func (s *Store) Ingest(ctx context.Context, content string, source string) error {
	// Simple chunking by paragraphs or max length
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

		// Update vector index
		s.mu.Lock()
		if s.collection != nil {
			// Ignore error for now, or log it
			_ = s.collection.Add(ctx, []string{id}, [][]float32{embedding}, nil, nil)
		}
		s.mu.Unlock()
	}

	return nil
}

// Retrieve finds relevant chunks using vector index
func (s *Store) Retrieve(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	s.mu.RLock()
	isReady := s.ready
	collection := s.collection
	s.mu.RUnlock()

	// If index is not ready, return empty or fallback.
	// For now, we assume LoadIndex is called on startup.
	if !isReady || collection == nil {
		logger.WarnC("rag", "Index not ready, returning empty results")
		return []SearchResult{}, nil
	}

	queryEmbedding, err := s.provider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// Search vector index
	results, err := collection.QueryEmbedding(ctx, queryEmbedding, limit, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query vector index: %w", err)
	}
	
	if len(results) == 0 {
		return []SearchResult{}, nil
	}

	// Fetch content for the found IDs
	ids := make([]string, len(results))
	idToScore := make(map[string]float32)

	for i, res := range results {
		ids[i] = res.ID
		idToScore[res.ID] = res.Similarity
	}

	// Construct query to fetch content
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	querySQL := fmt.Sprintf(`SELECT id, content, source, created_at FROM memory_chunks WHERE id IN (%s)`, strings.Join(placeholders, ","))
	rows, err := s.db.Query(querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chunks content: %w", err)
	}
	defer rows.Close()

	var finalResults []SearchResult
	for rows.Next() {
		var id string
		var content string
		var source string
		var createdAt time.Time

		if err := rows.Scan(&id, &content, &source, &createdAt); err != nil {
			continue
		}

		finalResults = append(finalResults, SearchResult{
			Content:   content,
			Score:     idToScore[id],
			Source:    source,
			CreatedAt: createdAt,
		})
	}

	// Sort results by score (descending)
	sort.Slice(finalResults, func(i, j int) bool {
		return finalResults[i].Score > finalResults[j].Score
	})

	return finalResults, nil
}

func splitText(text string, chunkSize int) []string {
	if len(text) <= chunkSize {
		return []string{text}
	}
	var chunks []string
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}
