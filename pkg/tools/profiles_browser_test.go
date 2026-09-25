package tools

import (
	"testing"
)

// A literal web address in the message is browser intent by itself —
// "check nairobiunwind.com" must reach the page tools while ordinary
// text does not.
func TestWebAddressPattern(t *testing.T) {
	hits := []string{
		"check out nairobiunwind.com and tell me what it is",
		"open https://example.com/pricing",
		"visit www.ghost.org for the docs",
		"compare docs.ghost.io with ghost.org",
		"go to mysite.dev and screenshot it",
	}
	for _, s := range hits {
		if !webAddressPattern.MatchString(s) {
			t.Errorf("expected web address match: %q", s)
		}
	}
	misses := []string{
		"read main.go and fix the bug",
		"open index.html in the editor",
		"bump version to v1.2.3",
		"summarize this conversation",
		"the file.txt lives in docs/",
	}
	for _, s := range misses {
		if webAddressPattern.MatchString(s) {
			t.Errorf("false positive on: %q", s)
		}
	}
}

// URL intent exposes observe+act but never the high-impact pair —
// submit keeps its checkout keywords, upload its own.
func TestURLIntentVisibility(t *testing.T) {
	reg := NewToolRegistry()
	for _, name := range append(browserToolNamesForTest(), "web_search", "read_file") {
		reg.Register(testTool{name: name})
	}
	msg := "check nairobiunwind.com and summarize the hero section"
	got := FilterToolsForTurn(reg, ProfileFull, msg, false)
	for _, want := range []string{"browser_navigate", "browser_snapshot", "browser_find",
		"browser_screenshot", "browser_console", "browser_network", "browser_fill_form", "browser_scroll"} {
		if _, ok := got.Get(want); !ok {
			t.Errorf("URL message must expose %s", want)
		}
	}
	for _, banned := range []string{"browser_submit", "browser_upload"} {
		if _, ok := got.Get(banned); ok {
			t.Errorf("URL message must NOT expose %s (high impact, explicit intent only)", banned)
		}
	}

	// Checkout keyword still unlocks submit.
	got = FilterToolsForTurn(reg, ProfileFull, "checkout and place the order", false)
	if _, ok := got.Get("browser_submit"); !ok {
		t.Error("checkout keyword must expose browser_submit")
	}

	// Upload keyword unlocks upload.
	got = FilterToolsForTurn(reg, ProfileFull, "upload the report to the form", false)
	if _, ok := got.Get("browser_upload"); !ok {
		t.Error("upload keyword must expose browser_upload")
	}

	// No intent → no browser surface (token hygiene + no phantom contract).
	got = FilterToolsForTurn(reg, ProfileFull, "what did I say earlier?", false)
	if _, ok := got.Get("browser_snapshot"); ok {
		t.Error("casual message must not expose browser tools")
	}
}

// Profiles: mobile gets observe+act under the broker; submit/upload are
// desktop/admin posture. Coding gets console/network for debugging but
// no purchases.
func TestBrowserProfileMembership(t *testing.T) {
	cases := []struct {
		profile ToolProfile
		tool    string
		allowed bool
	}{
		{ProfileMobileSafe, "browser_snapshot", true},
		{ProfileMobileSafe, "browser_console", true},
		{ProfileMobileSafe, "browser_fill_form", true},
		{ProfileMobileSafe, "browser_submit", false},
		{ProfileMobileSafe, "browser_upload", false},
		{ProfileCoding, "browser_console", true},
		{ProfileCoding, "browser_network", true},
		{ProfileCoding, "browser_a11y", true},
		{ProfileCoding, "browser_upload", true},
		{ProfileCoding, "browser_submit", false},
		{ProfileResearch, "browser_find", true},
		{ProfileResearch, "browser_submit", false},
		{ProfileHeartbeatSafe, "browser_snapshot", false},
		{ProfileMinimal, "browser_snapshot", false},
	}
	for _, c := range cases {
		if got := ProfileAllowlists[c.profile] != nil && stringInSlice(ProfileAllowlists[c.profile], c.tool); got != c.allowed {
			t.Errorf("%s allows %s = %v, want %v", c.profile, c.tool, got, c.allowed)
		}
	}
}

// browserToolNamesForTest enumerates the full browser surface as
// registered in loop.go (mirrors TestBrowserGovernanceConsistency's
// registry-side count).
func browserToolNamesForTest() []string {
	return concatNames(browserObserveToolNames, browserActToolNames, browserHighToolNames)
}
