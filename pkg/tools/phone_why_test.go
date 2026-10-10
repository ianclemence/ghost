package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/life"
)

// When no notification is found, Ghost says why as precisely as the phone
// lets it know, instead of guessing between "nothing came" and "it's off".
func TestNoNotesSaysWhy(t *testing.T) {
	loc := time.UTC
	at := time.Date(2026, 10, 10, 9, 12, 0, 0, loc)
	cases := []struct {
		name string
		sh   *life.Sharing
		app  string
		want string
	}{
		{"phone never said", nil, "Gmail", "arrive after an app is added"},
		{"sharing off", &life.Sharing{Notifications: false, At: at}, "Gmail", "off"},
		{"app not shared", &life.Sharing{Notifications: true, Apps: []string{"WhatsApp"}, At: at}, "Gmail", "isn't one of the apps"},
		{"shared, nothing new", &life.Sharing{Notifications: true, Apps: []string{"Gmail", "WhatsApp"}, At: at}, "gmail", "is shared, but no notification"},
	}
	for _, c := range cases {
		got := noNotesWhy(c.sh, c.app, 24, loc)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %q does not say %q", c.name, got, c.want)
		}
	}
}

func TestSharingIsKept(t *testing.T) {
	d := life.DeviceFor(t.TempDir())
	if sh, _ := d.SharingNow(); sh != nil {
		t.Fatal("nothing said yet")
	}
	if err := d.SetSharing(true, []string{"Gmail", " ", "WhatsApp"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	sh, _ := d.SharingNow()
	if sh == nil || !sh.Notifications || len(sh.Apps) != 2 {
		t.Fatalf("sharing not kept: %+v", sh)
	}
}
