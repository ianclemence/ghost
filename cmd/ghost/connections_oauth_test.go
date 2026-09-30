package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func oauthCall(t *testing.T, method, appID, step, body string) (int, map[string]interface{}) {
	t.Helper()
	var rest []string
	if step != "" {
		rest = []string{step}
	}
	r := httptest.NewRequest(method, "/v1/connected-apps/"+appID+"/oauth/"+step, strings.NewReader(body))
	r.Header.Set("X-Ghost-Device-ID", "dev-1")
	w := httptest.NewRecorder()
	serveConnectedAppOAuth(w, r, appID, rest)
	var out map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestPhoneCanSignInToGoogleWithItsOwnApp(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	for _, k := range []string{"GHOST_GOOGLE_CLIENT_ID", "GHOST_GOOGLE_CLIENT_SECRET"} {
		t.Setenv(k, "")
	}

	code, got := oauthCall(t, http.MethodGet, "gmail", "", "")
	if code != 200 || got["configured"] != false {
		t.Fatalf("before setup: %d %v", code, got)
	}
	if steps, _ := got["steps"].([]interface{}); len(steps) < 3 {
		t.Errorf("the phone needs the setup steps: %v", got["steps"])
	}

	// Starting before setup says setup is needed instead of failing.
	_, got = oauthCall(t, http.MethodPost, "gmail", "start", "{}")
	if got["status"] != "needs_setup" {
		t.Fatalf("start before setup: %v", got)
	}

	// A half-filled setup is refused.
	if code, _ = oauthCall(t, http.MethodPost, "gmail", "setup", `{"client_id":"only-id"}`); code != 400 {
		t.Errorf("a missing secret must be refused, got %d", code)
	}
	if code, _ = oauthCall(t, http.MethodPost, "gmail", "setup", `{"client_id":"id-1","client_secret":"sec-1"}`); code != 200 {
		t.Fatalf("setup: %d", code)
	}

	// Now it hands back Google's sign-in address, with the Pod's redirect and no secret in it.
	_, got = oauthCall(t, http.MethodPost, "gmail", "start", "{}")
	authURL, _ := got["auth_url"].(string)
	if got["status"] != "needs_authorization" || !strings.HasPrefix(authURL, "https://accounts.google.com/") {
		t.Fatalf("start after setup: %v", got)
	}
	if !strings.Contains(authURL, "client_id=id-1") || strings.Contains(authURL, "sec-1") {
		t.Errorf("the address must name the app and never carry its secret: %s", authURL)
	}

	// Calendar shares the same Google app.
	if _, got = oauthCall(t, http.MethodGet, "google-calendar", "", ""); got["configured"] != true {
		t.Errorf("Calendar should share the Google app: %v", got)
	}
}

func TestPastedAddressProblemsAreSaidInPlainWords(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	for _, c := range []struct{ body, want string }{
		{`{"url":""}`, "sign-in code"},
		{`{"url":"http://localhost/?error=access_denied&state=x"}`, "cancelled"},
		{`{"url":"http://localhost/?code=abc&state=never-issued"}`, "try again"},
	} {
		_, got := oauthCall(t, http.MethodPost, "spotify", "paste", c.body)
		msg, _ := got["error"].(string)
		if got["ok"] != false || !strings.Contains(strings.ToLower(msg), c.want) {
			t.Errorf("%s: %v", c.body, got)
		}
	}
}

func TestOnlySignInAppsHaveASignInFlow(t *testing.T) {
	if code, _ := oauthCall(t, http.MethodGet, "github", "", ""); code != 404 {
		t.Errorf("a key-based app has no browser sign-in, got %d", code)
	}
}
