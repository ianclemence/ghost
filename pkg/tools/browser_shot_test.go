package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

var testPNG = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)

func TestCaptureBrowserShotAcceptsFreshPNG(t *testing.T) {
	dir := t.TempDir()
	path, ok := captureBrowserShotWith(context.Background(), "sess-abc123", dir,
		func(ctx context.Context, p string) error {
			return os.WriteFile(p, testPNG, 0644)
		})
	if !ok || path == "" {
		t.Fatalf("valid capture must be accepted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("capture must persist the file: %v", err)
	}
}

func TestCaptureBrowserShotRejectsFailures(t *testing.T) {
	dir := t.TempDir()
	fail := func(ctx context.Context, p string) error {
		return context.DeadlineExceeded
	}
	if _, ok := captureBrowserShotWith(context.Background(), "s1", dir, fail); ok {
		t.Fatalf("failed command must yield no screenshot")
	}
	notPNG := func(ctx context.Context, p string) error {
		return os.WriteFile(p, []byte("definitely not an image"), 0644)
	}
	if _, ok := captureBrowserShotWith(context.Background(), "s1", dir, notPNG); ok {
		t.Fatalf("non-PNG output must be rejected")
	}
	huge := func(ctx context.Context, p string) error {
		f, err := os.Create(p)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.Write([]byte("\x89PNG\r\n\x1a\n")); err != nil {
			return err
		}
		if err := f.Truncate(browserShotBound + 1); err != nil {
			return err
		}
		return nil
	}
	if _, ok := captureBrowserShotWith(context.Background(), "s1", dir, huge); ok {
		t.Fatalf("oversize output must be rejected")
	}
	if _, ok := captureBrowserShotWith(context.Background(), "", dir, fail); ok {
		t.Fatalf("empty session must yield nothing")
	}
}

func TestCaptureBrowserShotSanitizesSession(t *testing.T) {
	if sanitizeShotSession("../../etc/passwd") != "etcpasswd" {
		t.Fatalf("session must be filename-safe: %q", sanitizeShotSession("../../etc/passwd"))
	}
	if sanitizeShotSession("") != "" {
		t.Fatalf("empty session must stay empty")
	}
}

func TestCaptureBrowserShotPrunesDirectory(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < browserShotKeep+5; i++ {
		p := filepath.Join(dir, "old-"+string(rune('a'+i%26))+string(rune('0'+i/26))+".png")
		if err := os.WriteFile(p, testPNG, 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	pruneBrowserShots(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) > browserShotKeep {
		t.Fatalf("shot directory must stay bounded, got %d", len(entries))
	}
}
