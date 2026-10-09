package motion

import (
	_ "embed"
	"encoding/json"
	"html"
	"strings"

	"github.com/ianclemence/ghost/pkg/documents"
)

//go:embed player.js
var playerJS string

//go:embed player.css
var playerCSS string

// Page is the motion as one self-contained HTML document: the spec, the
// player and Ghost's own type, nothing fetched. Preview mode fits the width it
// runs in and plays on a tap; render mode is the bare stage at its pixel size
// for the Pod to step through frame by frame.
func Page(s Spec, render bool) string {
	b, _ := json.Marshal(s)
	// The spec goes into a script: nothing in it may close the tag.
	js := strings.NewReplacer("<", `<`, ">", `>`, " ", ` `, " ", ` `).Replace(string(b))
	mode := "preview"
	if render {
		mode = "render"
	}
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><head><meta charset="utf-8">`)
	sb.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	sb.WriteString("<title>" + html.EscapeString(s.Title) + "</title>")
	sb.WriteString("<style>" + documents.FontFaces() + playerCSS + "</style></head><body>")
	sb.WriteString(`<div id="frame"><div id="motion"></div></div>`)
	sb.WriteString("<script>window.__MOTION__=" + js + `;window.__MOTION_MODE__="` + mode + `";</script>`)
	sb.WriteString("<script>" + playerJS + "</script></body></html>")
	return sb.String()
}
