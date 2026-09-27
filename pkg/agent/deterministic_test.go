package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/tools"
)

func testMessagesWithSkillRead(args string) []providers.Message {
	return []providers.Message{
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{ID: "call_1", Type: "function", Function: &providers.FunctionCall{Name: "read_file", Arguments: args}},
			},
		},
	}
}

func TestParseShoppingItems(t *testing.T) {
	items := parseShoppingItems("Add milk and eggs to my shopping list")
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %v", items)
	}
}

func TestResolvePendingResume(t *testing.T) {
	skills.SetPending("sess-resume", skills.PendingContinuation{
		CapabilityID: "flight.status", Skill: "flight",
		MissingField: "flight_number", Question: "Which flight number?",
		OriginalTask: "What's my flight status?",
	})
	defer skills.ClearPending("sess-resume")

	resumed, ok, _, _ := resolvePendingResume("", "sess-resume", "TG123")
	if !ok {
		t.Fatalf("expected resume for TG123")
	}
	if !strings.Contains(resumed, "TG123") || !strings.Contains(resumed, "flight") {
		t.Fatalf("resumed message missing context: %q", resumed)
	}

	// Full new task must not hijack.
	skills.SetPending("sess-resume2", skills.PendingContinuation{
		CapabilityID: "flight.status", Skill: "flight",
		MissingField: "flight_number", Question: "Which flight number?",
		OriginalTask: "What's my flight status?",
	})
	defer skills.ClearPending("sess-resume2")
	if _, ok, _, _ := resolvePendingResume("", "sess-resume2", "Remind me tomorrow at 9 AM to call John about the quarterly report"); ok {
		t.Fatalf("long new task should not resume")
	}
	skills.ClearPending("sess-resume2")
}

func TestResolvePendingDurableAcrossProcess(t *testing.T) {
	ws := t.TempDir()
	// Process A: deterministic weather fast-path sets a durable pending.
	skills.SetPendingDurable(ws, "sess-dup", skills.PendingContinuation{
		CapabilityID: "weather.current", Skill: "weather",
		MissingField: "location", Question: "Which city should I check?",
		OriginalTask: "What's the weather like?",
	})
	// Process B (fresh, empty in-memory map): the short answer resumes.
	resumed, ok, field, answer := resolvePendingResume(ws, "sess-dup", "Bangkok")
	if !ok {
		t.Fatalf("expected durable resume for Bangkok")
	}
	if field != "location" || answer != "Bangkok" {
		t.Fatalf("structured resume wrong: field=%q answer=%q", field, answer)
	}
	if !strings.Contains(resumed, "Bangkok") || !strings.Contains(resumed, "weather") {
		t.Fatalf("resumed message missing context: %q", resumed)
	}
	// Exact-once: after completion the durable request is consumed, so a
	// second short answer must NOT resume the same original task.
	if _, ok, _, _ := resolvePendingResume(ws, "sess-dup", "Phuket"); ok {
		t.Fatalf("durable pending must resume exactly once")
	}
}

func TestCapabilityInputs(t *testing.T) {
	inputs := capabilityInputsFromMessage("status of flight TG123?", nil)
	if inputs["flight_number"] == "" {
		t.Fatalf("expected flight_number extracted")
	}
	inputs = capabilityInputsFromMessage("weather in Bangkok?", nil)
	if inputs["location"] == "" {
		t.Fatalf("expected location extracted")
	}
	if got := capabilityInputsFromMessage("What is the weather in Bangkok right now?", nil); got["location"] != "Bangkok" {
		t.Fatalf("'right now' must trim cleanly, got %q", got["location"])
	}
	if got := locationFromText("what's the weather in chiang mai tomorrow?"); got != "chiang mai" {
		t.Fatalf("'tomorrow' must trim, got %q", got)
	}
}

func TestCommittedSkillGeneric(t *testing.T) {
	// No per-skill branches: any skills/<name>/SKILL.md path works.
	msgs := testMessagesWithSkillRead(`{"path":"skills/my-new-skill/SKILL.md"}`)
	if got := committedSkill(msgs); got != "my-new-skill" {
		t.Fatalf("expected my-new-skill, got %q", got)
	}
}

