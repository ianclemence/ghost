package activity

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/product"
)

func ev(t cevents.Type, payload map[string]interface{}) *cevents.Event {
	return &cevents.Event{ID: "e1", Type: t, Timestamp: time.Now(),
		Visibility: product.VisUserMessage, Payload: payload}
}

func TestProductNarrative(t *testing.T) {
	cases := map[cevents.Type]string{
		cevents.CapabilityStarted:    "Checking the weather",
		cevents.CapabilityCompleted:  "Weather checked",
		cevents.CapabilityFailed:     "Weather unavailable",
		cevents.PermissionRequested:  "Waiting for approval",
		cevents.RoutineCreated:       "Routine scheduled",
		cevents.IntegrationConnected: "Calendar connected",
	}
	payloads := map[cevents.Type]map[string]interface{}{
		cevents.CapabilityStarted:    {"capability": "weather.current"},
		cevents.CapabilityCompleted:  {"capability": "weather.current", "provider": "open-meteo"},
		cevents.CapabilityFailed:     {"capability": "weather.current"},
		cevents.PermissionRequested:  {"target": "contact:maria"},
		cevents.RoutineCreated:       {},
		cevents.IntegrationConnected: {"integration": "calendar"},
	}
	for typ, want := range cases {
		chip, ok := Project(ev(typ, payloads[typ]))
		if !ok {
			t.Fatalf("%s: no chip", typ)
		}
		if chip.Title != want {
			t.Fatalf("%s: got %q want %q", typ, chip.Title, want)
		}
	}
}

// An answered approval must never read as something still waiting on the
// owner: the feed would otherwise report decisions nobody owes.
func TestAnsweredApprovalsAreNotWaiting(t *testing.T) {
	ask := &cevents.Event{ID: "p1", Seq: 9, RequestID: "req-1", Type: cevents.PermissionRequested,
		Timestamp: time.Now(), Visibility: product.VisUserMessage,
		Payload: map[string]interface{}{"capability": "exec.shell"}}

	if rows := WithoutAnsweredApprovals([]*cevents.Event{ask}, map[string]bool{"req-1": true}); len(rows) != 1 {
		t.Fatalf("a request the owner still owes an answer to must stay in the feed, got %d rows", len(rows))
	}
	if rows := WithoutAnsweredApprovals([]*cevents.Event{ask}, map[string]bool{}); len(rows) != 0 {
		t.Fatalf("an answered request must not be replayed as waiting, got %d rows", len(rows))
	}
	// Unknown (no broker) keeps the row: filtering on a guess is worse.
	if rows := WithoutAnsweredApprovals([]*cevents.Event{ask}, nil); len(rows) != 1 {
		t.Fatalf("with no broker nothing may be dropped, got %d rows", len(rows))
	}
	// A request with no id cannot be checked, so it is never silently deleted.
	noid := *ask
	noid.RequestID = ""
	if rows := WithoutAnsweredApprovals([]*cevents.Event{&noid}, map[string]bool{}); len(rows) != 1 {
		t.Fatalf("an unidentifiable request must be kept, got %d rows", len(rows))
	}
	// Non-permission events are never touched.
	done := &cevents.Event{ID: "p2", Type: cevents.CapabilityCompleted, Timestamp: time.Now(),
		Visibility: product.VisUserMessage, Payload: map[string]interface{}{"capability": "weather.current"}}
	if rows := WithoutAnsweredApprovals([]*cevents.Event{done}, map[string]bool{}); len(rows) != 1 {
		t.Fatalf("completed work must survive the filter, got %d rows", len(rows))
	}
}

// A refusal and an expiry both end the request; showing "waiting" for a
// settled decision is a false claim about the present.
func TestSettledApprovalsAreNotWaiting(t *testing.T) {
	for _, typ := range []cevents.Type{cevents.PermissionDenied, cevents.PermissionExpired} {
		chip, ok := Project(ev(typ, map[string]interface{}{"capability": "email.send"}))
		if !ok {
			t.Fatalf("%s: no chip", typ)
		}
		if chip.State == StateWaiting {
			t.Fatalf("%s must not claim to be waiting, got %q", typ, chip.State)
		}
	}
}

