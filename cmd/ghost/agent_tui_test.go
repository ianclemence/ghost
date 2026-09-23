package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ianclemence/ghost/pkg/ideas"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/routines"
)

// fakeRuntime records calls and simulates a runtime for TUI tests.
type fakeRuntime struct {
	model    string
	presets  []string
	injected []string // steering messages queued while working
	aborted  []string // sessions aborted via Esc
	answered map[string]string
	options  []providers.ModelOption // nil = derive from presets
	turns    []string
	setCalls []string
	pending  *pendingApproval
	context  string
	contexts []string
	history  map[string][]historyEntry // session key -> transcript rows
	// Tasks surface state.
	routineList []*routines.Routine
	routinesErr error
	managed     []string
	manageErr   error
	// Ideas surface state.
	ideaList []*ideas.Idea
	ideasErr error
	decided  []string
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{model: "deepseek-flash", presets: []string{"fast", "deep"}, answered: map[string]string{}}
}

func (f *fakeRuntime) GetCurrentModel() string { return f.model }
func (f *fakeRuntime) ModelPresets() []string  { return f.presets }

func (f *fakeRuntime) LoadHistory(sessionKey string) ([]historyEntry, error) {
	return f.history[sessionKey], nil
}
func (f *fakeRuntime) SetModel(t string) error {
	f.setCalls = append(f.setCalls, t)
	f.model = t
	return nil
}
func (f *fakeRuntime) InjectSteering(_, content string) { f.injected = append(f.injected, content) }
func (f *fakeRuntime) AbortTurn(sessionKey string)      { f.aborted = append(f.aborted, sessionKey) }
func (f *fakeRuntime) RefreshModels()                   {}

func (f *fakeRuntime) ModelOptions() []providers.ModelOption {
	if f.options != nil {
		return f.options
	}
	var out []providers.ModelOption
	for _, p := range f.presets {
		out = append(out, providers.ModelOption{Name: p, Target: p, Kind: "preset", Available: true})
	}
	return out
}

func (f *fakeRuntime) AllModelOptions() []providers.ModelOption {
	return f.ModelOptions()
}

func (f *fakeRuntime) RespondClarify(questionID, response string) bool {
	f.answered[questionID] = response
	return true
}

// pending, when set, is reported by PendingApproval until cleared.
func (f *fakeRuntime) PendingApproval(sessionKey string) (string, string, string, bool) {
	if f.pending == nil {
		return "", "", "", false
	}
	return f.pending.id, f.pending.title, f.pending.risk, true
}
func (f *fakeRuntime) CurrentContext(string) string {
	if f.context == "" {
		return "personal"
	}
	return f.context
}
func (f *fakeRuntime) ListContexts() []string {
	if len(f.contexts) == 0 {
		return []string{"personal"}
	}
	return f.contexts
}
func (f *fakeRuntime) SwitchContext(_, id string) error {
	for _, c := range f.ListContexts() {
		if c == id {
			f.context = id
			return nil
		}
	}
	return errUnknownContext
}

func (f *fakeRuntime) ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string, media []string, onChunk func(string), onToolCall func(string, string)) (string, error) {
	f.turns = append(f.turns, content)
	if onChunk != nil {
		onChunk("ok")
	}
	return "ok", nil
}

func (f *fakeRuntime) ListRoutines() ([]*routines.Routine, error) {
	if f.routinesErr != nil {
		return nil, f.routinesErr
	}
	return f.routineList, nil
}

func (f *fakeRuntime) ManageRoutine(op, id string) error {
	f.managed = append(f.managed, op+" "+id)
	return f.manageErr
}

func (f *fakeRuntime) ListIdeas() ([]*ideas.Idea, error) {
	if f.ideasErr != nil {
		return nil, f.ideasErr
	}
	return f.ideaList, nil
}

func (f *fakeRuntime) DecideIdea(id string, accept bool) (*ideas.Idea, error) {
	for _, idea := range f.ideaList {
		if idea.ID == id {
			cp := *idea
			if accept {
				cp.Status = ideas.StatusAccepted
			} else {
				cp.Status = ideas.StatusDismissed
			}
			f.decided = append(f.decided, id)
			return &cp, nil
		}
	}
	return nil, errTestNoIdea
}

func readyForTest(m *agentTUI) *agentTUI {
	m.width, m.height, m.ready = 80, 24, true
	m.layout()
	return m
}

func TestTUISlashHelpDoesNotSendTurn(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/help")
	if len(f.turns) != 0 {
		t.Fatalf("/help must not start a turn, got %v", f.turns)
	}
	if !hasNotice(m, "Commands") {
		t.Errorf("/help should add a help notice")
	}
}

func TestTUIUnknownCommandIsHonest(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/frobnicate")
	if !hasError(m, "unknown command") {
		t.Errorf("unknown command must be reported")
	}
}

// Ghost is one conversation: /thread opens a clearly-labelled side thread,
// and /main returns to the shared conversation and reloads its rows.
func TestTUIThreadOpensSideThreadAndMainReturns(t *testing.T) {
	f := newFakeRuntime()
	f.history = map[string][]historyEntry{
		mainConversationKey: {
			{Role: "user", Content: "shared hello"},
			{Role: "assistant", Content: "shared reply"},
		},
	}
	m := readyForTest(newAgentTUI(f, mainConversationKey))

	m.runCommand("/thread")
	if m.session == mainConversationKey {
		t.Fatal("/thread must open a side thread, not stay on main")
	}
	if !hasNotice(m, "side thread") {
		t.Errorf("/thread must say it opened a side thread, entries=%v", m.entries)
	}
	if hasNotice(m, "fresh conversation") {
		t.Errorf("/thread must not claim a fresh conversation (Ghost is one conversation)")
	}

	m.runCommand("/main")
	if m.session != mainConversationKey {
		t.Fatalf("/main must return to %q, got %q", mainConversationKey, m.session)
	}
	if !hasUser(m, "shared hello") {
		t.Errorf("/main must reload the shared conversation, entries=%v", m.entries)
	}
}

// /session reports where this terminal is, and is honest that a side thread
// is separate from the shared conversation.
func TestTUISessionReportsSharedVsThread(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, mainConversationKey))
	m.runCommand("/session")
	if !hasNotice(m, "shared conversation") {
		t.Errorf("/session on main must say it is the shared conversation, entries=%v", m.entries)
	}

	m2 := readyForTest(newAgentTUI(f, "cli:thread"))
	m2.runCommand("/sessions")
	if !hasNotice(m2, "side thread") {
		t.Errorf("/sessions must flag a side thread, entries=%v", m2.entries)
	}
}

func TestTUIModelCycleAndSet(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.cycleModel() // deepseek-flash -> first preset "fast"
	if f.model != "fast" {
		t.Errorf("cycle should pick first preset, got %q", f.model)
	}
	m.runCommand("/model deep")
	if f.model != "deep" {
		t.Errorf("/model deep should switch, got %q", f.model)
	}
}

// The canonical active model (provider:model) matches no preset name, so
// cycling must still advance every press instead of sticking on the
// first preset — the "Ctrl+L always picks ollama" regression test.
func TestTUIModelCycleAdvancesPastCanonical(t *testing.T) {
	f := newFakeRuntime()
	f.model = "ollama:qwen3"
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.cycleModel()
	if f.model != "fast" {
		t.Fatalf("first press should take the first preset, got %q", f.model)
	}
	// Simulate the daemon reporting back the canonical form, as happens
	// after a real SetModel (preset name in, provider:model out).
	f.model = "testprovider:fast"
	m.cycleModel()
	if f.model != "deep" {
		t.Fatalf("second press must advance past the canonical match, got %q", f.model)
	}
	m.cycleModel()
	if f.model != "fast" {
		t.Fatalf("rotation must wrap around, got %q", f.model)
	}
}

func TestTUIQueueWhileWorkingInjectsSteering(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.send("change of plan")
	if len(m.queued) != 1 || m.queued[0] != "change of plan" {
		t.Fatalf("working send must queue, got %v", m.queued)
	}
	if len(f.injected) != 1 || f.injected[0] != "change of plan" {
		t.Errorf("working send must inject a steering message, got %v", f.injected)
	}
	if len(f.turns) != 0 {
		t.Errorf("working send must not start a new turn")
	}
}

func TestTUISendWhenIdleStartsTurn(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.send("hello")
	if !m.working {
		t.Errorf("sending while idle must mark working")
	}
	if !hasUser(m, "hello") {
		t.Errorf("user message must appear in transcript")
	}
}

