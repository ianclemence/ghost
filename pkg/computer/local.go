package computer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LocalComputer is the real executor for the Ghost placement
// (PlacementLocal). It drives the machine's own display through bounded,
// explicit tools — xdotool for input, scrot/import for observation — never
// through an arbitrary shell. Every operation is validated, bounded, and
// reported truthfully:
//
//   - If the required tool or a display is absent, the computer reports
//     view-only/unavailable with a reason and refuses control ops. It never
//     fabricates an execution.
//   - Verified is set only when the executor can observe the expected end
//     state (e.g. a screenshot file exists with content). Dispatched input
//     is reported as dispatched, not as confirmed — completion stays honest.
//
// This is Ghost's "preview vs control" boundary: control authority exists
// only when an executor is actually present.
type LocalComputer struct {
	id          string
	controlTool string // xdotool path or ""
	shotTool    string // scrot|import path or ""
	clipTool    string // xclip|xsel path or ""
	display     string // resolved DISPLAY (or WAYLAND-less X target) or ""
	// refs retains the last inspect snapshot's elements for click-by-ref;
	// any mutating action expires them (see refRetainer).
	refs refRetainer
	// a11y caches the AT-SPI backend probe: probed once, then trusted.
	a11yMu     sync.Mutex
	a11yProbed bool
	a11yOK     bool
}

// localControlCandidates and localShotCandidates are the bounded tool sets
// the executor is allowed to use. Order matters (first found wins).
var (
	localControlCandidates = []string{"xdotool"}
	localShotCandidates    = []string{"scrot", "import"} // ImageMagick import
	localClipCandidates    = []string{"xclip", "xsel"}
	allowedKeys            = map[string]bool{
		"Return": true, "Tab": true, "Escape": true, "space": true, "BackSpace": true,
		"Left": true, "Right": true, "Up": true, "Down": true, "Home": true, "End": true,
		"Page_Up": true, "Page_Down": true, "Delete": true, "Insert": true,
		"F1": true, "F2": true, "F3": true, "F4": true, "F5": true, "F6": true,
		"F7": true, "F8": true, "F9": true, "F10": true, "F11": true, "F12": true,
	}
	maxTypeLen   = 1000
	maxCoord     = 32767
	maxScrollKey = 100
	// maxClipboardGet bounds one clipboard read: enough for pasted text,
	// never a silent bulk exfiltration channel. Evidence records the
	// length, never the content.
	maxClipboardGet = 2000
	// a11yTimeout bounds one AT-SPI tree walk: accessibility buses can
	// stall, and observation must fail closed instead of hanging the turn.
	a11yTimeout = 8 * time.Second
	// a11yMaxElements caps one snapshot: refs stay few enough to act on.
	a11yMaxElements = 80
)

// LocalComputerConfigured builds an executor with explicit tool/display
// settings (used by tests and by operators pinning a specific toolchain).
// Paths are used verbatim. No clipboard tool: clipboard ops report
// unsupported on this executor.
func LocalComputerConfigured(id, controlTool, shotTool, display string) *LocalComputer {
	return &LocalComputer{id: id, controlTool: controlTool, shotTool: shotTool, display: display}
}

// LocalComputerConfiguredFull is LocalComputerConfigured with an explicit
// clipboard tool (xclip or xsel path, "" for none).
func LocalComputerConfiguredFull(id, controlTool, shotTool, clipTool, display string) *LocalComputer {
	return &LocalComputer{id: id, controlTool: controlTool, shotTool: shotTool, clipTool: clipTool, display: display}
}

