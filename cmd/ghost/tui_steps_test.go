package main

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestStepTitlesAreSaidInTheRightTense(t *testing.T) {
	cases := []struct{ tool, running, done string }{
		{"web_search", "Searching the web", "Searched the web"},
		{"exec", "Running a command", "Ran a command"},
		{"browser_click", "Clicking", "Clicked"},
		{"something_new", "Working on it", "Used a tool"},
		{"remember", "Checking memory", "Checked memory"},
	}
	for _, c := range cases {
		if got := stepTitle(c.tool, true); got != c.running {
			t.Errorf("%s running: %q want %q", c.tool, got, c.running)
		}
		if got := stepTitle(c.tool, false); got != c.done {
			t.Errorf("%s done: %q want %q", c.tool, got, c.done)
		}
	}
}

func TestStepDurationsReadLikeAPersonSaysThem(t *testing.T) {
	for d, want := range map[time.Duration]string{
		300 * time.Millisecond:  "<1s",
		3200 * time.Millisecond: "3.2s",
		14 * time.Second:        "14s",
		79 * time.Second:        "1m 19s",
	} {
		if got := fmtStepDur(d); got != want {
			t.Errorf("%v -> %q want %q", d, got, want)
		}
	}
}

func TestSummaryCountsByKindAndNamesTheFailure(t *testing.T) {
	steps := []toolStep{
		{id: "1", tool: "web_search"},
		{id: "2", tool: "exec"},
		{id: "3", tool: "exec", failed: true},
		{id: "4", tool: "browser_navigate"},
	}
	if got, want := summarizeSteps(steps), "Searched the web, ran 2 commands (1 failed), browsed the web"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	one := []toolStep{{id: "1", tool: "exec", failed: true}}
	if got, want := summarizeSteps(one), "Ran a command (1 failed)"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if summarizeSteps(nil) != "" || summarizeSteps([]toolStep{{tool: "phase", label: "Thinking"}}) != "" {
		t.Fatal("a phase word is not a step")
	}
}

func TestStepRowFitsItsWidthAndKeepsTheTimeOnTheRight(t *testing.T) {
	for _, w := range []int{40, 60, 100} {
		row := stepRow(toolStep{id: "1", tool: "web_search", detail: strings.Repeat("nairobi bar culture ", 8), dur: 1200 * time.Millisecond}, w, false)
		if got := lipgloss.Width(row); got > w {
			t.Errorf("width %d: row is %d cells", w, got)
		}
		if strings.Contains(row, "\n") {
			t.Errorf("width %d: a succeeded step is one line", w)
		}
		if !strings.HasSuffix(plain(row), "1.2s") {
			t.Errorf("width %d: time is not at the right edge: %q", w, row)
		}
	}
}

func TestAFailedStepSaysWhy(t *testing.T) {
	row := stepRow(toolStep{id: "1", tool: "exec", detail: "composer install", failed: true, note: "exit status 1", dur: 4100 * time.Millisecond}, 80, false)
	lines := strings.Split(row, "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "✗") || !strings.Contains(lines[1], "exit status 1") {
		t.Fatalf("a failure is two lines, the second saying why: %q", row)
	}
}

func TestDetailsShowsTheWholeCommand(t *testing.T) {
	cmd := "find / -name '*.log' -mtime +30 -size +100M -not -path '/proc/*' -print0 | xargs -0 ls -la"
	short := stepRow(toolStep{id: "1", tool: "exec", detail: cmd, dur: time.Second}, 60, false)
	long := stepRow(toolStep{id: "1", tool: "exec", detail: cmd, dur: time.Second}, 60, true)
	if strings.Contains(short, "xargs") {
		t.Fatalf("the compact row must cut a long command: %q", short)
	}
	if !strings.Contains(long, "xargs") || !strings.Contains(long, "ls -la") {
		t.Fatalf("/details must show it in full: %q", long)
	}
	for _, ln := range strings.Split(long, "\n") {
		if lipgloss.Width(ln) > 60 {
			t.Fatalf("line too wide (%d): %q", lipgloss.Width(ln), ln)
		}
	}
}

func TestAStepIsPrintedOnceWhenItEndsNotWhenItStarts(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.beginStep(toolStartMsg{id: "c1", tool: "exec", detail: "php -v"})
	if m.stepsPrinted != 0 {
		t.Fatal("a running step must not be printed")
	}
	if got := m.activeStepWord(); got != "Running a command · php -v" {
		t.Fatalf("live line: %q", got)
	}
	if m.finishStep(toolResultMsg{id: "c1", ok: true, dur: 300 * time.Millisecond}) == nil {
		t.Fatal("a finished step must produce its row")
	}
	if m.finishStep(toolResultMsg{id: "c1", ok: true}) != nil {
		t.Fatal("a step prints once")
	}
	if m.stepsPrinted != 1 || m.toolHistory[0].dur != 300*time.Millisecond {
		t.Fatalf("printed=%d dur=%v", m.stepsPrinted, m.toolHistory[0].dur)
	}
}

