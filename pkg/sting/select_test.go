package sting

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCosineBasics(t *testing.T) {
	if got := Cosine([]float32{1, 0}, []float32{1, 0}); math.Abs(got-1) > 1e-6 {
		t.Fatalf("identical = %f", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{0, 1}); math.Abs(got) > 1e-6 {
		t.Fatalf("orthogonal = %f", got)
	}
	if got := Cosine(nil, []float32{1}); got != 0 {
		t.Fatalf("degenerate must be 0, got %f", got)
	}
	if got := Cosine([]float32{0, 0}, []float32{0, 0}); got != 0 {
		t.Fatalf("zero vectors must be 0, got %f", got)
	}
}

func TestSelectRanksAndCaps(t *testing.T) {
	tools := []ToolSchema{{Name: "b"}, {Name: "a"}, {Name: "c"}}
	vecs := map[string][]float32{
		"a": {1, 0}, "b": {0, 1}, "c": {1, 1},
	}
	got := Select([]float32{1, 0}, tools, vecs, 2)
	if len(got) != 2 || got[0].Name != "a" {
		t.Fatalf("unexpected %+v", got)
	}
	all := Select([]float32{1, 0}, tools, vecs, 0)
	if len(all) != 3 {
		t.Fatalf("k<=0 must return all, got %d", len(all))
	}
}

func TestOllamaEmbedderRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("bad path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"embedding": []float32{0.1, 0.2}})
	}))
	defer srv.Close()
	e := NewOllamaEmbedder(srv.URL, "x", 5)
	vec, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 2 || vec[0] != 0.1 {
		t.Fatalf("unexpected %v", vec)
	}
}

func TestOllamaEmbedderDown(t *testing.T) {
	e := NewOllamaEmbedder("http://127.0.0.1:1", "x", 1)
	if _, err := e.Embed(context.Background(), "hi"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFingerprintStableAndSensitive(t *testing.T) {
	a := []ToolSchema{{Name: "x", Description: "y"}}
	b := []ToolSchema{{Name: "x", Description: "y"}}
	c := []ToolSchema{{Name: "x", Description: "z"}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("same schemas must fingerprint equal")
	}
	if Fingerprint(a) == Fingerprint(c) {
		t.Fatal("changed schema must fingerprint different")
	}
}
