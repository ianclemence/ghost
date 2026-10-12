package motion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const studio = `{"title":"Why the till lies","question":"Why did sales rise while profit fell?","audience":"Bakery owner","outcome":"They can say costs ate the rise","hook":"A busy till means a rich month","sources":["Till exports, Sep"],"scenes":[
 {"id":"q","duration":3,"teaches":"Busy is not profitable","voice":"The till was busier than ever, so why did profit fall?","sound":"motif returns","check":"They doubt the busy till","elements":[{"type":"title","id":"hook","text":"The busiest month","sub":"Sales up, profit down","focus":true}]},
 {"id":"model-1","duration":4,"teaches":"Profit is sales minus costs","voice":"Sales rose, but flour and power rose faster.","caption":"Till exports, Sep","elements":[{"type":"compare","id":"hook","labels":["Sales","Profit"],"values":[141,88],"prefix":"KES ","label":"September"}]},
 {"id":"payoff","duration":3,"teaches":"Watch costs, not just the queue","voice":"Watch the costs and the queue tells the truth.","check":"They name the cost rise","elements":[{"type":"text","text":"Watch the costs"}]}
]}`

func TestStudioArcIsChecked(t *testing.T) {
	s, err := Parse([]byte(studio))
	if err != nil {
		t.Fatal(err)
	}
	if s.Question == "" || len(s.Scenes) != 3 {
		t.Fatalf("studio fields lost: %+v", s)
	}
	for _, b := range []string{
		// Arc needs room.
		`{"title":"x","question":"q?","scenes":[{"duration":3,"teaches":"a","elements":[{"type":"text","text":"a"}]}]}`,
		// Every arc scene teaches.
		`{"title":"x","question":"q?","sources":["s"],"scenes":[{"duration":3,"teaches":"a","elements":[{"type":"text","text":"a"}]},{"duration":3,"elements":[{"type":"text","text":"b"}]},{"duration":3,"teaches":"c","elements":[{"type":"text","text":"c"}]}]}`,
		// One focal action.
		`{"title":"x","scenes":[{"duration":3,"elements":[{"type":"text","text":"a","focus":true},{"type":"text","text":"b","focus":true}]}]}`,
		// Data without a source in studio mode.
		`{"title":"x","question":"q?","scenes":[{"duration":3,"teaches":"a","elements":[{"type":"text","text":"a"}]},{"duration":3,"teaches":"b","elements":[{"type":"bars","labels":["a","b"],"values":[1,2]}]},{"duration":3,"teaches":"c","elements":[{"type":"text","text":"c"}]}]}`,
		// On-screen paragraph belongs in voice.
		`{"title":"x","scenes":[{"duration":3,"elements":[{"type":"text","text":"one two three four five six seven eight nine ten eleven twelve thirteen"}]}]}`,
		// Bad carry id.
		`{"title":"x","scenes":[{"duration":3,"elements":[{"type":"text","id":"Hook!","text":"a"}]}]}`,
	} {
		if _, err := Parse([]byte(b)); err == nil {
			t.Fatalf("accepted: %s", b)
		}
	}
	// Legacy motions without a question keep their old freedom.
	if _, err := Parse([]byte(sample)); err != nil {
		t.Fatalf("legacy sample broke: %v", err)
	}
}

func TestCaptionsAndVerify(t *testing.T) {
	s, err := Parse([]byte(studio))
	if err != nil {
		t.Fatal(err)
	}
	cues := s.Cues()
	if len(cues) != 3 {
		t.Fatalf("cues %d", len(cues))
	}
	srt := FormatSRT(cues)
	if !strings.Contains(srt, "-->") || !strings.Contains(srt, "profit fall") {
		t.Fatalf("srt:\n%s", srt)
	}
	v := s.Verify()
	if !v.OK {
		t.Fatalf("verify failed: %v", v.Failed)
	}
	// Deterministic source: the same spec renders the same page.
	if Page(s, true) != Page(s, true) {
		t.Fatal("render page is not deterministic")
	}
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	if err := os.MkdirAll(filepath.Join(ws, "motion"), 0o755); err != nil {
		t.Fatal(err)
	}
	rel, _, err := Save(ws, s)
	if err != nil {
		t.Fatal(err)
	}
	crel, err := WriteCaptions(ws, rel)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(crel)))
	if err != nil || !strings.Contains(string(b), "profit fall") {
		t.Fatalf("captions file: %v %q", err, string(b))
	}
}