func TestSkillToggleName(t *testing.T) {
	// Uses a temp workspace with fake skills.
	dir := t.TempDir()
	for _, n := range []string{"ascii-art", "weather"} {
		os.MkdirAll(dir+"/skills/"+n, 0755)
		os.WriteFile(dir+"/skills/"+n+"/SKILL.md", []byte("x"), 0644)
	}
	if got := skillToggleName(dir, "ascii art"); got != "ascii-art" {
		t.Fatalf("expected ascii-art, got %q", got)
	}
	if got := skillToggleName(dir, "the weather skill"); got != "weather" {
		t.Fatalf("expected weather, got %q", got)
	}
	if got := skillToggleName(dir, "the lights"); got != "" {
		t.Fatalf("must not guess, got %q", got)
	}
}

func TestSetSkillEnabledLocal(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(dir+"/skills/demo", 0755)
	os.WriteFile(dir+"/skills/demo/SKILL.md", []byte("x"), 0644)
	ok, msg := setSkillEnabledLocal(dir, "demo", false)
	if !ok || msg == "" {
		t.Fatalf("disable failed: %v %q", ok, msg)
	}
	if _, err := os.Stat(dir + "/skills/demo/SKILL.md"); err == nil {
		t.Fatalf("SKILL.md should be renamed")
	}
	ok, _ = setSkillEnabledLocal(dir, "demo", true)
	if !ok {
		t.Fatalf("re-enable failed")
	}
	if _, err := os.Stat(dir + "/skills/demo/SKILL.md"); err != nil {
		t.Fatalf("SKILL.md should be back")
	}
}

// TestResumeNotReasked guards the routing fix: after a durable
// clarification resume rewrites the message to carry the answer, the
// readiness fast-path must NOT re-ask (which would mint a second
// pending and lose the answer).
func TestResumeNotReaskedByReadiness(t *testing.T) {
	ws := t.TempDir()
	skills.SetPendingDurable(ws, "sess-w2", skills.PendingContinuation{
		CapabilityID: "weather.current", Skill: "weather",
		MissingField: "location", Question: "Which city should I check?",
		OriginalTask: "What's the weather?",
	})
	resumed, ok, field, answer := resolvePendingResume(ws, "sess-w2", "Bangkok")
	if !ok {
		t.Fatalf("expected resume")
	}
	// The rewritten message must not re-trigger the weather re-ask.
	if strings.Contains(strings.ToLower(resumed), "which city should i check") {
		t.Fatalf("resumed message still asks for the city: %q", resumed)
	}
	if field != "location" || answer != "Bangkok" {
		t.Fatalf("structured resume wrong: field=%q answer=%q", field, answer)
	}
	// And the durable pending is consumed exactly once.
	if _, ok, _, _ := resolvePendingResume(ws, "sess-w2", "Tokyo"); ok {
		t.Fatalf("resume must happen exactly once")
	}
}

func TestNetworkDispatchHonestForecast(t *testing.T) {
	al := &AgentLoop{}
	// Forecast ask must be answered honestly, never by the model inventing
	// numbers, and never dispatched to the current-conditions tool.
	ans, ok := al.tryDeterministicNetworkDispatch("What's the weather tomorrow in Bangkok?", "s", nil)
	if !ok {
		t.Fatalf("forecast ask must be handled deterministically")
	}
	if !strings.Contains(ans, "forecast") {
		t.Fatalf("honest limitation missing: %q", ans)
	}
}

func TestNetworkDispatchUnwiredFallsThrough(t *testing.T) {
	// No tools registry (unit-test loop): the dispatch must NOT panic and
	// must NOT claim success; it falls through to the agent.
	al := &AgentLoop{}
	ans, ok := al.tryDeterministicNetworkDispatch("What's the current weather in Bangkok?", "s", nil)
	if ok {
		t.Fatalf("unwired loop must not handle: %q", ans)
	}
}

func TestCapabilityInputsReadsResumeAnswer(t *testing.T) {
	md := map[string]string{"resume_field": "location", "resume_answer": "Bangkok"}
	inputs := capabilityInputsFromMessage("What's the weather?", md)
	if inputs["location"] != "Bangkok" {
		t.Fatalf("resume answer must feed location: %+v", inputs)
	}
	// Real device location still wins over resume.
	md2 := map[string]string{"city": "Chiang Mai", "resume_field": "location", "resume_answer": "Bangkok"}
	if got := capabilityInputsFromMessage("w", md2); got["location"] != "Chiang Mai" {
		t.Fatalf("device location must win: %+v", got)
	}
}

