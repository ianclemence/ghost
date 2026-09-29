package watch

import (
	"context"
	"testing"
	"time"
)

// BenchmarkDiffMeasure measures the deterministic diff of two snapshot maps.
func BenchmarkDiffMeasure(b *testing.B) {
	from := map[string]string{}
	to := map[string]string{}
	for i := 0; i < 20; i++ {
		from[formatField(i)] = formatValue(i)
		to[formatField(i)] = formatValue(i)
	}
	from["status"] = "Scheduled"
	to["status"] = "Delayed"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Diff(from, to)
	}
}

func formatField(i int) string { return "field_" + string(rune('a'+i%26)) + string(rune('a'+i/26)) }
func formatValue(i int) string { return "value_" + string(rune('a'+i%26)) + string(rune('a'+i/26)) }

// BenchmarkDetectAutomatic measures candidate extraction with guards.
func BenchmarkDetectAutomatic(b *testing.B) {
	now := time.Now()
	msg := "my flight BA123 tomorrow keeps getting delayed, keep an eye on it for me please"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Detect(msg, now, "UTC")
	}
}

// BenchmarkPolicyAllow measures the fixed-order policy gate for an automatic watch.
func BenchmarkPolicyAllow(b *testing.B) {
	p := Default()
	p.Sources = []string{"sandbox"}
	now := time.Now()
	existing := []Watch{}
	c := Candidate{
		Kind:       KindFlight,
		Entity:     "BA123",
		Label:      "Flight BA123",
		EventAt:    timePtr(now.Add(18 * time.Hour)),
		Quote:      "my flight BA123 tomorrow, keep an eye on it",
		Confidence: 0.9,
		Origin:     "deterministic",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Allow(c, existing, now, "sandbox")
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// BenchmarkSandboxFetch measures a full poll-cycle state read (no LLM, no network).
func BenchmarkSandboxFetch(b *testing.B) {
	ws := b.TempDir()
	s := NewSandbox(ws)
	w := Watch{ID: "wt-b", Kind: KindFlight, Entity: "BA123", Source: "sandbox"}
	if err := SetState(ws, KindFlight, "BA123", map[string]string{
		"status": "Scheduled", "departure": "18:30",
	}); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := s.Fetch(ctx, w); err != nil {
			b.Fatal(err)
		}
	}
}