// NewLocalComputer probes the environment once and builds the executor for
// id. It never errors: an executor with no tools/display is a truthful
// view-only/unavailable computer, not a configuration crash.
func NewLocalComputer(id string) *LocalComputer {
	c := &LocalComputer{id: id}
	c.display = os.Getenv("DISPLAY")
	if c.display == "" {
		c.display = os.Getenv("WAYLAND_DISPLAY") // informational; control needs X
		if c.display != "" {
			c.display = "" // no XTEST on wayland-only through xdotool
		}
	}
	for _, name := range localControlCandidates {
		if p, err := exec.LookPath(name); err == nil {
			c.controlTool = p
			break
		}
	}
	for _, name := range localShotCandidates {
		if p, err := exec.LookPath(name); err == nil {
			c.shotTool = p
			break
		}
	}
	for _, name := range localClipCandidates {
		if p, err := exec.LookPath(name); err == nil {
			c.clipTool = p
			break
		}
	}
	return c
}

func (c *LocalComputer) ID() string           { return c.id }
func (c *LocalComputer) Placement() Placement { return PlacementLocal }
func (c *LocalComputer) DisplayName() string {
	return "Ghost"
}

// Authority is Ghost's explicit preview-vs-control signal: "control" when a
// real input executor and display exist, "view_only" when observation may be
// possible but control is not, "none" otherwise. The reason explains why.
func (c *LocalComputer) Authority() (control string, reason string) {
	switch {
	case c.controlTool != "" && c.display != "":
		return "control", fmt.Sprintf("%s on display %s", filepath.Base(c.controlTool), c.display)
	case c.shotTool != "" && c.display != "":
		return "view_only", "observation tool present but no input executor on display " + c.display
	default:
		return "none", "no display and/or input or observation executor present on this Ghost"
	}
}

// SupportedOps lists the operations Do can actually perform given the tools
// and display discovered at construction. An empty list is honest: it means
// this personal AI currently has no computer-control executor.
func (c *LocalComputer) SupportedOps() []Op {
	var out []Op
	if c.shotTool != "" {
		out = append(out, OpScreenshot)
	}
	if c.controlTool != "" && c.display != "" {
		// UI inspection needs the X window tree (xdotool); element-level
		// accessibility detail arrives when an AT-SPI backend answers.
		out = append(out, OpInspectUI, OpClick, OpType, OpPressKey, OpScroll, OpDrag)
	}
	if c.clipTool != "" && c.display != "" {
		out = append(out, OpClipboardGet, OpClipboardSet)
	}
	return out
}

// Do executes one bounded operation. It fails closed on anything it cannot
// truthfully perform (unsupported op, missing tool, no display, invalid or
// out-of-range arguments) — never with a silent or fake success.
func (c *LocalComputer) Do(ctx context.Context, op Op, args Args) (Result, error) {
	switch op {
	case OpScreenshot:
		return c.doObserve(ctx, args)
	case OpInspectUI:
		return c.doInspectUI(ctx)
	case OpClick, OpType, OpPressKey, OpScroll, OpDrag:
		if c.controlTool == "" {
			return Result{}, ErrComputerUnavailable(c.id, "no input executor present (install xdotool and run on an X display)")
		}
		if c.display == "" {
			return Result{}, ErrComputerUnavailable(c.id, "no display available for input")
		}
		switch op {
		case OpClick:
			return c.doClick(ctx, args)
		case OpType:
			return c.doType(ctx, args)
		case OpPressKey:
			return c.doKey(ctx, args)
		case OpScroll:
			return c.doScroll(ctx, args)
		case OpDrag:
			return c.doDrag(ctx, args)
		}
	case OpClipboardGet, OpClipboardSet:
		if c.clipTool == "" {
			return Result{}, ErrComputerUnavailable(c.id, "no clipboard tool present (install xclip or xsel)")
		}
		if c.display == "" {
			return Result{}, ErrComputerUnavailable(c.id, "no display available for clipboard access")
		}
		switch op {
		case OpClipboardGet:
			return c.doClipboardGet(ctx)
		case OpClipboardSet:
			return c.doClipboardSet(ctx, args)
		}
	}
	return Result{}, ErrUnsupportedOp(c.id, op)
}