// TestResumeDoesNotHijackProposals guards the shared-durable-store bug:
// routine/standing proposals wait on "yes"/"no" and must be confirmed by
// their own fast-paths, never rewritten as clarification answers.
func TestResumeDoesNotHijackProposals(t *testing.T) {
	ws := t.TempDir()
	// A standing-permission proposal (no MissingField set).
	skills.SetPendingDurable(ws, "sess-g", skills.PendingContinuation{
		CapabilityID: "permission.standing", Skill: "permissions",
		MissingField: "", Question: "Ghost will be allowed to add calendar events for you. Nothing else changes. Say yes to confirm.",
		OriginalTask: "You can always add calendar events for me",
	})
	if _, ok, _, _ := resolvePendingResume(ws, "sess-g", "yes"); ok {
		t.Fatal("standing proposal must not be hijacked as a clarification")
	}
	// A routine proposal likewise.
	skills.SetPendingDurable(ws, "sess-g2", skills.PendingContinuation{
		CapabilityID: "routine.create", Skill: "routines",
		MissingField: "", Question: "I'll remind you to review finances every Monday morning. Say yes to confirm.",
		OriginalTask: "Every Monday morning remind me to review my finances",
	})
	if _, ok, _, _ := resolvePendingResume(ws, "sess-g2", "yes"); ok {
		t.Fatal("routine proposal must not be hijacked as a clarification")
	}
}

func TestLocationRefersHere(t *testing.T) {
	yes := []string{
		"What's the current weather here?",
		"weather here?",
		"Is it going to rain at my location?",
		"find cafes near me",
		"anything good around here?",
		"local weather",
		"What is the weather in my city?",
		"weather in my location",
		"is it hot where I am?",
	}
	for _, m := range yes {
		if !locationRefersHere(m) {
			t.Errorf("expected here-reference: %q", m)
		}
	}
	no := []string{
		"What's the current weather in Bangkok?",
		"find cafes near Central Park",
		"weather tomorrow",
		"remind me at 9",
	}
	for _, m := range no {
		if locationRefersHere(m) {
			t.Errorf("must not treat as here-reference: %q", m)
		}
	}
}

func TestKnownLocationFromMemory(t *testing.T) {
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	al := &AgentLoop{pcStore: store}
	if got := al.knownLocation("s"); got != "" {
		t.Fatalf("empty store must yield no location, got %q", got)
	}
	now := time.Now().UTC()
	mkEntry := func(id, pred, val string) personalcontext.Entry {
		raw, _ := personalcontext.RawValue(val)
		return personalcontext.Entry{ID: id, Kind: personalcontext.KindFact,
			Subject: "user", Predicate: pred, Value: raw,
			Status: personalcontext.StatusCurrent, Confidence: 0.9,
			Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation,
				Kind: personalcontext.SourceUserDeclared, Ref: "t:1", Timestamp: now}},
			CreatedAt: now, UpdatedAt: now}
	}
	// Structured predicate wins.
	if _, err := store.Create(mkEntry("loc1", "fact/city", "Chiang Mai")); err != nil {
		t.Fatal(err)
	}
	if got := al.knownLocation("s"); got != "Chiang Mai" {
		t.Fatalf("structured location must resolve, got %q", got)
	}
}

func TestKnownLocationFromGeneralFact(t *testing.T) {
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	al := &AgentLoop{pcStore: store}
	now := time.Now().UTC()
	raw, _ := personalcontext.RawValue("I am currently in Phang-Nga")
	if _, err := store.Create(personalcontext.Entry{ID: "g1", Kind: personalcontext.KindFact,
		Subject: "user", Predicate: "fact/general", Value: raw,
		Status: personalcontext.StatusCurrent, Confidence: 0.7,
		Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation,
			Kind: personalcontext.SourceInferred, Ref: "t:1", Timestamp: now}},
		CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if got := al.knownLocation("s"); got != "Phang-Nga" {
		t.Fatalf("inline location must resolve, got %q", got)
	}
}

