// Package siteicon fetches a website's icon for the Vault, on the Pod, and
// keeps it.
//
// The phone asks the Pod for it instead of asking a third-party icon service,
// because a request to such a service says which sites the owner has logins
// for. Here the Pod asks the site itself, once, and answers every later request
// from disk. The fetch is an outbound request to a name the owner typed, so it
// is held to the same rules as any other: public addresses only, checked when
// the connection is made, a small body, a short time, and a few redirects.
package siteicon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"
)

const (
	// Size is the edge of the square icon the phone is given.
	Size = 96

	maxPage   = 512 << 10
	maxIcon   = 256 << 10
	maxPixels = 4096
	fetchTime = 8 * time.Second

	positiveTTL = 14 * 24 * time.Hour
	negativeTTL = 24 * time.Hour
)

// Fetcher gets and caches icons.
type Fetcher struct {
	dir    string
	client *http.Client

	mu       sync.Mutex
	inflight map[string]*call
}

type call struct {
	done chan struct{}
	png  []byte
}

// New returns a Fetcher that keeps icons under dir.
func New(dir string) *Fetcher {
	return &Fetcher{dir: dir, client: safeClient(), inflight: map[string]*call{}}
}

var hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// ValidHost reports whether name is a public-looking site name: dotted labels,
// no address, no port, no path.
func ValidHost(name string) bool {
	if len(name) > 253 || net.ParseIP(name) != nil {
		return false
	}
	return hostRe.MatchString(strings.ToLower(name))
}

// Icon returns the site's icon as a PNG, from disk when it has it. ok is false
// when the site has none, or none could be read; the phone then draws a letter.
func (f *Fetcher) Icon(ctx context.Context, rawHost string) (data []byte, ok bool) {
	host := credentials.NormalizeWebHost(rawHost)
	if !ValidHost(host) {
		return nil, false
	}
	if b, fresh := f.cached(host); fresh {
		return b, len(b) > 0
	}
	// One fetch per site at a time: a screen of rows asks for each once, and a
	// slow site must not be asked twice.
	f.mu.Lock()
	if c, running := f.inflight[host]; running {
		f.mu.Unlock()
		select {
		case <-c.done:
			return c.png, len(c.png) > 0
		case <-ctx.Done():
			return nil, false
		}
	}
	c := &call{done: make(chan struct{})}
	f.inflight[host] = c
	f.mu.Unlock()

	fctx, cancel := context.WithTimeout(context.Background(), fetchTime)
	defer cancel()
	c.png = f.fetchBase(fctx, "https://"+host+"/")
	f.store(host, c.png)
	close(c.done)
	f.mu.Lock()
	delete(f.inflight, host)
	f.mu.Unlock()
	return c.png, len(c.png) > 0
}

func (f *Fetcher) path(host, ext string) string {
	sum := sha256.Sum256([]byte(host))
	return filepath.Join(f.dir, hex.EncodeToString(sum[:10])+ext)
}

func (f *Fetcher) cached(host string) ([]byte, bool) {
	if info, err := os.Stat(f.path(host, ".png")); err == nil && time.Since(info.ModTime()) < positiveTTL {
		if b, err := os.ReadFile(f.path(host, ".png")); err == nil {
			return b, true
		}
	}
	if info, err := os.Stat(f.path(host, ".none")); err == nil && time.Since(info.ModTime()) < negativeTTL {
		return nil, true
	}
	return nil, false
}

func (f *Fetcher) store(host string, b []byte) {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return
	}
	if len(b) == 0 {
		_ = os.WriteFile(f.path(host, ".none"), nil, 0o600)
		return
	}
	_ = os.Remove(f.path(host, ".none"))
	_ = os.WriteFile(f.path(host, ".png"), b, 0o600)
}

// Forget drops what is kept for a site.
func (f *Fetcher) Forget(rawHost string) {
	host := credentials.NormalizeWebHost(rawHost)
	if host == "" {
		return
	}
	_ = os.Remove(f.path(host, ".png"))
	_ = os.Remove(f.path(host, ".none"))
}

