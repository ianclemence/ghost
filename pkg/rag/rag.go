package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/retrieval"
	"github.com/philippgille/chromem-go"
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

// Reset drops the in-memory vector index (fresh-install state). The DB rows
// are deleted separately; without this, Retrieve keeps serving embeddings
// for deleted memories until restart.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chromemDB = chromem.NewDB()
	collection, err := s.chromemDB.CreateCollection("memory", nil, nil)
	if err != nil {
		logger.ErrorCF("rag", "Failed to recreate vector collection", map[string]interface{}{"error": err.Error()})
		return
	}
	s.collection = collection
	s.ready = false
}

// scopeTagSep separates a chunk's source from its scope tag. Chunks
// written from a context other than personal carry the source followed by
// this separator and the scope (e.g. "memory_tool@context:work"). Scope
// tags are plain strings, never secrets.
const scopeTagSep = "@"

// Ingest chunks text and stores embeddings (global/shared memory).
func (s *Store) Ingest(ctx context.Context, content string, source string) error {
	return s.IngestScoped(ctx, content, source, "")
}

// IngestScoped chunks text and stores embeddings tagged with an optional
// scope. A scoped chunk is only retrievable by callers authorized for that
// scope; an empty scope stores a global (shared) memory.
func (s *Store) IngestScoped(ctx context.Context, content string, source, scope string) error {
	tagged := source
	if scope != "" {
		tagged = source + scopeTagSep + scope
	}
	return s.ingest(ctx, content, tagged)
}

func (s *Store) ingest(ctx context.Context, content string, source string) error {
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

// ForgetValue removes vector chunks whose content contains the retired
// value (case-insensitive), from both SQLite and the live index, so a
// forgotten fact stops surfacing in recall. Only values of 3+ runes are
// honored — particles must never drive deletes. Best-effort on the index:
// a stale vector without its row is unreachable through Retrieve.
func (s *Store) ForgetValue(ctx context.Context, value string) (int, error) {
	v := strings.TrimSpace(value)
	if len([]rune(v)) < 3 {
		return 0, nil
	}
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(v)
	rows, err := s.db.Query(`SELECT id FROM memory_chunks WHERE content LIKE ? ESCAPE '\'`, "%"+esc+"%")
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	if _, err := s.db.Exec(`DELETE FROM memory_chunks WHERE id IN (`+placeholders+`)`, args...); err != nil {
		return 0, err
	}
	s.mu.Lock()
	if s.collection != nil {
		_ = s.collection.Delete(ctx, nil, nil, ids...)
	}
	s.mu.Unlock()
	return len(ids), nil
}
func (s *Store) Retrieve(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return s.RetrieveScoped(ctx, query, limit, nil)
}

// RetrieveScoped finds relevant chunks the caller is allowed to see. A nil
// or empty scopes set means global visibility (legacy/unwired behavior);
// otherwise a chunk is visible only when it is global (untagged) or tagged
// with one of the caller's scopes. Cross-context facts never reach a
// context that does not own them.
func (s *Store) RetrieveScoped(ctx context.Context, query string, limit int, scopes []string) ([]SearchResult, error) {
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

	// Over-fetch so scope filtering cannot starve a legitimate query.
	fetchN := limit * 5
	if fetchN < 10 {
		fetchN = 10
	}
	if fetchN > 100 {
		fetchN = 100
	}

	// Search vector index
	results, err := collection.QueryEmbedding(ctx, queryEmbedding, fetchN, nil, nil)
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

	visible := func(source string) bool {
		if len(scopes) == 0 {
			return true
		}
		tag := ""
		if idx := strings.Index(source, scopeTagSep); idx >= 0 {
			tag = source[idx+len(scopeTagSep):]
		}
		if tag == "" {
			return true // untagged (global/shared) memory
		}
		for _, sc := range scopes {
			if tag == sc {
				return true
			}
		}
		return false
	}

	var finalResults []SearchResult
	for rows.Next() {
		var id string
		var content string
		var source string
		var createdAt time.Time

		if err := rows.Scan(&id, &content, &source, &createdAt); err != nil {
			continue
		}
		if !visible(source) {
			continue
		}

		finalResults = append(finalResults, SearchResult{
			Content:   content,
			Score:     idToScore[id],
			Source:    source,
			CreatedAt: createdAt,
		})
	}

	// Hierarchical second stage: the vector index produced a coarse candidate
	// pool; a deterministic weighted score (semantic + lexical + recency +
	// source quality, minus staleness/contradiction) narrows it to the final
	// context. This keeps ranking inspectable rather than purely embedding-
	// similarity ordered.
	finalResults = rankResults(finalResults, query, limit)

	return finalResults, nil
}

// trustForSource maps a chunk's ingest source to retrieval trust.
// Model-written memory is inference; web-derived content is untrusted;
// anything else keeps the conservative unknown. The mapping is prefix
// based so scope tags (source@scope) never affect it.
func trustForSource(source string) string {
	base := source
	if idx := strings.Index(source, scopeTagSep); idx >= 0 {
		base = source[:idx]
	}
	if strings.Contains(base, "+web") || strings.HasPrefix(base, "web") ||
		strings.HasPrefix(base, "fetch") || strings.HasPrefix(base, "scrape") {
		return "untrusted"
	}
	switch base {
	case "memory_tool", "journal", "curate", "auto_journal":
		return "inferred"
	}
	return "unknown"
}

// rankResults re-orders a coarse candidate pool with the deterministic
// retrieval scorer and truncates to limit. It never drops a candidate's
// provenance; it only orders and bounds.
func rankResults(results []SearchResult, query string, limit int) []SearchResult {
	if len(results) == 0 {
		return results
	}
	now := time.Now()
	cands := make([]retrieval.Candidate, 0, len(results))
	byID := make(map[string]SearchResult, len(results))
	for i, r := range results {
		id := fmt.Sprintf("r%d", i)
		byID[id] = r
		cands = append(cands, retrieval.Candidate{
			ID:         id,
			Content:    r.Content,
			Source:     r.Source,
			CreatedAt:  r.CreatedAt,
			Similarity: float64(r.Score),
			Trust:      trustForSource(r.Source),
		})
	}
	ranked := retrieval.Rank(cands, query, now, retrieval.DefaultWeights(), limit)
	out := make([]SearchResult, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, byID[r.Candidate.ID])
	}
	return out
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
