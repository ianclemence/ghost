package siteicon

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func solid(w, h int, c color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestOnlyPublicAddressesAreEverDialled(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "10.0.0.5", "192.168.1.9", "172.16.0.1", "169.254.169.254",
		"100.64.1.1", "0.0.0.0", "224.0.0.1", "fe80::1", "fc00::1", "198.18.0.1"} {
		if PublicIP(net.ParseIP(ip)) {
			t.Errorf("%s is not a public address and must never be dialled", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"} {
		if !PublicIP(net.ParseIP(ip)) {
			t.Errorf("%s is public", ip)
		}
	}
}

func TestOnlySiteNamesAreAccepted(t *testing.T) {
	for _, h := range []string{"google.com", "ads.google.com", "a-b.example.co.uk"} {
		if !ValidHost(h) {
			t.Errorf("%q is a site", h)
		}
	}
	for _, h := range []string{"", "localhost", "127.0.0.1", "10.0.0.1", "[::1]", "a.b", "exa mple.com", "example.com/path", "example.com:8080", "-x.com", "x_y.com", "1.2.3.4"} {
		if ValidHost(h) {
			t.Errorf("%q must not be fetched", h)
		}
	}
}

func TestIconLinksAreChosenLikeABrowserWould(t *testing.T) {
	page := `<html><head>
	<link rel="stylesheet" href="/a.css">
	<link rel="icon" href="/small.png" sizes="16x16">
	<link rel="shortcut icon" href="/favicon.ico">
	<link rel="apple-touch-icon" sizes="180x180" href="/touch.png">
	<link rel="icon" type="image/svg+xml" href="/logo.svg">
	<link rel="icon" href="http://insecure.example/x.png">
	<link rel='icon' href='https://cdn.example.net/i.png' sizes='64x64'>
	</head></html>`
	base, _ := url.Parse("https://example.com/")
	got := iconLinks(page, base)
	if len(got) == 0 || got[0] != "https://example.com/touch.png" {
		t.Fatalf("the Apple touch icon should lead: %v", got)
	}
	for _, g := range got {
		if strings.HasSuffix(g, ".svg") || strings.HasPrefix(g, "http://") || strings.HasSuffix(g, ".css") {
			t.Errorf("unusable candidate offered: %s", g)
		}
	}
	if !strings.Contains(strings.Join(got, " "), "https://cdn.example.net/i.png") {
		t.Errorf("a CDN icon on https should still be a candidate: %v", got)
	}
}

func TestDecodesPNGAndIconsAndScalesToASquare(t *testing.T) {
	red := color.NRGBA{200, 30, 30, 255}
	big := solid(300, 150, red)
	out := encodeSquare(decodeIcon(big))
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil || img.Bounds().Dx() != Size || img.Bounds().Dy() != Size {
		t.Fatalf("not a %dx%d png: %v %v", Size, Size, err, img.Bounds())
	}
	if r, g, b, a := img.At(Size/2, Size/2).RGBA(); a == 0 || r>>8 < 150 || g>>8 > 80 || b>>8 > 80 {
		t.Fatalf("the picture was lost in scaling: %v %v %v %v", r, g, b, a)
	}
	if _, _, _, a := img.At(1, 1).RGBA(); a != 0 {
		t.Fatal("a wide icon should be centred with transparent space above it")
	}

	// An .ico that holds a PNG.
	inner := solid(32, 32, red)
	var ico bytes.Buffer
	ico.Write([]byte{0, 0, 1, 0, 1, 0})
	entry := make([]byte, 16)
	entry[0], entry[1] = 32, 32
	binary.LittleEndian.PutUint32(entry[8:], uint32(len(inner)))
	binary.LittleEndian.PutUint32(entry[12:], 22)
	ico.Write(entry)
	ico.Write(inner)
	if decodeIcon(ico.Bytes()) == nil {
		t.Fatal("an .ico holding a png should decode")
	}

	// An .ico that holds a 32-bit bitmap.
	var dib bytes.Buffer
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)
	binary.LittleEndian.PutUint32(hdr[4:], 2)
	binary.LittleEndian.PutUint32(hdr[8:], 4) // height doubled: 2 + mask
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], 32)
	dib.Write(hdr)
	for i := 0; i < 4; i++ {
		dib.Write([]byte{10, 20, 200, 255}) // BGRA
	}
	dib.Write(make([]byte, 8)) // mask
	var ico2 bytes.Buffer
	ico2.Write([]byte{0, 0, 1, 0, 1, 0})
	e2 := make([]byte, 16)
	e2[0], e2[1] = 2, 2
	binary.LittleEndian.PutUint32(e2[8:], uint32(dib.Len()))
	binary.LittleEndian.PutUint32(e2[12:], 22)
	ico2.Write(e2)
	ico2.Write(dib.Bytes())
	im := decodeIcon(ico2.Bytes())
	if im == nil {
		t.Fatal("an .ico holding a bitmap should decode")
	}
	if r, _, b, _ := im.At(0, 0).RGBA(); r>>8 != 200 || b>>8 != 10 {
		t.Fatalf("bitmap colours are wrong: r=%d b=%d", r>>8, b>>8)
	}

	for _, junk := range [][]byte{nil, []byte("<svg></svg>"), []byte("RIFFxxxxWEBP"), {0, 0, 1, 0, 0xff, 0xff}} {
		if decodeIcon(junk) != nil {
			t.Errorf("%q is not an icon we can read", junk)
		}
	}
}

