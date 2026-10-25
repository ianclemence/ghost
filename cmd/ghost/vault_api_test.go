package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/localtrust"
)

const vaultTok = "0123456789abcdef0123456789abcdef"

func vaultServer(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	prev := apiWorkspaceDir
	apiWorkspaceDir = t.TempDir()
	localtrust.Accept(vaultTok)
	t.Cleanup(func() { apiWorkspaceDir = prev; localtrust.Accept("") })
	mux := http.NewServeMux()
	registerVaultRoutes(mux)
	return mux
}

func vaultDo(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:18790"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50123"
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set(localtrust.Header, token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// A command the model runs in its shell can reach 127.0.0.1. It must not be able
// to read the Vault or write to it.
func TestTheVaultRefusesALoopbackCallerWithoutTheLocalToken(t *testing.T) {
	h := vaultServer(t)
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/v1/vault/items", ""},
		{"GET", "/v1/vault/items?kind=card", ""},
		{"POST", "/v1/vault/items", `{"kind":"key","title":"x","value":"abcdefgh"}`},
		{"PUT", "/v1/vault/items", `{"kind":"key","id":"x"}`},
		{"DELETE", "/v1/vault/items?kind=key&id=x", ""},
		{"GET", "/v1/vault/icon?host=google.com", ""},
		{"POST", "/v1/vault/credentials", `{"card_id":"c","username":"u","password":"p"}`},
	} {
		if w := vaultDo(h, c.method, c.path, c.body, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without the local token = %d, want 401", c.method, c.path, w.Code)
		}
		if w := vaultDo(h, c.method, c.path, c.body, "wrong-token-wrong-token-wrong-tok"); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with a wrong token = %d, want 401", c.method, c.path, w.Code)
		}
	}
}

func TestNothingSecretEverLeavesTheVaultApi(t *testing.T) {
	h := vaultServer(t)
	secrets := []string{"correct horse battery", "4242424242424242", "4242 4242 4242 4242", "sk_live_abcdef123456", "123\""}
	posts := []string{
		`{"kind":"login","url":"https://ads.google.com","username":"ian@example.com","password":"correct horse battery"}`,
		`{"kind":"card","title":"Visa","number":"4242 4242 4242 4242","exp_month":8,"exp_year":28,"cvc":"123"}`,
		`{"kind":"key","title":"Stripe","service":"Stripe","value":"sk_live_abcdef123456","env_name":"STRIPE_KEY"}`,
	}
	for _, p := range posts {
		w := vaultDo(h, "POST", "/v1/vault/items", p, vaultTok)
		if w.Code != 200 {
			t.Fatalf("save failed: %d %s", w.Code, w.Body.String())
		}
		for _, s := range secrets {
			if strings.Contains(w.Body.String(), s) {
				t.Fatalf("a save response carries %q: %s", s, w.Body.String())
			}
		}
	}
	for _, path := range []string{"/v1/vault/items", "/v1/vault/items?kind=card", "/v1/vault/items?kind=login"} {
		w := vaultDo(h, "GET", path, "", vaultTok)
		if w.Code != 200 {
			t.Fatalf("list %s: %d", path, w.Code)
		}
		for _, s := range secrets {
			if strings.Contains(w.Body.String(), s) {
				t.Fatalf("list %s carries %q: %s", path, s, w.Body.String())
			}
		}
	}
	var listed struct {
		Items  []credentials.View `json:"items"`
		Counts map[string]int     `json:"counts"`
	}
	json.Unmarshal(vaultDo(h, "GET", "/v1/vault/items", "", vaultTok).Body.Bytes(), &listed)
	if len(listed.Items) != 3 || listed.Counts["login"] != 1 || listed.Counts["card"] != 1 || listed.Counts["key"] != 1 {
		t.Fatalf("listing: %+v", listed)
	}
	var card credentials.View
	for _, it := range listed.Items {
		if it.Kind == credentials.KindCard {
			card = it
		}
	}
	if card.Last4 != "4242" || card.Brand != "visa" {
		t.Fatalf("a card should show its brand and last four: %+v", card)
	}

	// Edit the login's username: the password is kept without being sent again.
	w := vaultDo(h, "PUT", "/v1/vault/items", `{"kind":"login","id":"ads.google.com","host":"ads.google.com","url":"https://ads.google.com","username":"work@example.com"}`, vaultTok)
	if w.Code != 200 {
		t.Fatalf("edit: %d %s", w.Code, w.Body.String())
	}
	if l, ok := credentials.WebLoginFor("ads.google.com"); !ok || l.Password != "correct horse battery" || l.Username != "work@example.com" {
		t.Fatalf("editing a username lost the password: %+v", l)
	}
	if w := vaultDo(h, "DELETE", "/v1/vault/items?kind=login&id=ads.google.com", "", vaultTok); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
	if _, ok := credentials.WebLoginFor("ads.google.com"); ok {
		t.Fatal("a deleted login is still there")
	}
}

