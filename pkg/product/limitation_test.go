package product

import (
	"os"
	"strings"
	"testing"
)

// The policy exists to stop Ghost narrating its own plumbing. These cases are
// the contract in both directions: a limitation that does not change the answer
// must stay internal, and one that does must reach the owner.
func TestDecideLimitationTooQuiet(t *testing.T) {
	internal := []struct {
		name  string
		facts LimitationFacts
	}{
		// Two corroborating sources while a page would not parse: the answer is
		// sound and the disruption is Ghost's problem.
		{"corroborated despite an unreadable page", LimitationFacts{
			AnyEvidence: true, SnippetOnly: true, Corroborated: true,
		}},
		// An ordinary read-through answer with nothing wrong.
		{"clean read-through", LimitationFacts{AnyEvidence: true}},
		// A source Ghost merely tried, not one the owner asked for.
		{"page ghost chose to fetch failed", LimitationFacts{AnyEvidence: true}},
	}
	for _, c := range internal {
		if got := DecideLimitation(c.facts); got.Surfaces() {
			t.Errorf("%s: limitation %q would surface; it does not change the answer", c.name, got)
		}
	}
}

func TestDecideLimitationTooLoud(t *testing.T) {
	surfaced := []struct {
		name  string
		facts LimitationFacts
		want  Limitation
	}{
		{"owner asked for a source that could not be read", LimitationFacts{
			RequestedSource: "the Bangkok Post", RequestedSourceRead: false,
			AnyEvidence: true, Corroborated: true,
		}, LimitationMaterial},
		{"sources conflict", LimitationFacts{
			AnyEvidence: true, Corroborated: true, Conflicting: true,
		}, LimitationMaterial},
		{"action ran but the outcome is unconfirmed", LimitationFacts{
			AnyEvidence: true, UnverifiedAction: true,
		}, LimitationMaterial},
		{"the action failed", LimitationFacts{
			AnyEvidence: true, FailedAction: true,
		}, LimitationBlocking},
		{"no usable evidence at all", LimitationFacts{AnyEvidence: false}, LimitationBlocking},
		{"headline-level evidence standing alone", LimitationFacts{
			AnyEvidence: true, SnippetOnly: true, Corroborated: false,
		}, LimitationContextual},
	}
	for _, c := range surfaced {
		if got := DecideLimitation(c.facts); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// Severity ordering is part of the contract: a failure outranks a conflict, a
// conflict outranks a thin-sourcing note.
func TestDecideLimitationSeverityOrder(t *testing.T) {
	got := DecideLimitation(LimitationFacts{
		FailedAction: true, Conflicting: true, SnippetOnly: true,
		RequestedSource: "AP", RequestedSourceRead: false, AnyEvidence: true,
	})
	if got != LimitationBlocking {
		t.Fatalf("a failure must outrank every other limitation, got %q", got)
	}
	got = DecideLimitation(LimitationFacts{
		Conflicting: true, SnippetOnly: true, AnyEvidence: true,
		RequestedSource: "AP", RequestedSourceRead: false,
	})
	if got != LimitationMaterial {
		t.Fatalf("a conflict must outrank thin sourcing, got %q", got)
	}
}

// The prompt-facing rule and the runtime policy must not drift apart: the model
// follows GHOST.md, so the policy has to be stated there.
func TestCaveatRuleIsPresentInGhostPrompt(t *testing.T) {
	for _, path := range []string{"../../workspace/GHOST.md"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(raw), CaveatRule) {
			t.Fatalf("%s must carry the caveat policy verbatim so the model's prose and the runtime's decision cannot drift", path)
		}
	}
}

// The closing offer is the one piece of the old habit a prompt cannot reliably
// suppress, so the runtime enforces it — narrowly.
func TestTrimClosingOffer(t *testing.T) {
	cases := []struct {
		name     string
		reply    string
		failed   bool
		wantSame bool
		wantKeep string // the body that must survive
	}{
		{
			name:     "a plain information answer loses its courtesy offer",
			reply:    "Flooding leads the news today.\n\nSources: Bangkok Post, AP.\n\nWant me to pull the full article?",
			wantKeep: "Sources: Bangkok Post, AP.",
		},
		{
			name:     "an offer sentence after a fact keeps the fact",
			reply:    "All 50 districts are a disaster zone (Bangkok Post). Would you like me to dig into any one of these?",
			wantKeep: "All 50 districts are a disaster zone (Bangkok Post).",
		},
		{
			name:     "a failed action keeps its retry offer — it is material",
			reply:    "I couldn't reschedule it. Want me to try again?",
			failed:   true,
			wantSame: true,
		},
		{
			name:     "a reply that is only an offer is left alone",
			reply:    "Want me to look that up?",
			wantSame: true,
		},
		{
			name:     "a question that is not an offer survives",
			reply:    "Which city should I check?",
			wantSame: true,
		},
		{
			name:     "a long closing paragraph is not a courtesy and survives",
			reply:    "Flooding leads. Want me to explain the drainage plan in detail, including the five critical canals, the pumping capacity the city has committed, the timeline it gave for clearing them once the rain stops, and what each of the affected districts should expect over the next seventy-two hours as the runoff moves downstream toward the gulf?",
			wantSame: true,
		},
	}
	for _, c := range cases {
		got := TrimClosingOffer(c.reply, c.failed)
		if c.wantSame {
			if got != c.reply {
				t.Errorf("%s: reply was modified:\n got %q\nwant %q", c.name, got, c.reply)
			}
			continue
		}
		if !strings.Contains(got, c.wantKeep) {
			t.Errorf("%s: the body was lost: got %q, want it to contain %q", c.name, got, c.wantKeep)
		}
		if strings.Contains(got, "Want me to") || strings.Contains(got, "Would you like me to") {
			t.Errorf("%s: the closing offer survived: %q", c.name, got)
		}
	}
}

// An offer welded onto the end of a real sentence: only the offer goes, and
// the observation that carried it keeps its full stop. Observed live on a
// news answer, where the trailing paragraph began with content.
func TestTrimClosingOfferCutsAnOfferAttachedToARealSentence(t *testing.T) {
	reply := "Flooding leads the news today.\n\nSources: Bangkok Post, The Thaiger.\n\n" +
		"If you're out and about today, the eastern side of the city is the worst hit " +
		"— want me to check the current rain and AQI for your area?"
	got := TrimClosingOffer(reply, false)
	if !strings.Contains(got, "worst hit") {
		t.Fatalf("the observation was lost: %q", got)
	}
	if !strings.Contains(got, "Sources: Bangkok Post, The Thaiger.") {
		t.Fatalf("the sources line was lost: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "want me to") {
		t.Fatalf("the attached offer survived: %q", got)
	}
	if !strings.HasSuffix(got, "worst hit.") {
		t.Fatalf("the cut left the sentence unfinished: %q", got)
	}
}

// A conditional that points at content is content, not an offer.
func TestTrimClosingOfferKeepsAConditionalPointer(t *testing.T) {
	reply := "Flooding leads the news today.\n\nIf you want the raw listings, they are in the activity feed."
	if got := TrimClosingOffer(reply, false); got != reply {
		t.Fatalf("content was trimmed: %q", got)
	}
}

// Trimming must never remove a qualification the model wrote.
func TestTrimClosingOfferKeepsQualifiers(t *testing.T) {
	reply := "Reports are conflicting: Reuters says X, the Post says Y. I wouldn't treat it as confirmed yet. Want me to keep watching?"
	got := TrimClosingOffer(reply, false)
	if !strings.Contains(got, "conflicting") || !strings.Contains(got, "wouldn't treat it as confirmed") {
		t.Fatalf("the material qualification was lost: %q", got)
	}
	if strings.Contains(got, "Want me to") {
		t.Fatalf("the closing offer survived: %q", got)
	}
}

// Metadata is preserved internally regardless of what the prose shows.
func TestLimitationIsRecordedNotOnlySpoken(t *testing.T) {
	// Snippet-only evidence that is corroborated is recorded as such and does
	// not surface; the distinction exists for audit even when the owner sees
	// nothing.
	level := DecideLimitation(LimitationFacts{AnyEvidence: true, SnippetOnly: true, Corroborated: true})
	if level != LimitationNone {
		t.Fatalf("corroborated listings should not surface a caveat, got %q", level)
	}
	if level.Surfaces() {
		t.Fatal("LimitationNone must never surface")
	}
}

// The prompt bans the disclaimer frame; the model still produces it on roughly
// one news answer in fifteen, and a prompt rule cannot know whether anything
// failed. The runtime can, so the runtime decides.
func TestTrimLabelledCaveat(t *testing.T) {
	cases := []struct {
		name     string
		reply    string
		failed   bool
		wantSame bool
		wantKeep string
	}{
		{
			name:     "a trailing caveat about the shape of the sources goes",
			reply:    "Flooding leads the news today (The Star, 27 Sep).\n\nSources: The Star, Tuko.\n\nOne caveat: these come from The Star and Kenyans.co.ke homepages, so it's a snapshot of what they're covering.",
			wantKeep: "Sources: The Star, Tuko.",
		},
		{
			name:     "after a failure the caveat is earned and stays",
			reply:    "I could not reach the ministry's page.\n\nOne caveat: this comes from a homepage listing only.",
			failed:   true,
			wantSame: true,
		},
		{
			name:     "an inability is material and stays",
			reply:    "AP and Nation Thailand both report the flood order.\n\nOne caveat: I couldn't read the Bangkok Post's own coverage of this.",
			wantSame: true,
		},
		{
			name:     "a conflict is material and stays",
			reply:    "Reuters says the subsidy was announced.\n\nOne caveat: sources disagree — the ministry denied it.",
			wantSame: true,
		},
		{
			name:     "a single source is material and stays",
			reply:    "The rail line is reported to open in November.\n\nOne caveat: only The Star's headline carried this, so it's one outlet's account.",
			wantSame: true,
		},
		{
			name:     "a note in the middle of an answer is left where it is",
			reply:    "Flooding leads today.\n\nOne caveat: these come from homepages.\n\nSources: The Star.",
			wantSame: true,
		},
		{
			name:     "a caveat that is not about retrieval shape is left alone",
			reply:    "Treasury put inflation at 4.2% for September.\n\nOne caveat: the figures are provisional until the final revision.",
			wantSame: true,
		},
		{
			name:     "a reply that is only the caveat is left alone",
			reply:    "One caveat: these come from homepages.",
			wantSame: true,
		},
	}
	for _, c := range cases {
		got := TrimLabelledCaveat(c.reply, c.failed)
		if c.wantSame {
			if got != c.reply {
				t.Errorf("%s: reply was modified:\n got %q\nwant %q", c.name, got, c.reply)
			}
			continue
		}
		if !strings.Contains(got, c.wantKeep) {
			t.Errorf("%s: the body was lost: got %q, want it to contain %q", c.name, got, c.wantKeep)
		}
		if strings.Contains(strings.ToLower(got), "caveat") {
			t.Errorf("%s: the caveat frame survived: %q", c.name, got)
		}
	}
}

// The cut must land cleanly: the sentence that carried the frame keeps its full
// stop and nothing after it is left dangling.
func TestTrimLabelledCaveatFinishesTheSentence(t *testing.T) {
	reply := "Kenya's headlines lead with the refinery story (The Star).\n\nOne caveat: these are homepages, not the articles"
	got := TrimLabelledCaveat(reply, false)
	if !strings.HasSuffix(got, "(The Star).") {
		t.Fatalf("cut left the sentence unfinished: %q", got)
	}
}
