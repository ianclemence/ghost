package tools

import (
	"strings"
	"testing"
)

func TestCanvasDesignNotesFlagEachKnownProblem(t *testing.T) {
	cases := []struct {
		name, html, want string
	}{
		{"transition all", `<style>a{transition: all 200ms}</style>`, "`all` properties"},
		{"ease-in", `<style>a{transition: transform 200ms ease-in}</style>`, "eases in"},
		{"scale zero", `<style>.x{transform:scale(0)}</style>`, "scale(0)"},
		{"layout animation", `<style>.x{transition: height 200ms ease}</style>`, "layout properties"},
		{"placeholder", `<p>Lorem ipsum dolor</p>`, "placeholder copy"},
		{"default gradient", `<style>body{background:linear-gradient(135deg,#667eea,#764ba2)}</style>`, "purple-blue"},
		{"100vh", `<style>body{min-height:100vh}</style>`, "100dvh"},
		{"bare hover", `<style>a:hover{color:red}</style>`, "(hover: hover)"},
		{"motion without reduced", `<style>@keyframes a{to{opacity:1}}</style>`, "prefers-reduced-motion"},
		{"slow", `<style>a{transition: opacity 600ms}</style>`, "400ms"},
		{"slow seconds", `<style>a{transition: opacity 1.2s}</style>`, "400ms"},
		{"img alt", `<img src="a.png">`, "alt text"},
	}
	for _, c := range cases {
		got := strings.Join(canvasDesignNotes(c.html), "|")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: wanted a note containing %q, got %q", c.name, c.want, got)
		}
	}
}

func TestCanvasDesignNotesLeaveGoodCraftAlone(t *testing.T) {
	good := `<style>
	.btn{transition: transform 160ms var(--ease-out), opacity 160ms var(--ease-out)}
	@media (hover: hover) and (pointer: fine){.btn:hover{opacity:.9}}
	@media (prefers-reduced-motion: reduce){.btn{transition:none}}
	.spin{animation: spin 2s linear infinite}
	@keyframes spin{to{transform:rotate(1turn)}}
	</style><img src="a.png" alt="A roaster">`
	if notes := canvasDesignNotes(good); len(notes) != 0 {
		t.Fatalf("well-made CSS drew notes: %v", notes)
	}
}

func TestCanvasDesignNotesAreCapped(t *testing.T) {
	bad := `<style>a{transition: all 900ms ease-in}.x{transform:scale(0)} a:hover{} @keyframes k{} body{min-height:100vh;background:#667eea}</style>Lorem ipsum<img src=x>`
	if n := len(canvasDesignNotes(bad)); n != designNoteLimit {
		t.Fatalf("expected %d notes, got %d", designNoteLimit, n)
	}
}