func TestTheVaultApiRefusesWhatItDoesNotUnderstand(t *testing.T) {
	h := vaultServer(t)
	cases := []struct{ name, body string }{
		{"an unknown field", `{"kind":"key","title":"x","value":"abcdefgh","admin":true}`},
		{"trailing data", `{"kind":"key","title":"x","value":"abcdefgh"} {"kind":"key"}`},
		{"not json", `kind=key`},
		{"a bad card number", `{"kind":"card","number":"4242 4242 4242 4241","exp_month":8,"exp_year":28}`},
		{"no kind", `{"title":"x"}`},
	}
	for _, c := range cases {
		if w := vaultDo(h, "POST", "/v1/vault/items", c.body, vaultTok); w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400 (%s)", c.name, w.Code, w.Body.String())
		}
	}
	big := `{"kind":"key","title":"x","value":"` + strings.Repeat("a", 70<<10) + `"}`
	if w := vaultDo(h, "POST", "/v1/vault/items", big, vaultTok); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("an oversized body = %d, want 413", w.Code)
	}
	// The error never repeats the value that failed.
	w := vaultDo(h, "POST", "/v1/vault/items", `{"kind":"card","number":"4242 4242 4242 4241","exp_month":8,"exp_year":28}`, vaultTok)
	if strings.Contains(w.Body.String(), "4241") {
		t.Fatalf("an error echoed the card number: %s", w.Body.String())
	}
}

func TestACredentialsCardIsAnsweredOnceAndForItsOwnSiteOnly(t *testing.T) {
	h := vaultServer(t)
	card, err := cards.NewCredentialRequest("ads.google.com", "Google Ads Manager", "Pause the campaigns over budget.")
	if err != nil {
		t.Fatal(err)
	}
	cards.DefaultStore.Add("mobile", card)

	ask := func(id, body string) *httptest.ResponseRecorder {
		return vaultDo(h, "POST", "/v1/vault/credentials", body, vaultTok)
	}
	if w := ask(card.ID, `{"card_id":"`+card.ID+`","username":"","password":"x"}`); w.Code != 400 {
		t.Fatalf("a blank username = %d", w.Code)
	}
	// The request cannot name another site: the site is the card's.
	if w := ask(card.ID, `{"card_id":"`+card.ID+`","host":"evil.example","username":"u","password":"p"}`); w.Code != 400 {
		t.Fatalf("a request naming its own host must be refused outright, got %d", w.Code)
	}
	if w := ask("nope", `{"card_id":"card_nope","username":"u","password":"p"}`); w.Code != 404 {
		t.Fatalf("an unknown card = %d", w.Code)
	}

	w := ask(card.ID, `{"card_id":"`+card.ID+`","username":"ian@example.com","password":"a-long-secret-pass","save":true}`)
	if w.Code != 200 {
		t.Fatalf("answer: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "a-long-secret-pass") || strings.Contains(w.Body.String(), "ian@example.com") {
		t.Fatalf("the response repeats what the owner typed: %s", w.Body.String())
	}
	if l, ok := credentials.WebLoginFor("ads.google.com"); !ok || l.Password != "a-long-secret-pass" {
		t.Fatalf("the sign-in was not saved for the card's site: %+v", l)
	}
	if _, ok := credentials.WebLoginFor("evil.example"); ok {
		t.Fatal("a sign-in was stored for a site the card was not for")
	}
	_, got, _ := cards.DefaultStore.FindAnywhere(card.ID)
	if got.Resolved == nil || !strings.Contains(got.Resolved.Label, "Signed in to ads.google.com") || strings.Contains(got.Resolved.Label, "ian@") {
		t.Fatalf("the card should say a sign-in was given, and nothing of it: %+v", got.Resolved)
	}
	if w := ask(card.ID, `{"card_id":"`+card.ID+`","username":"u","password":"p"}`); w.Code != http.StatusConflict {
		t.Fatalf("a second answer = %d, want 409", w.Code)
	}
}

func TestAnUnsavedAnswerIsHeldForTheTaskNotKept(t *testing.T) {
	h := vaultServer(t)
	card, _ := cards.NewCredentialRequest("shop.example.com", "Example Shop", "")
	cards.DefaultStore.Add("mobile", card)
	w := vaultDo(h, "POST", "/v1/vault/credentials", `{"card_id":"`+card.ID+`","username":"ian","password":"one-task-only-pw","save":false}`, vaultTok)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if l, ok := credentials.WebLoginFor("shop.example.com"); !ok || l.Password != "one-task-only-pw" {
		t.Fatal("the browser should be able to use it for the task")
	}
	if len(credentials.ListItems("")) != 0 {
		t.Fatal("an unsaved sign-in must not appear in the Vault")
	}
	_, got, _ := cards.DefaultStore.FindAnywhere(card.ID)
	if got.Resolved == nil || !strings.Contains(got.Resolved.Label, "for this task only") {
		t.Fatalf("the card should say it was for this task only: %+v", got.Resolved)
	}
}

func TestTheIconRouteIsNotAnOpenFetcher(t *testing.T) {
	h := vaultServer(t)
	if w := vaultDo(h, "GET", "/v1/vault/icon?host=127.0.0.1", "", vaultTok); w.Code != 400 {
		t.Fatalf("an address is not a site: %d", w.Code)
	}
	if w := vaultDo(h, "GET", "/v1/vault/icon?host=localhost", "", vaultTok); w.Code != 400 {
		t.Fatalf("localhost is not a site: %d", w.Code)
	}
	// A real site name the owner has no login for is never fetched.
	w := vaultDo(h, "GET", "/v1/vault/icon?host=example.org", "", vaultTok)
	var out struct{ OK bool }
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || out.OK {
		t.Fatalf("a site that is not in the Vault must answer ok:false without a fetch: %d %s", w.Code, w.Body.String())
	}
}