func TestFindsAPageIconAndFallsBackToFaviconIco(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			io.WriteString(w, `<link rel="icon" href="/brand.png">`)
		case "/brand.png":
			w.Write(solid(64, 64, color.NRGBA{10, 120, 240, 255}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := &Fetcher{dir: t.TempDir(), client: srv.Client(), inflight: map[string]*call{}}
	if out := f.fetchBase(context.Background(), srv.URL+"/"); len(out) == 0 {
		t.Fatal("the page's icon should be found")
	}

	bare := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			w.Write(solid(16, 16, color.NRGBA{1, 2, 3, 255}))
			return
		}
		io.WriteString(w, "<html>no icon here</html>")
	}))
	defer bare.Close()
	f2 := &Fetcher{dir: t.TempDir(), client: bare.Client(), inflight: map[string]*call{}}
	if out := f2.fetchBase(context.Background(), bare.URL+"/"); len(out) == 0 {
		t.Fatal("/favicon.ico should be the fallback")
	}

	none := httptest.NewTLSServer(http.NotFoundHandler())
	defer none.Close()
	f3 := &Fetcher{dir: t.TempDir(), client: none.Client(), inflight: map[string]*call{}}
	if out := f3.fetchBase(context.Background(), none.URL+"/"); len(out) != 0 {
		t.Fatal("a site with no icon has none")
	}
}

func TestAnOversizedIconIsRefused(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("A"), maxIcon+10))
	}))
	defer srv.Close()
	f := &Fetcher{dir: t.TempDir(), client: srv.Client(), inflight: map[string]*call{}}
	if b, _ := f.get(context.Background(), srv.URL+"/x.png", maxIcon); b != nil {
		t.Fatal("a body over the limit must be refused, not truncated and used")
	}
}

type countingTransport struct{ n *int32 }

func (c countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	atomic.AddInt32(c.n, 1)
	return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

// A site is asked once. A site with no icon is not asked again for a day, and a
// request for an address that is not a site never leaves the Pod.
func TestACachedAnswerIsNotAskedForAgain(t *testing.T) {
	var n int32
	f := &Fetcher{dir: t.TempDir(), client: &http.Client{Transport: countingTransport{&n}}, inflight: map[string]*call{}}
	if _, ok := f.Icon(context.Background(), "no-icon.example.com"); ok {
		t.Fatal("expected no icon")
	}
	first := atomic.LoadInt32(&n)
	if first == 0 {
		t.Fatal("the site should have been asked")
	}
	f.Icon(context.Background(), "no-icon.example.com")
	if atomic.LoadInt32(&n) != first {
		t.Fatal("a site known to have no icon was asked again")
	}
	for _, h := range []string{"127.0.0.1", "localhost", "169.254.169.254", "10.0.0.1", "intranet"} {
		if _, ok := f.Icon(context.Background(), h); ok {
			t.Errorf("%s must not be fetched", h)
		}
	}
	if atomic.LoadInt32(&n) != first {
		t.Fatal("an address that is not a public site name was requested")
	}

	// A kept icon is served from disk.
	f.store("kept.example.com", solid(8, 8, color.NRGBA{1, 1, 1, 255}))
	if b, ok := f.Icon(context.Background(), "https://www.kept.example.com/login"); !ok || len(b) == 0 {
		t.Fatal("a kept icon should be served")
	}
	f.Forget("kept.example.com")
	if _, err := os.Stat(f.path("kept.example.com", ".png")); err == nil {
		t.Fatal("Forget left the icon behind")
	}
}
