package uploads

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
)

// MaxThumbSource bounds what is decoded for a thumbnail: a decoded photo is
// many times its file size, and a gallery page must not be able to make the
// Pod allocate without limit.
const MaxThumbSource = 16 * 1024 * 1024

// ErrNoThumbnail means the file is not an image this build can decode.
var ErrNoThumbnail = errors.New("no thumbnail for this file")

// Thumbnail returns a JPEG no larger than max pixels on its longest side.
// Only photos the standard library can read (JPEG, PNG, GIF) have one; any
// other file returns ErrNoThumbnail and the caller shows a type tile instead.
func Thumbnail(workspace string, it Item, max int) ([]byte, error) {
	if it.Kind != "image" || it.Size > MaxThumbSource || max < 16 {
		return nil, ErrNoThumbnail
	}
	f, err := os.Open(filepath.Join(workspace, it.Path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, ErrNoThumbnail
	}
	small := boxScale(src, max)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, small, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// boxScale shrinks img so its longest side is at most max, averaging each
// block of source pixels (a box filter), which keeps small text and edges
// legible where nearest-neighbour would shimmer. Transparent pixels are laid
// on white so a PNG logo does not turn black.
func boxScale(img image.Image, max int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return flatten(img)
	}
	nw, nh := max, max
	if w >= h {
		nh = h * max / w
	} else {
		nw = w * max / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	flat := flatten(img)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := y*h/nh, (y+1)*h/nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < nw; x++ {
			x0, x1 := x*w/nw, (x+1)*w/nw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := flat.RGBAAt(sx, sy)
					r += uint32(c.R)
					g += uint32(c.G)
					bl += uint32(c.B)
					n++
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
		}
	}
	return dst
}

func flatten(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Over)
	return out
}
