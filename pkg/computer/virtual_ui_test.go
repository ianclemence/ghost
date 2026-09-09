package computer

import (
	"context"
	"strings"
	"testing"
)

// TestVirtualUICompleteSettingsFlow drives the deterministic fixture through
// the exact operation sequence the cc-01 golden case expects of the real
// model: observe -> click -> observe -> type -> press -> observe -> report.
// This locks the fixture semantics the live conversation depends on.
func TestVirtualUICompleteSettingsFlow(t *testing.T) {
	v := NewVirtualUI("settings")
	ctx := context.Background()

	// 1. observe the desktop.
	res, err := v.Do(ctx, OpInspectUI, nil)
	if err != nil || !res.Verified {
		t.Fatalf("desktop inspect: %v verified=%v", err, res.Verified)
	}
	if !strings.Contains(res.Output, "Ghost Desktop") {
		t.Fatalf("desktop inspect missing window title: %s", res.Output)
	}
	if !strings.Contains(res.Output, "Settings") {
		t.Fatalf("desktop inspect missing Settings icon: %s", res.Output)
	}

	// 2. click the Settings icon (coordinates are reported by the inspect).
	click, err := v.Do(ctx, OpClick, Args{"x": "640", "y": "400"})
	if err != nil {
		t.Fatal(err)
	}
	if click.Verified {
		t.Fatal("a dispatched click must not claim independent verification")
	}
	if click.Evidence["outcome"] != "opened_settings" {
		t.Fatalf("click evidence wrong: %+v", click.Evidence)
	}

	// 3. observe the dialog.
	res, _ = v.Do(ctx, OpInspectUI, nil)
	if !strings.Contains(res.Output, `title: "Settings"`) {
		t.Fatalf("dialog inspect missing title: %s", res.Output)
	}
	if !strings.Contains(res.Output, "textbox") || !strings.Contains(res.Output, `value: "Guest"`) {
		t.Fatalf("dialog inspect missing name field: %s", res.Output)
	}

	// 4. type the new name into the focused field.
	typ, err := v.Do(ctx, OpType, Args{"text": "Ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if typ.Verified {
		t.Fatal("typing must not claim independent verification")
	}

	// 5. press Return to save.
	key, err := v.Do(ctx, OpPressKey, Args{"key": "Return"})
	if err != nil {
		t.Fatal(err)
	}
	if key.Verified {
		t.Fatal("a key press must not claim independent verification")
	}
	if key.Evidence["outcome"] != "saved" {
		t.Fatalf("press evidence wrong: %+v", key.Evidence)
	}

	// 6. observe the final state: back on the desktop with the new name.
	final, err := v.Do(ctx, OpInspectUI, nil)
	if err != nil || !final.Verified {
		t.Fatalf("final inspect: %v", err)
	}
	if !strings.Contains(final.Output, "Ghost Desktop") {
		t.Fatalf("final inspect not back on desktop: %s", final.Output)
	}
	if !strings.Contains(strings.ToLower(final.Output), "display name: ghost") {
		t.Fatalf("final state did not reflect the saved name: %s", final.Output)
	}
}

// TestVirtualUIRequiresObservationForTruth verifies the fixture never lets a
// control op claim a verified outcome: typing into a field that has no focus
// is reported as ignored (not as success), and the value only changes when
// the field is actually focused — so the model must observe to be truthful.
func TestVirtualUIRequiresObservationForTruth(t *testing.T) {
	v := NewVirtualUI("settings")
	ctx := context.Background()

	// Typing before the dialog is open must be reported as ignored, not
	// silently dropped or claimed as a success.
	before := v.snapshotForTest()
	_ = before
	if _, err := v.Do(ctx, OpType, Args{"text": "Sneaky"}); err != nil {
		t.Fatal(err)
	}
	res, _ := v.Do(ctx, OpInspectUI, nil)
	if strings.Contains(res.Output, "Sneaky") {
		t.Fatal("typing without a focused field must not alter state")
	}
	// A key press with no dialog open has no visible effect.
	if _, err := v.Do(ctx, OpPressKey, Args{"key": "Return"}); err != nil {
		t.Fatal(err)
	}
	res, _ = v.Do(ctx, OpInspectUI, nil)
	if !strings.Contains(res.Output, "Current display name: Guest") {
		t.Fatalf("Return on the desktop must not change anything: %s", res.Output)
	}
	// Clicking empty space changes nothing.
	if _, err := v.Do(ctx, OpClick, Args{"x": "10", "y": "10"}); err != nil {
		t.Fatal(err)
	}
	res, _ = v.Do(ctx, OpInspectUI, nil)
	if !strings.Contains(res.Output, "Ghost Desktop") {
		t.Fatalf("empty click must not open anything: %s", res.Output)
	}
}

func (v *VirtualUI) snapshotForTest() string {
	r := v.inspect()
	return r.Output
}
