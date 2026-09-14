package proactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Physical signals: ESP32 / GPIO / Pi sensor events as proactive inputs.
// Producers (an ESP32 bridge, a GPIO poller, a user script) drop one JSON
// file per event into <workspace>/state/sensors/*.json:
//
//	{"device":"hall-esp32","kind":"button","at":"2026-09-14T10:00:00Z"}
//	{"device":"loft-esp32","kind":"temperature","value":31.5,"unit":"C","at":"..."}
//	{"device":"kitchen-esp32","kind":"leak","value":1,"at":"..."}
//
// Ghost is a runtime, not a chatbot: these files are the physical boundary
// of the system. Reading is bounded (20 newest files, 64KiB each) and stale
// events (>24h) are ignored so a dead sensor never pages the owner forever.
// Consumers delete files they acted on; the scanner never deletes.

// SensorKind classifies a physical event.
type SensorKind string

const (
	SensorButton      SensorKind = "button"
	SensorMotion      SensorKind = "motion"
	SensorPresence    SensorKind = "presence"
	SensorTemperature SensorKind = "temperature"
	SensorHumidity    SensorKind = "humidity"
	SensorDoor        SensorKind = "door"
	SensorLeak        SensorKind = "leak"
)

// SensorEvent is one physical event.
type SensorEvent struct {
	Device string     `json:"device"`
	Kind   SensorKind `json:"kind"`
	Value  float64    `json:"value,omitempty"`
	Unit   string     `json:"unit,omitempty"`
	At     time.Time  `json:"at"`
	File   string     `json:"-"`
}

// Temperature thresholds (Celsius). A home that is freezing or overheating
// is worth one notice; comfort bands stay silent.
const (
	TempHighC = 30.0
	TempLowC  = 5.0
)

const (
	sensorMaxFiles = 20
	sensorMaxBytes = 64 << 10
	sensorMaxAge   = 24 * time.Hour
)

// SensorDir resolves the event inbox for a workspace.
func SensorDir(workspace string) string {
	return filepath.Join(workspace, "state", "sensors")
}

// ReadSensors returns fresh sensor events, newest first. Bounded and
// fail-open: a corrupt file is skipped, a missing dir yields none.
func ReadSensors(workspace string, now time.Time) []SensorEvent {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	entries, err := os.ReadDir(SensorDir(workspace))
	if err != nil {
		return nil
	}
	// Newest files first; bound the scan.
	type named struct {
		name string
		mod  time.Time
	}
	var names []named
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		names = append(names, named{e.Name(), fi.ModTime()})
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j].mod.After(names[i].mod) {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	if len(names) > sensorMaxFiles {
		names = names[:sensorMaxFiles]
	}
	var out []SensorEvent
	for _, n := range names {
		ev, ok := readSensorFile(filepath.Join(SensorDir(workspace), n.name), now)
		if !ok {
			continue
		}
		ev.File = n.name
		out = append(out, ev)
	}
	return out
}

func readSensorFile(path string, now time.Time) (SensorEvent, bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= 0 || fi.Size() > sensorMaxBytes {
		return SensorEvent{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return SensorEvent{}, false
	}
	var ev SensorEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return SensorEvent{}, false
	}
	ev.Device = strings.TrimSpace(ev.Device)
	ev.Kind = SensorKind(strings.ToLower(strings.TrimSpace(string(ev.Kind))))
	if ev.Device == "" || ev.Kind == "" {
		return SensorEvent{}, false
	}
	if ev.At.IsZero() {
		ev.At = fi.ModTime().UTC()
	}
	if now.Sub(ev.At) > sensorMaxAge {
		return SensorEvent{}, false
	}
	return ev, true
}

// SensorUrgent reports whether an event deserves an urgent notice.
// Button presses, water leaks, open doors, and out-of-range temperatures
// page; routine motion/presence/humidity never do (they would spam).
func SensorUrgent(ev SensorEvent) (bool, string) {
	switch ev.Kind {
	case SensorButton:
		return true, "button pressed on " + ev.Device
	case SensorLeak:
		if ev.Value != 0 {
			return true, "possible water leak at " + ev.Device
		}
	case SensorDoor:
		if ev.Value != 0 {
			return true, "door open at " + ev.Device
		}
	case SensorTemperature:
		if ev.Value >= TempHighC {
			return true, "high temperature at " + ev.Device
		}
		if ev.Value <= TempLowC && ev.Value != 0 {
			return true, "low temperature at " + ev.Device
		}
	}
	return false, ""
}

// ConsumeSensor removes an event file after it was acted on.
func ConsumeSensor(workspace, file string) {
	if file == "" || strings.Contains(file, "/") || strings.Contains(file, "\\") {
		return
	}
	_ = os.Remove(filepath.Join(SensorDir(workspace), file))
}
