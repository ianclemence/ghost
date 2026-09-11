package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

func styleEntry(predicate, value string) personalcontext.Entry {
	raw, _ := personalcontext.RawValue(value)
	return personalcontext.Entry{
		ID:        "e-" + predicate,
		Kind:      personalcontext.KindPreference,
		Subject:   "user",
		Predicate: predicate,
		Value:     raw,
		Status:    personalcontext.StatusCurrent,
		Confidence: 0.9,
		Sources: []personalcontext.Source{{
			Type:      personalcontext.SourceConversation,
			Kind:      personalcontext.SourceUserDeclared,
			Timestamp: time.Now(),
		}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func personalityBuilder(t *testing.T, entries ...personalcontext.Entry) *ContextBuilder {
	t.Helper()
	ws := t.TempDir()
	store, err := personalcontext.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if _, err := store.Create(e); err != nil {
			t.Fatal(err)
		}
	}
	cb := NewContextBuilder(ws)
	cb.SetPersonalContext(store)
	return cb
}

// Builtin personalities must actually inject (regression: the file-only
// loader resolved builtins to empty).
func TestBuiltinPersonalityInjects(t *testing.T) {
	cb := personalityBuilder(t)
	cb.SetPersonality("hacker")
	if got := cb.loadPersonalityContent(); !strings.Contains(got, "senior engineer") {
		t.Fatalf("hacker content missing: %q", got)
	}
	cb.SetPersonality("nope")
	if got := cb.loadPersonalityContent(); got != "" {
		t.Fatalf("unknown personality must resolve empty, got %q", got)
	}
}

// Adaptive renders reinforced style beliefs plus the non-movable floor.
func TestAdaptiveRendersLearnedStyle(t *testing.T) {
	cb := personalityBuilder(t, styleEntry("preference/communication.style", "concise"))
	cb.SetPersonality("adaptive")
	prompt := cb.BuildSystemPrompt(nil)
	if !strings.Contains(prompt, "preference/communication.style") || !strings.Contains(prompt, "concise") {
		t.Fatalf("learned style missing from prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Style floor") {
		t.Fatal("warmth floor must ship with every adaptive render")
	}
}

// No style beliefs: no learned section, builtin content still applies.
func TestAdaptiveWithoutStyle(t *testing.T) {
	cb := personalityBuilder(t)
	cb.SetPersonality("adaptive")
	if got := cb.loadPersonalityContent(); !strings.Contains(got, "adaptive mode") {
		t.Fatalf("adaptive builtin missing: %q", got)
	}
	prompt := cb.BuildSystemPrompt(nil)
	if strings.Contains(prompt, "Learned Style") {
		t.Fatal("no style beliefs must mean no learned section")
	}
}

// Non-style beliefs never leak into the adaptive profile.
func TestAdaptiveIgnoresNonStyle(t *testing.T) {
	other := styleEntry("preference/favorite_color", "green")
	cb := personalityBuilder(t, other)
	cb.SetPersonality("adaptive")
	prompt := cb.BuildSystemPrompt(nil)
	if strings.Contains(prompt, "favorite_color") {
		t.Fatal("non-style beliefs must not enter the style profile")
	}
}

// recordAffect folds scored turns into the persisted aggregate.
func TestRecordAffectPersists(t *testing.T) {
	al := pinTestLoop(t, nil)
	al.recordAffect("I love this, amazing work, thank you!")
	if al.affect.Turns != 1 {
		t.Fatalf("turn must be recorded: %+v", al.affect)
	}
	if al.affect.Affinity <= 0.5 {
		t.Fatalf("warm turn must raise affinity: %+v", al.affect)
	}
	got := al.Affect()
	if got.Turns != 1 {
		t.Fatalf("Affect() must expose the aggregate: %+v", got)
	}
}

// Affect grounds the prompt: the render line must inject.
func TestAffectInjectsIntoPrompt(t *testing.T) {
	cb := personalityBuilder(t)
	cb.SetAffectRender(func() string { return "Relational state: affinity 0.90 (close); mood +0.70 (bright)." })
	prompt := cb.BuildSystemPrompt(nil)
	if !strings.Contains(prompt, "Relational state:") {
		t.Fatal("affect render must inject into the system prompt")
	}
}