// doInspectUI reads the current UI through the X window tree and reports a
// bounded, structured snapshot. It never dumps process/system state and
// never exposes content it cannot honestly read: element-level
// accessibility detail rides along when an AT-SPI backend answers, and the
// report says so otherwise. The snapshot's elements are retained for
// click-by-ref until the next observation or any action expires them.
func (c *LocalComputer) doInspectUI(ctx context.Context) (Result, error) {
	if c.controlTool == "" || c.display == "" {
		return Result{}, ErrComputerUnavailable(c.id, "no UI observation backend on this display (xdotool on X required)")
	}
	title, err := c.activeWindowTitle(ctx)
	if err != nil {
		return Result{}, ErrComputerUnavailable(c.id, "could not read the focused window: "+err.Error())
	}
	shot := UISnapshot{
		WindowTitle: title,
		VisibleText: "No element-level accessibility information is exposed on this display backend. Use the visible window title to navigate.",
	}
	if els, text, skipped, ok := c.a11ySnapshot(ctx); ok {
		shot.Elements = els
		if text != "" {
			shot.VisibleText = text
		} else {
			shot.VisibleText = ""
		}
		if skipped > 0 {
			if shot.VisibleText != "" {
				shot.VisibleText += "\n"
			}
			shot.VisibleText += fmt.Sprintf("(%d credential fields withheld.)", skipped)
		}
	}
	epoch := c.refs.retain(title, shot.Elements)
	ev := map[string]string{"op": "inspect_ui", "window": title,
		"elements": strconv.Itoa(len(shot.Elements)), "epoch": strconv.FormatUint(epoch, 10)}
	return Result{
		Output:   FormatUISnapshot(shot),
		Verified: true, // observed: the focused window title was read
		Evidence: ev,
	}, nil
}

// activeWindowTitle resolves the focused window's title through xdotool.
func (c *LocalComputer) activeWindowTitle(ctx context.Context) (string, error) {
	if c.controlTool == "" {
		return "", fmt.Errorf("no window tool present")
	}
	cmd := exec.CommandContext(ctx, c.controlTool, "getactivewindow")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("no active window")
	}
	cmd2 := exec.CommandContext(ctx, c.controlTool, "getwindowname", id)
	name, err := cmd2.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(name)), nil
}

func (c *LocalComputer) doObserve(ctx context.Context, args Args) (Result, error) {
	if c.shotTool == "" {
		return Result{}, ErrComputerUnavailable(c.id, "no observation tool present (install scrot or ImageMagick import)")
	}
	path := args["path"]
	if path == "" {
		return Result{}, fmt.Errorf("screenshot requires a destination path")
	}
	if !isSafeOutputPath(path) {
		return Result{}, fmt.Errorf("screenshot path must be an absolute path under the workspace")
	}
	cmd := exec.CommandContext(ctx, c.shotTool)
	switch filepath.Base(c.shotTool) {
	case "import":
		cmd.Args = append(cmd.Args, "-window", "root", path)
	default: // scrot
		cmd.Args = append(cmd.Args, path)
	}
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("screenshot failed: %w", err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		return Result{}, fmt.Errorf("screenshot produced no readable output")
	}
	return Result{
		Output:   path,
		Verified: true, // observed: file exists with content
		Evidence: map[string]string{"op": "screenshot", "path": path, "bytes": strconv.FormatInt(fi.Size(), 10)},
	}, nil
}

