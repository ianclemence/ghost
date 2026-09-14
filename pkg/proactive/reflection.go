package proactive

import (
	"os"
	"path/filepath"
	"time"
)

// reflectionMarker is a date-stamped file proving today's reflection ran.
func reflectionMarker(workspace, date string) string {
	return filepath.Join(workspace, "proactive", "reflected-"+date+".mark")
}

// ReflectionDue reports whether the evening reflection should run: inside
// the evening hour (pol.EveningReflection ±30m) and not yet run today.
func ReflectionDue(workspace string, now time.Time, loc *time.Location, pol Policy) bool {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	want := pol.EveningReflection
	if len(want) != 5 {
		want = "22:00"
	}
	hm := local.Format("15:04")
	if hm < addMinutes(want, -30) || hm > addMinutes(want, 30) {
		return false
	}
	if _, err := os.Stat(reflectionMarker(workspace, local.Format("2006-01-02"))); err == nil {
		return false
	}
	return true
}

// MarkReflected records today's reflection as done.
func MarkReflected(workspace string, now time.Time, loc *time.Location) {
	if loc == nil {
		loc = time.UTC
	}
	_ = os.MkdirAll(filepath.Join(workspace, "proactive"), 0755)
	_ = os.WriteFile(reflectionMarker(workspace, now.In(loc).Format("2006-01-02")), []byte(now.Format(time.RFC3339)), 0644)
}

func addMinutes(hm string, delta int) string {
	h, m := 0, 0
	sscanf2(hm, &h, &m)
	t := h*60 + m + delta
	if t < 0 {
		t += 24 * 60
	}
	t %= 24 * 60
	return itoa2(t / 60) + ":" + itoa2(t%60)
}

func sscanf2(s string, h, m *int) {
	if len(s) != 5 || s[2] != ':' {
		return
	}
	*h = int(s[0]-'0')*10 + int(s[1]-'0')
	*m = int(s[3]-'0')*10 + int(s[4]-'0')
}

func itoa2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