// fetchBase looks for a site's icon the way a browser does: the page's own
// <link rel=icon> tags, then /favicon.ico. base is the site's front page.
func (f *Fetcher) fetchBase(ctx context.Context, base string) []byte {
	var candidates []string
	if page, final := f.get(ctx, base, maxPage); page != nil {
		if bu, err := url.Parse(final); err == nil {
			candidates = append(candidates, iconLinks(string(page), bu)...)
		}
	}
	candidates = append(candidates, base+"favicon.ico")
	seen := map[string]bool{}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		body, _ := f.get(ctx, c, maxIcon)
		if body == nil {
			continue
		}
		if img := decodeIcon(body); img != nil {
			if out := encodeSquare(img); len(out) > 0 {
				return out
			}
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	return nil
}

// get fetches one address and returns at most max bytes of a 200 response, and
// the address it ended at. Nothing else about the response is trusted.
func (f *Fetcher) get(ctx context.Context, rawURL string, max int64) ([]byte, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GhostPod/1.0)")
	req.Header.Set("Accept", "image/*,text/html;q=0.8,*/*;q=0.5")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, ""
	}
	return b, resp.Request.URL.String()
}

// --------------------------------------------------------------- networking

// safeClient connects only to public addresses. The check is made on the
// address actually dialled, so a name that resolves somewhere public for the
// check and somewhere internal for the connection gets nowhere.
func safeClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, errors.New("no address")
			}
			for _, ip := range ips {
				if !PublicIP(ip.IP) {
					return nil, fmt.Errorf("refusing a non-public address")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		MaxIdleConns:          0,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   fetchTime,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return errors.New("redirect off https")
			}
			return nil
		},
	}
}

var cgnat = mustCIDR("100.64.0.0/10")
var benchmark = mustCIDR("198.18.0.0/15")

func mustCIDR(s string) *net.IPNet {
	_, n, _ := net.ParseCIDR(s)
	return n
}

// PublicIP reports whether ip is an address on the public internet: not this
// machine, not a private or link-local network, not carrier-grade NAT, not
// multicast, not the cloud metadata address.
func PublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	switch {
	case ip.IsLoopback(), ip.IsPrivate(), ip.IsUnspecified(), ip.IsMulticast(),
		ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsInterfaceLocalMulticast():
		return false
	case cgnat.Contains(ip), benchmark.Contains(ip):
		return false
	}
	return true
}

// -------------------------------------------------------------- finding icons