func (c *LocalComputer) doClick(ctx context.Context, args Args) (Result, error) {
	if ref := strings.TrimSpace(args["element"]); ref != "" {
		e, epoch, err := c.refs.resolve(ref)
		if err != nil {
			return Result{}, err
		}
		if err := c.xdo(ctx, "mousemove", strconv.Itoa(e.X), strconv.Itoa(e.Y)); err != nil {
			return Result{}, err
		}
		if err := c.xdo(ctx, "click", "1"); err != nil {
			return Result{}, err
		}
		// The screen may have changed: retained refs expire with the act.
		c.refs.expire()
		return Result{
			Output:   "click dispatched",
			Verified: false,
			Evidence: map[string]string{"op": "click", "ref": ref, "epoch": strconv.FormatUint(epoch, 10),
				"x": strconv.Itoa(e.X), "y": strconv.Itoa(e.Y), "outcome": "dispatched"},
		}, nil
	}
	x, err1 := strconv.Atoi(args["x"])
	y, err2 := strconv.Atoi(args["y"])
	if err1 != nil || err2 != nil || x < 0 || y < 0 || x > maxCoord || y > maxCoord {
		return Result{}, fmt.Errorf("click requires integer x,y coordinates within 0..%d, or an element ref like e3 from the latest inspect_ui", maxCoord)
	}
	if err := c.xdo(ctx, "mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return Result{}, err
	}
	if err := c.xdo(ctx, "click", "1"); err != nil {
		return Result{}, err
	}
	// Dispatched, not independently observed on the screen: report honestly.
	// Any retained element refs expire: the click may have changed them.
	c.refs.expire()
	return Result{
		Output:   "click dispatched",
		Verified: false,
		Evidence: map[string]string{"op": "click", "x": strconv.Itoa(x), "y": strconv.Itoa(y), "outcome": "dispatched"},
	}, nil
}

func (c *LocalComputer) doType(ctx context.Context, args Args) (Result, error) {
	text := args["text"]
	if text == "" {
		return Result{}, fmt.Errorf("type requires text")
	}
	if len(text) > maxTypeLen {
		return Result{}, fmt.Errorf("type text exceeds %d characters", maxTypeLen)
	}
	if err := c.xdo(ctx, "type", "--delay", "12", text); err != nil {
		return Result{}, err
	}
	c.refs.expire()
	return Result{
		Output:   "typing dispatched",
		Verified: false,
		Evidence: map[string]string{"op": "type", "chars": strconv.Itoa(len(text)), "outcome": "dispatched"},
	}, nil
}

func (c *LocalComputer) doKey(ctx context.Context, args Args) (Result, error) {
	key := args["key"]
	if !allowedKeys[key] {
		return Result{}, fmt.Errorf("key %q is not on the allowed key list", key)
	}
	if err := c.xdo(ctx, "key", key); err != nil {
		return Result{}, err
	}
	c.refs.expire()
	return Result{
		Output:   "key dispatched",
		Verified: false,
		Evidence: map[string]string{"op": "press_key", "key": key, "outcome": "dispatched"},
	}, nil
}

// scrollButtons maps a direction to the X button xdotool clicks to scroll.
var scrollButtons = map[string]string{
	"up": "4", "down": "5", "left": "6", "right": "7",
}

func (c *LocalComputer) doScroll(ctx context.Context, args Args) (Result, error) {
	button, ok := scrollButtons[strings.ToLower(strings.TrimSpace(args["direction"]))]
	if !ok {
		return Result{}, fmt.Errorf("scroll direction is up, down, left or right")
	}
	amount := 1
	if raw := strings.TrimSpace(args["amount"]); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxScrollKey {
			return Result{}, fmt.Errorf("scroll amount is 1..%d", maxScrollKey)
		}
		amount = n
	}
	// Scroll where the pointer is, unless a focus point is given.
	if x, xok := args["x"]; xok && strings.TrimSpace(x) != "" {
		y := strings.TrimSpace(args["y"])
		xi, err1 := strconv.Atoi(strings.TrimSpace(x))
		yi, err2 := strconv.Atoi(y)
		if err1 != nil || err2 != nil || xi < 0 || yi < 0 || xi > maxCoord || yi > maxCoord {
			return Result{}, fmt.Errorf("scroll focus requires integer x,y coordinates within 0..%d", maxCoord)
		}
		if err := c.xdo(ctx, "mousemove", strconv.Itoa(xi), strconv.Itoa(yi)); err != nil {
			return Result{}, err
		}
	}
	if err := c.xdo(ctx, "click", "--repeat", strconv.Itoa(amount), button); err != nil {
		return Result{}, err
	}
	c.refs.expire()
	return Result{
		Output:   "scroll dispatched",
		Verified: false,
		Evidence: map[string]string{"op": "scroll", "direction": strings.ToLower(strings.TrimSpace(args["direction"])),
			"amount": strconv.Itoa(amount), "outcome": "dispatched"},
	}, nil
}

