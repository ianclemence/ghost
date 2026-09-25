package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/goals"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/proactive"
)

// Mentioned once → persisted → digest surfaces it: the compounding loop
// in miniature. A fact applied to structured memory must appear in the
// regenerated MEMORY.md digest and in a fresh store's context.
func TestMentionedOnceCompounds(t *testing.T) {
	ws := t.TempDir()
	ms := NewMemoryStore(ws)
	if err := os.MkdirAll(filepath.Join(ws, "personal-context"), 0755); err != nil {
		t.Fatal(err)
	}
	g, err := goals.NewStore(ws).Create("take care of school emails", "school.edu", "", []string{"email.read"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	_ = g
	// Mentioned once: a user-declared fact enters structured memory.
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := personalcontext.RawValue("Green Valley High")
	if _, err := store.Create(personalcontext.Entry{ID: "school-1", Kind: personalcontext.KindFact,
		Subject: "user", Predicate: "fact/school", Value: raw,
		Status: personalcontext.StatusCurrent, Confidence: 0.9,
		Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation,
			Kind: personalcontext.SourceUserDeclared, Ref: "m1", Timestamp: time.Now().UTC()}}}); err != nil {
		t.Fatal(err)
	}
	if err := ms.AppendToday("- [19:02] user mentioned daughter's recital Friday\n"); err != nil {
		t.Fatal(err)
	}
	if err := ms.DigestLongTerm(); err != nil {
		t.Fatalf("digest must succeed: %v", err)
	}
	fresh := NewMemoryStore(ws)
	ctx := fresh.GetMemoryContext()
	if ctx == "" {
		t.Fatal("fresh store must surface compounded context")
	}
	// The once-mentioned fact must survive the digest round-trip.
	digestData, err := os.ReadFile(filepath.Join(ws, "memory", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(digestData)), "green valley high") {
		t.Fatalf("digest must carry the once-mentioned fact:\n%s", digestData)
	}
	today := time.Now().Format("2006-01-02") + ".md"
	if _, err := os.Stat(filepath.Join(ws, "memory", today)); err != nil {
		t.Fatalf("daily note must use Muse layout %s: %v", today, err)
	}
}

// Evening reflection writes once per day with goal names, then stays quiet.
func TestEveningReflectionWritesOnce(t *testing.T) {
	ws := t.TempDir()
	al := &AgentLoop{workspace: ws}
	if _, err := goals.NewStore(ws).Create("take care of school emails", "", "", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	loc := proactive.UserLocation(nil)
	evening := time.Date(2026, 9, 13, 22, 5, 0, 0, loc)
	if !al.WriteEveningReflection(evening) {
		t.Fatal("reflection must write inside the evening window")
	}
	matches, _ := filepath.Glob(filepath.Join(ws, "dreams", "2026-*.md"))
	if len(matches) == 0 {
		t.Fatal("reflection must write a dream artifact")
	}
	data, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(data), "take care of school emails") {
		t.Fatalf("reflection must name active goals, got:\n%s", data)
	}
	if !strings.Contains(string(data), "prompt_hoisted: false") {
		t.Fatal("reflection must carry the prompt_hoisted: false leash marker")
	}
	// The leash: a reflection is recorded for review, never hoisted into the
	// prompt-facing memory notes.
	if notes, _ := filepath.Glob(filepath.Join(ws, "memory", "2026-*.md")); len(notes) != 0 {
		t.Fatalf("reflection must not write into memory/: %v", notes)
	}
	if al.WriteEveningReflection(evening.Add(5 * time.Minute)) {
		t.Fatal("reflection must not repeat the same day")
	}
}
