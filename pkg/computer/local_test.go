package computer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Without any executor tooling the local computer is an honest view-only/
// none computer: authority reports it, SupportedOps is empty, and every
// control/observe Do fails closed with a reason — never a fake success.
func TestLocalComputerNoExecutorIsTruthful(t *testing.T) {
	c := &LocalComputer{id: "local", controlTool: "", shotTool: "", display: ""}
	state, reason := c.Authority()
	if state != "none" || reason == "" {
		t.Fatalf("authority = %q %q", state, reason)
	}
	if len(c.SupportedOps()) != 0 {
		t.Fatalf("no-tool computer must support nothing, got %v", c.SupportedOps())
	}
	if _, err := c.Do(context.Background(), OpClick, Args{"x": "1", "y": "2"}); err == nil {
		t.Fatal("click without executor must fail closed")
	}
	if _, err := c.Do(context.Background(), OpScreenshot, Args{"path": "/tmp/x.png"}); err == nil {
		t.Fatal("screenshot without observation tool must fail closed")
	}
	if _, err := c.Do(context.Background(), OpExecute, nil); err == nil {
		t.Fatal("unsupported op must fail closed")
	}
}

// With only an observation tool and no display/input executor the computer
// reports view_only and refuses control while allowing nothing fabricated.
func TestLocalComputerViewOnlyRefusesControl(t *testing.T) {
	c := &LocalComputer{id: "local", controlTool: "", shotTool: "scrot", display: ":99"}
	if state, _ := c.Authority(); state != "view_only" {
		t.Fatalf("authority = %q, want view_only", state)
	}
	if _, err := c.Do(context.Background(), OpClick, Args{"x": "0", "y": "0"}); err == nil {
		t.Fatal("control op must be refused in view_only")
	}
}

// Arg validation is strict and server-side: bad coordinates, oversized
// text, and disallowed keys never reach the executor.
func TestLocalComputerArgValidation(t *testing.T) {
	c := &LocalComputer{id: "local", controlTool: "xdotool", display: ":99"}
	ctx := context.Background()
	if _, err := c.Do(ctx, OpClick, Args{"x": "abc", "y": "2"}); err == nil {
		t.Fatal("non-integer coordinate must fail")
	}
	if _, err := c.Do(ctx, OpClick, Args{"x": "-1", "y": "2"}); err == nil {
		t.Fatal("negative coordinate must fail")
	}
	if _, err := c.Do(ctx, OpClick, Args{"x": "99999999", "y": "2"}); err == nil {
		t.Fatal("out-of-range coordinate must fail")
	}
	if _, err := c.Do(ctx, OpType, Args{"text": strings.Repeat("x", maxTypeLen+1)}); err == nil {
		t.Fatal("oversized text must fail")
	}
	if _, err := c.Do(ctx, OpPressKey, Args{"key": "rm"}); err == nil {
		t.Fatal("disallowed key must fail")
	}
	if _, err := c.Do(ctx, OpScreenshot, Args{"path": "/etc/shadow"}); err == nil {
		t.Fatal("output path outside allowed dirs must fail")
	}
	if !isSafeOutputPath(filepath.Join(os.TempDir(), "shot.png")) {
		t.Fatal("temp-dir path must be allowed")
	}
}

// With a real stub executor present, bounded input ops dispatch through the
// actual exec boundary (a real subprocess), return evidence, and report
// "dispatched" rather than a fabricated confirmation.
func TestLocalComputerDispatchWithStubExecutor(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "xdotool")
	script := "#!/bin/sh\necho \"$@\" > \"" + dir + "/calls.txt\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &LocalComputer{id: "local", controlTool: stub, display: ":99"}
	if state, _ := c.Authority(); state != "control" {
		t.Fatalf("authority = %q, want control", state)
	}
	res, err := c.Do(context.Background(), OpClick, Args{"x": "10", "y": "20"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verified {
		t.Fatal("dispatched input must not claim independent verification")
	}
	if res.Evidence["op"] != "click" || res.Evidence["outcome"] != "dispatched" {
		t.Fatalf("evidence wrong: %+v", res.Evidence)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "calls.txt"))
	if !strings.Contains(string(data), "click") {
		t.Fatalf("executor stub not invoked with bounded args: %s", data)
	}
}

// Placement availability distinguishes preview from control truthfully.
func TestLocalAvailabilityPreviewVsControl(t *testing.T) {
	viewOnly := AvailabilityOf(Descriptor{Placement: PlacementLocal, Capabilities: nil}, time.Now())
	if !strings.Contains(viewOnly.Detail, "view only") {
		t.Fatalf("view-only detail missing: %+v", viewOnly)
	}
	control := AvailabilityOf(Descriptor{Placement: PlacementLocal, Capabilities: []Op{OpClick}}, time.Now())
	if !strings.Contains(control.Detail, "executor ready") {
		t.Fatalf("control detail missing: %+v", control)
	}
}

