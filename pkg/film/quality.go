package film

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Quality is a deterministic look at one drawn frame. It exists because a
// model can look at a contact sheet and still ship a film that is nearly
// invisible on a phone: judging "too faint" and "too empty" by eye is not
// reliable, so the runtime measures it instead. The numbers are about the
// picture as it will be seen, not about the code that drew it.
type Quality struct {
	Frame       int     `json:"frame"`
	InkPct      float64 `json:"ink_pct"`      // % of sampled pixels with any tonal difference from paper
	CoveragePct float64 `json:"coverage_pct"` // % of pixels carrying a clearly visible mark
	DrawnCells  int     `json:"drawn_cells"`  // of gridCells, areas with any real mark
	Contrast    float64 `json:"contrast"`     // p98-p2 luminance: the picture's tonal range
	Paper       float64 `json:"paper"`        // bright paper estimate (0 dark, 1 white)
}

// gridCells is the coarse spatial grid a frame is divided into: enough to
// notice a subject lost in empty paper, coarse enough not to punish negative
// space.
const gridCells = 36 // 6x6

const (
	// Warn: worth fixing before a render is called done.
	warnCoveragePct = 3.0
	warnContrast    = 0.15
	warnInkPct      = 1.0
	warnDrawnCells  = 18

	// Fatal: the deliverable would look blank. A render refuses rather than
	// hand the owner an empty video. A cell counts as drawn only when at
	// least 1% of it carries a clearly visible mark (0.20 below the paper),
	// so a whole frame of hairlines is treated as empty. Coverage measures
	// the same visible marks over the whole frame: the shipped paper-boat
	// film covered 1.2-1.4% and read as blank; a normal drawing covers 20%.
	fatalCoveragePct = 2.0
	fatalContrast    = 0.03
	fatalInkPct      = 0.5
	fatalDrawnCells  = 14
)

// Notes are the frame's own warnings, phrased for the model to act on.
func (q Quality) Notes() []string {
	var out []string
	if q.CoveragePct < warnCoveragePct {
		out = append(out, fmt.Sprintf("frame %d's marks are too thin or too sparse to read on a phone (%.1f%% of the frame is visibly drawn; aim for %.0f%% or more); use heavier strokes and let the subject fill more of the picture", q.Frame, q.CoveragePct, warnCoveragePct))
	}
	if q.Contrast < warnContrast {
		out = append(out, fmt.Sprintf("frame %d is very faint: its tonal range is only %.2f (aim for %.2f or more); darken the strokes or add tone", q.Frame, q.Contrast, warnContrast))
	}
	if q.InkPct < warnInkPct {
		out = append(out, fmt.Sprintf("frame %d has almost no marks (%.1f%% of the picture); draw more of the subject, or make the lines heavier", q.Frame, q.InkPct))
	}
	if q.DrawnCells < warnDrawnCells {
		out = append(out, fmt.Sprintf("frame %d is mostly empty paper (%d of %d areas carry a mark); the subject should fill a quarter or more of the frame, not sit in a corner", q.Frame, q.DrawnCells, gridCells))
	}
	return out
}

// Fatal reports whether a frame is too faint or too empty to release.
func (q Quality) Fatal() bool {
	return q.CoveragePct < fatalCoveragePct || q.Contrast < fatalContrast ||
		q.InkPct < fatalInkPct || q.DrawnCells < fatalDrawnCells
}

