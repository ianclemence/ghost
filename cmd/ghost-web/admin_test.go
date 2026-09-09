package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
)

func TestLoginThrottleLockout(t *testing.T) {
	tl := newLoginThrottle()
	ip := "10.0.0.5"

	// First five attempts are allowed.
	for i := 0; i < maxLoginAttempts; i++ {
		if ok, _ := tl.allowed(ip); !ok {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
		tl.recordFailure(ip)
	}

	// Sixth attempt is blocked.
	if ok, wait := tl.allowed(ip); ok {
		t.Fatalf("attempt %d should be blocked", maxLoginAttempts+1)
	} else if wait <= 0 {
		t.Fatalf("expected positive cooldown wait, got %v", wait)
	}

	// Successful login resets the counter.
	tl.recordSuccess(ip)
	if ok, _ := tl.allowed(ip); !ok {
		t.Fatalf("should be allowed after success reset")
	}
}

func TestLoginThrottleDifferentIPs(t *testing.T) {
	tl := newLoginThrottle()
	for i := 0; i < maxLoginAttempts; i++ {
		tl.recordFailure("10.0.0.1")
	}
	if ok, _ := tl.allowed("10.0.0.2"); !ok {
		t.Fatalf("different IP should not be blocked")
	}
}

func TestMaskKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"short", "••••••••"},
		{"sk-abcdef1234567890", "••••••••7890"},
	}
	for _, c := range cases {
		if got := maskKey(c.in); got != c.want {
			t.Errorf("maskKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUpdateEnvFile(t *testing.T) {
	if fb == nil {
		fb = &appliance.SetupState{}
	}
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	fb.EnvPath = envPath

	if err := os.WriteFile(envPath, []byte("TZ=UTC\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := updateEnvFile("DEEPSEEK_API_KEY", "newkey"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(envPath)
	got := string(b)
	for _, want := range []string{"TZ=UTC", "DEEPSEEK_API_KEY=newkey"} {
		if !contains(got, want) {
			t.Errorf("env file missing %q:\n%s", want, got)
		}
	}

	// Updating an existing key should replace, not duplicate.
	if err := updateEnvFile("DEEPSEEK_API_KEY", "updated"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(envPath)
	got = string(b)
	if count := countOccurrences(got, "DEEPSEEK_API_KEY="); count != 1 {
		t.Errorf("expected 1 DEEPSEEK_API_KEY line, got %d:\n%s", count, got)
	}
	if !contains(got, "DEEPSEEK_API_KEY=updated") {
		t.Errorf("expected updated value:\n%s", got)
	}
}

func TestSkillDescriptionParsing(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	skillDir := filepath.Join(skillsDir, "testskill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: testskill
description: A test skill description.
---
# Real instructions here`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	original := fb.Workspace
	fb.Workspace = dir
	defer func() { fb.Workspace = original }()

	entries, err := os.ReadDir(workspaceSkillsDir())
	if err != nil {
		t.Fatal(err)
	}
	var desc string
	for _, e := range entries {
		if e.Name() != "testskill" {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(workspaceSkillsDir(), e.Name(), "SKILL.md"))
		desc = skillSummary(string(b))
	}
	if desc != "A test skill description." {
		t.Fatalf("got description %q, want %q", desc, "A test skill description.")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}

func TestLoginThrottleEscalation(t *testing.T) {
	tl := newLoginThrottle()
	ip := "10.0.0.9"

	for i := 0; i < 5; i++ {
		tl.recordFailure(ip)
	}
	if ok, wait := tl.allowed(ip); ok || wait < 9*time.Minute || wait > 10*time.Minute {
		t.Fatalf("after 5 failures want ~10m lockout, ok=%v wait=%v", ok, wait)
	}
	for i := 0; i < 5; i++ {
		tl.recordFailure(ip)
	}
	if ok, wait := tl.allowed(ip); ok || wait < 29*time.Minute || wait > 30*time.Minute {
		t.Fatalf("after 10 failures want ~30m lockout, ok=%v wait=%v", ok, wait)
	}
	for i := 0; i < 5; i++ {
		tl.recordFailure(ip)
	}
	if ok, wait := tl.allowed(ip); ok || wait < 59*time.Minute || wait > time.Hour {
		t.Fatalf("after 15 failures want ~1h lockout, ok=%v wait=%v", ok, wait)
	}
	for i := 0; i < 10; i++ {
		tl.recordFailure(ip)
	}
	if ok, wait := tl.allowed(ip); ok || wait < 119*time.Minute || wait > 2*time.Hour {
		t.Fatalf("lockout must cap at 2h, ok=%v wait=%v", ok, wait)
	}
}

func TestLoginThrottleStaleDecay(t *testing.T) {
	tl := newLoginThrottle()
	ip := "10.0.0.10"
	tl.mu.Lock()
	tl.failures[ip] = time.Now().Add(-25 * time.Hour)
	tl.attemptCounts[ip] = 5
	tl.mu.Unlock()
	if ok, _ := tl.allowed(ip); !ok {
		t.Fatal("day-old failures must decay")
	}
}

func TestLoginThrottlePersistence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/throttle.json"
	tl := newLoginThrottle()
	for i := 0; i < 5; i++ {
		tl.recordFailure("10.0.0.11")
	}
	tl.persistPath = path
	tl.persist(path)

	restored := newLoginThrottle()
	restored.load(path)
	if ok, wait := restored.allowed("10.0.0.11"); ok || wait <= 0 {
		t.Fatalf("restored throttle must still lock, ok=%v wait=%v", ok, wait)
	}
	if ok, _ := restored.allowed("10.0.0.12"); !ok {
		t.Fatal("other IPs unaffected after restore")
	}

	// Corrupt file fails open, never locks out the owner.
	os.WriteFile(path, []byte("{broken"), 0600)
	restored2 := newLoginThrottle()
	restored2.load(path)
	if ok, _ := restored2.allowed("10.0.0.11"); !ok {
		t.Fatal("corrupt state must fail open")
	}
}

// authTestEnv swaps globals for black-box handler tests.
func authTestEnv(t *testing.T, password string) {
	t.Helper()
	oldFb, oldSessions, oldThrottle := fb, sessions, loginThrottle
	dir := t.TempDir()
	if err := appliance.SetAdminPassword(dir, password); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}
	fb = &appliance.SetupState{
		GhostDir:  dir,
		ConfigDir: dir + "/config",
		DataDir:   dir + "/data",
		Workspace: dir + "/workspace",
	}
	sessions = newSessionStore()
	loginThrottle = newLoginThrottle()
	t.Cleanup(func() { fb, sessions, loginThrottle = oldFb, oldSessions, oldThrottle })
}

func doLogin(t *testing.T, password string, remember bool) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"password":` + strconv.Quote(password) + `,"remember_me":` + strconv.FormatBool(remember) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
	req.RemoteAddr = "192.0.2.44:1234"
	rec := httptest.NewRecorder()
	handleLogin(rec, req)
	return rec
}

func TestLoginBlackBox(t *testing.T) {
	authTestEnv(t, "correct-horse-1")

	// Five wrong guesses → 401 with a generic message (no enumeration).
	for i := 0; i < 5; i++ {
		rec := doLogin(t, "wrong-password", false)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password must 401, got %d (attempt %d)", rec.Code, i)
		}
		var j map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&j)
		if j["error"] != "invalid password" {
			t.Fatalf("error must stay generic, got %v", j["error"])
		}
	}
	// Sixth → 429, and the right password is also gated while locked.
	if rec := doLogin(t, "wrong-password", false); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("sixth attempt must 429, got %d", rec.Code)
	}
	if rec := doLogin(t, "correct-horse-1", false); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("lockout must gate even correct password, got %d", rec.Code)
	}

	// Fresh throttle (as after cooldown): correct password → 200 + cookie.
	loginThrottle = newLoginThrottle()
	rec := doLogin(t, "correct-horse-1", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct password must 200, got %d", rec.Code)
	}
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "ghost_admin_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.Value == "" {
		t.Fatal("login must set an HttpOnly session cookie")
	}

	// Cookie authenticates an admin endpoint.
	req := httptest.NewRequest(http.MethodGet, "/api/admin/auth/check", nil)
	req.AddCookie(sessionCookie)
	checkRec := httptest.NewRecorder()
	handleAuthCheck(checkRec, req)
	if checkRec.Code != http.StatusOK {
		t.Fatalf("authed check must 200, got %d", checkRec.Code)
	}

	// Logout kills it server-side.
	outReq := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	outReq.AddCookie(sessionCookie)
	outRec := httptest.NewRecorder()
	handleLogout(outRec, outReq)
	if outRec.Code != http.StatusOK {
		t.Fatalf("logout must 200, got %d", outRec.Code)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/api/admin/auth/check", nil)
	req2.AddCookie(sessionCookie)
	checkRec2 := httptest.NewRecorder()
	handleAuthCheck(checkRec2, req2)
	if checkRec2.Code != http.StatusUnauthorized {
		t.Fatalf("session must be dead after logout, got %d", checkRec2.Code)
	}
}

func TestPasswordChangeRevokesSessions(t *testing.T) {
	authTestEnv(t, "old-password-1")
	rec := doLogin(t, "old-password-1", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("login must 200, got %d", rec.Code)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "ghost_admin_session" {
			cookie = c
		}
	}

	changeBody := `{"current":"old-password-1","new":"brand-new-password-2","confirm":"brand-new-password-2"}`
	creq := httptest.NewRequest(http.MethodPost, "/api/admin/password", strings.NewReader(changeBody))
	creq.AddCookie(cookie)
	crec := httptest.NewRecorder()
	handleChangePassword(crec, creq)
	if crec.Code != http.StatusOK {
		t.Fatalf("password change must 200, got %d", crec.Code)
	}

	// The session that made the call is dead too.
	chk := httptest.NewRequest(http.MethodGet, "/api/admin/auth/check", nil)
	chk.AddCookie(cookie)
	chkRec := httptest.NewRecorder()
	handleAuthCheck(chkRec, chk)
	if chkRec.Code != http.StatusUnauthorized {
		t.Fatalf("old session must die on password change, got %d", chkRec.Code)
	}
}

func TestSessionListMaskedAndRevokeByID(t *testing.T) {
	authTestEnv(t, "session-test-1")
	rec := doLogin(t, "session-test-1", false)
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "ghost_admin_session" {
			cookie = c
		}
	}
	// Second session from another IP.
	tok2, err := sessions.issue("192.0.2.99", "other", false)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	_ = tok2

	lreq := httptest.NewRequest(http.MethodGet, "/api/admin/sessions", nil)
	lreq.AddCookie(cookie)
	lrec := httptest.NewRecorder()
	handleAdminSessions(lrec, lreq)
	if lrec.Code != http.StatusOK {
		t.Fatalf("list must 200, got %d", lrec.Code)
	}
	var list struct {
		Sessions []struct {
			ID      string `json:"id"`
			Token   string `json:"token"`
			Current bool   `json:"current"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(lrec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(list.Sessions))
	}
	var otherID, leaked string
	for _, s := range list.Sessions {
		if s.ID == "" {
			t.Fatal("every session must expose an id")
		}
		if s.Token == tok2 {
			leaked = s.Token
		}
		if !s.Current {
			otherID = s.ID
		}
	}
	if leaked != "" {
		t.Fatal("raw session token must not appear in the list")
	}
	if otherID == "" {
		t.Fatal("must identify the non-current session")
	}

	// Revoke by ID works; the masked token from the list is useless.
	revBody := `{"id":` + strconv.Quote(otherID) + `}`
	rreq := httptest.NewRequest(http.MethodPost, "/api/admin/sessions/revoke", strings.NewReader(revBody))
	rreq.AddCookie(cookie)
	rrec := httptest.NewRecorder()
	handleAdminSessionRevoke(rrec, rreq)
	if rrec.Code != http.StatusOK {
		t.Fatalf("revoke by id must 200, got %d", rrec.Code)
	}
	if sessions.valid(tok2) {
		t.Fatal("revoked session must be invalid")
	}
}