func coordArg(args Args, key string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(args[key]))
	if err != nil || n < 0 || n > maxCoord {
		return 0, fmt.Errorf("drag requires integer %s within 0..%d", key, maxCoord)
	}
	return n, nil
}

func (c *LocalComputer) doDrag(ctx context.Context, args Args) (Result, error) {
	x1, err := coordArg(args, "x1")
	if err != nil {
		return Result{}, err
	}
	y1, err := coordArg(args, "y1")
	if err != nil {
		return Result{}, err
	}
	x2, err := coordArg(args, "x2")
	if err != nil {
		return Result{}, err
	}
	y2, err := coordArg(args, "y2")
	if err != nil {
		return Result{}, err
	}
	if err := c.xdo(ctx, "mousemove", strconv.Itoa(x1), strconv.Itoa(y1)); err != nil {
		return Result{}, err
	}
	if err := c.xdo(ctx, "mousedown", "1"); err != nil {
		return Result{}, err
	}
	dragErr := c.xdo(ctx, "mousemove", strconv.Itoa(x2), strconv.Itoa(y2))
	upErr := c.xdo(ctx, "mouseup", "1")
	// The button must never stick down: report the move failure, if any,
	// after releasing.
	if dragErr != nil {
		return Result{}, dragErr
	}
	if upErr != nil {
		return Result{}, upErr
	}
	c.refs.expire()
	return Result{
		Output:   "drag dispatched",
		Verified: false,
		Evidence: map[string]string{"op": "drag",
			"x1": strconv.Itoa(x1), "y1": strconv.Itoa(y1),
			"x2": strconv.Itoa(x2), "y2": strconv.Itoa(y2), "outcome": "dispatched"},
	}, nil
}

// doClipboardGet reads the X clipboard selection. The content goes to the
// model (that is the point of the op) but never to evidence or logs: only
// the length is recorded. Bounded: pastes longer than the cap are cut with
// the cut marked, never silently.
func (c *LocalComputer) doClipboardGet(ctx context.Context) (Result, error) {
	var cmd *exec.Cmd
	switch filepath.Base(c.clipTool) {
	case "xsel":
		cmd = exec.CommandContext(ctx, c.clipTool, "--clipboard", "--output")
	default: // xclip
		cmd = exec.CommandContext(ctx, c.clipTool, "-o", "-selection", "clipboard")
	}
	out, err := cmd.Output()
	if err != nil {
		// An empty selection is a valid observation, not a failure: xclip
		// exits nonzero when nothing is selected.
		if len(out) == 0 {
			return Result{Output: "", Verified: true,
				Evidence: map[string]string{"op": "clipboard_get", "chars": "0"}}, nil
		}
		return Result{}, fmt.Errorf("clipboard read failed: %w", err)
	}
	text := string(out)
	truncated := false
	if len(text) > maxClipboardGet {
		text = text[:maxClipboardGet]
		truncated = true
	}
	ev := map[string]string{"op": "clipboard_get", "chars": strconv.Itoa(len(text))}
	if truncated {
		ev["truncated"] = "true"
		text += "…[cut]"
	}
	return Result{Output: text, Verified: true, Evidence: ev}, nil
}

