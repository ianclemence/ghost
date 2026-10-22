package film

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// framePNG builds a 600x600 frame: a paper colour, then whatever is drawn on
// it. It is deliberately crude — the measurement is about light, not art.
func framePNG(t *testing.T, paper color.RGBA, paint func(*image.RGBA)) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 600, 600))
	draw.Draw(img, img.Bounds(), image.NewUniform(paper), image.Point{}, draw.Src)
	if paint != nil {
		paint(img)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestAFaintFrameIsMeasuredAsFatalAndABoldOneIsNot(t *testing.T) {
	white := color.RGBA{245, 240, 230, 255}
	faint := framePNG(t, white, func(img *image.RGBA) {
		// One thin near-paper line: the failure we saw shipped.
		for x := 100; x < 500; x++ {
			img.Set(x, 300, color.RGBA{230, 226, 218, 255})
		}
	})
	bold := framePNG(t, white, func(img *image.RGBA) {
		draw.Draw(img, image.Rect(80, 80, 520, 520), image.NewUniform(color.RGBA{20, 20, 20, 255}), image.Point{}, draw.Src)
	})

	fq, err := AnalyzePNG(faint, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !fq.Fatal() {
		t.Fatalf("a near-paper line must be fatal, got %+v", fq)
	}
	if notes := fq.Notes(); len(notes) == 0 {
		t.Fatal("a faint frame must explain what to fix")
	}

	bq, err := AnalyzePNG(bold, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bq.Fatal() {
		t.Fatalf("a bold drawing must be releasable, got %+v", bq)
	}
	if bq.InkPct < 20 || bq.DrawnCells < 20 {
		t.Fatalf("a bold drawing must measure as drawn, got %+v", bq)
	}
	if len(bq.Notes()) != 0 {
		t.Fatalf("a bold drawing needs no notes: %v", bq.Notes())
	}
}

func TestAnEmptyFrameIsFatal(t *testing.T) {
	b := framePNG(t, color.RGBA{245, 240, 230, 255}, nil)
	q, err := AnalyzePNG(b, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Fatal() || q.InkPct != 0 {
		t.Fatalf("an empty frame must be fatal, got %+v", q)
	}
	if !strings.Contains(strings.Join(q.Notes(), " "), "almost no marks") {
		t.Fatalf("an empty frame must say so: %v", q.Notes())
	}
}

func TestCheckWorstIsTheLeastLegibleSample(t *testing.T) {
	r := Report{Quality: []Quality{
		{Frame: 0, Contrast: 0.5, DrawnCells: 30},
		{Frame: 50, Contrast: 0.09, DrawnCells: 12},
		{Frame: 99, Contrast: 0.4, DrawnCells: 6},
	}}
	if got := r.Worst().Frame; got != 99 {
		t.Fatalf("worst should be the least drawn frame, got %d", got)
	}
	if got := (Report{}).Worst(); got != (Quality{}) {
		t.Fatalf("an unmeasured report has no worst frame, got %+v", got)
	}
}

func TestPruneFramesKeepsRendersAndRecentPreviews(t *testing.T) {
	ws := t.TempDir()
	oldDir := filepath.Join(ws, Dir, "old", "out", "old-frames")
	newDir := filepath.Join(ws, Dir, "new", "out", "new-frames")
	oldMP4 := filepath.Join(ws, Dir, "old", "out", "old.mp4")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "0000.png"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(oldMP4, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	_ = os.Chtimes(oldDir, old, old)

	if n := PruneFrames(ws, 7*24*time.Hour); n != 1 {
		t.Fatalf("expected one folder pruned, got %d", n)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Error("an old preview folder should be gone")
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Error("a recent preview folder must stay")
	}
	if _, err := os.Stat(oldMP4); err != nil {
		t.Error("a render must never be pruned")
	}
}
