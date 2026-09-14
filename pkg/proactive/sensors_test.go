package proactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSensor(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSensorsButton(t *testing.T) {
	ws := t.TempDir()
	writeSensor(t, SensorDir(ws), "a.json", `{"device":"hall","kind":"button","at":"2099-01-01T00:00:00Z"}`)
	evs := ReadSensors(ws, time.Date(2099, 1, 1, 1, 0, 0, 0, time.UTC))
	if len(evs) != 1 || evs[0].Kind != SensorButton {
		t.Fatalf("expected button event, got %+v", evs)
	}
	if ok, _ := SensorUrgent(evs[0]); !ok {
		t.Fatal("button must be urgent")
	}
}

func TestReadSensorsStaleIgnored(t *testing.T) {
	ws := t.TempDir()
	writeSensor(t, SensorDir(ws), "old.json", `{"device":"x","kind":"button","at":"2020-01-01T00:00:00Z"}`)
	if evs := ReadSensors(ws, time.Now().UTC()); len(evs) != 0 {
		t.Fatalf("stale event returned: %+v", evs)
	}
}

func TestSensorTemperatureThresholds(t *testing.T) {
	hot := SensorEvent{Device: "loft", Kind: SensorTemperature, Value: 31}
	if ok, reason := SensorUrgent(hot); !ok || reason == "" {
		t.Fatal("heat must page")
	}
	okTemp := SensorEvent{Device: "loft", Kind: SensorTemperature, Value: 21}
	if ok, _ := SensorUrgent(okTemp); ok {
		t.Fatal("comfort must stay silent")
	}
	motion := SensorEvent{Device: "hall", Kind: SensorMotion, Value: 1}
	if ok, _ := SensorUrgent(motion); ok {
		t.Fatal("motion must never page")
	}
}

func TestReadSensorsBoundedAndTolerant(t *testing.T) {
	ws := t.TempDir()
	writeSensor(t, SensorDir(ws), "bad.json", `not json`)
	writeSensor(t, SensorDir(ws), "empty-kind.json", `{"device":"x"}`)
	_ = json.Marshal
	if evs := ReadSensors(ws, time.Now().UTC()); len(evs) != 0 {
		t.Fatalf("corrupt events returned: %+v", evs)
	}
	if evs := ReadSensors(t.TempDir(), time.Now().UTC()); len(evs) != 0 {
		t.Fatal("missing dir must yield none")
	}
}
