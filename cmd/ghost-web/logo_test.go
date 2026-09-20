package main

import (
	"strings"
	"testing"
)

// The console brand mark must be the SAME artwork as the mobile app's
// GhostMark (viewBox 0 0 32 32, the geometric bar mark), and the favicon must
// match too — the two surfaces present one identity.
func TestConsoleMarkMatchesMobileArtwork(t *testing.T) {
	data, err := webFiles.ReadFile("web/js/components.js")
	if err != nil {
		t.Fatalf("components.js: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, `viewBox="0 0 32 32"`) {
		t.Fatalf("console mark must use the 32x32 mobile viewBox")
	}
	// The distinctive leading path data of the mobile mark.
	if !strings.Contains(src, "M4.859 7.401v2.115h13.256v-4.231h-13.256") {
		t.Fatalf("console mark path does not match the mobile GhostMark artwork")
	}
	if strings.Contains(src, "M12 2C7.58 2 4 5.58 4 10v10") {
		t.Fatalf("console still uses the old generic ghost icon")
	}
}

func TestConsoleFaviconUsesMark(t *testing.T) {
	idx, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	src := string(idx)
	if !strings.Contains(src, "M4.859 7.401") {
		t.Fatalf("favicon should use the Ghost mark, not an emoji")
	}
	if strings.Contains(src, "👻") {
		t.Fatalf("favicon still uses the emoji placeholder")
	}
}
