package computer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// LocalComputer is the real executor for the Ghost appliance placement
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
	display     string // resolved DISPLAY (or WAYLAND-less X target) or ""
}

// localControlCandidates and localShotCandidates are the bounded tool sets
// the executor is allowed to use. Order matters (first found wins).
var (
	localControlCandidates = []string{"xdotool"}
	localShotCandidates    = []string{"scrot", "import"} // ImageMagick import
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
)

// LocalComputerConfigured builds an executor with explicit tool/display
// settings (used by tests and by operators pinning a specific toolchain).
// Paths are used verbatim.
func LocalComputerConfigured(id, controlTool, shotTool, display string) *LocalComputer {
	return &LocalComputer{id: id, controlTool: controlTool, shotTool: shotTool, display: display}
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
	return c
}

func (c *LocalComputer) ID() string           { return c.id }
func (c *LocalComputer) Placement() Placement { return PlacementLocal }
func (c *LocalComputer) DisplayName() string {
	return "Ghost appliance"
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
// this appliance currently has no computer-control executor.
func (c *LocalComputer) SupportedOps() []Op {
	var out []Op
	if c.shotTool != "" {
		out = append(out, OpScreenshot)
	}
	if c.controlTool != "" && c.display != "" {
		// UI inspection needs the X window tree (xdotool); element-level
		// accessibility data is exposed only when a backend provides it.
		out = append(out, OpInspectUI, OpClick, OpType, OpPressKey)
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
	case OpClick, OpType, OpPressKey:
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
		}
	}
	return Result{}, ErrUnsupportedOp(c.id, op)
}

// doInspectUI reads the current UI through the X window tree and reports a
// bounded, structured snapshot. It never dumps process/system state and
// never exposes content it cannot honestly read: element-level
// accessibility detail is included only when the backend provides it, and
// the report says so otherwise.
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
	return Result{
		Output:   FormatUISnapshot(shot),
		Verified: true, // observed: the focused window title was read
		Evidence: map[string]string{"op": "inspect_ui", "window": title, "elements": "0"},
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
	x, err1 := strconv.Atoi(args["x"])
	y, err2 := strconv.Atoi(args["y"])
	if err1 != nil || err2 != nil || x < 0 || y < 0 || x > maxCoord || y > maxCoord {
		return Result{}, fmt.Errorf("click requires integer x,y coordinates within 0..%d", maxCoord)
	}
	if err := c.xdo(ctx, "mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return Result{}, err
	}
	if err := c.xdo(ctx, "click", "1"); err != nil {
		return Result{}, err
	}
	// Dispatched, not independently observed on the screen: report honestly.
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
	return Result{
		Output:   "key dispatched",
		Verified: false,
		Evidence: map[string]string{"op": "press_key", "key": key, "outcome": "dispatched"},
	}, nil
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
