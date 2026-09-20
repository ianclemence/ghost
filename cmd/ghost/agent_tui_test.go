package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ianclemence/ghost/pkg/providers"
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

// Ghost is one conversation: /new opens a clearly-labelled side thread, and
// /main returns to the shared conversation and reloads its rows.
func TestTUINewOpensSideThreadAndMainReturns(t *testing.T) {
	f := newFakeRuntime()
	f.history = map[string][]historyEntry{
		mainConversationKey: {
			{Role: "user", Content: "shared hello"},
			{Role: "assistant", Content: "shared reply"},
		},
	}
	m := readyForTest(newAgentTUI(f, mainConversationKey))

	m.runCommand("/new")
	if m.session == mainConversationKey {
		t.Fatal("/new must open a side thread, not stay on main")
	}
	if !hasNotice(m, "side thread") {
		t.Errorf("/new must say it opened a side thread, entries=%v", m.entries)
	}
	if hasNotice(m, "fresh conversation") {
		t.Errorf("/new must not claim a fresh conversation (Ghost is one conversation)")
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

func TestTUIStatusLineShowsLocalityAndState(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	line := m.statusLine()
	for _, want := range []string{"deepseek-flash", "cloud", "cli:test", "ready"} {
		if !strings.Contains(line, want) {
			t.Errorf("status line %q must contain %q", line, want)
		}
	}
	m.working = true
	m.toolCount = 3
	if !strings.Contains(m.statusLine(), "3 tools") {
		t.Errorf("working status must show tool count")
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
	return false
}
func hasError(m *agentTUI, sub string) bool {
	for _, e := range m.entries {
		if e.kind == entryError && strings.Contains(e.text, sub) {
			return true
		}
	}
	return false
}
func hasUser(m *agentTUI, sub string) bool {
	for _, e := range m.entries {
		if e.kind == entryUser && strings.Contains(e.text, sub) {
			return true
		}
	}
	return false
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
	// Welcome card renders into the viewport only — never duplicated.
	if n := strings.Count(view, "switch thinking engine"); n != 1 {
		t.Errorf("welcome card should appear once, found %d: %q", n, view)
	}
	// Footer is exactly 2 lines: stats/model, shortcuts. The tagline lives
	// on the welcome card, never in the footer.
	if got := len(m.footerLines()); got != 2 {
		t.Fatalf("footer must be 2 lines, got %d", got)
	}
	if strings.Contains(strings.Join(m.footerLines(), "\n"), ghostTagline) {
		t.Errorf("the tagline must not appear in the footer")
	}
	// The welcome card carries the Ghost tagline exactly once.
	if n := strings.Count(view, ghostTagline); n != 1 {
		t.Errorf("welcome card must show the tagline once, found %d: %q", n, view)
	}
	if !strings.Contains(view, "Your AI. Your Memory. Your Machine.") {
		t.Errorf("welcome card must carry the Ghost tagline, got %q", view)
	}
	// The idle hint lists the command surface first and the exit last, and
	// never spells out the obvious Enter-to-send.
	keys := m.footerKeysLine()
	if strings.Contains(keys, "enter send") {
		t.Errorf("idle footer must not spell out enter send, got %q", keys)
	}
	if !strings.HasPrefix(keys, "/ commands · tab complete") {
		t.Errorf("idle keys must lead with the command surface, got %q", keys)
	}
	if !strings.HasSuffix(keys, "esc quit") {
		t.Errorf("esc quit must come last, got %q", keys)
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
	if !strings.Contains(m.footerKeysLine(), "tab/enter complete") {
		t.Errorf("palette keys must name tab, got %q", m.footerKeysLine())
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

// The composer is exactly two full-width rules, no side borders, no
// prefix, and the text rows between them. While working, the live status
// is embedded once in the top rule — never duplicated.
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
	m.toolHistory = []toolStep{{tool: "read_file", label: "x"}, {tool: "web_search", label: "y"}}
	m.spinFrame = 0
	working := m.promptBox()
	top := strings.Split(working, "\n")[0]
	if !strings.Contains(top, m.spinner()) {
		t.Errorf("working top rule must carry the spinner, got %q", top)
	}
	if n := strings.Count(top, m.spinner()); n != 1 {
		t.Errorf("spinner must appear once in the top rule, found %d: %q", n, top)
	}
	if !strings.Contains(top, "2 tools") {
		t.Errorf("working top rule must carry the tool count, got %q", top)
	}
}

// The user bubble is a full-width background block: edge to edge, text
// inset by one cell, one blank padding row above and below.
func TestTUIUserBubblePI(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	out := m.renderEntry(entry{kind: entryUser, text: "hello"})
	rows := strings.Split(out, "\n")
	if len(rows) != 3 {
		t.Fatalf("one-line bubble must be pad + line + pad, got %d: %q", len(rows), out)
	}
	for _, ln := range rows {
		if got := lipgloss.Width(ln); got != m.width {
			t.Errorf("bubble must span the canvas (%d), got %d: %q", m.width, got, ln)
		}
	}
	if !strings.HasPrefix(rows[1], " hello") {
		t.Errorf("bubble text must be inset by one cell, got %q", rows[1])
	}
}

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
	m.renderTranscript()
	content := m.viewport.View()
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
	if len(labels) != 3 || labels[1] != "deepseek" {
		t.Fatalf("picker must list the provider option, got %v", labels)
	}
	// Cycling skips the unavailable entry and lands the provider target.
	f.model = "weird:thing"
	m.cycleModel()
	if f.model != "local" {
		t.Fatalf("first press should take the first option, got %q", f.model)
	}
	m.cycleModel()
	if f.model != "deepseek:deepseek-flash" {
		t.Fatalf("cycle must reach the provider target, got %q", f.model)
	}
	m.cycleModel()
	if f.model != "local" {
		t.Fatalf("cycle must wrap past unavailable entries, got %q", f.model)
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

// User messages render as a full-width background panel: name line,
// inset rows, explicit newlines preserved as paragraph breaks (never
// markdown-rendered).
func TestTUIUserBubbleMultiline(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	out := m.renderEntry(entry{kind: entryUser, text: "first **not bold**\n\nsecond"})
	rows := strings.Split(out, "\n")
	// Panel: blank padding row, 2 text rows, blank row, blank padding row.
	if len(rows) != 5 {
		t.Fatalf("user bubble must be 5 rows (pad + text + blank + text + pad), got %d: %q", len(rows), out)
	}
	if !strings.Contains(out, "first **not bold**") {
		t.Errorf("user text must stay literal markdown, got %q", out)
	}
	if !strings.Contains(out, "second") {
		t.Errorf("paragraphs must survive, got %q", out)
	}
	// The bubble is edge to edge: the background spans the whole canvas,
	// only the text is inset by one cell.
	for _, ln := range rows {
		if got := lipgloss.Width(ln); got != 80 {
			t.Errorf("bubble rows must fill the canvas (80), got %d: %q", got, ln)
		}
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
	found := false
	for _, e := range m.entries {
		if e.kind == entryAssistant {
			found = true
			if e.text != "Hello!" {
				t.Fatalf("final must win verbatim, got %q", e.text)
			}
		}
	}
	if !found {
		t.Fatalf("assistant entry missing: %+v", m.entries)
	}
}
