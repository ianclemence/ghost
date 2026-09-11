package affect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestScoreBasics(t *testing.T) {
	v, _, c, f := Score("I love this, amazing work, thank you so much!")
	if v <= 0.2 || c < 0.3 || f {
		t.Fatalf("warm text must score positive: v=%v c=%v f=%v", v, c, f)
	}
	v, _, c, f = Score("This is terrible, I hate it, awful and broken.")
	if v >= -0.2 || f {
		t.Fatalf("cold text must score negative: v=%v f=%v", v, f)
	}
	v, _, c, _ = Score("What is the status of server 7 in rack 3?")
	if c >= MinConfidence {
		t.Fatalf("flat factual text must be low confidence: c=%v", c)
	}
	_ = v
}

func TestScoreNegation(t *testing.T) {
	plain, _, _, _ := Score("this is good")
	neg, _, _, _ := Score("this is not good")
	if neg >= plain {
		t.Fatalf("negation must flip sentiment: plain=%v neg=%v", plain, neg)
	}
}

func TestScoreFriction(t *testing.T) {
	cases := []string{
		"No, that's wrong, do it again",
		"you misunderstood me completely",
		"not what I asked for, stop that",
	}
	for _, c := range cases {
		_, _, _, f := Score(c)
		if !f {
			t.Fatalf("must detect friction: %q", c)
		}
	}
	if _, _, _, f := Score("looks good, thanks!"); f {
		t.Fatal("warm text must not flag friction")
	}
}

func TestTurnBoundsAndAsymmetry(t *testing.T) {
	now := time.Now()
	s := New()
	for i := 0; i < 100; i++ {
		s = s.Turn(0.9, 0.8, 0.9, false, now.Add(time.Duration(i)*time.Minute))
	}
	if s.Affinity != 1 || s.Valence > 1 || s.Valence < -1 {
		t.Fatalf("state must clamp: %+v", s)
	}
	s = New()
	s = s.Turn(0.9, 0.5, 0.9, false, now)
	up := s.Affinity - NeutralAffinity
	s = New()
	s = s.Turn(-0.9, 0.5, 0.9, true, now)
	down := NeutralAffinity - s.Affinity
	if down <= up {
		t.Fatalf("trust must break faster than it builds: up=%v down=%v", up, down)
	}
}

func TestDecayCools(t *testing.T) {
	now := time.Now()
	s := New()
	s = s.Turn(0.9, 0.9, 0.9, false, now)
	cooled := s.Decayed(now.Add(7 * 24 * time.Hour))
	if cooled.Valence >= s.Valence || cooled.Affinity >= s.Affinity {
		t.Fatalf("a week of silence must cool state: %+v -> %+v", s, cooled)
	}
	if cooled.Affinity < NeutralAffinity {
		t.Fatalf("affinity must cool toward neutral, not below: %+v", cooled)
	}
}

func TestLowConfidenceHoldsMood(t *testing.T) {
	now := time.Now()
	s := New()
	before := s.Valence
	s = s.Turn(-0.8, 0.5, 0.01, false, now)
	if s.Valence != before {
		t.Fatal("noise must not move mood")
	}
	if s.Affinity != NeutralAffinity {
		t.Fatal("noise must not move affinity either")
	}
}

func TestBandsAndRender(t *testing.T) {
	s := New()
	r := s.Render(time.Now())
	if !strings.Contains(r, "cordial") || !strings.Contains(r, "near neutral") {
		t.Fatalf("neutral render wrong: %q", r)
	}
	s.Affinity = 0.9
	s.Valence = 0.7
	r = s.Render(time.Now())
	if !strings.Contains(r, "close") || !strings.Contains(r, "bright") {
		t.Fatalf("warm render wrong: %q", r)
	}
}

func TestPersistenceRoundtrip(t *testing.T) {
	ws := t.TempDir()
	s := New()
	s = s.Turn(0.8, 0.6, 0.8, false, time.Now())
	if err := Save(ws, s); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(ws, "personal-context", "affect.json"))
	if strings.Contains(string(raw), "turn text") {
		t.Fatal("only aggregates persist")
	}
	got, err := Load(ws)
	if err != nil {
		t.Fatal(err)
	}
	// Load cools to now; allow epsilon for elapsed microseconds.
	if got.Turns != 1 || abs(got.Affinity-s.Affinity) > 1e-6 || abs(got.Valence-s.Valence) > 1e-6 {
		t.Fatalf("roundtrip mismatch: %+v vs %+v", got, s)
	}
	info, _ := os.Stat(Path(ws))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("affect file must be 0600, got %o", info.Mode().Perm())
	}
}

func TestLoadMissingIsNeutral(t *testing.T) {	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Affinity != NeutralAffinity || got.Turns != 0 {
		t.Fatalf("missing file must load neutral: %+v", got)
	}
}
