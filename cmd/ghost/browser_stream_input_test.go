package main

import "testing"

func TestViewerInputOnlyReachesTheBrowserDuringATakeover(t *testing.T) {
	mouse := []byte(`{"type":"input_mouse","eventType":"mousePressed","x":40,"y":40,"button":"left","clickCount":1}`)
	key := []byte(`{"type":"input_keyboard","eventType":"keyDown","key":"a","text":"a"}`)
	touch := []byte(`{"type":"input_touch","eventType":"touchStart","touchPoints":[]}`)
	for _, m := range [][]byte{mouse, key, touch} {
		if screencastClientMessageAllowed(m, false) {
			t.Errorf("input must be dropped while Ghost has control: %s", m)
		}
		if !screencastClientMessageAllowed(m, true) {
			t.Errorf("input must pass during a takeover: %s", m)
		}
	}
}

func TestViewersMayAlwaysPaceTheStreamButSendNothingElse(t *testing.T) {
	for _, m := range []string{`{"type":"config","maxFps":10}`, `{"type":"config","pacing":"ack"}`, `{"type":"ack","seq":41}`} {
		if !screencastClientMessageAllowed([]byte(m), false) {
			t.Errorf("%s is harmless and must pass", m)
		}
	}
	for _, m := range []string{
		`{"type":"screencast_start"}`, `{"type":"eval","js":"alert(1)"}`, `not json`, ``, `{"type":42}`, `{}`,
	} {
		if screencastClientMessageAllowed([]byte(m), true) {
			t.Errorf("%q must never reach the browser, even during a takeover", m)
		}
	}
	huge := make([]byte, maxScreencastClientMessage+1)
	copy(huge, `{"type":"input_keyboard"}`)
	if screencastClientMessageAllowed(huge, true) {
		t.Error("an oversized message must be dropped")
	}
}