func TestLocationFallbackFillsHere(t *testing.T) {
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	al := &AgentLoop{pcStore: store}
	now := time.Now().UTC()
	raw, _ := personalcontext.RawValue("Phang-Nga")
	if _, err := store.Create(personalcontext.Entry{ID: "loc", Kind: personalcontext.KindFact,
		Subject: "user", Predicate: "fact/location", Value: raw,
		Status: personalcontext.StatusCurrent, Confidence: 0.9,
		Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation,
			Kind: personalcontext.SourceUserDeclared, Ref: "t:1", Timestamp: now}},
		CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	inputs := map[string]string{}
	got := al.locationWithMemoryFallback("What's the current weather here?", "s", inputs)
	if got["location"] != "Phang-Nga" {
		t.Fatalf("here must resolve from memory, got %q", got["location"])
	}
	// Explicit locations are never overwritten.
	explicit := map[string]string{"location": "Bangkok"}
	if got := al.locationWithMemoryFallback("weather here?", "s", explicit); got["location"] != "Bangkok" {
		t.Fatalf("explicit location must win, got %q", got["location"])
	}
	// Non-here messages are untouched.
	plain := map[string]string{}
	if got := al.locationWithMemoryFallback("What's the current weather?", "s", plain); got["location"] != "" {
		t.Fatalf("must not invent a location, got %q", got["location"])
	}
	// "my city" phrasing: extraction yields no literal place, and the
	// fallback resolves it through memory — the live bug where "my city"
	// was geocoded as a place and failed.
	msg := "What is the weather in my city?"
	inputs = capabilityInputsFromMessage(msg, nil)
	if inputs["location"] != "" {
		t.Fatalf("here-phrase must not be extracted as a place, got %q", inputs["location"])
	}
	if got := al.locationWithMemoryFallback(msg, "s", inputs); got["location"] != "Phang-Nga" {
		t.Fatalf("my city must resolve from memory, got %q", got["location"])
	}
}

func TestSupersedeSemanticCorrection(t *testing.T) {
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	al := &AgentLoop{pcStore: store}
	now := time.Now().UTC()
	mkEntry := func(id, val, status string) personalcontext.Entry {
		raw, _ := personalcontext.RawValue(val)
		return personalcontext.Entry{ID: id, Kind: personalcontext.KindFact,
			Subject: "user", Predicate: "fact/work", Value: raw,
			Status: personalcontext.Status(status), Confidence: 0.9,
			Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation,
				Kind: personalcontext.SourceUserDeclared, Ref: "t:1", Timestamp: now}},
			CreatedAt: now, UpdatedAt: now}
	}
	if _, err := store.Create(mkEntry("old", "I work remotely", "current")); err != nil {
		t.Fatal(err)
	}
	current := store.Current()

	// Same value: restatement, not a correction.
	if al.supersedeSemanticCorrection(current, mkEntry("new1", "I work remotely", "current")) {
		t.Fatal("identical value must not supersede")
	}
	// Uncertain candidate: must never evict a belief.
	if al.supersedeSemanticCorrection(current, mkEntry("new2", "I do not work remotely", "uncertain")) {
		t.Fatal("uncertain extraction must not supersede")
	}
	// Genuine correction: retires the old row, single current survives.
	if !al.supersedeSemanticCorrection(current, mkEntry("new3", "I do not work remotely", "current")) {
		t.Fatal("contradicting current extraction must supersede")
	}
	got, ok := store.Get("old")
	if !ok || got.Status != personalcontext.StatusSuperseded {
		t.Fatalf("old entry must be superseded, got %+v", got)
	}
	n := 0
	for _, e := range store.Current() {
		if e.Subject == "user" && e.Predicate == "fact/work" && e.Status == personalcontext.StatusCurrent {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("exactly one current fact/work must survive, got %d", n)
	}
}

