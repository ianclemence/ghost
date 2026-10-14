package canvascheck

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestInstrumentPutsTheHarnessFirst(t *testing.T) {
	for _, page := range []string{
		`<!doctype html><html><head><title>x</title></head><body>hi</body></html>`,
		`<html><body>hi</body></html>`,
		`<p>just a fragment</p>`,
	} {
		out := Instrument(page)
		i, j := strings.Index(out, "window.__errs"), strings.Index(out, "hi")
		if i < 0 || (j >= 0 && i > j) {
			t.Errorf("harness must come before the page's own content: %q", out)
		}
	}
}

func TestFindingsNameTheWorstFirstAndStayQuietWhenFine(t *testing.T) {
	if got := Findings(measured{Text: 200, SW: 390, IW: 390}, nil); len(got) != 0 {
		t.Fatalf("a fine page drew findings: %v", got)
	}
	got := Findings(
		measured{Errs: []string{"x is not defined"}, Text: 5, SW: 520, IW: 390, Small: []string{"button ×"}, Tiny: 3},
		[]tapped{{Label: "Add", Errs: []string{"boom"}}},
	)
	joined := strings.Join(got, "|")
	for _, want := range []string{"throws on load: x is not defined", `tapping "Add" throws: boom`, "no visible text", "130px wider", "tap targets under 40px", "under 12px"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	if !strings.HasPrefix(got[0], "it throws on load") {
		t.Errorf("errors must lead: %v", got)
	}
}

func chromeAvailable() bool {
	_, err := exec.LookPath("chromium")
	return err == nil
}

func TestCheckRendersThePageAndCatchesWhatItThrows(t *testing.T) {
	if !chromeAvailable() {
		t.Skip("no chromium on this machine")
	}
	broken := `<!doctype html><html><body><p>A page with enough visible words to count as rendered.</p>
<button style="min-height:48px;min-width:48px" onclick="nope()">Go</button>
<script>localStorage.setItem('a','b')</script></body></html>`
	got := strings.Join(Check(context.Background(), broken), "|")
	if !strings.Contains(got, "throws on load") || !strings.Contains(got, `tapping "Go" throws`) {
		t.Fatalf("storage use and a broken handler must both be caught: %q", got)
	}
	fine := `<!doctype html><html><body style="margin:0"><p style="font-size:16px">A page with enough visible words to count as rendered.</p>
<button style="min-height:48px;min-width:96px">Go</button></body></html>`
	if got := Check(context.Background(), fine); len(got) != 0 {
		t.Fatalf("a fine page drew findings: %v", got)
	}
}
