package computer

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"sync"
)

// VirtualUI is a deterministic, local-first computer fixture: a small
// desktop state machine (an app launcher plus a Settings dialog) that the
// real model drives through the real Computer capability — real tool
// discovery, the permission broker gate, the lease discipline, and the
// evidence rule — with none of the nondeterminism of a physical display.
//
// It is NOT a model mock and it does not bypass the runtime: it is the
// executor behind the exact same computer.Computer interface a physical
// placement implements. Ghost's "no evidence = no success" rule still
// holds — clicks/typing/keys are reported as dispatched, never as
// independently screen-verified; only a subsequent inspect observation can
// confirm a resulting state.
//
// State machine:
//
//	Desktop ("Ghost Desktop")
//	  - visible text reports the current display name
//	  - a "Settings" app icon opens the Settings dialog when clicked
//	Settings dialog ("Settings")
//	  - "Name" textbox (focused on open) holds the display name
//	  - "Save" and "Cancel" buttons; pressing Return saves, Escape cancels
//	  - saving returns to the desktop with the new name shown
type VirtualUI struct {
	id string

	mu           sync.Mutex
	dialogOpen   bool
	displayName  string
	fieldFocused bool
	statusNote   string // transient human-readable state ("saved"/"cancelled")
}

// NewVirtualUI creates a fresh deterministic fixture with a default display
// name so a change is observable.
func NewVirtualUI(id string) *VirtualUI {
	return &VirtualUI{id: id, displayName: "Guest"}
}

func (v *VirtualUI) ID() string           { return v.id }
func (v *VirtualUI) Placement() Placement { return PlacementLocal }
func (v *VirtualUI) DisplayName() string  { return "Ghost settings fixture" }

// SupportedOps lists the deterministic operations the fixture performs.
func (v *VirtualUI) SupportedOps() []Op {
	return []Op{OpScreenshot, OpInspectUI, OpClick, OpType, OpPressKey}
}

// screen layout used by the fixture (window contents live on a 1280x800
// logical screen; coordinates are stable and reported in every snapshot).
const (
	vuiDesktopX, vuiDesktopY           = 320, 120
	vuiDialogX, vuiDialogY             = 380, 200
	vuiNameX, vuiNameY                 = 620, 300
	vuiSaveX, vuiSaveY                 = 560, 390
	vuiCancelX, vuiCancelY             = 700, 390
	vuiSettingsIconX, vuiSettingsIconY = 640, 400
)

// Do executes one bounded operation against the state machine.
func (v *VirtualUI) Do(ctx context.Context, op Op, args Args) (Result, error) {
	switch op {
	case OpScreenshot:
		return v.screenshot(ctx, args)
	case OpInspectUI:
		return v.inspect(), nil
	case OpClick:
		res, err := v.click(args)
		if err != nil {
			return Result{}, err
		}
		return res, nil
	case OpType:
		res, err := v.typ(args)
		if err != nil {
			return Result{}, err
		}
		return res, nil
	case OpPressKey:
		res, err := v.key(args)
		if err != nil {
			return Result{}, err
		}
		return res, nil
	default:
		return Result{}, ErrUnsupportedOp(v.id, op)
	}
}

