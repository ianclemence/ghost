package skills

import (
	"errors"
	"strings"
	"testing"
)

func TestPastedSignInAddresses(t *testing.T) {
	cases := []struct {
		in, code, state, denied string
		ok                      bool
	}{
		{"http://localhost/?state=abc&code=4%2F0AX&scope=email", "4/0AX", "abc", "", true},
		{"http://127.0.0.1:8888/callback?code=xyz&state=s1", "xyz", "s1", "", true},
		{"  ?code=c&state=s  ", "c", "s", "", true},
		{"code=c&state=s", "c", "s", "", true},
		{"http://localhost/?error=access_denied&state=s", "", "", "access_denied", true},
		{"http://localhost/", "", "", "", false},
		{"", "", "", "", false},
		{"http://localhost/?code=onlycode", "onlycode", "", "", false},
	}
	for _, c := range cases {
		code, state, denied, ok := ParseSignInAddress(c.in)
		if code != c.code || state != c.state || denied != c.denied || ok != c.ok {
			t.Errorf("%q: got (%q,%q,%q,%v)", c.in, code, state, denied, ok)
		}
	}
}

func TestSignInErrorsAreInPlainWords(t *testing.T) {
	for _, e := range []string{"calendar_oauth_bad_state", "gmail_oauth_revoked_or_expired", "spotify_oauth_misconfigured", "outlook_oauth_exchange_failed", "something_unexpected"} {
		msg := FriendlySignInError(errors.New(e))
		if strings.Contains(msg, "_oauth_") || msg == "" {
			t.Errorf("%s leaked or is empty: %q", e, msg)
		}
	}
	if !strings.Contains(FriendlySignInError(errors.New("gmail_oauth_misconfigured")), "ID or secret") {
		t.Error("a rejected app must say to check the ID or secret")
	}
}

func TestEveryServiceExplainsItsSetup(t *testing.T) {
	for name, s := range OAuthServices {
		if len(s.Steps) < 3 || s.Console == "" || s.Provider == "" {
			t.Errorf("%s needs steps, a link and a provider", name)
		}
	}
	if len(oauthCompleters()) != len(OAuthServices) {
		t.Error("every service needs a way to finish its sign-in")
	}
}