// doClipboardSet writes the X clipboard selection so a later paste lands
// it. Bounded like typing; evidence records the length, never the bytes.
func (c *LocalComputer) doClipboardSet(ctx context.Context, args Args) (Result, error) {
	text := args["text"]
	if text == "" {
		return Result{}, fmt.Errorf("clipboard_set requires text")
	}
	if len(text) > maxTypeLen {
		return Result{}, fmt.Errorf("clipboard text exceeds %d characters", maxTypeLen)
	}
	var cmd *exec.Cmd
	switch filepath.Base(c.clipTool) {
	case "xsel":
		cmd = exec.CommandContext(ctx, c.clipTool, "--clipboard", "--input")
	default: // xclip
		cmd = exec.CommandContext(ctx, c.clipTool, "-i", "-selection", "clipboard")
	}
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return Result{}, fmt.Errorf("clipboard write failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	c.refs.expire()
	return Result{
		Output:   "clipboard dispatched",
		Verified: false,
		Evidence: map[string]string{"op": "clipboard_set", "chars": strconv.Itoa(len(text)), "outcome": "dispatched"},
	}, nil
}

// a11yProbeScript walks the desktop accessibility tree through pyatspi
// and prints one tab-separated element per line:
//
//	depth \t role \t name \t value \t enabled \t focused \t x \t y \t w \t h
//
// Only showing elements print; password fields are withheld entirely (and
// counted); extents that cannot be read print -1. The walker caps depth
// and element count so a pathological tree cannot flood the turn. Python
// is the only AT-SPI client guaranteed scriptable here: there is no
// bounded CLI for the accessibility bus, and Ghost never shells anywhere
// else for observation.
const a11yProbeScript = `
import sys
MAX_DEPTH = 6
MAX_ELEMENTS = 80
count = [0]
skipped = [0]
def clean(s):
    if not s:
        return ""
    return " ".join(str(s).replace("\t", " ").split())
def emit(depth, role, name, value, enabled, focused, x, y, w, h):
    if count[0] >= MAX_ELEMENTS:
        return
    count[0] += 1
    sys.stdout.write("%d\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\n" % (
        depth, clean(role), clean(name)[:120], clean(value)[:120],
        enabled, focused, x, y, w, h))
def walk(acc, depth):
    if count[0] >= MAX_ELEMENTS or depth > MAX_DEPTH:
        return
    try:
        role = acc.getRoleName()
    except Exception:
        return
    if role == "password text":
        skipped[0] += 1
        return
    try:
        st = acc.getStateSet()
        showing = 1 if st.contains(pyatspi.STATE_SHOWING) else 0
        enabled = 1 if st.contains(pyatspi.STATE_ENABLED) else 0
        focused = 1 if st.contains(pyatspi.STATE_FOCUSED) else 0
    except Exception:
        showing, enabled, focused = 0, 0, 0
    if not showing:
        pass
    else:
        name, value = "", ""
        try:
            name = acc.name or ""
        except Exception:
            pass
        try:
            txt = acc.queryText()
            value = txt.getText(0, -1) or ""
        except Exception:
            pass
        x, y, w, h = -1, -1, -1, -1
        try:
            ext = acc.queryComponent().getExtents(pyatspi.DESKTOP_COORDS)
            x, y, w, h = ext.x, ext.y, ext.width, ext.height
        except Exception:
            pass
        if name or value:
            emit(depth, role, name, value, enabled, focused, x, y, w, h)
    try:
        n = acc.childCount
    except Exception:
        return
    for i in range(n):
        if count[0] >= MAX_ELEMENTS:
            return
        try:
            walk(acc.getChildAtIndex(i), depth + 1)
        except Exception:
            continue
try:
    import pyatspi
except Exception:
    sys.exit(3)
try:
    desktop = pyatspi.Registry.getDesktop(0)
except Exception:
    sys.exit(4)
for i in range(desktop.childCount):
    try:
        walk(desktop.getChildAtIndex(i), 0)
    except Exception:
        continue
sys.stdout.write("SKIPPED\t%d\n" % skipped[0])
`

// a11ySnapshot returns element-level UI detail when an AT-SPI backend
// answers: elements with click centers, visible text, and the count of
// withheld credential fields. ok=false means no backend (no python3, no
// pyatspi, bus unreachable, timeout) and the caller falls back to the
// window title. Availability is probed once and cached: a Ghost without a
// bus must not pay an 8-second timeout on every inspect.
func (c *LocalComputer) a11ySnapshot(ctx context.Context) (els []UIElement, text string, skipped int, ok bool) {
	c.a11yMu.Lock()
	probed, available := c.a11yProbed, c.a11yOK
	c.a11yMu.Unlock()
	if !probed {
		available = c.probeA11y(ctx)
		c.a11yMu.Lock()
		c.a11yProbed, c.a11yOK = true, available
		c.a11yMu.Unlock()
	}
	if !available {
		return nil, "", 0, false
	}
	tctx, cancel := context.WithTimeout(ctx, a11yTimeout)
	defer cancel()
	cmd := exec.CommandContext(tctx, "python3", "-c", a11yProbeScript)
	cmd.Env = append([]string{}, os.Environ()...)
	out, err := cmd.Output()
	if err != nil {
		return nil, "", 0, false
	}
	return parseA11yDump(string(out))
}

// probeA11y reports whether python3 can import pyatspi here.
func (c *LocalComputer) probeA11y(ctx context.Context) bool {
	tctx, cancel := context.WithTimeout(ctx, a11yTimeout)
	defer cancel()
	cmd := exec.CommandContext(tctx, "python3", "-c", "import pyatspi")
	return cmd.Run() == nil
}

// parseA11yDump reads the probe script's TSV into elements and visible
// text. Pure and bounded: malformed lines are dropped, names and values
// are capped, credential roles never survive (belt over the walker's own
// withholding), and elements without a name or value are skipped —
// nameless boxes are layout, not choices.
func parseA11yDump(dump string) (els []UIElement, text string, skipped int, ok bool) {
	var texts []string
	n := 0
	for _, line := range strings.Split(dump, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "SKIPPED\t") {
			fmt.Sscanf(line, "SKIPPED\t%d", &skipped)
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 10 {
			continue
		}
		role := strings.TrimSpace(f[1])
		if role == "" || role == "password text" {
			continue
		}
		name := capRunes(strings.TrimSpace(f[2]), 120)
		value := capRunes(strings.TrimSpace(f[3]), 120)
		if name == "" && value == "" {
			continue
		}
		if n >= a11yMaxElements {
			break
		}
		n++
		enabled := f[4] == "1"
		focused := f[5] == "1"
		x, _ := strconv.Atoi(f[6])
		y, _ := strconv.Atoi(f[7])
		w, _ := strconv.Atoi(f[8])
		h, _ := strconv.Atoi(f[9])
		ex, ey := 0, 0
		if x >= 0 && y >= 0 && w > 0 && h > 0 && x+w <= maxCoord && y+h <= maxCoord {
			ex, ey = x+w/2, y+h/2
		}
		label := name
		if label == "" {
			label = value
		}
		els = append(els, UIElement{
			Label: label, Role: role, Value: value,
			Enabled: enabled, Focused: focused, X: ex, Y: ey,
		})
		lower := strings.ToLower(role)
		if strings.Contains(lower, "text") || strings.Contains(lower, "label") || lower == "menu item" {
			if name != "" && len(texts) < 20 {
				texts = append(texts, name)
			} else if value != "" && len(texts) < 20 {
				texts = append(texts, value)
			}
		}
	}
	return els, strings.Join(texts, "\n"), skipped, true
}
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (c *LocalComputer) xdo(ctx context.Context, sub ...string) error {
	cmd := exec.CommandContext(ctx, c.controlTool)
	cmd.Args = append(cmd.Args, sub...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", c.controlTool, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// isSafeOutputPath restricts screenshot output to directories Ghost owns:
// the OS temp dir or the workspace named by GHOST_WORKSPACE. Absolute paths
// elsewhere are refused.
func isSafeOutputPath(p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	allowed := []string{filepath.Clean(os.TempDir())}
	if ws := os.Getenv("GHOST_WORKSPACE"); ws != "" {
		allowed = append(allowed, filepath.Clean(ws))
	}
	clean := filepath.Clean(p)
	for _, root := range allowed {
		if clean == root || strings.HasPrefix(clean, root+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
