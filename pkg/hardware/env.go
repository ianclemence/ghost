package hardware

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Reading is one measurement from something physical: the Pod's own processor,
// or a sensor plugged into it.
type Reading struct {
	Source string  `json:"source"` // "cpu", or the sensor's name (bme280, dht11…)
	Kind   string  `json:"kind"`   // temperature | humidity | pressure
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"` // C | % | hPa
	// Internal is true for the Pod's own parts (its processor), false for a
	// sensor measuring the room.
	Internal bool `json:"internal,omitempty"`
}

// ReadEnvironment reads what the Pod can feel through Linux's standard sensor
// interfaces, under root ("" means the real filesystem): the processor's
// thermal zone, industrial-I/O sensors (BME280, DHT11/22, SHT3x and similar,
// once their kernel driver is loaded) and hwmon temperature inputs. It reads
// only; nothing is opened for writing and no bus is driven, so it is safe to
// call at any time. No sensor is not an error: it returns just the processor.
func ReadEnvironment(root string) []Reading {
	if root == "" {
		root = "/"
	}
	var out []Reading
	add := func(r Reading) { out = append(out, r) }

	// The processor.
	zones, _ := filepath.Glob(filepath.Join(root, "sys/class/thermal/thermal_zone*/temp"))
	sort.Strings(zones)
	for _, z := range zones {
		if v, ok := readMilli(z); ok {
			add(Reading{Source: "cpu", Kind: "temperature", Value: v, Unit: "C", Internal: true})
			break
		}
	}

	// Industrial I/O: the driver names the sensor and exposes scaled inputs.
	devs, _ := filepath.Glob(filepath.Join(root, "sys/bus/iio/devices/iio:device*"))
	sort.Strings(devs)
	for _, d := range devs {
		name := readText(filepath.Join(d, "name"))
		if name == "" {
			name = filepath.Base(d)
		}
		if v, ok := readMilli(filepath.Join(d, "in_temp_input")); ok {
			add(Reading{Source: name, Kind: "temperature", Value: v, Unit: "C"})
		} else if v, ok := readScaled(d, "in_temp"); ok {
			add(Reading{Source: name, Kind: "temperature", Value: v, Unit: "C"})
		}
		if v, ok := readMilli(filepath.Join(d, "in_humidityrelative_input")); ok {
			add(Reading{Source: name, Kind: "humidity", Value: v, Unit: "%"})
		} else if v, ok := readScaled(d, "in_humidityrelative"); ok {
			add(Reading{Source: name, Kind: "humidity", Value: v, Unit: "%"})
		}
		if v, ok := readFloat(filepath.Join(d, "in_pressure_input")); ok {
			add(Reading{Source: name, Kind: "pressure", Value: v * 10, Unit: "hPa"}) // IIO reports kPa
		}
	}

	// hwmon temperatures that are not the processor.
	hw, _ := filepath.Glob(filepath.Join(root, "sys/class/hwmon/hwmon*"))
	sort.Strings(hw)
	for _, h := range hw {
		name := readText(filepath.Join(h, "name"))
		if name == "" || internalHwmon[name] {
			// The processor, the board's own chips and the drives measure the
			// machine, not the room. Reporting a chip's die temperature as
			// "the temperature" would be a confident wrong answer.
			continue
		}
		ins, _ := filepath.Glob(filepath.Join(h, "temp*_input"))
		sort.Strings(ins)
		for _, in := range ins {
			if v, ok := readMilli(in); ok {
				add(Reading{Source: name, Kind: "temperature", Value: v, Unit: "C"})
				break
			}
		}
	}
	return out
}

func readText(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readMilli reads a value the kernel reports in thousandths.
func readMilli(p string) (float64, bool) {
	s := readText(p)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f / 1000, true
}

// readFloat reads a plain decimal value.
func readFloat(p string) (float64, bool) {
	f, err := strconv.ParseFloat(readText(p), 64)
	return f, err == nil
}

// readScaled reads (raw + offset) * scale, how IIO drivers without a ready
// "input" file report a channel; the result is in thousandths.
func readScaled(dir, prefix string) (float64, bool) {
	raw := readText(filepath.Join(dir, prefix+"_raw"))
	if raw == "" {
		return 0, false
	}
	r, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	scale, offset := 1.0, 0.0
	if s := readText(filepath.Join(dir, prefix+"_scale")); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			scale = f
		}
	}
	if o := readText(filepath.Join(dir, prefix+"_offset")); o != "" {
		if f, err := strconv.ParseFloat(o, 64); err == nil {
			offset = f
		}
	}
	return (r + offset) * scale / 1000, true
}

// internalHwmon are hwmon devices that report the machine's own parts.
var internalHwmon = map[string]bool{
	"cpu_thermal": true, "rpi_volt": true, "rp1_adc": true, "pwmfan": true, "coretemp": true,
	"k10temp": true, "acpitz": true, "nvme": true, "amdgpu": true, "soc_thermal": true,
	"bcm2835_thermal": true, "gpu_thermal": true,
}

// Room returns the first non-processor temperature and humidity, if any.
func Room(rs []Reading) (temp, hum *Reading) {
	for i := range rs {
		switch {
		case rs[i].Kind == "temperature" && !rs[i].Internal && temp == nil:
			temp = &rs[i]
		case rs[i].Kind == "humidity" && hum == nil:
			hum = &rs[i]
		}
	}
	return
}

// CPU returns the processor temperature, if the system reports one.
func CPU(rs []Reading) *Reading {
	for i := range rs {
		if rs[i].Internal && rs[i].Kind == "temperature" {
			return &rs[i]
		}
	}
	return nil
}

// Describe says, in one plain sentence, what the Pod can feel. With no room
// sensor it says so and names the processor instead of inventing a climate.
func Describe(rs []Reading) string {
	temp, hum := Room(rs)
	cpu := CPU(rs)
	var parts []string
	switch {
	case temp != nil && hum != nil:
		parts = append(parts, fmt.Sprintf("It's %.1f°C and %.0f%% humidity in the room (%s)", temp.Value, hum.Value, temp.Source))
	case temp != nil:
		parts = append(parts, fmt.Sprintf("It's %.1f°C in the room (%s); there's no humidity sensor connected", temp.Value, temp.Source))
	case hum != nil:
		parts = append(parts, fmt.Sprintf("Humidity in the room is %.0f%% (%s); there's no temperature sensor connected", hum.Value, hum.Source))
	default:
		parts = append(parts, "I have no temperature or humidity sensor connected, so I can't feel the room")
	}
	if cpu != nil {
		parts = append(parts, fmt.Sprintf("the Pod itself is at %.0f°C", cpu.Value))
	}
	return strings.Join(parts, "; ") + "."
}
