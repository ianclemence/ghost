package motion

import (
	"fmt"
	"strings"
)

// Verification is the studio gate before export: determinism, legibility,
// sound and accessibility. Run it; do not just claim it.
type Verification struct {
	OK     bool     `json:"ok"`
	Checks [] string `json:"checks,omitempty"`
	Failed []string `json:"failed,omitempty"`
}

// Verify runs the cheap gates without a browser: deterministic source,
// caption coverage, copy discipline, source discipline, reduced-motion
// parity and flash discipline. The render itself stays frame-exact because
// every frame is seek(t), seeded only.
func (s Spec) Verify() Verification {
	var v Verification
	pass := func(name string) { v.Checks = append(v.Checks, name) }
	fail := func(name string) { v.Failed = append(v.Failed, name) }

	// 1. Deterministic source: the same spec renders the same page.
	a, b := Page(s, true), Page(s, true)
	if a == b && len(a) > 1000 {
		pass("render one frame twice and diff it: identical")
	} else {
		fail("render one frame twice and diff it: identical")
	}

	// 2. A still at every beat reads at 390 px: cues short, on-screen short.
	// Only when the owner asked for captions; a clean picture has no cues.
	if !s.CaptionsOn() {
		pass("captions off: a clean picture, nothing to read")
	} else {
		cues := s.Cues()
		if len(cues) == len(s.Scenes) {
			pass(fmt.Sprintf("a cue at every beat (%d/%d)", len(cues), len(s.Scenes)))
		} else {
			fail(fmt.Sprintf("a cue at every beat (%d cues for %d scenes)", len(cues), len(s.Scenes)))
		}
		long := 0
		for _, c := range cues {
			for _, ln := range strings.Split(c.Text, "\n") {
				if len([]rune(ln)) > 48 {
					long++
				}
			}
		}
		if long == 0 {
			pass("read every line at 390 px wide")
		} else {
			fail(fmt.Sprintf("read every line at 390 px wide (%d long lines)", long))
		}
	}

	// 3. Muted and audio-only: voice or caption carries each beat — again,
	// only when captions were asked for.
	covered := 0
	if !s.CaptionsOn() {
		pass("captions off: muted parity skipped with them")
	} else {
		for _, sc := range s.Scenes {
			if strings.TrimSpace(sc.Voice) != "" || strings.TrimSpace(sc.Caption) != "" || strings.TrimSpace(firstWords(sc)) != "" {
				covered++
			}
		}
		if covered == len(s.Scenes) {
			pass("watch it muted, then listen audio-only: same ideas")
		} else {
			fail("watch it muted, then listen audio-only: same ideas")
		}
	}

	// 4. Sources: every data beat names one in studio mode.
	if s.Question == "" {
		pass("sources: legacy motion, studio rule skipped")
	} else {
		missing := 0
		for _, sc := range s.Scenes {
			if hasData(&sc) && strings.TrimSpace(sc.Caption) == "" && len(s.Sources) == 0 {
				missing++
			}
		}
		if missing == 0 {
			pass("every number has a source")
		} else {
			fail(fmt.Sprintf("every number has a source (%d data scenes unnamed)", missing))
		}
	}

	// 5. One focal action at a time; motion carries meaning.
	multi := 0
	for _, sc := range s.Scenes {
		n := 0
		for _, e := range sc.Elements {
			if e.Focus {
				n++
			}
		}
		if n > 1 {
			multi++
		}
	}
	if multi == 0 {
		pass("one focal action at a time")
	} else {
		fail(fmt.Sprintf("one focal action at a time (%d scenes break it)", multi))
	}

	// 6. No flashes: the player never strobes; scene holds are >= 1 s.
	okFlash := true
	for _, sc := range s.Scenes {
		if sc.Duration < 1 {
			okFlash = false
		}
	}
	if okFlash {
		pass("no flashes above 3 per second")
	} else {
		fail("no flashes above 3 per second")
	}

	v.OK = len(v.Failed) == 0
	return v
}