func TestALongTurnPrintsEightRowsThenCounts(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	for i := 0; i < 12; i++ {
		id := string(rune('a' + i))
		m.beginStep(toolStartMsg{id: id, tool: "web_search", detail: "q"})
		m.finishStep(toolResultMsg{id: id, ok: true, dur: time.Second})
	}
	if m.stepsPrinted != stepRowCap || m.stepsHidden != 4 {
		t.Fatalf("printed=%d hidden=%d", m.stepsPrinted, m.stepsHidden)
	}
	if line := hiddenStepsLine(m.toolHistory, m.stepsHidden, 20*time.Second, 120); !strings.Contains(line, "4 more steps") || !strings.Contains(line, "Searched the web 12 times") {
		t.Fatalf("summary line: %q", line)
	}
	m.showTools = true
	m.beginStep(toolStartMsg{id: "z", tool: "exec"})
	m.finishStep(toolResultMsg{id: "z", ok: true})
	if m.stepsPrinted != stepRowCap+1 {
		t.Fatal("/details must print every step")
	}
}

func TestOlderStatusFramesDoNotDoubleUpOnceStepsArrive(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.Update(toolProgressMsg{tool: "exec", label: "Working on it…"})
	if len(m.toolHistory) != 1 {
		t.Fatalf("an older Pod still gets its label step: %+v", m.toolHistory)
	}
	m.Update(toolStartMsg{id: "c1", tool: "exec", detail: "ls"})
	m.Update(toolProgressMsg{tool: "exec", label: "Working on it…"})
	if len(m.toolHistory) != 2 || !m.toolHistory[0].done {
		t.Fatalf("a Pod that reports calls replaces the label steps: %+v", m.toolHistory)
	}
}

func TestMessagesTypedWhileWorkingAreNeverLost(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.queued = []string{"use blue", "and bold"}
	m.noteSteer(steerMsg{contents: []string{"use blue"}, picked: true})
	if len(m.queued) != 1 || m.queued[0] != "and bold" {
		t.Fatalf("picked up message must leave the queue: %v", m.queued)
	}
	// The turn ends without reading the other one: it goes out next.
	if next := m.settleQueue(true); next != "and bold" || len(m.queued) != 0 {
		t.Fatalf("next=%q queue=%v", next, m.queued)
	}
	// Two left over go out one per turn, in order.
	m.podReportsPickup = true
	m.queued = []string{"a", "b"}
	if next := m.settleQueue(true); next != "a" || len(m.queued) != 1 {
		t.Fatalf("first: %q %v", next, m.queued)
	}
	if next := m.settleQueue(true); next != "b" {
		t.Fatalf("second: %q", next)
	}
}

func TestAnOlderPodThatNeverReportsPickupKeepsTheOldBehaviour(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.podReportsPickup = false
	m.queued = []string{"x"}
	if next := m.settleQueue(true); next != "" || len(m.queued) != 0 {
		t.Fatalf("accepted steering counts as read on an older Pod: %q %v", next, m.queued)
	}
}

func TestTurnEventsFromTheInProcessAgentBecomeMessages(t *testing.T) {
	if m, ok := turnEventMsg(map[string]interface{}{"type": "tool_start", "id": "c1", "tool": "exec", "detail": "ls"}).(toolStartMsg); !ok || m.id != "c1" || m.detail != "ls" {
		t.Fatalf("tool_start: %+v", m)
	}
	r, ok := turnEventMsg(map[string]interface{}{"type": "tool_result", "id": "c1", "ok": false, "ms": int64(1500), "note": "boom"}).(toolResultMsg)
	if !ok || r.ok || r.dur != 1500*time.Millisecond || r.note != "boom" {
		t.Fatalf("tool_result: %+v", r)
	}
	if s, ok := turnEventMsg(map[string]interface{}{"type": "steer_picked", "contents": []string{"a"}}).(steerMsg); !ok || !s.picked || len(s.contents) != 1 {
		t.Fatalf("steer_picked: %+v", s)
	}
	if turnEventMsg(map[string]interface{}{"type": "nothing"}) != nil {
		t.Fatal("unknown events are ignored")
	}
}

func TestTheMoreStepsLineNeverRunsPastTheTerminal(t *testing.T) {
	steps := []toolStep{{id: "1", tool: "web_search"}, {id: "2", tool: "exec"}, {id: "3", tool: "read_file"}, {id: "4", tool: "browser_click"}}
	for _, w := range []int{30, 56, 80, 140} {
		if got := lipgloss.Width(hiddenStepsLine(steps, 9, 90*time.Second, w)); got > w {
			t.Errorf("width %d: line is %d cells", w, got)
		}
	}
	if hiddenStepsLine(steps, 0, time.Second, 80) != "" {
		t.Fatal("nothing hidden: nothing said")
	}
}