func TestSupersedeRefusesAmbiguousStore(t *testing.T) {
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	al := &AgentLoop{pcStore: store}
	now := time.Now().UTC()
	mkEntry := func(id, val string) personalcontext.Entry {
		raw, _ := personalcontext.RawValue(val)
		return personalcontext.Entry{ID: id, Kind: personalcontext.KindFact,
			Subject: "user", Predicate: "fact/work", Value: raw,
			Status: personalcontext.StatusCurrent, Confidence: 0.9,
			Sources: []personalcontext.Source{{Type: personalcontext.SourceConversation,
				Kind: personalcontext.SourceUserDeclared, Ref: "t:1", Timestamp: now}},
			CreatedAt: now, UpdatedAt: now}
	}
	// Two currents for one belief (pre-existing inconsistency): refuse to
	// pick a winner blindly.
	if _, err := store.Create(mkEntry("a", "I work remotely")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(mkEntry("b", "I work on-site")); err != nil {
		t.Fatal(err)
	}
	current := store.Current()
	if al.supersedeSemanticCorrection(current, mkEntry("c", "I do not work remotely")) {
		t.Fatal("ambiguous store-wide current must not be superseded")
	}
}

// Locations people actually name: "near Cebu City", "around New York",
// "in Bangkok" — not just "in <Place>" at the end of the sentence. Missing
// these made the find-nearby fast-path ask for a location that was right
// there in the message.
func TestLocationFromTextPrepositions(t *testing.T) {
	cases := map[string]string{
		"Find a coffee shop near Cebu City. One line.": "Cebu City",
		"cafes around New York today":                  "New York",
		"what's the weather in Bangkok?":               "Bangkok",
		"places close to Cebu City please":             "Cebu City",
		"restaurants near me":                          "",
		"show me something cool":                       "",
	}
	for msg, want := range cases {
		if got := locationFromText(msg); got != want {
			t.Errorf("locationFromText(%q) = %q, want %q", msg, got, want)
		}
	}
}

// Here-phrases must never be extracted as literal places: "weather in my
// city" captures "my city" (the preposition regex is case-insensitive), and
// geocoding that literal phrase guarantees a no-results failure instead of
// the user's actual location. Returning "" lets locationWithMemoryFallback
// resolve through the stored location instead.
func TestLocationFromTextIgnoresHerePhrases(t *testing.T) {
	cases := map[string]string{
		"what is the weather in my city?":   "",
		"weather in my location":            "",
		"find parks around my town":         "",
		"cafes near my place":               "",
		"what's the weather in my area now": "",
		// Real places still extract (case-insensitive capture included).
		"what's the weather in chiang mai?": "chiang mai",
	}
	for msg, want := range cases {
		if got := locationFromText(msg); got != want {
			t.Errorf("locationFromText(%q) = %q, want %q", msg, got, want)
		}
	}
}

// The readiness fast-path must never ask a question the conversation already
// answered. "the weather like there" names no place, so nothing local can
// resolve it — but the previous turn did. Hand it to the model (which holds
// the history) instead of short-circuiting into "Which city should I check?".
func TestWeatherAnaphoraDefersToModel(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())

	// An anaphoric reference reaches the model, unhandled.
	if answer, handled := al.tryReadinessFastPath("what's the weather like there", "s-anaphora", map[string]string{}); handled {
		t.Fatalf("anaphoric weather ask was short-circuited to %q", answer)
	}
	if answer, handled := al.tryReadinessFastPath("find coffee near that city", "s-anaphora-nearby", map[string]string{}); handled {
		t.Fatalf("anaphoric nearby ask was short-circuited to %q", answer)
	}

	// A bare ask with no place and no reference still asks, once, durably.
	answer, handled := al.tryReadinessFastPath("what's the weather", "s-bare", map[string]string{})
	if !handled || answer != "Which city should I check?" {
		t.Fatalf("bare weather ask = (%q, %v), want the city question", answer, handled)
	}

	// A named place is never turned into a question.
	if _, handled := al.tryReadinessFastPath("what's the weather in phuket", "s-named", map[string]string{}); handled {
		t.Fatal("a named place must not be asked about")
	}
}

// TestCapabilityFastPathNeedsWeatherAsk, not the words "temperature" and
// "weather" appearing anywhere in a message.
//
// Both of these came from one substring match. isWeatherIntent returned true
// for any message containing "temperature" or "weather", so:
//
//  1. a product-spec ask — "…read room temperature and humidity … manufacture
//     it in China" — matched on "room temperature", locationFromText matched
//     "in China", and the deterministic dispatch answered with wttr.in for
//     China without ever consulting the model;
//  2. the owner's complaint about that answer matched on "weather", the
//     readiness fast-path asked "Which city should I check?" and stored a
//     durable location continuation — which then swallowed the owner's next
//     message, rewriting it to "<original task> Location answer: are you dumb"
//     and geocoding it as a place.
//
// The fast path exists to save an LLM call on an unambiguous ask. It must not
// fire on a message whose subject is something else; when in doubt the turn
// belongs to the model, which is the safe default anyway.
func TestCapabilityFastPathNeedsWeatherAsk(t *testing.T) {
	notWeather := []string{
		// The two real turns, verbatim.
		"great. assuming you were to design your Ghost Pod and want to manufacture it in China. What specifications do you need fo the pod assuming it can also read room temperature and humidity for the first version. i also bought a KLYSTR Kit to learn hardware and sensors stuff to build you",
		"why are you tellingnme the weather i did not ask you aboht it",
		// Same shape, so the fix is not a lookup table of these strings.
		"does the pod read room temperature and humidity?",
		"list the operating temperature range for the sensor",
		"i did not ask you about the weather",
		"the weather was nice today so we went hiking",
	}
	for _, m := range notWeather {
		if isWeatherIntent(strings.ToLower(m)) {
			t.Errorf("isWeatherIntent(%q) = true, but this is not a weather ask", m)
		}
	}

	// Genuine asks must keep firing: this is a recall trade, not a shutdown.
	stillWeather := []string{
		"what's the weather in tokyo",
		"temperature in manila",
		"how hot is it today",
		"will i need an umbrella",
		"is it going to rain",
		"whats the temperature",
		"weather",
	}
	for _, m := range stillWeather {
		if !isWeatherIntent(strings.ToLower(m)) {
			t.Errorf("isWeatherIntent(%q) = false, but this is a genuine weather ask", m)
		}
	}
}

