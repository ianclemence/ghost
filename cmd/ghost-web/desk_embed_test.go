package main

import (
	"strings"
	"testing"
)

// The Desk section and its nav entry must be embedded and wired, or the owner
// sees a nav item that loads nothing.
func TestDeskSectionEmbedded(t *testing.T) {
	data, err := webFiles.ReadFile("web/js/sections/desk.js")
	if err != nil {
		t.Fatalf("desk.js not embedded: %v", err)
	}
	if !strings.Contains(string(data), "registerSection('desk'") {
		t.Fatalf("desk.js must register the desk section")
	}
	idx, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	if !strings.Contains(string(idx), "sections/desk.js") {
		t.Fatalf("index.html must load the desk section")
	}
	app, err := webFiles.ReadFile("web/js/app.js")
	if err != nil {
		t.Fatalf("app.js: %v", err)
	}
	if !strings.Contains(string(app), "name: 'desk'") {
		t.Fatalf("app.js nav must include the desk destination")
	}
}
