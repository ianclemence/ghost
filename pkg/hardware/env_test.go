package hardware

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadsProcessorRoomSensorAndScaledChannels(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sys/class/thermal/thermal_zone0/temp", "52300")
	write(t, root, "sys/bus/iio/devices/iio:device0/name", "bme280")
	write(t, root, "sys/bus/iio/devices/iio:device0/in_temp_input", "27400")
	write(t, root, "sys/bus/iio/devices/iio:device0/in_humidityrelative_input", "61250")
	write(t, root, "sys/bus/iio/devices/iio:device0/in_pressure_input", "101.3")
	// A driver that only exposes raw + scale.
	write(t, root, "sys/bus/iio/devices/iio:device1/name", "dht11")
	write(t, root, "sys/bus/iio/devices/iio:device1/in_temp_raw", "24")
	write(t, root, "sys/bus/iio/devices/iio:device1/in_temp_scale", "1000")

	rs := ReadEnvironment(root)
	temp, hum := Room(rs)
	if temp == nil || temp.Value != 27.4 || temp.Source != "bme280" {
		t.Fatalf("room temperature: %+v", temp)
	}
	if hum == nil || hum.Value != 61.25 {
		t.Fatalf("humidity: %+v", hum)
	}
	if cpu := CPU(rs); cpu == nil || cpu.Value != 52.3 {
		t.Fatalf("processor: %+v", cpu)
	}
	found := false
	for _, r := range rs {
		if r.Source == "dht11" && r.Kind == "temperature" && r.Value == 24 {
			found = true
		}
		if r.Kind == "pressure" && (r.Value < 1012 || r.Value > 1014) {
			t.Fatalf("pressure must be in hPa: %+v", r)
		}
	}
	if !found {
		t.Fatalf("a raw+scale channel must be read: %+v", rs)
	}
	d := Describe(rs)
	if !strings.Contains(d, "27.4°C") || !strings.Contains(d, "61%") || !strings.Contains(d, "Pod itself is at 52°C") {
		t.Fatalf("description: %q", d)
	}
}

func TestNoSensorIsHonestNotAnError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sys/class/thermal/thermal_zone0/temp", "48000")
	d := Describe(ReadEnvironment(root))
	if !strings.Contains(d, "no temperature or humidity sensor connected") || !strings.Contains(d, "48°C") {
		t.Fatalf("with no sensor Ghost must say so and not invent a climate: %q", d)
	}
	if Describe(ReadEnvironment(t.TempDir())) == "" {
		t.Fatal("an empty system still yields a sentence")
	}
}

func TestHwmonTemperatureThatIsNotTheProcessor(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sys/class/hwmon/hwmon0/name", "cpu_thermal")
	write(t, root, "sys/class/hwmon/hwmon0/temp1_input", "50000")
	write(t, root, "sys/class/hwmon/hwmon1/name", "ds18b20")
	write(t, root, "sys/class/hwmon/hwmon1/temp1_input", "21500")
	temp, _ := Room(ReadEnvironment(root))
	if temp == nil || temp.Source != "ds18b20" || temp.Value != 21.5 {
		t.Fatalf("a 1-wire probe is a room sensor: %+v", temp)
	}
}

// A chip's die temperature is not the room. The Pi 5's RP1 reports one, and
// calling it "the temperature" would be a confident wrong answer.
func TestBoardChipsAreNotTheRoom(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sys/class/hwmon/hwmon1/name", "rp1_adc")
	write(t, root, "sys/class/hwmon/hwmon1/temp1_input", "55000")
	if temp, _ := Room(ReadEnvironment(root)); temp != nil {
		t.Fatalf("an internal chip must not be reported as the room: %+v", temp)
	}
}
