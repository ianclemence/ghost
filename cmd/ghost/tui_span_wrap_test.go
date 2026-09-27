package main

import (
	"strings"
	"testing"
)

// norm collapses wrap-induced line breaks so content assertions can look
// across rows (styling may split a phrase between two wrapped lines).
func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// An inline span that crosses the terminal-width wrap boundary must still
// conceal. The renderer wraps the RAW line first and styles each wrapped
// slice on its own, so a span split across the break used to show its
// markers literally — and the words inside the span unstyled. That is the
// "sometimes the styles are applied, sometimes they miss" defect: styling
// worked only when the whole span happened to fit one wrapped row.
func TestMarkdownLineNeverLeaksSpanMarkersWhenWrapping(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		width int
		// words that must survive styling untouched.
		keep []string
	}{
		{
			name:  "paragraph bold",
			line:  "This is a **very long bold sentence that the model emitted on one physical line and it definitely wraps at eighty columns or so, priced at $349.** done.",
			width: 70,
			keep:  []string{"$349", "very long", "done."},
		},
		{
			name:  "blockquote bold",
			line:  "> **Build Ghost Pod around an 8-core ARM64 SoC with 8 GB RAM, 256 GB NVMe, Gigabit Ethernet, Wi-Fi 6, an internal ESP32, and passive cooling — priced at $349.**",
			width: 60,
			keep:  []string{"ESP32", "$349", "passive cooling"},
		},
		{
			name:  "bullet bold",
			line:  "- **Specification** the board ships with 8 GB LPDDR5 memory, 256 GB NVMe storage, gigabit ethernet and Wi-Fi 6, all passively cooled.",
			width: 60,
			keep:  []string{"LPDDR5", "Wi-Fi 6"},
		},
		{
			name:  "ordered bold",
			line:  "1. **RAM** install 8 GB of LPDDR5 and verify the modules seat correctly before closing the chassis on the ARM64 build box.",
			width: 60,
			keep:  []string{"LPDDR5", "chassis"},
		},
		{
			name:  "checkbox bold",
			line:  "- [ ] **Order the ESP32** module together with the passive cooling kit and the NVMe drive before the pricing window closes.",
			width: 60,
			keep:  []string{"ESP32", "NVMe"},
		},
		{
			name:  "code span",
			line:  "Run `go build -ldflags \"-X main.version=v1.2.3\" -o bin/ghost ./cmd/ghost` on the ARM64 box, then restart the daemon.",
			width: 60,
			keep:  []string{"ldflags", "daemon"},
		},
		{
			name:  "italic span",
			line:  "The *passive cooling design keeps the SoC under thermal limits even at full load across every benchmark we ran here*, verified twice.",
			width: 70,
			keep:  []string{"thermal limits", "verified twice"},
		},
		{
			name:  "strike span",
			line:  "The ~~prototype pricing of $499 is dead~~ final pricing lands at $349 with margin intact at volume, confirmed by the vendor.",
			width: 70,
			keep:  []string{"$349", "vendor"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trim := strings.TrimSpace(tc.line)
			out := strings.Join(renderMarkdownLine(trim, tc.line, tc.width), "\n")
			for _, marker := range []string{"*", "`", "~~"} {
				if strings.Contains(out, marker) {
					t.Errorf("raw marker %q leaked at the wrap boundary:\n%s", marker, out)
				}
			}
			for _, word := range tc.keep {
				if !strings.Contains(norm(out), word) {
					t.Errorf("styled wrap lost content %q:\n%s", word, out)
				}
			}
		})
	}
}

// Headings carry their own role style but must still style (and conceal)
// inline markup inside the title, on every wrapped row of it.
func TestMarkdownHeadingStylesInlineMarkup(t *testing.T) {
	out := strings.Join(renderMarkdownLine("## Build the pod with `gcc` and **speed**", "## Build the pod with `gcc` and **speed**", 40), "\n")
	for _, marker := range []string{"##", "`", "**"} {
		if strings.Contains(out, marker) {
			t.Errorf("heading leaked raw marker %q:\n%s", marker, out)
		}
	}
	for _, word := range []string{"Build the pod", "gcc", "speed"} {
		if !strings.Contains(norm(out), word) {
			t.Errorf("heading lost content %q:\n%s", word, out)
		}
	}
}

