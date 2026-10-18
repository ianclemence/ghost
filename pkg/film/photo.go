package film

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PhotoOptions tune how an image is prepared.
type PhotoOptions struct {
	Tol    int  // background tolerance for the flood cut (colour distance, default 34)
	Keep   bool // the image already has its own transparency: only scale, crop and register
	Max    int  // longest side in pixels (default 1100)
	Credit string
	Source string
}

var photoName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

// AddPhoto prepares an image for the doodle look: it scales it, cuts a plain
// background away from the edges (unless the image already has transparency),
// crops to the object, and registers it in the project's photos.js as a data
// URL, because a film's page may not load files or reach the network and a
// canvas that ever drew an outside image can no longer be read. It writes a
// check sheet, the cutout on magenta with a 10x10 grid, and returns its path.
//
// The cut is a flood from the borders by colour, so it only works on a flat,
// evenly lit backdrop. There is no matting model on the Pod.
func AddPhoto(ctx context.Context, workspace, project, src, name string, o PhotoOptions) (string, int, int, error) {
	if !photoName.MatchString(name) {
		return "", 0, 0, errors.New("name the photo with lowercase letters, digits and dashes, starting with a letter (for example teapot)")
	}
	if strings.Contains(src, "..") || filepath.IsAbs(src) {
		return "", 0, 0, errors.New("the image must be a path inside the workspace")
	}
	srcAbs := filepath.Join(workspace, filepath.FromSlash(src))
	if fi, err := os.Stat(srcAbs); err != nil || fi.IsDir() {
		return "", 0, 0, fmt.Errorf("no image at %s", src)
	}
	file, err := Resolve(workspace, project)
	if err != nil {
		return "", 0, 0, err
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", 0, 0, errors.New("ffmpeg is not installed on this Pod")
	}
	if o.Max <= 0 || o.Max > 2000 {
		o.Max = 1100
	}
	if o.Tol <= 0 {
		o.Tol = 34
	}
	tmp, err := os.MkdirTemp("", "ghost-photo-")
	if err != nil {
		return "", 0, 0, err
	}
	defer os.RemoveAll(tmp)
	in := filepath.Join(tmp, "in.png")
	scale := fmt.Sprintf("scale='if(gt(iw,ih),min(%d,iw),-2)':'if(gt(iw,ih),-2,min(%d,ih))'", o.Max, o.Max)
	if out, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-y", "-i", srcAbs, "-frames:v", "1", "-vf", scale, "-pix_fmt", "rgba", in).CombinedOutput(); err != nil {
		return "", 0, 0, fmt.Errorf("couldn't read that image: %s", strings.TrimSpace(string(out)))
	}
	f, err := os.Open(in)
	if err != nil {
		return "", 0, 0, err
	}
	decoded, err := png.Decode(f)
	f.Close()
	if err != nil {
		return "", 0, 0, err
	}
	img := image.NewNRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	if !o.Keep && !hasAlpha(img) {
		floodBackground(img, o.Tol)
	}
	img = cropToObject(img)
	if img == nil {
		return "", 0, 0, errors.New("nothing was left after cutting the background: raise tol, or use keep for an image that already has transparency")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", 0, 0, err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()

	dir := filepath.Dir(filepath.Join(workspace, filepath.FromSlash(file)))
	entry := fmt.Sprintf("registerPhoto(%q,{src:%q,w:%d,h:%d,credit:%q,source:%q});\n",
		name, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(buf.Bytes()), w, h, o.Credit, o.Source)
	js := filepath.Join(dir, "photos.js")
	var kept []string
	if old, err := os.ReadFile(js); err == nil {
		for _, line := range strings.SplitAfter(string(old), "\n") {
			if line != "" && !strings.HasPrefix(line, "registerPhoto("+strconv.Quote(name)+",") {
				kept = append(kept, line)
			}
		}
	}
	if err := os.WriteFile(js, []byte(strings.Join(kept, "")+entry), 0o644); err != nil {
		return "", 0, 0, err
	}

	// The check sheet: the object on magenta (it must be whole, without a halo,
	// handles empty inside) with a 10x10 grid whose cells are the u,v anchor units.
	cut := filepath.Join(tmp, "cut.png")
	_ = os.WriteFile(cut, buf.Bytes(), 0o644)
	sheetRel := OutDir(file) + "/photo-" + name + ".jpg"
	sheet := filepath.Join(workspace, filepath.FromSlash(sheetRel))
	_ = os.MkdirAll(filepath.Dir(sheet), 0o755)
	if out, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=magenta:s=%dx%d", w, h), "-i", cut,
		"-filter_complex", "[0][1]overlay,drawgrid=w=iw/10:h=ih/10:t=1:c=white@0.55", "-frames:v", "1", sheet).CombinedOutput(); err != nil {
		return "", w, h, fmt.Errorf("registered, but the check sheet failed: %s", strings.TrimSpace(string(out)))
	}
	return sheetRel, w, h, nil
}