// The capability registry is the product's own catalogue of owner-facing
// names. Not one of them may fall through to the neutral fallback — a row
// that says only "Finished a step" tells the owner nothing about their own
// system, which is exactly the class of row this feed must never show.
func TestRegisteredCapabilitiesNeverRenderVagueTitles(t *testing.T) {
	vague := []string{"Done", "Step failed", "Finished a step", "Starting",
		"Unavailable right now", "Couldn't finish that", "Approved", "Declined"}
	for _, id := range capability.IDs() {
		for _, typ := range []cevents.Type{cevents.CapabilityStarted,
			cevents.CapabilityCompleted, cevents.CapabilityFailed} {
			chip, ok := Project(&cevents.Event{ID: "e", Type: typ, Timestamp: time.Now(),
				Visibility: product.VisUserMessage,
				Payload:    map[string]interface{}{"capability": id}})
			if !ok {
				t.Fatalf("%s %s: expected a chip", typ, id)
			}
			for _, word := range vague {
				if chip.Title == word {
					t.Errorf("%s %s rendered the vague title %q", typ, id, chip.Title)
				}
			}
			if strings.TrimSpace(chip.Title) == "" {
				t.Errorf("%s %s rendered an empty title", typ, id)
			}
		}
	}
}

func TestStates(t *testing.T) {
	if c, _ := Project(ev(cevents.PermissionRequested, nil)); c.State != StateWaiting {
		t.Fatal("permission must show waiting")
	}
	if c, _ := Project(ev(cevents.CapabilityFailed, map[string]interface{}{"capability": "x"})); c.State != StateFailed {
		t.Fatal("failure must show failed")
	}
	if c, _ := Project(ev(cevents.RoutineCompleted, nil)); c.State != StateSuccess {
		t.Fatal("completed must show success")
	}
}

func TestInternalNeverProjects(t *testing.T) {
	internal := &cevents.Event{ID: "x", Type: cevents.ToolStarted, Timestamp: time.Now(),
		Visibility: product.VisInternalTrace, Payload: map[string]interface{}{"tool": "exec"}}
	if _, ok := Project(internal); ok {
		t.Fatal("internal trace must never become a chip")
	}
	// Unknown type with user visibility still yields no chip (no raw leak).
	unknown := &cevents.Event{ID: "y", Type: "provider.http.request", Timestamp: time.Now(),
		Visibility: product.VisUserMessage}
	if _, ok := Project(unknown); ok {
		t.Fatal("unknown type must not project raw names")
	}
}

func TestLeakRegression(t *testing.T) {
	// Previously observed real failures: manifests, DIR:/FILE: dumps,
	// tool schemas, internal paths must never surface in chips.
	nasty := map[string]interface{}{
		"capability": "weather.current",
		"summary":    "FILE: SKILL.md DIR: skills/ tool instructions exec manifest .bundled /var/lib/ghost/secret",
	}
	chip, ok := Project(ev(cevents.CapabilityCompleted, nasty))
	if !ok {
		t.Fatal("expected chip")
	}
	// The chip title itself must be clean narrative (summary is truncated
	// payload echo — titles, the prominent surface, are allowlisted).
	if chip.Title != "Weather checked" {
		t.Fatalf("title polluted: %q", chip.Title)
	}
	for _, banned := range []string{"SKILL.md", "DIR:", "manifest", ".bundled", "/var/lib"} {
		if strings.Contains(chip.Title, banned) {
			t.Fatalf("title leaked %q", banned)
		}
	}
	// Detail layer carries only safe provenance.
	d := ev(cevents.CapabilityCompleted, map[string]interface{}{"capability": "weather.current", "provider": "open-meteo"})
	chip2, _ := Project(d)
	if !strings.Contains(chip2.Detail, "Open Meteo") {
		t.Fatalf("detail must show safe provenance: %q", chip2.Detail)
	}
	for _, banned := range []string{"exec", "strategy", "breaker"} {
		if strings.Contains(strings.ToLower(chip2.Detail), banned) {
			t.Fatalf("detail leaked implementation %q: %q", banned, chip2.Detail)
		}
	}
}

func TestMemoryChip(t *testing.T) {
	chip, ok := Project(ev(cevents.MemoryCreated, map[string]interface{}{"title": "Prefers tea over coffee"}))
	if !ok || chip.Title != "Remembered: Prefers tea over coffee" {
		t.Fatalf("memory chip wrong: %+v", chip)
	}
}

