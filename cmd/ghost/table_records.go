package main

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// renderTableRecords shows a table that cannot fit as a grid as one small card
// per row: the first column as the title, every other column as a labelled
// line. A wide table used to fall back to raw `| a | b |` lines, which the
// terminal wrapped mid-row and read as broken. Cards stay readable at any width.
func renderTableRecords(header []string, rows [][]string, width int) []string {
	if width < 24 {
		width = 24
	}
	const indent = 3
	labelW := 0
	for i, h := range header {
		if i == 0 {
			continue
		}
		if w := lipgloss.Width(stripInline(h)); w > labelW {
			labelW = w
		}
	}
	if labelW > 14 {
		labelW = 14
	}
	valueW := width - indent - labelW - 2
	if valueW < 12 {
		// Too narrow for a label column: stack label above value.
		labelW = 0
		valueW = width - indent
	}

	var out []string
	for ri, row := range rows {
		title := ""
		if len(row) > 0 {
			title = stripInline(row[0])
		}
		if title == "" {
			title = "—"
		}
		for i, ln := range wrapHard(title, width-2) {
			prefix := "▸ "
			if i > 0 {
				prefix = "  "
			}
			out = append(out, styleMDTableBorder.Render(prefix)+styleMDTableHead.Render(ln))
		}
		for ci := 1; ci < len(row) && ci < len(header); ci++ {
			value := stripInline(row[ci])
			if strings.TrimSpace(value) == "" {
				continue
			}
			label := stripInline(header[ci])
			if labelW > 0 {
				label = cellTruncate(label, labelW)
				lines := wrapHard(value, valueW)
				for li, ln := range lines {
					l := strings.Repeat(" ", labelW)
					if li == 0 {
						l = label + strings.Repeat(" ", labelW-lipgloss.Width(label))
					}
					out = append(out, strings.Repeat(" ", indent)+styleMDTableBorder.Render(l)+"  "+styleMDTableRow.Render(ln))
				}
			} else {
				out = append(out, strings.Repeat(" ", indent)+styleMDTableBorder.Render(label))
				for _, ln := range wrapHard(value, valueW) {
					out = append(out, strings.Repeat(" ", indent)+styleMDTableRow.Render(ln))
				}
			}
		}
		if ri < len(rows)-1 {
			out = append(out, "")
		}
	}
	return out
}

// wrapHard wraps text to w columns and breaks any single word that is still
// wider than a line, so no output line can exceed w.
func wrapHard(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	for _, ln := range wrapText(s, w) {
		if lipgloss.Width(ln) <= w {
			out = append(out, ln)
			continue
		}
		var cur strings.Builder
		curW := 0
		for _, r := range ln {
			rw := lipgloss.Width(string(r))
			if curW+rw > w {
				out = append(out, cur.String())
				cur.Reset()
				curW = 0
			}
			cur.WriteRune(r)
			curW += rw
		}
		if cur.Len() > 0 {
			out = append(out, cur.String())
		}
	}
	return out
}