func hasAlpha(img *image.NRGBA) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += max(1, b.Dy()/60) {
		for x := b.Min.X; x < b.Max.X; x += max(1, b.Dx()/60) {
			if img.NRGBAAt(x, y).A < 250 {
				return true
			}
		}
	}
	return false
}

func dist(a, b color.NRGBA) float64 {
	dr, dg, db := float64(a.R)-float64(b.R), float64(a.G)-float64(b.G), float64(a.B)-float64(b.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// floodBackground clears everything connected to the border whose colour is
// within tol of the backdrop (the mean of the four corners).
func floodBackground(img *image.NRGBA, tol int) {
	b := img.Bounds()
	corners := []color.NRGBA{img.NRGBAAt(b.Min.X, b.Min.Y), img.NRGBAAt(b.Max.X-1, b.Min.Y), img.NRGBAAt(b.Min.X, b.Max.Y-1), img.NRGBAAt(b.Max.X-1, b.Max.Y-1)}
	var r, g, bl int
	for _, c := range corners {
		r += int(c.R)
		g += int(c.G)
		bl += int(c.B)
	}
	bg := color.NRGBA{uint8(r / 4), uint8(g / 4), uint8(bl / 4), 255}
	w, h := b.Dx(), b.Dy()
	seen := make([]bool, w*h)
	var stack [][2]int
	push := func(x, y int) {
		if x < 0 || y < 0 || x >= w || y >= h || seen[y*w+x] {
			return
		}
		if dist(img.NRGBAAt(b.Min.X+x, b.Min.Y+y), bg) > float64(tol) {
			return
		}
		seen[y*w+x] = true
		stack = append(stack, [2]int{x, y})
	}
	for x := 0; x < w; x++ {
		push(x, 0)
		push(x, h-1)
	}
	for y := 0; y < h; y++ {
		push(0, y)
		push(w-1, y)
	}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		img.SetNRGBA(b.Min.X+p[0], b.Min.Y+p[1], color.NRGBA{})
		push(p[0]+1, p[1])
		push(p[0]-1, p[1])
		push(p[0], p[1]+1)
		push(p[0], p[1]-1)
	}
	// A one-pixel fringe of backdrop colour clings to the cut edge: soften it.
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			c := img.NRGBAAt(b.Min.X+x, b.Min.Y+y)
			if c.A == 0 {
				continue
			}
			if img.NRGBAAt(b.Min.X+x-1, b.Min.Y+y).A == 0 || img.NRGBAAt(b.Min.X+x+1, b.Min.Y+y).A == 0 ||
				img.NRGBAAt(b.Min.X+x, b.Min.Y+y-1).A == 0 || img.NRGBAAt(b.Min.X+x, b.Min.Y+y+1).A == 0 {
				if dist(c, bg) < float64(tol)*1.8 {
					c.A = 90
					img.SetNRGBA(b.Min.X+x, b.Min.Y+y, c)
				}
			}
		}
	}
}

// cropToObject trims fully transparent margins; nil when nothing is left.
func cropToObject(img *image.NRGBA) *image.NRGBA {
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.NRGBAAt(x, y).A > 8 {
				minX, maxX = min(minX, x), max(maxX, x)
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
	}
	if maxX < minX {
		return nil
	}
	r := image.Rect(minX, minY, maxX+1, maxY+1)
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out
}