// AnalyzePNG measures one drawn frame. A PNG that cannot be decoded is not
// judged (nil, err): the caller keeps its own error.
func AnalyzePNG(b []byte, frame int) (Quality, error) {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return Quality{}, err
	}
	bd := img.Bounds()
	if bd.Dx() <= 0 || bd.Dy() <= 0 {
		return Quality{}, fmt.Errorf("frame %d has no pixels", frame)
	}
	// Sample every other pixel: at 1080x1920 that is about half a million
	// points, plenty for a ratio and fast enough to run on every check.
	step := 2
	var lums []float64
	ink, strong := 0, 0
	cells := make([]int, gridCells)
	totals := make([]int, gridCells)
	// First pass: luminance of every sampled pixel, for percentiles.
	for y := bd.Min.Y; y < bd.Max.Y; y += step {
		for x := bd.Min.X; x < bd.Max.X; x += step {
			lums = append(lums, luminance(img.At(x, y).RGBA()))
		}
	}
	if len(lums) == 0 {
		return Quality{}, fmt.Errorf("frame %d has no pixels", frame)
	}
	sorted := append([]float64(nil), lums...)
	sort.Float64s(sorted)
	pct := func(p float64) float64 {
		i := int(math.Round(p * float64(len(sorted)-1)))
		if i < 0 {
			i = 0
		}
		if i >= len(sorted) {
			i = len(sorted) - 1
		}
		return sorted[i]
	}
	// Paper is the bright end of the picture, not the median: these are
	// drawings on light paper, and a bold subject can cover most of the frame.
	// Taking the median there would call the ink "paper" and measure nothing.
	paper := pct(0.90)
	// Second pass: what counts as a mark, and where on the frame it is.
	i := 0
	for y := bd.Min.Y; y < bd.Max.Y; y += step {
		for x := bd.Min.X; x < bd.Max.X; x += step {
			l := lums[i]
			i++
			if paper-l > 0.06 {
				ink++
			}
			if paper-l > 0.20 {
				strong++
			}
			cx := int(float64(x-bd.Min.X) * 6 / float64(bd.Dx()))
			cy := int(float64(y-bd.Min.Y) * 6 / float64(bd.Dy()))
			if cx > 5 {
				cx = 5
			}
			if cy > 5 {
				cy = 5
			}
			cell := cy*6 + cx
			totals[cell]++
			// A cell "carries a mark" only when the mark is clearly
			// visible, not when grain, a background wash or a single
			// hairline happens to tint it. This is what makes a frame
			// that is 60% empty paper measure as empty.
			if paper-l > 0.20 {
				cells[cell]++
			}
		}
	}
	drawn := 0
	for n := range cells {
		if totals[n] > 0 && float64(cells[n])/float64(totals[n]) > 0.01 {
			drawn++
		}
	}
	return Quality{
		Frame:       frame,
		InkPct:      100 * float64(ink) / float64(len(lums)),
		CoveragePct: 100 * float64(strong) / float64(len(lums)),
		DrawnCells:  drawn,
		Contrast:    pct(0.98) - pct(0.02),
		Paper:       paper,
	}, nil
}

// AnalyzeFile measures a frame written to disk.
func AnalyzeFile(path string) (Quality, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Quality{}, err
	}
	return AnalyzePNG(b, 0)
}

// luminance is the perceptual brightness of a 16-bit RGBA sample, 0 (black)
// to 1 (white). Alpha is folded in so a transparent page reads as its paper.
func luminance(r, g, b, a uint32) float64 {
	if a == 0 {
		return 1
	}
	af := float64(a) / 65535.0
	rf, gf, bf := float64(r)/65535.0, float64(g)/65535.0, float64(b)/65535.0
	// Un-premultiply, then weigh the channels the way the eye does.
	if af > 0 {
		rf, gf, bf = rf/af, gf/af, bf/af
	}
	return math.Min(1, 0.2126*rf+0.7152*gf+0.0722*bf)
}

// PruneFrames deletes full-size frame dumps older than olderThan from every
// film's out/ folder. Renders and contact sheets are kept: those are what the
// owner was shown. It returns how many folders it removed.
func PruneFrames(workspace string, olderThan time.Duration) int {
	cutoff := time.Now().Add(-olderThan)
	matches, _ := filepath.Glob(filepath.Join(workspace, Dir, "*", "out", "*-frames"))
	n := 0
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil || !fi.IsDir() || fi.ModTime().After(cutoff) {
			continue
		}
		if err := os.RemoveAll(m); err == nil {
			n++
		}
	}
	return n
}