// A table that cannot fit its grid falls back to showing its rows as
// text — that fallback must conceal inline markers like every other path.
// It used to emit the raw markdown, wrapping **spans** and code spans
// mid-token: the live TUI showed literal ** and ` in streamed replies.
func TestTableFallbackConcealsMarkers(t *testing.T) {
	cases := []struct {
		name  string
		block []string
		width int
		keep  []string
	}{
		{
			name:  "single row fallback",
			block: []string{"| **85% used** of the root disk with `df -h` showing 4.2 GiB left of 28.7 GiB total |"},
			width: 60,
			keep:  []string{"85% used", "4.2 GiB"},
		},
		{
			name: "too-wide grid fallback",
			block: []string{
				"| SoC | RAM | Storage | Cooling | Price | Margin | Status |",
				"|-----|-----|---------|---------|-------|--------|-------|",
				"| RK3588 | **16 GB minimum**, 32 GB preferred | **NVMe, 256 GB** | fanless | $349 | 12% | `build ok` |",
			},
			width: 20,
			keep:  []string{"RK3588", "16 GB", "fanless"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := strings.Join(renderTable(tc.block, tc.width), "\n")
			for _, marker := range []string{"*", "`"} {
				if strings.Contains(out, marker) {
					t.Errorf("table fallback leaked raw marker %q:\n%s", marker, out)
				}
			}
			for _, word := range tc.keep {
				if !strings.Contains(norm(out), word) {
					t.Errorf("table fallback lost content %q:\n%s", word, out)
				}
			}
		})
	}
}

// The committed reply path (renderAssistantBody) renders the same long
// lines — it must conceal on wrap too, and stay identical in content.
func TestCommittedBodyNeverLeaksSpanMarkersWhenWrapping(t *testing.T) {
	doc := strings.Join([]string{
		"## Final recommendation",
		"> **Build Ghost Pod around an 8-core ARM64 SoC with 8 GB RAM, 256 GB NVMe, Gigabit Ethernet, Wi-Fi 6, an internal ESP32, and passive cooling — priced at $349.**",
		"- **CPU:** ARM64, 8-core @ ~2.4 GHz with headroom for sustained compile loads on `go build`.",
		"1. **Validate** the thermals under full load and record the results before committing to the enclosure design.",
	}, "\n")
	out := renderAssistantBody(doc, 60)
	for _, marker := range []string{"*", "`", "##"} {
		if strings.Contains(out, marker) {
			t.Errorf("committed render leaked raw marker %q:\n%s", marker, out)
		}
	}
	for _, word := range []string{"$349", "ESP32", "2.4", "enclosure"} {
		if !strings.Contains(norm(out), word) {
			t.Errorf("committed render lost content %q:\n%s", word, out)
		}
	}
}

// The streaming path styles lines progressively through streamStyler; a
// span broken by the terminal wrap must conceal there as well — the live
// text and the committed text never disagree about markers.
func TestStreamingStylerNeverLeaksSpanMarkersWhenWrapping(t *testing.T) {
	lines := []string{
		"> **Build Ghost Pod around an 8-core ARM64 SoC with 8 GB RAM, 256 GB NVMe, Gigabit Ethernet, Wi-Fi 6, and passive cooling — priced at $349.**",
		"- **Storage** 256 GB NVMe boot drive with a second bay reserved for bulk media on the home network.",
	}
	s := streamStyler{width: 60}
	var out []string
	for _, ln := range lines {
		out = append(out, s.line(ln)...)
	}
	out = append(out, s.flush()...)
	got := strings.Join(out, "\n")
	for _, marker := range []string{"*", "`"} {
		if strings.Contains(got, marker) {
			t.Errorf("streaming render leaked raw marker %q:\n%s", marker, got)
		}
	}
	for _, word := range []string{"$349", "bulk media"} {
		if !strings.Contains(norm(got), word) {
			t.Errorf("streaming render lost content %q:\n%s", word, got)
		}
	}
}
