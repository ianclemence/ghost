package tools

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/film"
)

func TestFilmIsOfferedForAnAnimationRequest(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(NewFilmTool(t.TempDir(), film.NewJobs()))
	for _, msg := range []string{
		"make me a short hand-drawn animation of a little paper boat drifting across a pond, pencil look",
		"draw me a fox in a snowy forest",
		"animate a bouncing ball",
	} {
		got := FilterToolsForTurn(reg, ProfileFull, msg, false)
		if _, ok := got.Get("film"); !ok {
			t.Errorf("film not offered for %q", msg)
		}
	}
	if got := FilterToolsForTurn(reg, ProfileFull, "what's the weather", false); got != nil {
		if _, ok := got.Get("film"); ok {
			t.Error("film should not be offered for unrelated chat")
		}
	}
}