var (
	linkTagRe = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attrRe    = regexp.MustCompile(`(?is)([a-z\-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
)

type iconLink struct {
	href  string
	score int
}

// iconLinks lists the icon addresses a page names, best first. An Apple touch
// icon is a clean square at a good size, so it leads; then the largest declared
// size; plain icons keep their page order.
func iconLinks(page string, base *url.URL) []string {
	var found []iconLink
	for i, tag := range linkTagRe.FindAllString(page, 40) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			v := m[3]
			if v == "" {
				v = m[4]
			}
			if v == "" {
				v = m[5]
			}
			attrs[strings.ToLower(m[1])] = v
		}
		rel := strings.ToLower(attrs["rel"])
		if !strings.Contains(rel, "icon") || attrs["href"] == "" {
			continue
		}
		ref, err := url.Parse(strings.TrimSpace(attrs["href"]))
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref)
		if abs.Scheme != "https" || abs.Host == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(abs.Path), ".svg") || strings.Contains(strings.ToLower(attrs["type"]), "svg") {
			continue
		}
		score := 100 - i
		if strings.Contains(rel, "apple-touch-icon") {
			score += 1000
		}
		if w := declaredSize(attrs["sizes"]); w > 0 {
			if w > 256 {
				w = 128 // a huge icon is a poor use of the wire
			}
			score += w
		}
		found = append(found, iconLink{abs.String(), score})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.href)
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func declaredSize(sizes string) int {
	best := 0
	for _, s := range strings.Fields(strings.ToLower(sizes)) {
		if w, _, ok := strings.Cut(s, "x"); ok {
			if n, err := strconv.Atoi(w); err == nil && n > best {
				best = n
			}
		}
	}
	return best
}

// ----------------------------------------------------------------- the image

// decodeIcon reads a PNG, JPEG, GIF or ICO. Anything else (SVG, WebP) is not
// handled and the caller tries the next candidate.
func decodeIcon(b []byte) image.Image {
	if len(b) > 6 && b[0] == 0 && b[1] == 0 && b[2] == 1 && b[3] == 0 {
		return decodeICO(b)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxPixels || cfg.Height > maxPixels {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	return img
}

func init() {
	// image.Decode finds a format by its registered header.
	image.RegisterFormat("png", "\x89PNG\r\n\x1a\n", png.Decode, png.DecodeConfig)
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("gif", "GIF8", gif.Decode, gif.DecodeConfig)
}

// decodeICO picks the largest image in an .ico: a PNG inside it, or an
// uncompressed 24 or 32 bit bitmap. Most icons are one of those.
func decodeICO(b []byte) image.Image {
	if len(b) < 6 {
		return nil
	}
	n := int(binary.LittleEndian.Uint16(b[4:6]))
	if n < 1 || n > 32 || len(b) < 6+16*n {
		return nil
	}
	type entry struct{ w, off, size int }
	var best *entry
	for i := 0; i < n; i++ {
		e := b[6+16*i : 6+16*(i+1)]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		size := int(binary.LittleEndian.Uint32(e[8:12]))
		off := int(binary.LittleEndian.Uint32(e[12:16]))
		if off < 0 || size <= 0 || off+size > len(b) {
			continue
		}
		if best == nil || w > best.w {
			best = &entry{w, off, size}
		}
	}
	if best == nil {
		return nil
	}
	data := b[best.off : best.off+best.size]
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		return img
	}
	return decodeDIB(data)
}

// decodeDIB reads the bitmap inside an .ico entry: a BITMAPINFOHEADER whose
// height counts the image and its mask, then bottom-up pixels.
func decodeDIB(d []byte) image.Image {
	if len(d) < 40 {
		return nil
	}
	hdr := int(binary.LittleEndian.Uint32(d[0:4]))
	w := int(int32(binary.LittleEndian.Uint32(d[4:8])))
	h := int(int32(binary.LittleEndian.Uint32(d[8:12]))) / 2
	bpp := int(binary.LittleEndian.Uint16(d[14:16]))
	comp := binary.LittleEndian.Uint32(d[16:20])
	if hdr < 40 || w < 1 || h < 1 || w > 512 || h > 512 || comp != 0 || (bpp != 32 && bpp != 24) {
		return nil
	}
	bytesPer := bpp / 8
	stride := ((w*bpp + 31) / 32) * 4
	pix := d[hdr:]
	if len(pix) < stride*h {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := pix[(h-1-y)*stride:]
		for x := 0; x < w; x++ {
			p := row[x*bytesPer:]
			a := uint8(255)
			if bytesPer == 4 {
				a = p[3]
			}
			img.SetNRGBA(x, y, color.NRGBA{R: p[2], G: p[1], B: p[0], A: a})
		}
	}
	return img
}

// encodeSquare scales an image to fit a Size square, centred and transparent
// around it, and writes a PNG.
func encodeSquare(src image.Image) []byte {
	b := src.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return nil
	}
	scale := float64(Size) / float64(max(b.Dx(), b.Dy()))
	if scale > 1 {
		scale = 1
	}
	w, h := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	// Box average: each output pixel is the mean of the source pixels it covers.
	for y := 0; y < h; y++ {
		sy0, sy1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+(y+1)*b.Dy()/h
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < w; x++ {
			sx0, sx1 := b.Min.X+x*b.Dx()/w, b.Min.X+(x+1)*b.Dx()/w
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var r, g, bl, a, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			if n == 0 || a == 0 {
				continue
			}
			// RGBA() is premultiplied; undo it for NRGBA.
			aa := a / n
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8((r / n) * 255 / aa), G: uint8((g / n) * 255 / aa), B: uint8((bl / n) * 255 / aa), A: uint8(aa >> 8),
			})
		}
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, Size, Size))
	draw.Draw(canvas, image.Rect((Size-w)/2, (Size-h)/2, (Size-w)/2+w, (Size-h)/2+h), dst, image.Point{}, draw.Over)
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil
	}
	return out.Bytes()
}
