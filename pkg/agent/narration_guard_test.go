package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/skills"
)

// Narration guard (audit P1/P2, change C3): the deterministic routine layer
// must never turn narration, venting, thinking aloud, or described habits
// into durable work. Only explicit scheduling asks propose. One-time
// requests never belong here (the reminder path owns them).
//
// Each corpus doubles as the under-action boundary: delegation must KEEP
// working, or restraint has become passivity.
var narrationCorpus = []string{
	"every morning I feel groggy",
	"daily standup is painful",
	"I barely slept",
	"kinda tired today",
	"I keep forgetting to call Maria",
	"I should probably finish the project today",
	"Today was rough",
	"That meeting was ridiculous",
	"I'm exhausted",
	"I wish I had more time",
	"Every Monday at 9",
}

var delegationCorpus = []string{
	"Every Monday at 9 remind me to review my finances",
	"remind me every morning at 8 to take pills",
	"set up a daily reminder to stand up",
	"every Friday notify me about deployments",
	"Every evening remind me to read",
	"make sure I stretch every evening",
}

var oneTimeCorpus = []string{
	"Remind me tomorrow at 10 to email Maria",
	"I need to email Maria tomorrow morning",
	"Make sure I don't forget to call Maria tomorrow",
}

func TestRoutineNeverProposesOnNarration(t *testing.T) {
	for _, text := range narrationCorpus {
		al := testRoutineLoop(t)
		if ans, ok := al.tryRoutineTurn(routineMsg("sess-n", text)); ok {
			t.Errorf("narration must not propose a routine %q: %q", text, ans)
		}
	}
}

func TestRoutineStillProposesOnDelegation(t *testing.T) {
	for _, text := range delegationCorpus {
		al := testRoutineLoop(t)
		ans, ok := al.tryRoutineTurn(routineMsg("sess-d", text))
		if !ok {
			t.Errorf("delegation must still propose a routine %q", text)
			continue
		}
		if !containsConfirm(ans) {
			t.Errorf("delegation must ask confirmation %q: %q", text, ans)
		}
	}
}

func TestRoutineNeverTouchesOneTime(t *testing.T) {
	for _, text := range oneTimeCorpus {
		al := testRoutineLoop(t)
		if ans, ok := al.tryRoutineTurn(routineMsg("sess-o", text)); ok {
			t.Errorf("one-time request must not become a routine %q: %q", text, ans)
		}
	}
}

func containsConfirm(s string) bool {
	return strings.Contains(s, "Say yes to confirm") || strings.Contains(s, "What should happen")
}

// A pre-existing task-pending (created before the narration guard, or by a
// confirmed flow) still answers: the guard gates creation, never replies.
func TestRoutineLegacyTaskPendingStillAnswers(t *testing.T) {
	al := testRoutineLoop(t)
	store := skills.NewPendingStore(al.workspace)
	store.Create("sess-l", routinePendingCapability, "routines", "task",
		"What should happen every Monday at 9?", "Every Monday at 9", 0,
		map[string]string{"schedule_text": "every Monday at 9", "timezone": "UTC"})
	if ans, ok := al.tryRoutineTurn(routineMsg("sess-l", "review my finances")); !ok || !contains(ans, "Say yes to confirm") {
		t.Fatalf("legacy task answer must still propose: %q", ans)
	}
}

// Narration is never a fast-path fact lookup: it must reach the deliberate
// loop (or fall through conversationally), never shortcut to a canned fact.
func TestNarrationNeverFastPath(t *testing.T) {
	for _, text := range narrationCorpus {
		if got := classifyEffort(text); got == EffortFast {
			t.Errorf("narration must not classify Fast %q", text)
		}
	}
}

// Stored names render capitalized: "ian" answers "You're Ian.", never
// "You're ian." The stored value is untouched; only display changes.
func TestFastAnswerCapitalizesName(t *testing.T) {
	if got := phraseFastAnswer("identity/name", []string{"ian"}); got != "You're Ian." {
		t.Fatalf("name must render capitalized, got %q", got)
	}
	if got := phraseFastAnswer("identity/name", []string{"Ian"}); got != "You're Ian." {
		t.Fatalf("already-capitalized name must pass through, got %q", got)
	}
}