// A removal has to read as a removal, with the reason and what was rebuilt,
// so the owner's activity feed shows a forgetting happened and why.
func TestMemoryDeletedChip(t *testing.T) {
	chip, ok := Project(ev(cevents.MemoryDeleted, map[string]interface{}{
		"title":   "preference/favorite_color",
		"summary": "Recorded so old messages cannot bring it back. Rebuilt notes, search, digest.",
		"reason":  "the owner asked Ghost to forget it",
	}))
	if !ok {
		t.Fatal("memory.deleted was dropped from the activity feed")
	}
	if chip.Title != "Forgot: preference/favorite_color" {
		t.Fatalf("title = %q, want the removed belief named", chip.Title)
	}
	if !strings.Contains(chip.Summary, "cannot bring it back") {
		t.Fatalf("summary = %q, want what was done to make it stick", chip.Summary)
	}
}

func TestDiagnosticsSafe(t *testing.T) {
	e := ev(cevents.CapabilityCompleted, map[string]interface{}{
		"provider": "open-meteo", "duration_ms": 382, "attempt": 1, "api_key": "«redacted 8 chars»",
	})
	d := Diagnostics(e)
	if d["provider"] != "open-meteo" || d["duration_ms"] != 382 {
		t.Fatalf("diagnostics missing safe fields: %v", d)
	}
	if _, ok := d["api_key"]; ok {
		t.Fatal("diagnostics must not include credential fields")
	}
}

// The mobile client resumes activity with since_seq, so every chip must
// carry the canonical sequence number it was projected from.
func TestChipCarriesSeq(t *testing.T) {
	e := &cevents.Event{ID: "e9", Seq: 42, Type: cevents.CapabilityCompleted,
		Timestamp: time.Now(), Visibility: product.VisUserMessage,
		Payload: map[string]interface{}{"capability": "weather.current", "provider": "open-meteo"}}
	chip, ok := Project(e)
	if !ok {
		t.Fatal("no chip")
	}
	if chip.Seq != 42 || chip.ID != "e9" {
		t.Fatalf("chip seq/id wrong: %+v", chip)
	}
}

// Skill lifecycle events project to user-safe narrative chips.
func TestSkillLifecycleProjects(t *testing.T) {
	cases := map[cevents.Type]string{
		cevents.SkillInstalled: "Installed skill: weather",
		cevents.SkillEnabled:   "Enabled skill: weather",
		cevents.SkillDisabled:  "Disabled skill: weather",
		cevents.SkillRemoved:   "Removed skill: weather",
		cevents.SkillUpdated:   "Updated skill: weather",
	}
	for typ, want := range cases {
		chip, ok := Project(&cevents.Event{ID: "e", Seq: 1, Type: typ, Timestamp: time.Now(),
			Visibility: product.VisUserMessage, Payload: map[string]interface{}{"name": "weather"}})
		if !ok {
			t.Fatalf("%s: no chip", typ)
		}
		if chip.Title != want {
			t.Fatalf("%s: got %q want %q", typ, chip.Title, want)
		}
	}
}

// A settled lifecycle change must never project as "running": showing an
// in-progress state for a completed fact is a false claim by omission.
func TestSkillLifecycleProjectsSuccessNotRunning(t *testing.T) {
	for _, typ := range []cevents.Type{
		cevents.SkillInstalled, cevents.SkillUpdated, cevents.SkillEnabled,
		cevents.SkillDisabled, cevents.SkillRemoved,
	} {
		chip, ok := Project(&cevents.Event{ID: "e", Seq: 7, Type: typ, Timestamp: time.Now(),
			Visibility: product.VisUserMessage, Payload: map[string]interface{}{"name": "weather"}})
		if !ok {
			t.Fatalf("%s: no chip", typ)
		}
		if chip.State != StateSuccess {
			t.Fatalf("%s: got state %q, want success", typ, chip.State)
		}
	}
}