func TestProviderLocality(t *testing.T) {
	cases := map[string]string{
		"deepseek-flash": "cloud",
		"gpt-4o":         "cloud",
		"qwen3:8b":       "local",
		"llama3":         "local",
		"mystery-model":  "pod",
	}
	for model, want := range cases {
		if got := providerLocality(model); got != want {
			t.Errorf("providerLocality(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestTUIHistoryRecall(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.history = []string{"one", "two"}
	m.input.Reset()
	m.recallHistory(-1)
	if m.input.Value() != "two" {
		t.Errorf("first ↑ should recall the last message, got %q", m.input.Value())
	}
	m.recallHistory(-1)
	if m.input.Value() != "one" {
		t.Errorf("second ↑ should recall the earlier message, got %q", m.input.Value())
	}
}

// helpers
func hasNotice(m *agentTUI, sub string) bool {
	for _, e := range m.entries {
		if e.kind == entryNotice && strings.Contains(e.text, sub) {
			return true
		}
	}
	return strings.Contains(m.lastFlush, sub)
}
func hasError(m *agentTUI, sub string) bool {
	for _, e := range m.entries {
		if e.kind == entryError && strings.Contains(e.text, sub) {
			return true
		}
	}
	return strings.Contains(m.lastFlush, sub)
}
func hasUser(m *agentTUI, sub string) bool {
	for _, e := range m.entries {
		if e.kind == entryUser && strings.Contains(e.text, sub) {
			return true
		}
	}
	return strings.Contains(m.lastFlush, sub)
}

// A turn that ends blocked on a durable approval shows a card, not prose, and
// records nothing as done.
func TestTUIApprovalCardOnPending(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	f.pending = &pendingApproval{id: "req-1", title: "Send this email?", risk: "consequential"}
	m.Update(turnDoneMsg{text: "waiting", err: nil})
	if m.approval == nil {
		t.Fatalf("a pending approval must raise the card")
	}
	if !hasNotice(m, "needs your approval") {
		t.Errorf("transcript should note the approval")
	}
}

// The inline approval block is measured, not guessed: its painted height
// must equal the layout estimate at any width, so the dock never jumps
// between the wide one-row options and the narrow stacked options.
func TestTUIApprovalHeightMatchesPaint(t *testing.T) {
	f := newFakeRuntime()
	for _, w := range []int{120, 80, 40} {
		m := readyForTest(newAgentTUI(f, "cli:test"))
		m.width, m.height = w, 24
		m.approval = &pendingApproval{id: "r", title: "Send this email to the landlord about the deposit?", risk: "consequential"}
		painted := len(strings.Split(m.approvalCard(), "\n"))
		if got := m.estimatedApprovalHeight(); got != painted {
			t.Errorf("width %d: approval estimate %d must equal paint %d", w, got, painted)
		}
	}
}

// Choosing "always allow" sends the recognized phrase as a normal turn, so the
// governed resume path runs — the CLI never authorizes around the broker.
func TestTUIApprovalSendsGrantPhrase(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.approval = &pendingApproval{id: "req-1", title: "Send?", risk: "consequential"}
	m.resolveApproval("always allow")
	if m.approval != nil {
		t.Errorf("resolving must clear the card")
	}
	if !waitForTurns(f, 1) || f.turns[0] != "always allow" {
		t.Fatalf("grant phrase must be sent as a turn, got %v", f.turns)
	}
}

// While the card is up, keys 1/2/3 map to the three grants; other keys do
// nothing (so a stray keystroke cannot approve).
func TestTUIApprovalKeys(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.approval = &pendingApproval{id: "req-1", title: "Send?", risk: "consequential"}
	// A stray key must not resolve.
	m.handleKey(keyMsgFor('x'))
	if m.approval == nil {
		t.Fatalf("a non-choice key must not resolve the approval")
	}
	m.handleKey(keyMsgFor('3'))
	if m.approval != nil {
		t.Errorf("key 3 must resolve (deny)")
	}
	if !waitForTurns(f, 1) || f.turns[0] != "deny" {
		t.Fatalf("key 3 should send deny, got %v", f.turns)
	}
}

func TestApprovalRiskNote(t *testing.T) {
	if approvalRiskNote("high_impact") == "" || approvalRiskNote("consequential") == "" {
		t.Errorf("known risks must have notes")
	}
	if approvalRiskNote("weird") != "" {
		t.Errorf("unknown risk must be silent")
	}
}

var errUnknownContext = fmtError("unknown context")

var errTestRoutinesDown = fmtError("routines down")

var errTestNoIdea = fmtError("no idea")

func fmtError(s string) error { return &simpleErr{s} }

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }

func keyMsgFor(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// waitForTurns waits briefly for the async turn goroutine to record its turn.
func waitForTurns(f *fakeRuntime, n int) bool {
	for i := 0; i < 100; i++ {
		if len(f.turns) >= n {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// waitForAnswer waits briefly for the async clarify post to land.
func waitForAnswer(f *fakeRuntime, qid string) bool {
	for i := 0; i < 100; i++ {
		if _, ok := f.answered[qid]; ok {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

func TestTUIContextShowAndSwitch(t *testing.T) {
	f := newFakeRuntime()
	f.contexts = []string{"personal", "work"}
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/context")
	if !hasNotice(m, "context: personal") {
		t.Errorf("showing context should report the current one")
	}
	m.runCommand("/context work")
	if f.CurrentContext("cli:test") != "work" {
		t.Errorf("switch should move the session, got %q", f.CurrentContext("cli:test"))
	}
}

func TestTUIContextUnknownFailsClosed(t *testing.T) {
	f := newFakeRuntime()
	f.contexts = []string{"personal"}
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/context doesnotexist")
	if f.CurrentContext("cli:test") != "personal" {
		t.Errorf("unknown context must not move the session")
	}
	if !hasError(m, "context") {
		t.Errorf("unknown context must be reported")
	}
}

func TestTUIRewindRestoresLastUserMessage(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.append(entry{kind: entryUser, text: "first"})
	m.append(entry{kind: entryAssistant, text: "reply"})
	m.append(entry{kind: entryUser, text: "second"})
	m.rewind()
	if m.input.Value() != "second" {
		t.Errorf("rewind should restore the last user message, got %q", m.input.Value())
	}
}

// View renders each region exactly once: no top header,
// transcript, prompt box, 3-line footer. Regression test for the doubled
// header/welcome/input and the stray title label above the prompt box.
func TestTUIViewRendersSingleChrome(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.renderTranscript()
	view := m.View()
	// No "prompt" title label above the box (the box is its own label).
	if strings.Contains(view, " prompt\n") || strings.Contains(view, "\n prompt ") {
		t.Errorf("view must not label the prompt box, got %q", view)
	}
	// The footer no longer repeats internal identifiers; the model appears
	// exactly once, on the stats line.
	if n := strings.Count(view, "cli:test"); n != 0 {
		t.Errorf("session id must not surface in the footer, found %d: %q", n, view)
	}
	if strings.Contains(view, "context personal") || strings.Contains(view, "context ") {
		t.Errorf("footer must not print the context label, got %q", view)
	}
	if n := strings.Count(view, "deepseek-flash"); n != 1 {
		t.Errorf("model should appear once in the footer, found %d: %q", n, view)
	}
	// Footer is exactly 2 lines: stats/model, shortcuts. The tagline lives
	// on the welcome card, never in the footer.
	if got := len(m.footerLines()); got != 2 {
		t.Fatalf("footer must be 2 lines, got %d", got)
	}
	if strings.Contains(strings.Join(m.footerLines(), "\n"), ghostTagline) {
		t.Errorf("the tagline must not appear in the footer")
	}
	// The welcome card is printed into the scrollback (not the live view),
	// and carries the Ghost tagline.
	welcome := m.welcomeCard()
	if n := strings.Count(welcome, "switch thinking engine"); n != 1 {
		t.Errorf("welcome card should appear once, found %d: %q", n, welcome)
	}
	if !strings.Contains(welcome, "Your AI. Your Memory. Your Machine.") {
		t.Errorf("welcome card must carry the Ghost tagline, got %q", welcome)
	}
	if strings.Contains(view, ghostTagline) {
		t.Errorf("the live view must not carry the welcome card, got %q", view)
	}
	// The idle hint leads with the command surface left and the exit right,
	// and never spells out the obvious Enter-to-send.
	keys := m.footerKeysLine()
	if strings.Contains(keys, "enter send") {
		t.Errorf("idle footer must not spell out enter send, got %q", keys)
	}
	trimmed := keys
	if !strings.Contains(trimmed, "/ commands") {
		t.Errorf("idle keys must lead with the command surface, got %q", keys)
	}
	if !strings.Contains(trimmed, "esc quit") {
		t.Errorf("esc quit must come last, got %q", keys)
	}
	if strings.Index(trimmed, "/ commands") > strings.Index(trimmed, "esc quit") {
		t.Errorf("/ commands must sit left, esc quit right, got %q", keys)
	}
	if strings.Contains(keys, "tab complete") || strings.Contains(keys, "ctrl+l") || strings.Contains(keys, "ctrl+p") {
		t.Errorf("idle keys must not carry shortcut cruft, got %q", keys)
	}
}

// The footer digest is real Ghost session state: turns accumulate, extra
// topic contexts surface, queued steering is counted, and a held approval
// reads "waiting for you" — never a decorative token/context figure.
func TestTUIFooterSummaryReflectsGhostState(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	if got := m.footerSummary(); got != "ready" {
		t.Errorf("fresh session digest must read ready, got %q", got)
	}
	m.turnCount = 3
	if got := m.footerSummary(); !strings.Contains(got, "3 turns") {
		t.Errorf("digest must count turns, got %q", got)
	}
	f.contexts = []string{"personal", "work", "health"}
	if got := m.footerSummary(); !strings.Contains(got, "3 contexts") {
		t.Errorf("digest must count Ghost contexts, got %q", got)
	}
	m.queued = []string{"a", "b"}
	if got := m.footerSummary(); !strings.Contains(got, "2 queued") {
		t.Errorf("digest must count queued steering, got %q", got)
	}
	m.approval = &pendingApproval{id: "r", title: "t", risk: "consequential"}
	if got := m.footerSummary(); got != "waiting for you" {
		t.Errorf("held approval must read waiting for you, got %q", got)
	}
	// The digest is never the live spinner (that lives in the composer rule).
	m.approval = nil
	m.working = true
	if got := m.footerSummary(); strings.Contains(got, m.spinner()) {
		t.Errorf("digest must not duplicate the composer spinner, got %q", got)
	}
}

// The shortcuts line names the escape routes for each state
// (terminal-ui skill: input-escape-routes).
func TestTUIFooterKeysContextual(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	if !strings.Contains(m.footerKeysLine(), "esc quit") {
		t.Errorf("idle keys must name esc quit, got %q", m.footerKeysLine())
	}
	m.working = true
	if !strings.Contains(m.footerKeysLine(), "esc aborts") {
		t.Errorf("working keys must name esc abort, got %q", m.footerKeysLine())
	}
	m.working = false
	m.approval = &pendingApproval{id: "1", title: "Send?", risk: "consequential"}
	if !strings.Contains(m.footerKeysLine(), "esc leaves pending") {
		t.Errorf("approval keys must name the esc outcome, got %q", m.footerKeysLine())
	}
	m.approval = nil
	m.input.SetValue("/mod")
	m.paletteSel = 0
	if !strings.Contains(m.footerKeysLine(), "tab complete") || !strings.Contains(m.footerKeysLine(), "enter run") {
		t.Errorf("palette keys must name tab-complete and enter-run, got %q", m.footerKeysLine())
	}
}

// The composer is responsive: it opens at one row, grows as the sentence
// wraps, and stops at the cap (30% of the viewport, floor five), where it
// scrolls inside the box. The estimate always equals rows + both rules.
func TestTUIComposerGrowsWithContentAndCaps(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24

	m.input.SetValue("")
	m.layout()
	if h := m.input.Height(); h != 1 {
		t.Errorf("empty composer must be one row, got %d", h)
	}
	if got := m.estimatedInputHeight(); got != 3 {
		t.Errorf("one-row estimate must be rules + 1 = 3, got %d", got)
	}

	// A short sentence stays one row.
	m.input.SetValue("hello")
	m.layout()
	if h := m.input.Height(); h != 1 {
		t.Errorf("short input must stay one row, got %d", h)
	}

	// A long sentence wraps and grows the box (width 80, so ~79 cols/row).
	m.input.SetValue(strings.Repeat("word ", 60)) // ~300 cols → several rows
	m.layout()
	grew := m.input.Height()
	if grew <= 1 {
		t.Fatalf("wrapped input must grow the composer, got %d rows", grew)
	}
	if got := m.estimatedInputHeight(); got != grew+2 {
		t.Errorf("estimate %d must equal rows %d + 2 rules", got, grew)
	}

	// A long unbroken word (no spaces) hard-breaks and still grows the box,
	// exactly as the textarea wraps it — a long URL or token must not stay
	// a one-row box that hides the text.
	m.input.SetValue(strings.Repeat("x", 79*4)) // ~4 rows at width 80
	m.layout()
	if h := m.input.Height(); h < 3 {
		t.Errorf("a long unbroken word must grow the composer, got %d rows", h)
	}
	if got, want := m.input.Height(), m.composerRows(); got != want {
		t.Errorf("textarea height %d must match measured rows %d", got, want)
	}

	// Past the cap the box stops growing and scrolls instead.
	cap := m.composerCapRows()
	m.input.SetValue(strings.Repeat("word ", 400))
	m.layout()
	if h := m.input.Height(); h != cap {
		t.Errorf("over-cap input must hold at the cap %d, got %d", cap, h)
	}
	if got := m.estimatedInputHeight(); got != cap+2 {
		t.Errorf("capped estimate must be cap %d + 2, got %d", cap, got)
	}
}

// The composer holds at seven rows maximum (never more), regardless of a
// tall terminal, and scrolls once past that.
// The dock (preview area + composer + footer) must not change height at a
// turn boundary. Bubbletea only repaints the lines the view occupies, so a
// view that shrank in the same tick as tea.Println clobbered the printed
// reply — the bug where an answer only appeared after reopening the TUI.
func TestTUIDockHeightStableAcrossTurn(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24
	m.layout()
	height := func() int { return len(strings.Split(m.View(), "\n")) }
	idle := height()

	m.send("hello")
	if got := height(); got != idle {
		t.Errorf("dock height changed on send: idle %d, after send %d", idle, got)
	}
	m.Update(streamChunkMsg{text: "a reply that streams in over time"})
	if got := height(); got != idle {
		t.Errorf("dock height changed while streaming: idle %d, got %d", idle, got)
	}
	m.Update(turnDoneMsg{text: "a reply that streams in over time", err: nil})
	if got := height(); got != idle {
		t.Errorf("dock height changed on turn end: idle %d, got %d", idle, got)
	}
}

func TestTUIComposerCapsAtSevenRows(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 60 // 30% would be 18 rows; the cap is 7
	if cap := m.composerCapRows(); cap != 7 {
		t.Fatalf("composer cap must be 7 rows, got %d", cap)
	}
	m.input.SetValue(strings.Repeat("word ", 400))
	m.layout()
	if h := m.input.Height(); h != 7 {
		t.Errorf("composer must hold at 7 rows, got %d", h)
	}
}

// Past the cap the textarea scrolls to keep the caret visible: the text
// being typed must be on screen, not pushed below the window. This was the
// bug where the first line showed while the cursor typed off-screen.
func TestTUIComposerScrollsToCaretPastCap(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 40
	first, last := "FIRSTSENTENCE", "LASTWORD"
	m.input.SetValue(first + " " + strings.Repeat("more words here ", 80) + last)
	m.layout()
	view := m.input.View()
	if !strings.Contains(view, last) {
		t.Errorf("past the cap the caret's text must be visible\n%s", view)
	}
	// Under the cap everything stays visible from the top.
	short := readyForTest(newAgentTUI(f, "cli:test"))
	short.width, short.height = 80, 40
	short.input.SetValue("a short sentence")
	short.layout()
	if !strings.Contains(short.input.View(), "a short sentence") {
		t.Errorf("under the cap the whole text must be visible")
	}
}

// The composer is exactly two full-width rules, no side borders, no
// The transcript lives in the terminal's own scrollback: committed entries
// are emitted once via tea.Println so native scrolling reaches every
// previous message, and the live View() holds only the bottom region (never
// the committed transcript). This is the opencode-CLI model.
func TestTUITranscriptPrintsToScrollback(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 20
	m.append(entry{kind: entryUser, text: "first question", at: time.Now()})
	m.append(entry{kind: entryAssistant, text: "first answer", at: time.Now()})

	if len(m.entries) != 2 {
		t.Fatalf("nothing should flush before a flush, entries=%d", len(m.entries))
	}
	if cmd := m.flushScrollback(); cmd == nil {
		t.Fatalf("committed entries must flush to the scrollback")
	}
	if len(m.entries) != 0 {
		t.Errorf("flush must drain the buffer (Scout flushCmds model), left %d", len(m.entries))
	}
	// A second flush with nothing new is a no-op (never reprints history).
	if cmd := m.flushScrollback(); cmd != nil {
		t.Errorf("a second flush with no new entries must be a no-op")
	}

	// The live view must not contain the committed transcript.
	m.renderTranscript()
	view := m.View()
	if strings.Contains(view, "first question") || strings.Contains(view, "first answer") {
		t.Errorf("committed entries must not be in the live view, got %q", view)
	}
}

// The palette is ordered the way the help lists commands: discovery and
// state, memory and behavior, navigation, display, then housekeeping.
func TestTUIPaletteOrderMatchesHelp(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("/")
	want := []string{"help", "session", "model", "context", "memory", "routines", "tasks", "task", "ideas", "idea", "thread", "main", "rewind", "details", "clear", "quit"}
	items := m.paletteMatches()
	if len(items) != len(want) {
		t.Fatalf("palette has %d commands, want %d", len(items), len(want))
	}
	for i, name := range want {
		if items[i].name != name {
			t.Errorf("palette[%d] = %q, want %q", i, items[i].name, name)
		}
	}
}

// Starting a turn restarts the activity spinner, so the cube before
// "thinking"/"searching" always animates — the tick loop stops when a turn
// ends and must resume on the next one.
func TestTUISendRestartsSpinner(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	if cmd := m.send("hello"); cmd == nil {
		t.Fatalf("starting a turn must return a spinner tick command")
	}
	if !m.working {
		t.Fatalf("sending must mark the turn working")
	}
}

// The prompt rule keeps its idle colour while a turn runs; only the
// embedded status text is accented.
func TestTUIComposerRuleKeepsIdleColor(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24
	m.working = true
	m.spinFrame = 0
	m.toolHistory = []toolStep{{tool: "web_search", label: "Searching the web…"}}
	rule := m.composerTopRule()
	// The rule never takes a different colour than the idle bar.
	if !strings.Contains(rule, stylePromptBar.Render("── ")) {
		t.Errorf("the top rule prefix must keep the idle colour, got %q", rule)
	}
	// The status is the only accented part.
	if !strings.Contains(rule, styleWorking.Render(spinnerFrames[0]+" Searching the web…")) {
		t.Errorf("the status must be accented, got %q", rule)
	}
}

func TestTUIComposerIsPIRules(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24
	box := m.promptBox()
	rows := strings.Split(box, "\n")
	want := m.composerRows() + 2 // text rows + top/bottom rules
	if len(rows) != want {
		t.Fatalf("composer must be %d rows (rule + %d text + rule), got %d: %q", want, m.composerRows(), len(rows), box)
	}
	for i, ln := range rows {
		if got := lipgloss.Width(ln); got != 80 {
			t.Errorf("composer row %d must span the full width (80), got %d: %q", i, got, ln)
		}
	}
	if !strings.HasPrefix(rows[0], "──") && strings.Trim(rows[0], "─") != "" {
		t.Errorf("top rule must be all ─ when idle, got %q", rows[0])
	}
	if strings.Contains(box, "│") || strings.Contains(box, "┃") {
		t.Errorf("the composer has no side borders, got %q", box)
	}

	// While working the status appears once, embedded in the top rule.
	m.working = true
	m.elapsed = 4200_000_000
	// No tool active yet: the rule reads "Thinking".
	m.toolHistory = nil
	m.spinFrame = 0
	top := strings.Split(m.promptBox(), "\n")[0]
	if !strings.Contains(top, m.spinner()) {
		t.Errorf("working top rule must carry the spinner, got %q", top)
	}
	if n := strings.Count(top, m.spinner()); n != 1 {
		t.Errorf("spinner must appear once in the top rule, found %d: %q", n, top)
	}
	if !strings.Contains(top, "Thinking") {
		t.Errorf("the rule must read 'Thinking' when no tool runs, got %q", top)
	}
	if strings.Contains(top, "working") {
		t.Errorf("the rule must not say 'working', got %q", top)
	}

	// An active tool names itself in the rule instead of "Thinking".
	m.toolHistory = []toolStep{{tool: "read_file", label: "Reading notes.md", done: true}, {tool: "web_search", label: "Searching the web…"}}
	top = strings.Split(m.promptBox(), "\n")[0]
	if !strings.Contains(top, "Searching the web…") {
		t.Errorf("the rule must name the active tool, got %q", top)
	}
	if strings.Contains(top, "Thinking") {
		t.Errorf("the rule must not say 'Thinking' while a tool runs, got %q", top)
	}
	// The rule names the activity and elapsed time only — never a tool
	// count (that detail lives in the /details trail).
	if strings.Contains(top, "tool") || strings.Contains(top, "2 tool") {
		t.Errorf("the rule must not carry a tool count, got %q", top)
	}

	// The dock preview must not repeat the activity: no tool row above the
	// composer by default.
	if block := m.dockPreview(); strings.Contains(block, "Searching the web") {
		t.Errorf("the dock must not repeat the active step, got %q", block)
	}
}

// The user bubble is a full-width background block: edge to edge, text
// inset by one cell, one blank padding row above and below.
// The model picker is not a floating modal: it renders below the prompt
// box, sharing the palette's bare list rows.
func TestTUIModelPickerIsInlineBelowBox(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/model")
	if m.modal == nil {
		t.Fatal("/model must open the picker")
	}
	m.layout()
	m.renderTranscript()
	view := m.View()
	viewRows := strings.Split(strings.TrimRight(view, "\n"), "\n")
	box := strings.Split(m.promptBox(), "\n")
	bottomRule := box[len(box)-1]
	// The picker's title and filter must sit below the composer's bottom rule.
	ruleIdx, titleIdx := -1, -1
	for i, ln := range viewRows {
		if ruleIdx < 0 && strings.Contains(ln, bottomRule) {
			ruleIdx = i
		}
		if titleIdx < 0 && strings.Contains(ln, "Models") && strings.Contains(ln, "esc") {
			titleIdx = i
		}
	}
	if ruleIdx < 0 || titleIdx < 0 {
		t.Fatalf("picker title and composer rule must both be present\nview:\n%s", view)
	}
	if titleIdx <= ruleIdx {
		t.Errorf("picker must render below the prompt box (rule row %d, title row %d)", ruleIdx, titleIdx)
	}
	if strings.Contains(view, "╭") || strings.Contains(view, "╰") {
		t.Errorf("picker must not be a bordered modal, got %q", view)
	}
}

// Day dividers group the transcript like chat apps: one opens the
// transcript, another appears wherever the calendar day flips — never
// between same-day messages, never for undated rows.
func TestTUIDayDividers(t *testing.T) {
	if dayLabel(time.Time{}) != "" {
		t.Errorf("zero time must have no divider")
	}
	if dayLabel(time.Now()) != "Today" {
		t.Errorf("today must label Today")
	}
	if dayLabel(time.Now().AddDate(0, 0, -1)) != "Yesterday" {
		t.Errorf("yesterday must label Yesterday")
	}
	old := time.Now().AddDate(0, 0, -5)
	if dayLabel(old) != old.Format("2 January 2006") {
		t.Errorf("older days must show the date, got %q", dayLabel(old))
	}
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	now := time.Now()
	m.append(entry{kind: entryUser, text: "a", at: now})
	m.append(entry{kind: entryAssistant, text: "b", at: now})
	m.append(entry{kind: entryUser, text: "c", at: now.AddDate(0, 0, -1)})
	content := m.pendingScrollback()
	if n := strings.Count(content, "Today"); n != 1 {
		t.Errorf("same-day rows share one divider, found %d", n)
	}
	if !strings.Contains(content, "Yesterday") {
		t.Errorf("day flip must divide, got %q", content)
	}
}

// Enter with the palette open completes the highlighted command AND runs
// it immediately — every slash command is valid with zero args, so there
// is no dead complete-only state. (Tab is the compose-first key.) This is
// the "command unknown unless written in full" regression test: the
// half-typed text must never execute literally.
func TestTUIEnterAcceptsPaletteCompletion(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("/mod")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.modal == nil {
		t.Fatalf("enter on /mod should run the completed /model picker, modal=%v input=%q", m.modal, m.input.Value())
	}
	if len(f.turns) != 0 {
		t.Fatalf("a slash command must not start a chat turn, got %v", f.turns)
	}
}

// Tab completes without submitting, for composing arguments first.
func TestTUITabCompletesPalette(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("/mem")
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.input.Value(); got != "/memory " {
		t.Fatalf("tab should complete to /memory, got %q", got)
	}
	if len(f.turns) != 0 {
		t.Fatalf("completing must not start a turn, got %v", f.turns)
	}
}

// Completion preserves already-typed arguments, then runs.
func TestTUICompletionPreservesArgs(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("/mod deep")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if f.model != "deep" {
		t.Fatalf("completed /model deep must switch the model, got %q (input %q)", f.model, m.input.Value())
	}
}

// Esc is contextual cancel at every level: palette, then the running
// turn, then the TUI itself (the session persists, so quitting is safe).
func TestTUIEscPriorityChain(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	// Palette open: Esc dismisses it without quitting.
	m.input.SetValue("/mod")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.input.Value() != "" {
		t.Errorf("esc must dismiss the palette, input=%q", m.input.Value())
	}
	if m.quitting {
		t.Errorf("dismissing the palette must not quit")
	}
	// Working: Esc aborts the turn.
	m.working = true
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if len(f.aborted) != 1 {
		t.Errorf("esc must abort the running turn, got %v", f.aborted)
	}
	if !hasNotice(m, "aborted") {
		t.Errorf("abort must be narrated")
	}
	// Idle and empty: Esc closes the TUI.
	m.working = false
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.quitting {
		t.Errorf("idle esc must close the TUI")
	}
}

// The palette scrolls: the select list keeps the selection inside the
// maxVisible window, prints a `(n/total)` footer while scrolled, and the
// painted height matches the layout estimate exactly.
func TestTUIPaletteScrollWindow(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("/")
	total := len(m.paletteMatches())
	if total <= paletteMaxRows {
		t.Fatalf("need >%d commands to test scrolling, got %d", paletteMaxRows, total)
	}
	m.paletteSel = total - 1
	m.clampPalette()
	items, off, more := m.paletteWindow()
	if !more {
		t.Errorf("bottom selection must flag the scroll footer")
	}
	if off+len(items) != total {
		t.Errorf("window must end at the last row, off=%d shown=%d total=%d", off, len(items), total)
	}
	view := m.paletteView()
	if !strings.Contains(view, fmt.Sprintf("(%d/%d)", total, total)) {
		t.Errorf("scroll footer must show (n/total), got %q", view)
	}
	if strings.Contains(view, "┃") {
		t.Errorf("the palette has no side bar, got %q", view)
	}
	rows := strings.Count(strings.TrimSpace(view), "\n") + 1
	if rows != m.paletteHeight() {
		t.Errorf("painted rows (%d) must equal estimated height (%d)", rows, m.paletteHeight())
	}
}

// The model picker modal: opens on bare /model, filters as you type,
// Enter picks through setModel, Esc closes without touching anything.
func TestTUIModelModal(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/model")
	if m.modal == nil {
		t.Fatalf("/model should open the picker modal")
	}
	if m.modal.title != "Models" {
		t.Errorf("modal title should be Models, got %q", m.modal.title)
	}
	// Filter narrows to one preset.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 's', 't'}})
	items := m.modalMatches()
	if len(items) != 1 || items[0].label != "fast" {
		t.Fatalf("filter 'ast' should match fast only, got %+v", items)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.modal != nil {
		t.Errorf("picking must close the modal")
	}
	if f.model != "fast" {
		t.Errorf("picking must switch the model, got %q", f.model)
	}
}

func TestTUIModelModalEscCloses(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/model")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.modal != nil {
		t.Errorf("esc must close the modal")
	}
	if len(f.setCalls) != 0 {
		t.Errorf("dismissing must not switch models, got %v", f.setCalls)
	}
}

// Approval panel: ←/→ moves the cursor, Enter confirms the selection;
// 1/2/3 keep answering directly.
func TestTUIApprovalEnterConfirmsSelection(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.approval = &pendingApproval{id: "req-1", title: "Send?", risk: "consequential"}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}) // → always allow
	if m.approvalSel != 1 {
		t.Fatalf("right should move to index 1, got %d", m.approvalSel)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !waitForTurns(f, 1) || f.turns[0] != "always allow" {
		t.Fatalf("enter should confirm the selection, got %v", f.turns)
	}
}

// A configured-but-unlisted provider (the deepseek case) must appear in
// the picker and the Ctrl+L rotation — never silently missing.
// The picker offers only usable models (Pi style): a configured-but-unlisted
// provider appears, while an unkeyed preset is hidden. Cycling rotates the
// usable set and never lands on an unavailable entry.
func TestTUIModelOptionsIncludeProviders(t *testing.T) {
	f := newFakeRuntime()
	f.options = []providers.ModelOption{
		{Name: "local", Provider: "ollama", Model: "ollama/qwen3:0.6b", Target: "local", Kind: "preset", Available: true},
		{Name: "deepseek", Provider: "deepseek", Model: "deepseek-flash", Target: "deepseek:deepseek-flash", Kind: "provider", Available: true},
		{Name: "broken", Provider: "x", Model: "y", Target: "broken", Kind: "preset", Available: false, Reason: "no key"},
	}
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/model")
	if m.modal == nil {
		t.Fatalf("/model should open the picker")
	}
	var labels []string
	for _, it := range m.modalMatches() {
		labels = append(labels, it.label)
	}
	if len(labels) != 2 || labels[0] != "local" || labels[1] != "deepseek" {
		t.Fatalf("picker must list only usable options, got %v", labels)
	}
	for _, it := range m.modalMatches() {
		if it.label == "broken" {
			t.Fatalf("unavailable preset must not be offered: %v", labels)
		}
	}
	// Cycling rotates the usable set and wraps without hitting unavailable.
	f.model = "weird:thing"
	m.cycleModel()
	if f.model != "local" {
		t.Fatalf("first press should take the first usable option, got %q", f.model)
	}
	m.cycleModel()
	if f.model != "deepseek:deepseek-flash" {
		t.Fatalf("cycle must reach the provider target, got %q", f.model)
	}
	m.cycleModel()
	if f.model != "local" {
		t.Fatalf("cycle must wrap over usable entries only, got %q", f.model)
	}
}

// The picker lists every usable model with no scope toggle: Tab is a
// no-op and Enter switches.
func TestTUIModelPickerShowsAll(t *testing.T) {
	f := newFakeRuntime()
	f.options = []providers.ModelOption{
		{Name: "local", Provider: "ollama", Model: "ollama/qwen3:0.6b", Target: "local", Kind: "preset", Available: true},
		{Name: "deepseek", Provider: "deepseek", Model: "deepseek-flash", Target: "deepseek:deepseek-flash", Kind: "provider", Available: true},
	}
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/model")
	if m.modal == nil {
		t.Fatalf("/model should open the picker")
	}
	if len(m.modalMatches()) != 2 {
		t.Fatalf("picker should show all usable models, got %d", len(m.modalMatches()))
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.modal == nil || len(m.modalMatches()) != 2 {
		t.Fatalf("tab must not change the picker")
	}
}

// Cycling covers every usable model and wraps; a single usable model
// reports honestly instead of switching nowhere.
func TestTUICycleAcrossAll(t *testing.T) {
	f := newFakeRuntime()
	f.options = []providers.ModelOption{
		{Name: "local", Provider: "ollama", Model: "ollama/qwen3:0.6b", Target: "local", Kind: "preset", Available: true},
		{Name: "deepseek", Provider: "deepseek", Model: "deepseek-flash", Target: "deepseek:deepseek-flash", Kind: "provider", Available: true},
	}
	m := readyForTest(newAgentTUI(f, "cli:test"))
	f.model = "weird:thing"
	m.cycleModel()
	if f.model != "local" {
		t.Fatalf("first press should take the first usable option, got %q", f.model)
	}
	m.cycleModel()
	if f.model != "deepseek:deepseek-flash" {
		t.Fatalf("cycle must reach the provider target, got %q", f.model)
	}
	m.cycleModel()
	if f.model != "local" {
		t.Fatalf("cycle must wrap, got %q", f.model)
	}
	f.options = f.options[:1]
	f.model = "local"
	m.cycleModel()
	if f.model != "local" {
		t.Fatalf("single usable model must not switch, got %q", f.model)
	}
	if !hasNotice(m, "no usable model configured") {
		t.Fatalf("expected an honest notice, entries: %+v", m.entries)
	}
}

// Picking /task bare from the palette must stay composed with an arg hint:
// running it with zero args only errors, which reads as a broken command.
func TestTUIPaletteEnterComposesTask(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("/task")
	m.updateInner(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.input.Value(); got != "/task " {
		t.Fatalf("bare /task must stay composed, input=%q", got)
	}
	if !hasNotice(m, "add arguments") {
		t.Fatalf("expected an arg hint, entries: %+v", m.entries)
	}
	if hasError(m, "usage") {
		t.Fatalf("composing must not print the usage error")
	}
}

// A fully typed /task with args still runs from the palette path.
func TestTUIPaletteEnterRunsTaskWithArgs(t *testing.T) {
	f := newFakeRuntime()
	f.routineList = []*routines.Routine{{ID: "abc123", Name: "Morning brief"}}
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("/task pause 1")
	m.updateInner(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.input.Value(); got != "" {
		t.Fatalf("ran command must clear the input, input=%q", got)
	}
	if len(f.managed) != 1 || f.managed[0] != "pause abc123" {
		t.Fatalf("expected pause to run, managed=%v", f.managed)
	}
}

// A clarify request mid-turn must surface the question and route the next
// Enter to the blocked turn (mobile-card parity) — never a new turn.
func TestTUIClarifyFlow(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.Update(clarifyRequestMsg{questionID: "q-1", question: "Which color?", choices: []string{"red", "blue"}})
	if m.clarify == nil || m.clarify.questionID != "q-1" {
		t.Fatalf("clarify must pend, got %+v", m.clarify)
	}
	if !hasNotice(m, "Which color?") {
		t.Errorf("question must appear in the transcript")
	}
	m.input.SetValue("2")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !waitForAnswer(f, "q-1") || f.answered["q-1"] != "blue" {
		t.Fatalf("number must map to the choice, got %q", f.answered["q-1"])
	}
	if len(f.turns) != 0 {
		t.Fatalf("answering must not start a new turn, got %v", f.turns)
	}
	if !hasUser(m, "blue") {
		t.Errorf("the answer must appear as the user's words")
	}
}

// A failed clarify answer must say so and clear the pending question.
func TestTUIClarifyAnswerFailed(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.clarify = &pendingClarify{questionID: "q-9", question: "Q?", choices: nil}
	m.Update(clarifyAnswerMsg{ok: false})
	if m.clarify != nil {
		t.Errorf("failed answer must clear the pending question")
	}
	if !hasError(m, "expired") {
		t.Errorf("failed answer must be reported")
	}
}

// Esc while a question pends must drop it along with the turn.
func TestTUIEscClearsClarify(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.clarify = &pendingClarify{questionID: "q-1", question: "Q?", choices: nil}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.clarify != nil {
		t.Errorf("abort must clear the pending question")
	}
	if len(f.aborted) != 1 || f.aborted[0] != "cli:test" {
		t.Errorf("abort must reach the runtime, got %v", f.aborted)
	}
}

// Daemon tool labels arrive complete and must not be relabeled.
func TestTUIToolProgressPassthrough(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.Update(toolProgressMsg{tool: "exec", label: "Running: ls"})
	if len(m.toolHistory) != 1 || m.toolHistory[0].label != "Running: ls" || m.toolHistory[0].tool != "exec" {
		t.Fatalf("server label must land verbatim, got %+v", m.toolHistory)
	}
}

// The composer has no manual newline binding: Ctrl+J does nothing. Growth
// comes from wrapping a long sentence, not from typed line breaks.
func TestTUIComposerHasNoNewlineBinding(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("line one")
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if got := m.input.Value(); strings.Contains(got, "\n") {
		t.Fatalf("ctrl+j must not insert a newline, got %q", got)
	}
	if len(f.turns) != 0 {
		t.Fatalf("ctrl+j must not send, got %v", f.turns)
	}
}

// User messages render as a name line then a left-bar panel: bar-prefixed
// rows, explicit newlines preserved as paragraph breaks (never
// markdown-rendered).
func TestTUIUserBubbleMultiline(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	out := m.renderEntry(entry{kind: entryUser, text: "first **not bold**\n\nsecond"})
	if n := strings.Count(out, "┃"); n != 3 {
		t.Errorf("user bubble must bar-prefix every row (2 text + 1 blank), got %d: %q", n, out)
	}
	if !strings.Contains(out, "first **not bold**") {
		t.Errorf("user text must stay literal markdown, got %q", out)
	}
	if !strings.Contains(out, "second") {
		t.Errorf("paragraphs must survive, got %q", out)
	}
}

// Markdown follows the Ghost theme: markers concealed, semantic colors.
func TestTUIMarkdownRoles(t *testing.T) {
	body := renderAssistantBody("# Title\nSome **bold** and *em* with `code`\n```go\nfmt.Println()\n```\n[docs](https://x.test/y) and https://bare.test/z\n- item\n1. first\n> quote\n---", 60)
	for _, concealed := range []string{"```go", "# Title", "`code`", "(https://x.test/y)"} {
		if strings.Contains(body, concealed) {
			t.Errorf("markers must be concealed, found %q in %q", concealed, body)
		}
	}
	for _, want := range []string{"Title", "bold", "em", "code", "docs", "https://bare.test/z", "item", "first", "quote"} {
		if !strings.Contains(body, want) {
			t.Errorf("content %q must survive, got %q", want, body)
		}
	}
	// Task lists get semantic markers.
	tasks := renderAssistantBody("- [ ] todo\n- [x] done", 60)
	if !strings.Contains(tasks, "○") || !strings.Contains(tasks, "●") {
		t.Errorf("task markers must render, got %q", tasks)
	}
}

// Markdown tables render as an opencode-style box-drawn grid: top/mid/
// bottom borders, a bold header, and a separator between rows. Columns
// wrap to fit, and a table too narrow to render falls back to raw markdown.
func TestTUIMarkdownTable(t *testing.T) {
	md := "| Command | Purpose |\n| --- | --- |\n| /help | List commands |\n| /model | Switch model |"
	body := renderAssistantBody(md, 60)
	for _, want := range []string{"┌", "┬", "┐", "├", "┼", "┤", "└", "┴", "┘", "│", "Command", "/help", "Switch model"} {
		if !strings.Contains(body, want) {
			t.Errorf("table must contain %q, got %q", want, body)
		}
	}
	// The delimiter row must be concealed (rendered as a border, not text).
	if strings.Contains(body, "---") {
		t.Errorf("the delimiter row must be concealed, got %q", body)
	}
	// The grid must not overflow the requested width.
	for _, ln := range strings.Split(body, "\n") {
		if lipgloss.Width(ln) > 60 {
			t.Errorf("table line exceeds width 60: %q", ln)
		}
	}

	// Too narrow for a stable grid: raw markdown, not a broken box.
	narrow := renderAssistantBody("| ABCDEFGHIJ | KLMNOPQRST |\n| --- | --- |\n| x | y |", 12)
	if strings.Contains(narrow, "┌") {
		t.Errorf("a table too narrow must not draw a box, got %q", narrow)
	}
}

func TestTUIToolDedupe(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.Update(toolCallMsg{tool: "exec", label: "Running: ls"})
	m.Update(toolCallMsg{tool: "exec", label: "Running: ls"})
	if len(m.toolHistory) != 1 {
		t.Fatalf("repeated active tool must not append rows, got %v", m.toolHistory)
	}
	m.Update(toolCallMsg{tool: "read_file", label: "Reading: foo"})
	if len(m.toolHistory) != 2 || !m.toolHistory[0].done {
		t.Fatalf("new tool must close the previous step, got %+v", m.toolHistory)
	}
}

// The final response wins over the stream preview: never concatenated.
func TestTUIFinalWinsOverStream(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.streaming = "Hel"
	m.Update(turnDoneMsg{text: "Hello!", err: nil})
	if !strings.Contains(m.lastFlush, "Hello!") {
		t.Fatalf("final must win verbatim, flushed %q", m.lastFlush)
	}
	if strings.Contains(m.lastFlush, "HelHello!") {
		t.Fatalf("stream must not concatenate with final, flushed %q", m.lastFlush)
	}
}

// A reply that lands after /clear, /thread, or a history reload must still
// reach the scrollback. The buffer drains on flush (no print cursor exists
// to desync), so nothing appended can be silently skipped — the "user
// messages with no Ghost response" shape. Regression test for the
// 2026-09-22 terminal incident (two turns completed server-side, zero
// replies displayed).
func TestTUIReplyAfterEntriesResetStillFlushes(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24

	// Two entries flushed: the buffer drains.
	m.append(entry{kind: entryUser, text: "good morning", at: time.Now()})
	m.append(entry{kind: entryAssistant, text: "Good morning!", at: time.Now()})
	if cmd := m.flushScrollback(); cmd == nil {
		t.Fatal("expected initial entries to flush")
	}
	if len(m.entries) != 0 {
		t.Fatalf("flush must drain, left %d", len(m.entries))
	}

	// /clear wipes the transcript mid-turn (the reply below simulates a
	// turnDone that lands after the reset).
	m.runCommand("/clear")

	// Late reply + next user message must both reach the scrollback.
	m.append(entry{kind: entryAssistant, text: "late reply", at: time.Now()})
	m.append(entry{kind: entryUser, text: "what is the status today?", at: time.Now()})
	content := m.pendingScrollback()
	if !strings.Contains(content, "late reply") {
		t.Errorf("reply after /clear must flush, got %q", content)
	}
	if !strings.Contains(content, "what is the status today?") {
		t.Errorf("message after /clear must flush, got %q", content)
	}
	if cmd := m.flushScrollback(); cmd == nil {
		t.Errorf("flush after reset must emit, not swallow")
	}
}

// /thread starts a fresh transcript epoch: the buffer already drained, so
// the notice and the first turns of the side thread always display.
func TestTUIThreadResetsFlushCursor(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, mainConversationKey))
	m.width, m.height = 80, 24
	m.append(entry{kind: entryUser, text: "before", at: time.Now()})
	if cmd := m.flushScrollback(); cmd == nil {
		t.Fatal("expected initial entry to flush")
	}
	m.runCommand("/thread")
	if len(m.entries) != 0 {
		t.Fatalf("/thread notice must flush immediately, pending=%d", len(m.entries))
	}
	m.append(entry{kind: entryUser, text: "thread question", at: time.Now()})
	if content := m.pendingScrollback(); !strings.Contains(content, "thread question") {
		t.Errorf("first thread message must flush, got %q", content)
	}
}

// TestTUIDumpStatesForJevReview renders real transcript states for the
// external JEV terminal review. It only runs when GHOST_TUI_DUMP is set to
// a file path, and writes one JSON object per scenario: the rendered
// scrollback plus the deterministic facts (entries, printed cursor) the
// reviewer correlates against.
func TestTUIDumpStatesForJevReview(t *testing.T) {
	path := os.Getenv("GHOST_TUI_DUMP")
	if path == "" {
		t.Skip("set GHOST_TUI_DUMP to dump review states")
	}
	type state struct {
		ID      string   `json:"id"`
		Title   string   `json:"title"`
		Render  string   `json:"render"`
		Entries []string `json:"entries"`
		Pending int      `json:"pending"`
	}
	var out []state
	emit := func(id, title string, m *agentTUI) {
		kinds := map[entryKind]string{
			entryUser: "user", entryAssistant: "assistant", entryTool: "tool",
			entryNotice: "notice", entryError: "error",
		}
		var ks []string
		for _, e := range m.entries {
			ks = append(ks, kinds[e.kind])
		}
		out = append(out, state{ID: id, Title: title, Render: m.pendingScrollback(), Entries: ks, Pending: len(ks)})
	}
	newM := func() *agentTUI {
		m := readyForTest(newAgentTUI(newFakeRuntime(), "cli:test"))
		m.width, m.height = 80, 24
		return m
	}
	now := time.Now()

	m1 := newM()
	m1.append(entry{kind: entryUser, text: "hello", at: now.AddDate(0, 0, -1)})
	m1.append(entry{kind: entryAssistant, text: "Hello!", at: now.AddDate(0, 0, -1)})
	m1.append(entry{kind: entryUser, text: "good morning", at: now})
	m1.append(entry{kind: entryAssistant, text: "Good morning!", at: now})
	emit("dividers-mixed-days", "preloaded yesterday rows plus live today rows", m1)

	m2 := newM()
	m2.append(entry{kind: entryUser, text: "q1", at: now})
	m2.append(entry{kind: entryAssistant, text: "a1", at: now})
	m2.flushScrollback()
	m2.runCommand("/clear")
	m2.append(entry{kind: entryAssistant, text: "late reply", at: now})
	m2.append(entry{kind: entryUser, text: "q2", at: now})
	emit("clear-then-late-reply", "reply landing after /clear plus next question", m2)

	m3 := newM()
	m3.working = true
	m3.ready = false // hold the flush: emit reads exactly what it would print
	m3.updateInner(turnDoneMsg{err: fmt.Errorf("boom")})
	emit("turn-error", "failed turn", m3)

	m4 := newM()
	m4.working = true
	m4.streaming = ""
	m4.ready = false // hold the flush: emit reads exactly what it would print
	m4.updateInner(turnDoneMsg{text: "  ", err: nil})
	emit("empty-response", "turn returning empty text with empty stream", m4)

	m5 := newM()
	m5.append(entry{kind: entryUser, text: "x", at: time.Time{}})
	m5.append(entry{kind: entryAssistant, text: "y", at: time.Time{}})
	emit("zero-timestamps", "entries with unknown time", m5)

	m6 := newM()
	m6.working = true
	m6.streamSty = streamStyler{width: m6.textWidth()}
	m6.ready = false // hold the entry flush; progressive prints bypass it
	m6.updateInner(streamChunkMsg{text: "## Model releases\n"})
	first := m6.lastFlush
	m6.updateInner(streamChunkMsg{text: "- **Claude Mythos 5.1** ships today\n- Gemini 3.8 Flash follows\n\n```go\nfmt.Println()\n```\n| a | b |\n|---|---|\n| 1 | 2 |\nTail."})
	// Progressive blocks print straight to the scrollback (not via entries):
	// join both blocks to show what the user saw grow.
	out = append(out, state{ID: "mid-stream", Title: "reply growing line by line while the turn runs", Render: first + "\n" + m6.lastFlush, Entries: []string{"stream:2-blocks"}, Pending: 0})

	raw, _ := json.MarshalIndent(out, "", " ")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	// Composer states for the input-area review: the painted box plus its
	// height at short, long, and over-cap input.
	type cstate struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Render string `json:"render"`
		Height int    `json:"height"`
	}
	var cout []cstate
	cemit := func(id, title, value string) {
		m := newM()
		m.input.SetValue(value)
		m.layout()
		cout = append(cout, cstate{id, title, m.promptBox(), m.input.Height()})
	}
	cemit("composer-short", "short message stays one row", "hi")
	cemit("composer-long", "long message grows the box", "word word word word word word word word word word word word word word word word word word word word word word word word ")
	cemit("composer-capped", "over-cap message stops at seven rows", "word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word\nsecond para")
	craw, _ := json.MarshalIndent(cout, "", " ")
	if err := os.WriteFile(path+".composer", craw, 0600); err != nil {
		t.Fatal(err)
	}
}

// nextStreamBlock is pure accounting: the header prints once, completed
// lines print once each, the trailing partial line is always held back.
func TestNextStreamBlockAccounting(t *testing.T) {
	wh, lines, flushed := nextStreamBlock("", false, 0)
	if wh || len(lines) != 0 || flushed != 0 {
		t.Fatalf("empty stream must emit nothing, got %v %v %d", wh, lines, flushed)
	}
	wh, lines, flushed = nextStreamBlock("Hello", false, 0)
	if !wh || len(lines) != 0 || flushed != 0 {
		t.Fatalf("partial line: header due, no lines, got %v %v %d", wh, lines, flushed)
	}
	wh, lines, flushed = nextStreamBlock("Hello\nWorld", false, 0)
	if !wh || len(lines) != 1 || lines[0] != "Hello" || flushed != 1 {
		t.Fatalf("first line completes, got %v %q %d", wh, lines, flushed)
	}
	wh, lines, flushed = nextStreamBlock("Hello\nWorld", true, 1)
	if wh || len(lines) != 0 || flushed != 1 {
		t.Fatalf("same buffer twice must emit nothing new, got %v %v %d", wh, lines, flushed)
	}
	wh, lines, flushed = nextStreamBlock("Hello\n\nWorld\nTail", true, 1)
	if wh || len(lines) != 2 || lines[0] != "" || lines[1] != "World" || flushed != 3 {
		t.Fatalf("blanks pass through and count, got %v %q %d", wh, lines, flushed)
	}
}

// A streamed reply grows in the scrollback and is never reprinted whole at
// completion: chunks print completed lines, turnDone prints only the tail.
func TestTUIStreamsReplyProgressively(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24
	m.working = true
	m.updateInner(streamChunkMsg{text: "Hello\n"})
	m.updateInner(streamChunkMsg{text: "World"})
	if !m.streamHeaderShown || m.streamFlushedLines != 1 {
		t.Fatalf("header + first line must print progressively, shown=%v flushed=%d", m.streamHeaderShown, m.streamFlushedLines)
	}
	m.updateInner(turnDoneMsg{text: "Hello\nWorld", err: nil})
	for _, e := range m.entries {
		if e.kind == entryAssistant {
			t.Fatalf("streamed reply must not be committed again, entries=%v", m.entries)
		}
	}
	if !strings.Contains(m.lastFlush, "World") {
		t.Errorf("turn tail must print, lastFlush=%q", m.lastFlush)
	}
	if n := strings.Count(m.lastFlush, "Hello"); n != 1 {
		t.Errorf("completed lines must print exactly once, lastFlush=%q", m.lastFlush)
	}
}

// A turn with no streamed chunks still commits the full rendered reply.
func TestTUINoChunksStillCommitsFullReply(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24
	m.working = true
	m.updateInner(turnDoneMsg{text: "Instant answer", err: nil})
	if !strings.Contains(m.lastFlush, "Instant answer") {
		t.Errorf("chunkless reply must commit whole, lastFlush=%q", m.lastFlush)
	}
}

// TestTUIProgressiveNoDuplication drives a realistic multi-chunk markdown
// reply through chunks + turnDone and records every printed block. It fails
// if any content line prints twice (progressive + final duplication).
func TestTUIProgressiveNoDuplication(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24
	m.working = true
	var blocks []string
	feed := func(s string) {
		m.updateInner(streamChunkMsg{text: s})
		if m.lastFlush != "" {
			blocks = append(blocks, m.lastFlush)
			m.lastFlush = ""
		}
	}
	feed("## Head\n")
	feed("Body line one\nBody line two\n")
	feed("Tail.")
	m.updateInner(turnDoneMsg{text: "## Head\nBody line one\nBody line two\nTail.", err: nil})
	if m.lastFlush != "" {
		blocks = append(blocks, m.lastFlush)
	}
	joined := strings.Join(blocks, "\n")
	for _, want := range []string{"Head", "Body line one", "Body line two", "Tail."} {
		if n := strings.Count(joined, want); n != 1 {
			t.Errorf("content %q printed %d times, want exactly once\nblocks=%q", want, n, joined)
		}
	}
	for _, e := range m.entries {
		if e.kind == entryAssistant {
			t.Errorf("streamed reply must not commit a duplicate entry, entries=%v", m.entries)
		}
	}
}

// Typing a long message must grow the composer box rune by rune (Pi editor
// parity: grow to cap, then scroll). Regression test: the typing path once
// skipped layout, so the box stayed one row no matter the message length.
func TestTUIComposerGrowsWhileTyping(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24
	m.layout()
	for _, r := range "word word word word word word word word word word word word word word word word word word word word " {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		m.updateInner(msg)
	}
	if h := m.input.Height(); h <= 1 {
		t.Fatalf("composer must grow while typing long input, height=%d value=%q", h, m.input.Value())
	}
	if got := m.estimatedInputHeight(); got != m.input.Height()+2 {
		t.Errorf("estimate must track the grown box, estimate=%d height=%d", got, m.input.Height())
	}
}

// The name shares its row with the pipe and the first text line.
func TestTUIUserBubbleSameRow(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	out := m.renderEntry(entry{kind: entryUser, text: "hello"})
	rows := strings.Split(out, "\n")
	if len(rows) != 1 {
		t.Fatalf("single-line message must render one row, got %q", out)
	}
	you, bar := strings.Index(rows[0], "You"), strings.Index(rows[0], "┃")
	if you < 0 || bar < 0 || you > bar {
		t.Fatalf("name and pipe must share the first row in order, got %q", out)
	}
	if !strings.Contains(rows[0], "hello") {
		t.Fatalf("first text line must share the row, got %q", out)
	}
}

// Inline spans split across the model's own line breaks must conceal on
// both paths: a completed line ending inside an unclosed span waits for
// its continuation instead of leaking markers.
func TestTUIContinuedSpansConcealed(t *testing.T) {
	in := "The **Supreme Court blocked Trump's effort to restrict\nmail-in voting** ahead of November's midterms, in a brief\nunsigned order upholding a lower court ruling *(Democracy\nNow, Sep 15)*."
	if out := renderAssistantBody(in, 76); strings.Contains(out, "**") || strings.Contains(out, "*(") {
		t.Errorf("full render leaked markers:\n%s", out)
	}
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24
	m.working = true
	m.streamSty = streamStyler{width: m.textWidth()}
	var shown strings.Builder
	feed := func(s string) {
		m.updateInner(streamChunkMsg{text: s})
		shown.WriteString(m.lastFlush + "\n")
		m.lastFlush = ""
	}
	for _, ln := range strings.Split(in, "\n") {
		feed(ln + "\n")
	}
	m.updateInner(turnDoneMsg{text: in, err: nil})
	shown.WriteString(m.lastFlush + "\n")
	got := shown.String()
	for _, bad := range []string{"**", "*("} {
		if strings.Contains(got, bad) {
			t.Errorf("progressive render leaked %q:\n%s", bad, got)
		}
	}
	for _, want := range []string{"mail-in voting", "Democracy Now, Sep 15", "unsigned order"} {
		if !strings.Contains(got, want) {
			t.Errorf("progressive render lost %q:\n%s", want, got)
		}
	}
}

// Buffered lines (span repair, table rows) count as consumed: re-feeding
// them self-joins against the buffer and duplicates.
func TestTUIBufferedLinesAdvanceAccounting(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.width, m.height = 80, 24
	m.working = true
	m.streamSty = streamStyler{width: m.textWidth()}
	var shown strings.Builder
	feed := func(s string) {
		m.updateInner(streamChunkMsg{text: s})
		shown.WriteString(m.lastFlush + "\n")
		m.lastFlush = ""
	}
	feed("unsigned order upholding a lower court ruling *(Democracy\n")
	feed("Now, Sep 15)*.\n")
	m.updateInner(turnDoneMsg{text: "unsigned order upholding a lower court ruling *(Democracy\nNow, Sep 15)*.", err: nil})
	shown.WriteString(m.lastFlush + "\n")
	got := shown.String()
	if n := strings.Count(got, "unsigned order"); n != 1 {
		t.Errorf("buffered line printed %d times, want once:\n%s", n, got)
	}
}

// Span repair never crosses block structure: fences, tables, headings,
// quotes, and lists keep their boundaries even after an unclosed span.
func TestJoinContinuedLinesRespectsBlocks(t *testing.T) {
	// Fence lines never donate, even with odd backticks.
	if got := joinContinuedLines("```go\nfmt.Println()\n```\ncode `x` here"); strings.Contains(got, "```go fmt") {
		t.Errorf("fence must not join forward: %q", got)
	}
	// A closing fence never absorbs the next line.
	if got := joinContinuedLines("text **bold\n```\ncode"); !strings.Contains(got, "\n```\n") {
		t.Errorf("closing fence must stand alone: %q", got)
	}
	// Table rows never donate to the delimiter lookahead.
	if got := joinContinuedLines("| a **b |\n| --- |\n| c |"); strings.Contains(got, "| a **b | | --- |") {
		t.Errorf("table rows must not merge: %q", got)
	}
	// Headings, quotes, list items are not absorbed from above...
	if got := joinContinuedLines("open **span\n# Head"); strings.Contains(got, "open **span # Head") {
		t.Errorf("heading must stand alone: %q", got)
	}
	if got := joinContinuedLines("open **span\n> quoted"); strings.Contains(got, "open **span > quoted") {
		t.Errorf("quote must stand alone: %q", got)
	}
	if got := joinContinuedLines("open **span\n- item"); strings.Contains(got, "open **span - item") {
		t.Errorf("list item must stand alone: %q", got)
	}
	// ...but a list item's own continued span still repairs.
	if got := renderAssistantBody("- **bold item\ncontinued** end", 60); strings.Contains(got, "**") {
		t.Errorf("list continuation must repair: %q", got)
	}
}

// /tasks lists durable routines with status through the loop, never a chat turn.
func TestTUITasksListsRoutines(t *testing.T) {
	f := newFakeRuntime()
	f.routineList = []*routines.Routine{
		{ID: "r1", Name: "Water", Status: routines.StatusActive},
		{ID: "r2", Name: "Brief", Status: routines.StatusPaused},
	}
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/tasks")
	if len(f.turns) != 0 {
		t.Fatalf("/tasks must not start a chat turn, got %v", f.turns)
	}
	if !hasNotice(m, "Water") || !hasNotice(m, "active") {
		t.Errorf("/tasks must show routines with status, entries=%v", m.entries)
	}
}

// /task pause 1 resolves through the routines service.
func TestTUITaskManageResolves(t *testing.T) {
	f := newFakeRuntime()
	f.routineList = []*routines.Routine{{ID: "abc123", Name: "Water", Status: routines.StatusActive}}
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/task")
	if !hasError(m, "usage") {
		t.Errorf("/task without args must show usage, entries=%v", m.entries)
	}
	m.runCommand("/task pause 1")
	if len(f.managed) != 1 || f.managed[0] != "pause abc123" {
		t.Fatalf("/task pause 1 must manage by row number, got %v", f.managed)
	}
	m.runCommand("/task resume abc")
	if len(f.managed) != 2 || f.managed[1] != "resume abc123" {
		t.Fatalf("/task must resolve id prefixes, got %v", f.managed)
	}
	m.runCommand("/task pause 9")
	if !hasError(m, "no routine") {
		t.Errorf("unknown rows must fail visibly, entries=%v", m.entries)
	}
}

// /tasks degrades visibly when routines are unavailable.
func TestTUITasksUnavailableHonest(t *testing.T) {
	f := newFakeRuntime()
	f.routinesErr = errTestRoutinesDown
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/tasks")
	if !hasError(m, "tasks unavailable") {
		t.Errorf("/tasks must fail visibly, entries=%v", m.entries)
	}
}

// /ideas lists pending ideas with evidence; /idea reads and decides.
func TestTUIIdeasSurface(t *testing.T) {
	f := newFakeRuntime()
	f.ideaList = []*ideas.Idea{{
		ID: "idea-1", Title: "Pause Water?", Body: "It failed twice.",
		Sources: []ideas.Source{{Kind: ideas.SourceRoutine, Ref: "run-9", Excerpt: "status=error"}},
		Status:  ideas.StatusPending,
	}}
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/ideas")
	if !hasNotice(m, "Pause Water?") {
		t.Errorf("/ideas must list pending ideas, entries=%v", m.entries)
	}
	m.runCommand("/idea 1")
	if !hasNotice(m, "status=error") {
		t.Errorf("/idea must show why, entries=%v", m.entries)
	}
	m.runCommand("/idea accept 1")
	if len(f.decided) != 1 || f.decided[0] != "idea-1" {
		t.Fatalf("accept must decide by row, got %v", f.decided)
	}
	if !hasNotice(m, "Accepted") {
		t.Errorf("accept must receipt, entries=%v", m.entries)
	}
	m.runCommand("/idea bogus")
	if !hasError(m, "usage") && !hasError(m, "no idea") {
		t.Errorf("bad /idea args must fail visibly, entries=%v", m.entries)
	}
}

// /ideas degrades visibly when the surface is unavailable.
func TestTUIIdeasUnavailableHonest(t *testing.T) {
	f := newFakeRuntime()
	f.ideasErr = errTestNoIdea
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/ideas")
	if !hasError(m, "ideas unavailable") {
		t.Errorf("/ideas must fail visibly, entries=%v", m.entries)
	}
}

// All four arrows plus vim keys move the approval cursor; the card lists
// options vertically, so up/down must work, not just left/right.
func TestTUIApprovalArrowKeys(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.approval = &pendingApproval{id: "req-1", title: "Send?", risk: "consequential"}
	arrow := func(typ tea.KeyType) {
		m.handleKey(tea.KeyMsg{Type: typ})
		if m.approval == nil {
			t.Fatalf("arrow key must move, never resolve")
		}
	}
	arrow(tea.KeyDown)
	if m.approvalSel != 1 {
		t.Fatalf("down must move to 1, got %d", m.approvalSel)
	}
	arrow(tea.KeyDown)
	if m.approvalSel != 2 {
		t.Fatalf("down must move to 2, got %d", m.approvalSel)
	}
	arrow(tea.KeyDown)
	if m.approvalSel != 2 {
		t.Fatalf("down must clamp at 2, got %d", m.approvalSel)
	}
	arrow(tea.KeyUp)
	if m.approvalSel != 1 {
		t.Fatalf("up must move to 1, got %d", m.approvalSel)
	}
	m.handleKey(keyMsgFor('k'))
	if m.approvalSel != 0 {
		t.Fatalf("k must move to 0, got %d", m.approvalSel)
	}
	m.handleKey(keyMsgFor('j'))
	if m.approvalSel != 1 {
		t.Fatalf("j must move to 1, got %d", m.approvalSel)
	}
	m.handleKey(keyMsgFor('l'))
	if m.approvalSel != 2 {
		t.Fatalf("l must move to 2, got %d", m.approvalSel)
	}
	m.handleKey(keyMsgFor('h'))
	if m.approvalSel != 1 {
		t.Fatalf("h must move to 1, got %d", m.approvalSel)
	}
	if len(f.turns) != 0 {
		t.Fatalf("cursor moves must never start turns, got %v", f.turns)
	}
}

// Long user messages must keep the pipe on its own column: every row fits
// the width and every bar sits at the same cell, so the rule never lands
// on the text.
func TestTUIUserBubbleBarGrowsWithWrap(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	long := "i would like you to remind me to also send an email to Maria about the quarterly report and the follow-up call tomorrow morning please"
	out := m.renderEntry(entry{kind: entryUser, text: long})
	rows := strings.Split(out, "\n")
	if len(rows) < 2 {
		t.Fatalf("long message must wrap, got %q", out)
	}
	barCol := -1
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	for _, r := range rows {
		plain := ansi.ReplaceAllString(r, "")
		if lipgloss.Width(plain) > m.contentWidth() {
			t.Fatalf("row overflows width %d: %q", m.contentWidth(), plain)
		}
		col := strings.Index(plain, "┃")
		if col < 0 {
			t.Fatalf("every bubble row must carry the pipe, got %q", plain)
		}
		if barCol < 0 {
			barCol = col
		} else if col != barCol {
			t.Fatalf("pipe column drifted %d -> %d in %q", barCol, col, out)
		}
	}
}

// The reply header names Ghost only: the model lives in the footer stats
// line, so the transcript reads as a conversation, not a process log.
func TestTUIAssistantHeadHasNoModel(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	head := m.assistantHead()
	if !strings.Contains(head, "Ghost") {
		t.Fatalf("header must name Ghost, got %q", head)
	}
	if strings.Contains(head, "deepseek") || strings.Contains(head, "test-model") || strings.Contains(head, "·") {
		t.Fatalf("header must not carry the model, got %q", head)
	}
}

// Bold-bullet memory lists must survive rendering with items on separate
// rows and spacing intact: regression lock for the glued-list report.
func TestTUIAssistantBoldListKeepsRows(t *testing.T) {
	in := "Here is what is on file:\n\n- **Name:** ian\n- **Favorite language:** Rust\n- **Considering:** buying an NVMe drive\n- **Timezone:** Asia/Bangkok\n\nThat is the durable stuff."
	body := renderAssistantBody(in, 78)
	rows := strings.Split(body, "\n")
	var bullets []string
	for _, r := range rows {
		if strings.HasPrefix(strings.TrimSpace(r), "- ") {
			bullets = append(bullets, strings.TrimSpace(r))
		}
	}
	if len(bullets) != 4 {
		t.Fatalf("expected 4 bullet rows, got %d:\n%s", len(bullets), body)
	}
	for _, want := range []string{"- Name: ian", "- Favorite language: Rust", "- Considering: buying an NVMe drive", "- Timezone: Asia/Bangkok"} {
		found := false
		for _, b := range bullets {
			if strings.HasPrefix(b, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing spaced bullet %q in:\n%s", want, body)
		}
	}
	// Streaming path must agree row-for-row with the committed render.
	sty := streamStyler{width: 78}
	var streamed []string
	for _, raw := range strings.Split(in, "\n") {
		streamed = append(streamed, sty.line(raw)...)
	}
	streamed = append(streamed, sty.flush()...)
	strip := func(s string) string { return strings.TrimSpace(s) }
	var sc, cc []string
	for _, r := range streamed {
		sc = append(sc, strip(r))
	}
	for _, r := range strings.Split(renderAssistantBody(in, 78), "\n") {
		cc = append(cc, strip(r))
	}
	if strings.Join(sc, "\n") != strings.Join(cc, "\n") {
		t.Errorf("stream vs committed diverged:\nstream:\n%s\ncommitted:\n%s", strings.Join(sc, "\n"), strings.Join(cc, "\n"))
	}
}