// screenshot renders a deterministic PNG of the current fixture state to
// the requested path, so the observation operation is as real as inspect:
// the file exists with content and the screenshot reports Verified only
// when it was actually written.
func (v *VirtualUI) screenshot(ctx context.Context, args Args) (Result, error) {
	path := args["path"]
	if path == "" {
		return Result{}, fmt.Errorf("screenshot requires a destination path")
	}
	if !isSafeOutputPath(path) {
		return Result{}, fmt.Errorf("screenshot path must be an absolute path under the workspace")
	}
	v.mu.Lock()
	dialog := v.dialogOpen
	snap := FormatUISnapshot(v.snapshot())
	v.mu.Unlock()

	// Deterministic frame colors by state (so the capture is observable and
	// stable across runs, but never carries secret content).
	base := color.RGBA{R: 0x2b, G: 0x2b, B: 0x2b, A: 0xff}
	if dialog {
		base = color.RGBA{R: 0x1e, G: 0x3a, B: 0x5f, A: 0xff}
	}
	img := image.NewRGBA(image.Rect(0, 0, 640, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 640; x++ {
			c := base
			if x < 2 || y < 2 || x >= 638 || y >= 398 {
				c = color.RGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return Result{}, err
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		return Result{}, fmt.Errorf("screenshot produced no readable output")
	}
	// The virtual UI has no real pixels to hand to a non-vision model, so
	// the capture result also carries the same bounded textual snapshot a
	// real screen reader would produce. This keeps the observation channel
	// deterministic and immediately useful whichever observation tool the
	// model chooses.
	return Result{
		Output:   fmt.Sprintf("Screenshot written to %s. Screen contents:\n%s", path, snap),
		Verified: true,
		Evidence: map[string]string{"op": "screenshot", "path": path, "bytes": fmt.Sprintf("%d", fi.Size())},
	}, nil
}

func (v *VirtualUI) snapshot() UISnapshot {
	if !v.dialogOpen {
		title := "Ghost Desktop"
		text := fmt.Sprintf("Current display name: %s.", v.displayName)
		if v.statusNote != "" {
			text += " " + v.statusNote + "."
		}
		return UISnapshot{
			WindowTitle:  title,
			VisibleText:  text,
			FocusedLabel: "",
			Elements: []UIElement{
				{Label: "Settings", Role: "button", Value: "Open settings", Enabled: true,
					X: vuiSettingsIconX, Y: vuiSettingsIconY},
			},
		}
	}
	text := "Change your Ghost display name."
	if v.statusNote != "" {
		text += " " + v.statusNote + "."
	}
	return UISnapshot{
		WindowTitle:  "Settings",
		VisibleText:  text,
		FocusedLabel: "Name",
		Elements: []UIElement{
			{Label: "Name", Role: "textbox", Value: v.displayName, Enabled: true, Focused: v.fieldFocused,
				X: vuiNameX, Y: vuiNameY},
			{Label: "Save", Role: "button", Enabled: true, X: vuiSaveX, Y: vuiSaveY},
			{Label: "Cancel", Role: "button", Enabled: true, X: vuiCancelX, Y: vuiCancelY},
		},
	}
}

func (v *VirtualUI) inspect() Result {
	v.mu.Lock()
	defer v.mu.Unlock()
	s := v.snapshot()
	title := s.WindowTitle
	return Result{
		Output:   FormatUISnapshot(s),
		Verified: true, // the fixture deterministically read its own UI
		Evidence: map[string]string{"op": "inspect_ui", "window": title,
			"elements": fmt.Sprintf("%d", len(s.Elements))},
	}
}

// hitTest maps a click coordinate to the focused control.
func hitTest(x, y int) string {
	switch {
	case x >= vuiNameX-80 && x <= vuiNameX+80 && y >= vuiNameY-16 && y <= vuiNameY+16:
		return "name"
	case x >= vuiSaveX-50 && x <= vuiSaveX+50 && y >= vuiSaveY-18 && y <= vuiSaveY+18:
		return "save"
	case x >= vuiCancelX-50 && x <= vuiCancelX+50 && y >= vuiCancelY-18 && y <= vuiCancelY+18:
		return "cancel"
	case x >= vuiSettingsIconX-70 && x <= vuiSettingsIconX+70 && y >= vuiSettingsIconY-20 && y <= vuiSettingsIconY+20:
		return "settings"
	}
	return ""
}

func (v *VirtualUI) click(args Args) (Result, error) {
	x, err1 := atoi(args["x"])
	y, err2 := atoi(args["y"])
	if err1 != nil || err2 != nil {
		return Result{}, fmt.Errorf("click requires integer x,y coordinates")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	target := hitTest(x, y)
	effect := "none"
	if !v.dialogOpen && target == "settings" {
		v.dialogOpen = true
		v.fieldFocused = true
		v.statusNote = ""
		effect = "opened_settings"
	} else if v.dialogOpen && target == "name" {
		v.fieldFocused = true
		v.statusNote = ""
		effect = "focused_name"
	} else if v.dialogOpen && target == "save" {
		v.commitLocked()
		effect = "saved"
	} else if v.dialogOpen && target == "cancel" {
		v.cancelLocked()
		effect = "cancelled"
	}
	return Result{
		Output:   "click dispatched at (" + itoa(x) + ", " + itoa(y) + ")",
		Verified: false,
		Evidence: map[string]string{"op": "click", "x": itoa(x), "y": itoa(y), "outcome": effect},
	}, nil
}

func (v *VirtualUI) typ(args Args) (Result, error) {
	text := args["text"]
	if text == "" {
		return Result{}, fmt.Errorf("type requires text")
	}
	if len(text) > maxTypeLen {
		return Result{}, fmt.Errorf("type text exceeds %d characters", maxTypeLen)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	outcome := "dispatched"
	if !v.dialogOpen || !v.fieldFocused {
		outcome = "ignored_no_focus"
	} else {
		v.displayName = text
		v.statusNote = ""
	}
	return Result{
		Output:   "typing dispatched",
		Verified: false,
		Evidence: map[string]string{"op": "type", "chars": itoa(len(text)), "outcome": outcome},
	}, nil
}

func (v *VirtualUI) key(args Args) (Result, error) {
	key := args["key"]
	key, benign := vuiKeyNormalize(key)
	if !allowedKeys[key] && !benign {
		return Result{}, fmt.Errorf("key %q is not on the allowed key list", key)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	outcome := "dispatched"
	switch {
	case benign:
		// Text-editing shortcuts that a real field would accept but that
		// have no visible effect on this fixture are harmless no-ops, not
		// failures. Typing always replaces the field value, so clearing is
		// unnecessary — but a model trying to clear must not be punished.
		outcome = "no_visible_effect"
	case key == "Return":
		if v.dialogOpen {
			v.commitLocked()
			outcome = "saved"
		} else {
			outcome = "ignored_no_dialog"
		}
	case key == "Escape":
		if v.dialogOpen {
			v.cancelLocked()
			outcome = "cancelled"
		} else {
			outcome = "ignored_no_dialog"
		}
	case key == "BackSpace", key == "Tab", key == "space", key == "Left", key == "Right",
		key == "Up", key == "Down", key == "Home", key == "End", key == "Delete",
		key == "Page_Up", key == "Page_Down", key == "Insert",
		key == "F1", key == "F2", key == "F3", key == "F4", key == "F5", key == "F6",
		key == "F7", key == "F8", key == "F9", key == "F10", key == "F11", key == "F12":
		outcome = "no_visible_effect"
	}
	return Result{
		Output:   "key dispatched",
		Verified: false,
		Evidence: map[string]string{"op": "press_key", "key": key, "outcome": outcome},
	}, nil
}

// vuiKeyNormalize maps common text-editing key spellings a model may send
// onto the fixture's bounded vocabulary. Real keyboards spell the delete
// key "BackSpace"; models sometimes send "Backspace". The select-all
// shortcut is accepted as a benign no-op (never an arbitrary command).
func vuiKeyNormalize(key string) (string, bool) {
	switch key {
	case "Backspace", "backspace":
		return "BackSpace", false
	case "Enter", "enter", "return":
		return "Return", false
	case "Control+A", "control+a", "ctrl+a", "Ctrl+A", "ctrl+A", "CTRL+A":
		return key, true
	default:
		return key, false
	}
}

func (v *VirtualUI) commitLocked() {
	v.dialogOpen = false
	v.fieldFocused = false
	v.statusNote = "Display name saved as " + v.displayName
}

func (v *VirtualUI) cancelLocked() {
	v.dialogOpen = false
	v.fieldFocused = false
	v.statusNote = "Change cancelled"
}

func atoi(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty coordinate")
	}
	n := 0
	neg := false
	for i, r := range strings.TrimSpace(s) {
		if i == 0 && (r == '-' || r == '+') {
			neg = r == '-'
			continue
		}
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not an integer")
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
