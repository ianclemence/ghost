package connectedapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestAKeyTheServiceRefusesIsNotConnected(t *testing.T) {
	s := serve(t, 401, `{"message":"Bad credentials"}`)
	CheckBase["github"] = s.URL
	defer delete(CheckBase, "github")
	r := Check(context.Background(), "github", "ghp_made_up", "")
	if r.Verdict != Rejected || r.Message == "" {
		t.Fatalf("a refused key must be rejected with a reason: %+v", r)
	}
}

func TestAKeyTheServiceAcceptsIsConnected(t *testing.T) {
	s := serve(t, 200, `{"login":"ian"}`)
	CheckBase["notion"] = s.URL
	defer delete(CheckBase, "notion")
	if r := Check(context.Background(), "notion", "secret_ok", ""); r.Verdict != Verified {
		t.Fatalf("%+v", r)
	}
}

func TestAServiceThatCannotBeReachedIsSavedButNotClaimedVerified(t *testing.T) {
	CheckBase["github"] = "http://127.0.0.1:1"
	defer delete(CheckBase, "github")
	r := Check(context.Background(), "github", "ghp_x", "")
	if r.Verdict != Unreachable || r.Message == "" {
		t.Fatalf("unreachable must be said honestly, not called connected: %+v", r)
	}
}

func TestAviationStackBadKeyIsCaughtEvenThoughItAnswers200(t *testing.T) {
	s := serve(t, 200, `{"error":{"code":"invalid_access_key","message":"bad"}}`)
	CheckBase["aviationstack"] = s.URL
	defer delete(CheckBase, "aviationstack")
	if r := Check(context.Background(), "aviationstack", "bad", ""); r.Verdict != Rejected {
		t.Fatalf("%+v", r)
	}
}

func TestHomeAssistantNeedsARealAddressAndAcceptsEitherOrder(t *testing.T) {
	if r := Check(context.Background(), "home-assistant", "tok", "not a url"); r.Verdict != Rejected {
		t.Fatalf("nonsense is not an address: %+v", r)
	}
	s := serve(t, 200, `{"message":"API running."}`)
	for _, pair := range [][2]string{{"longtoken", s.URL}, {s.URL, "longtoken"}} {
		if r := Check(context.Background(), "home-assistant", pair[0], pair[1]); r.Verdict != Verified {
			t.Fatalf("either order works: %+v", r)
		}
	}
	bad := serve(t, 401, `{}`)
	if r := Check(context.Background(), "home-assistant", "wrong", bad.URL); r.Verdict != Rejected {
		t.Fatalf("a refused token is rejected: %+v", r)
	}
}

func TestSplitHomeAssistantRefusesTwoAddressesOrNone(t *testing.T) {
	if _, _, ok := SplitHomeAssistant("http://a:8123", "http://b:8123"); ok {
		t.Fatal("two addresses is a mistake")
	}
	if _, _, ok := SplitHomeAssistant("tok1", "tok2"); ok {
		t.Fatal("no address is a mistake")
	}
}
