package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeRuntime records calls and simulates a runtime for TUI tests.
type fakeRuntime struct {
	model    string
	presets  []string
	injected []string // steering messages queued while working
	aborted  []string // sessions aborted via Esc
	answered map[string]string
	turns    []string
	setCalls []string
	pending  *pendingApproval
	context  string
	contexts []string
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{model: "deepseek-flash", presets: []string{"fast", "deep"}, answered: map[string]string{}}
}

func (f *fakeRuntime) GetCurrentModel() string { return f.model }
func (f *fakeRuntime) ModelPresets() []string  { return f.presets }
func (f *fakeRuntime) SetModel(t string) error {
	f.setCalls = append(f.setCalls, t)
	f.model = t
	return nil
}
func (f *fakeRuntime) InjectSteering(_, content string) { f.injected = append(f.injected, content) }
func (f *fakeRuntime) AbortTurn(sessionKey string)      { f.aborted = append(f.aborted, sessionKey) }
func (f *fakeRuntime) RefreshModels()                   {}
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

// View renders each region exactly once with pi layout: no top header,
// transcript, prompt box, 3-line footer. Regression test for the doubled
// header/welcome/input and the stray title label above the prompt box.
func TestTUIViewRendersSingleChrome(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.renderTranscript()
	view := m.View()
	// No "prompt" title label above the box (pi has none).
	if strings.Contains(view, " prompt\n") || strings.Contains(view, "\n prompt ") {
		t.Errorf("view must not label the prompt box, got %q", view)
	}
	// Footer carries session/model/state exactly once each.
	if n := strings.Count(view, "cli:test"); n != 1 {
		t.Errorf("session should appear once in the footer, found %d: %q", n, view)
	}
	if n := strings.Count(view, "deepseek-flash"); n != 1 {
		t.Errorf("model should appear once in the footer, found %d: %q", n, view)
	}
	// Welcome card renders into the viewport only — never duplicated.
	if n := strings.Count(view, "switch thinking engine"); n != 1 {
		t.Errorf("welcome card should appear once, found %d: %q", n, view)
	}
	// Footer is exactly 3 lines: session, stats/model, shortcuts.
	if got := len(m.footerLines()); got != 3 {
		t.Fatalf("footer must be 3 lines, got %d", got)
	}
	if !strings.Contains(m.footerKeysLine(), "enter send") {
		t.Errorf("idle footer must name the send key, got %q", m.footerKeysLine())
	}
}

// The shortcuts line names the escape routes for each state
// (terminal-ui skill: input-escape-routes).
func TestTUIFooterKeysContextual(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	if !strings.Contains(m.footerKeysLine(), "esc abort") {
		t.Errorf("idle keys must name esc, got %q", m.footerKeysLine())
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

// Box height estimates must count visual (wrapped) lines, not physical
// ones, or the viewport drifts when a long line wraps
// (terminal-ui skill: tuicomp-measure-element).
func TestTUIInputHeightCountsWrappedLines(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test")) // width 80 → inner 78
	m.input.SetValue(strings.Repeat("x", 200))
	if got := m.estimatedInputHeight(); got != 3 { // bare panel: wrapped rows only
		t.Errorf("200 cols at inner 78 should estimate 3 rows, got %d", got)
	}
}

// Enter with the palette open must ACCEPT the completion (opencode
// prompt.autocomplete.select) — never run the half-typed text. This is
// the "command unknown unless written in full" regression test.
func TestTUIEnterAcceptsPaletteCompletion(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("/mod")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.input.Value(); got != "/model " {
		t.Fatalf("enter should complete to /model, got %q", got)
	}
	if len(f.turns) != 0 {
		t.Fatalf("completing must not start a turn, got %v", f.turns)
	}
	if m.working {
		t.Errorf("completing must not mark working")
	}
}

// Tab completes the same way without submitting.
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

// Completion preserves already-typed arguments.
func TestTUICompletionPreservesArgs(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("/mod deep")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.input.Value(); got != "/model deep " {
		t.Fatalf("args must survive completion, got %q", got)
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

// Ctrl+J inserts a newline (universal multiline key); Enter still sends.
// The composer grows to maxPromptLines, then scrolls inside the box.
func TestTUIMultilineComposer(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.input.SetValue("line one")
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if got := m.input.Value(); got != "line one\n" {
		t.Fatalf("ctrl+j must insert a newline, got %q", got)
	}
	if len(f.turns) != 0 {
		t.Fatalf("newline must not send, got %v", f.turns)
	}
	m.input.SetValue(strings.Repeat("x\n", 10))
	m.layout()
	if h := m.input.Height(); h != maxPromptLines {
		t.Errorf("composer must cap at %d lines, got %d", maxPromptLines, h)
	}
}

// User messages render as an opencode panel: name line, bar-prefixed
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

// Markdown follows the opencode spec: markers concealed, semantic colors.
func TestTUIMarkdownOpencodeRoles(t *testing.T) {
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