// TestWeatherComplaintNeverStoresContinuation locks down the full second
// symptom: the complaint must not ask for a city, must not leave a pending
// location continuation behind, and the owner's next message must arrive at
// resolvePendingResume untouched rather than rewritten into a geocode query.
func TestWeatherComplaintNeverStoresContinuation(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)
	const session = "s-complaint"

	complaint := "why are you tellingnme the weather i did not ask you aboht it"
	if answer, handled := al.tryReadinessFastPath(complaint, session, map[string]string{}); handled {
		t.Fatalf("a complaint about weather was turned into %q", answer)
	}
	if p, ok := skills.GetPendingDurable(ws, session); ok {
		t.Fatalf("a complaint must not store a pending continuation: %+v", p)
	}

	// The next message the owner sent in the real transcript.
	eff, resumed, _, _ := resolvePendingResume(ws, session, "are you dumb")
	if resumed {
		t.Fatalf("short reply hijacked as %q", eff)
	}
}

// recordWeatherTool stands in for the provider-backed weather tool so the real
// dispatch path can be driven end to end without a network call. Every
// invocation is recorded, which is what makes the assertion below the user's
// symptom itself rather than a proxy for it.
type recordWeatherTool struct {
	calls *[]map[string]interface{}
}

func (r *recordWeatherTool) Name() string { return "weather_now" }
func (r *recordWeatherTool) Description() string {
	return "records invocation for the dispatch test"
}
func (r *recordWeatherTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
func (r *recordWeatherTool) Execute(_ context.Context, args map[string]interface{}) *tools.ToolResult {
	*r.calls = append(*r.calls, args)
	return tools.NewToolResult("Weather in test: 1.0°C (via stub, observed 00:00).")
}

// TestDispatchNeverWeatherOnSpecQuestion drives the real dispatch path with a
// stub tool, so the loop asserts the reported symptom directly: a product-spec
// question mentioning "room temperature" and "in China" must never reach the
// weather tool, while a genuine ask must still take the fast path. The
// classifier test above pins the decision; this pins the call site, so a later
// gate added around it cannot silently reintroduce the bug.
func TestDispatchNeverWeatherOnSpecQuestion(t *testing.T) {
	var calls []map[string]interface{}
	reg := tools.NewToolRegistry()
	reg.Register(&recordWeatherTool{calls: &calls})
	al := &AgentLoop{tools: reg}

	spec := "great. assuming you were to design your Ghost Pod and want to manufacture it in China. What specifications do you need fo the pod assuming it can also read room temperature and humidity for the first version. i also bought a KLYSTR Kit to learn hardware and sensors stuff to build you"
	if ans, ok := al.tryDeterministicNetworkDispatch(spec, "s-spec", nil); ok {
		t.Fatalf("a product-spec question was answered deterministically: %q", ans)
	}
	if len(calls) != 0 {
		t.Fatalf("weather tool invoked %d time(s) for a spec question: %+v", len(calls), calls)
	}

	if _, ok := al.tryDeterministicNetworkDispatch("what's the weather in tokyo", "s-ask", nil); !ok {
		t.Fatal("a genuine weather ask must still take the fast path")
	}
	if len(calls) != 1 {
		t.Fatalf("a genuine ask must invoke the tool exactly once, got %d", len(calls))
	}
}