// Scroll and drag validate like click (bounded directions, amounts and
// coordinates) and dispatch through the same exec boundary with evidence.
func TestLocalScrollDragValidationAndDispatch(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "xdotool")
	script := "#!/bin/sh\necho \"$@\" >> \"" + dir + "/calls.txt\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &LocalComputer{id: "local", controlTool: stub, display: ":99"}
	ctx := context.Background()
	for _, args := range []Args{
		{"direction": "diagonal"}, {"direction": ""},
		{"direction": "up", "amount": "0"}, {"direction": "up", "amount": "101"},
		{"direction": "up", "amount": "many"}, {"direction": "down", "x": "1"},
	} {
		if _, err := c.Do(ctx, OpScroll, args); err == nil {
			t.Fatalf("bad scroll args must fail: %+v", args)
		}
	}
	for _, args := range []Args{
		{"x1": "1", "y1": "1", "x2": "2"}, {"x1": "-1", "y1": "1", "x2": "2", "y2": "2"},
		{"x1": "1", "y1": "1", "x2": "99999999", "y2": "2"},
	} {
		if _, err := c.Do(ctx, OpDrag, args); err == nil {
			t.Fatalf("bad drag args must fail: %+v", args)
		}
	}
	res, err := c.Do(ctx, OpScroll, Args{"direction": "down", "amount": "3"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verified || res.Evidence["direction"] != "down" || res.Evidence["amount"] != "3" {
		t.Fatalf("scroll evidence wrong: %+v", res.Evidence)
	}
	res, err = c.Do(ctx, OpDrag, Args{"x1": "10", "y1": "20", "x2": "30", "y2": "40"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verified {
		t.Fatal("dispatched drag must not claim verification")
	}
	for _, k := range []string{"x1", "y1", "x2", "y2"} {
		_ = k
	}
	if res.Evidence["x2"] != "30" || res.Evidence["y2"] != "40" {
		t.Fatalf("drag evidence wrong: %+v", res.Evidence)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "calls.txt"))
	calls := string(data)
	if !strings.Contains(calls, "click --repeat 3 5") {
		t.Fatalf("scroll must wheel-click button 5 three times: %s", calls)
	}
	for _, want := range []string{"mousemove 10 20", "mousedown 1", "mousemove 30 40", "mouseup 1"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("drag sequence missing %q: %s", want, calls)
		}
	}
}

// Clipboard reads/writes go through a bounded selection tool only.
// Evidence records lengths, never bytes; overlong pastes are cut marked.
func TestLocalClipboardBounded(t *testing.T) {
	ctx := context.Background()
	bare := &LocalComputer{id: "local", controlTool: "/bin/true", display: ":99"}
	if _, err := bare.Do(ctx, OpClipboardGet, Args{}); err == nil {
		t.Fatal("clipboard without a tool must fail closed")
	}
	if _, err := bare.Do(ctx, OpClipboardSet, Args{"text": "x"}); err == nil {
		t.Fatal("clipboard write without a tool must fail closed")
	}
	if _, err := bare.Do(ctx, OpClipboardSet, Args{"text": strings.Repeat("x", maxTypeLen+1)}); err == nil {
		t.Fatal("oversized clipboard write must fail")
	}

	dir := t.TempDir()
	clip := filepath.Join(dir, "xclip")
	script := "#!/bin/sh\nif [ \"$1\" = \"-o\" ]; then cat \"" + dir + "/clip.txt\" 2>/dev/null; else cat > \"" + dir + "/clip.txt\"; fi\n"
	if err := os.WriteFile(clip, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := LocalComputerConfiguredFull("local", "/bin/true", "", clip, ":99")
	if got := Supports(c, OpClipboardGet); !got {
		t.Fatal("clipboard ops must be advertised with a tool present")
	}
	if _, err := c.Do(ctx, OpClipboardSet, Args{"text": "paste me"}); err != nil {
		t.Fatal(err)
	}
	res, err := c.Do(ctx, OpClipboardGet, Args{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Verified || res.Output != "paste me" {
		t.Fatalf("clipboard roundtrip wrong: %+v", res)
	}
	for k := range res.Evidence {
		if k != "op" && k != "chars" && k != "truncated" {
			t.Fatalf("evidence must never carry clipboard bytes (key %q)", k)
		}
	}
	// Overlong pastes cut marked, never silently.
	big := strings.Repeat("z", maxClipboardGet+10)
	if err := os.WriteFile(filepath.Join(dir, "clip.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = c.Do(ctx, OpClipboardGet, Args{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Evidence["truncated"] != "true" || !strings.HasSuffix(res.Output, "…[cut]") {
		t.Fatalf("overlong paste must cut marked: %+v len=%d", res.Evidence, len(res.Output))
	}
	// Empty selection is a valid empty observation, not a failure.
	_ = os.Remove(filepath.Join(dir, "clip.txt"))
	emptyClip := filepath.Join(dir, "xclip-empty")
	emptyScript := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(emptyClip, []byte(emptyScript), 0o755); err != nil {
		t.Fatal(err)
	}
	empty := LocalComputerConfiguredFull("local", "/bin/true", "", emptyClip, ":99")
	res, err = empty.Do(ctx, OpClipboardGet, Args{})
	if err != nil || !res.Verified || res.Output != "" {
		t.Fatalf("empty clipboard must read as verified empty: %+v %v", res, err)
	}
}

// The AT-SPI dump parser is pure: malformed lines drop, credential fields
// never survive, centers are computed, and output stays bounded.
func TestParseA11yDumpWithholdsPasswords(t *testing.T) {
	dump := "0\tframe\tSettings\t\t1\t0\t-1\t-1\t-1\t-1\n" +
		"1\tpush button\tSave\t\t1\t0\t100\t200\t60\t24\n" +
		"1\tpassword text\tSecret\thunter2\t1\t0\t100\t240\t200\t24\n" +
		"1\ttext\tName\tAda\t1\t1\t100\t120\t200\t24\n" +
		"garbage line\n" +
		"1\tlabel\t\t\t1\t0\t-1\t-1\t-1\t-1\n" +
		"SKIPPED\t2\n"
	els, text, skipped, ok := parseA11yDump(dump)
	if !ok {
		t.Fatal("valid dump must parse")
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want walker count 2", skipped)
	}
	if len(els) != 3 {
		t.Fatalf("elements = %d, want 3 (frame, button, text)", len(els))
	}
	for _, e := range els {
		if e.Label == "Secret" || e.Value == "hunter2" {
			t.Fatalf("credential field survived: %+v", e)
		}
	}
	if els[1].X != 130 || els[1].Y != 212 {
		t.Fatalf("button center wrong: %+v", els[1])
	}
	if els[0].X != 0 || els[0].Y != 0 {
		t.Fatalf("frame without useful extents must carry no click target: %+v", els[0])
	}
	_ = text
}

// Refs live for exactly one screen: unknown shapes, missing snapshots,
// expired snapshots, out-of-range refs and targetless elements all fail
// closed with an instruction.
func TestRefRetainerExpiry(t *testing.T) {
	var r refRetainer
	if _, _, err := r.resolve("e1"); err == nil || !strings.Contains(err.Error(), "inspect_ui first") {
		t.Fatalf("never-inspected must say inspect first: %v", err)
	}
	r.retain("win", []UIElement{{Label: "Save", Role: "button", X: 10, Y: 20}, {Label: "Big", Role: "frame"}})
	if _, _, err := r.resolve("bogus"); err == nil {
		t.Fatal("bad ref shape must fail")
	}
	if _, _, err := r.resolve("e9"); err == nil || !strings.Contains(err.Error(), "1..2") {
		t.Fatalf("out-of-range must name the live range: %v", err)
	}
	if _, _, err := r.resolve("e2"); err == nil || !strings.Contains(err.Error(), "no click target") {
		t.Fatalf("targetless element must fall back to coordinates: %v", err)
	}
	e, epoch, err := r.resolve("e1")
	if err != nil || e.X != 10 || epoch != 1 {
		t.Fatalf("live ref must resolve: %+v %d %v", e, epoch, err)
	}
	r.expire()
	if _, _, err := r.resolve("e1"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("post-action ref must say expired: %v", err)
	}
}

// Click-by-ref dispatches through the same exec boundary with ref
// evidence, then expires: the twin call without a fresh inspect fails.
func TestLocalClickByRefThenExpires(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "xdotool")
	script := "#!/bin/sh\necho \"$@\" >> \"" + dir + "/calls.txt\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &LocalComputer{id: "local", controlTool: stub, display: ":99"}
	c.refs.retain("win", []UIElement{{Label: "Save", Role: "button", X: 50, Y: 60}})
	res, err := c.Do(context.Background(), OpClick, Args{"element": "e1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Evidence["ref"] != "e1" || res.Evidence["x"] != "50" || res.Evidence["y"] != "60" {
		t.Fatalf("ref click evidence wrong: %+v", res.Evidence)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "calls.txt"))
	if !strings.Contains(string(data), "mousemove 50 60") {
		t.Fatalf("ref must resolve to coordinates: %s", data)
	}
	if _, err := c.Do(context.Background(), OpClick, Args{"element": "e1"}); err == nil {
		t.Fatal("post-action ref must expire")
	}
}
