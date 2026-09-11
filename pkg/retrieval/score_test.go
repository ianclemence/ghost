package retrieval

import (
	"testing"
	"time"
)

func TestSourceQualityOrdering(t *testing.T) {
	if SourceQuality("canonical") <= SourceQuality("observed") {
		t.Fatal("canonical must outrank observed")
	}
	if SourceQuality("observed") <= SourceQuality("inferred") {
		t.Fatal("observed must outrank inferred")
	}
	if SourceQuality("inferred") <= SourceQuality("untrusted") {
		t.Fatal("inferred must outrank untrusted network content")
	}
	if SourceQuality("untrusted") >= SourceQuality("unknown") {
		t.Fatal("untrusted must rank below unknown legacy content")
	}
	if SourceQuality("nonsense") != SourceQuality("unknown") {
		t.Fatal("unknown trust must map to the neutral default")
	}
}

func TestScoreComponents(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	w := DefaultWeights()

	strong := Candidate{ID: "a", Content: "the user prefers quiet restaurants", Similarity: 0.9, Confidence: 0.9,
		Trust: "canonical", CreatedAt: now.Add(-time.Hour)}
	weak := Candidate{ID: "b", Content: "unrelated weather note", Similarity: 0.1,
		Trust: "inferred", CreatedAt: now.Add(-400 * 24 * time.Hour)}

	cs := Score(strong, "quiet restaurants preference", now, w)
	cw := Score(weak, "quiet restaurants preference", now, w)
	if cs.Total <= cw.Total {
		t.Fatalf("strong candidate must outrank weak: %+v vs %+v", cs, cw)
	}
	if cs.Semantic <= 0 || cs.Lexical <= 0 || cs.SourceQuality <= 0 {
		t.Fatalf("expected positive semantic/lexical/source components: %+v", cs)
	}
	if cw.Staleness <= 0 {
		t.Fatalf("old candidate must accrue staleness: %+v", cw)
	}
}

func TestSupersededPenalized(t *testing.T) {
	now := time.Now()
	w := DefaultWeights()
	base := Candidate{ID: "a", Content: "x", Similarity: 0.8, Trust: "canonical", CreatedAt: now}
	retired := base
	retired.Superseded = true
	if Score(retired, "x", now, w).Total >= Score(base, "x", now, w).Total {
		t.Fatal("superseded candidate must score lower")
	}
}

func TestRankDeterministicAndBounded(t *testing.T) {
	now := time.Now()
	w := DefaultWeights()
	cands := []Candidate{
		{ID: "b", Content: "beta", Similarity: 0.5, CreatedAt: now},
		{ID: "a", Content: "alpha", Similarity: 0.5, CreatedAt: now},
		{ID: "c", Content: "gamma", Similarity: 0.9, CreatedAt: now},
	}
	got := Rank(cands, "alpha", now, w, 2)
	if len(got) != 2 {
		t.Fatalf("limit not applied: %d", len(got))
	}
	// Ties (a,b at equal similarity) break on ID ascending.
	all := Rank(cands, "none", now, w, 0)
	if len(all) != 3 {
		t.Fatalf("limit<=0 must return all, got %d", len(all))
	}
	// Running twice yields identical order.
	again := Rank(cands, "none", now, w, 0)
	for i := range all {
		if all[i].Candidate.ID != again[i].Candidate.ID {
			t.Fatal("ranking must be deterministic")
		}
	}
}

func TestLexicalOverlap(t *testing.T) {
	if lexicalOverlap("quiet restaurants", "the user prefers quiet restaurants") <= 0.9 {
		t.Fatal("expected near-full overlap")
	}
	if lexicalOverlap("quiet restaurants", "unrelated note") != 0 {
		t.Fatal("expected zero overlap")
	}
	if lexicalOverlap("", "anything") != 0 {
		t.Fatal("empty query has no terms")
	}
}