// The revelation layer: an owner must be able to see WHY Ghost asked or
// acted on consequential work, and get no noise for routine reads.
func TestWhyForConsequentialActions(t *testing.T) {
	cases := []struct {
		name    string
		event   *cevents.Event
		wantWhy string
	}{
		{
			"approval requested explains itself",
			ev(cevents.PermissionRequested, map[string]interface{}{"capability": "email.send", "risk": "consequential"}),
			"This changes something, so Ghost asked first.",
		},
		{
			"approval says what the yes did, in plain words",
			ev(cevents.PermissionApproved, map[string]interface{}{"capability": "browser", "risk": "consequential"}),
			"You said yes, so Ghost went ahead.",
		},
		{
			"high-impact approval warns it is hard to undo",
			ev(cevents.PermissionApproved, map[string]interface{}{"capability": "browser.transact", "risk": "high_impact"}),
			"You approved this high-impact action; it may be hard to undo.",
		},
		{
			"denial is honest that nothing ran",
			ev(cevents.PermissionDenied, map[string]interface{}{"capability": "message.send", "risk": "consequential"}),
			"You declined this action, so Ghost did not do it.",
		},
		{
			"expiry is honest that nothing ran",
			ev(cevents.PermissionExpired, map[string]interface{}{"risk": "consequential"}),
			"The approval expired before it was used, so nothing ran.",
		},
		{
			"consequential completion records it ran under approval",
			ev(cevents.CapabilityCompleted, map[string]interface{}{"capability": "email.send", "risk": "consequential"}),
			"Done under your approval.",
		},
		{
			"failed consequential action says nothing changed",
			ev(cevents.CapabilityFailed, map[string]interface{}{"capability": "email.send", "risk": "consequential"}),
			"The action did not complete; nothing was changed.",
		},
		{
			"a read needs no justification",
			ev(cevents.CapabilityCompleted, map[string]interface{}{"capability": "weather.get", "risk": "read_only"}),
			"",
		},
		{
			"routine housekeeping gets no line",
			ev(cevents.RoutineCreated, map[string]interface{}{}),
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chip, ok := Project(tc.event)
			if !ok {
				t.Fatalf("event should project")
			}
			if chip.Why != tc.wantWhy {
				t.Errorf("Why = %q, want %q", chip.Why, tc.wantWhy)
			}
		})
	}
}

// Activity reports what Ghost did, with provenance. It is not a place for
// defensive prose: a source list is useful, a caveat is noise.
func TestActivityCarriesSourcesWithoutCaveatProse(t *testing.T) {
	ev := &cevents.Event{
		ID: "e1", Type: cevents.ToolCompleted, Timestamp: time.Now(),
		Visibility: product.VisUserMessage, Status: "success",
		Payload: map[string]interface{}{
			"tool":     "web_search",
			"summary":  "Searched the web: \"latest news in bangkok\"",
			"sources":  []interface{}{"nationthailand.com", "apnews.com"},
			"status":   "success",
			"provider": "brave",
		},
	}
	chip, ok := Project(ev)
	if !ok {
		t.Fatal("a completed web search must project to a chip")
	}
	if !strings.Contains(chip.Summary, "Searched the web") {
		t.Errorf("summary = %q, want the action", chip.Summary)
	}
	if !strings.Contains(chip.Detail, "nationthailand.com") || !strings.Contains(chip.Detail, "apnews.com") {
		t.Errorf("detail must carry the sources, got %q", chip.Detail)
	}
	joined := strings.ToLower(chip.Title + " " + chip.Summary + " " + chip.Detail)
	for _, banned := range []string{"caveat", "note that", "please note", "for transparency", "unavailable"} {
		if strings.Contains(joined, banned) {
			t.Errorf("activity must not carry defensive prose (%q): %q", banned, joined)
		}
	}
}

func TestHumanizeFailureAndTechnicalSummaries(t *testing.T) {
	cases := map[string]string{
		"query is required":                     "didn't give the tool everything",
		"Get https://x: dial tcp: no such host": "couldn't reach",
		"HTTP 429 rate limit":                   "busy",
		"401 Unauthorized":                      "key may be missing",
		"weird thing":                           "Something went wrong",
		"The forecast for Shenzhen covers 2026-10-03 to 2026-10-17 only, so 2026-10-26 is out of reach. Say so; do not estimate.": "doesn't reach that date",
	}
	for raw, want := range cases {
		if got := humanizeFailure(raw); !strings.Contains(got, want) {
			t.Errorf("humanizeFailure(%q) = %q, want it to contain %q", raw, got, want)
		}
	}
	if !looksTechnical(`{"q":"x"}`) || !looksTechnical("web_search failed") || !looksTechnical("SELECT * FROM memory") {
		t.Error("machine output should be technical")
	}
	if looksTechnical("Searched the web: \"latest news in bangkok\"") {
		t.Error("a plain sentence is not technical")
	}
}
