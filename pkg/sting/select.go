package sting

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Embedding pre-selection is Sting's zero-dependency Jev: tool
// selection over a small set needs no generation. Embed the query with
// the on-device embedder (nomic via Ollama, already deployed for
// RAG), cosine-match against embedded tool descriptions, and the top-k
// enter the generative grammar — the parallel-scored single-pass
// switch, with zero new artifacts and zero training. The generative
// call then only fills arguments; the strict gate stays as the
// deterministic per-field verifier.
//
// This package stays stdlib-only: the Ollama client below is plain
// net/http, no SDK.

// Embedder turns text into a vector. Implemented by OllamaEmbedder;
// fakes implement it in tests.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// OllamaEmbedder speaks Ollama's /api/embeddings over loopback.
type OllamaEmbedder struct {
	base  string
	model string
	http  *http.Client
}

// NewOllamaEmbedder builds a client. Empty base means localhost:11434;
// empty model means nomic-embed-text (the RAG default Ghost already
// ships); non-positive timeout uses 60s (cold model load is slow once,
// warm embeds are fast).
func NewOllamaEmbedder(baseURL, model string, timeoutSecs int) *OllamaEmbedder {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "http://localhost:11434"
	}
	if strings.TrimSpace(model) == "" {
		model = "nomic-embed-text"
	}
	if timeoutSecs <= 0 {
		timeoutSecs = 60
	}
	return &OllamaEmbedder{
		base:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		model: strings.TrimSpace(model),
		http:  &http.Client{Timeout: time.Duration(timeoutSecs) * time.Second},
	}
}

// Embed returns the vector for one text.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]string{"model": e.model, "prompt": text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sting: embedder unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sting: embedder status %d", resp.StatusCode)
	}
	var out struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("sting: decode embedding: %w", err)
	}
	if len(out.Embedding) == 0 {
		return nil, fmt.Errorf("sting: empty embedding")
	}
	return out.Embedding, nil
}

// Cosine returns cosine similarity in [-1, 1]; 0 on degenerate input
// (never NaN — a selector must not poison rankings).
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// RankedTool is one tool with its selection score.
type RankedTool struct {
	Name  string
	Score float64
}

// Select ranks tools by cosine similarity between the query vector and
// each tool vector, returning the top-k (k<=0 means all, still ranked).
// Ties break by name for determinism.
func Select(queryVec []float32, tools []ToolSchema, toolVecs map[string][]float32, k int) []RankedTool {
	ranked := make([]RankedTool, 0, len(tools))
	for _, t := range tools {
		ranked = append(ranked, RankedTool{Name: t.Name, Score: Cosine(queryVec, toolVecs[t.Name])})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score == ranked[j].Score {
			return ranked[i].Name < ranked[j].Name
		}
		return ranked[i].Score > ranked[j].Score
	})
	if k > 0 && len(ranked) > k {
		ranked = ranked[:k]
	}
	return ranked
}

// ToolText is the embedded surface per tool: name plus description.
// Names carry the verb ("search", "convert"); descriptions carry the
// domain ("weather", "currency"). Both matter for matching.
func ToolText(t ToolSchema) string {
	return strings.TrimSpace(t.Name + " " + t.Description)
}

// Fingerprint keys embedding caches: same schemas, same key. A changed
// schema re-embeds only what changed (compare per-tool hashes).
func Fingerprint(schemas []ToolSchema) string {
	names := make([]string, 0, len(schemas))
	byName := map[string]ToolSchema{}
	for _, s := range schemas {
		names = append(names, s.Name)
		byName[s.Name] = s
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		s := byName[n]
		fmt.Fprintf(h, "%s\x00%s\x00%v\x00", s.Name, s.Description, s.Parameters)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
