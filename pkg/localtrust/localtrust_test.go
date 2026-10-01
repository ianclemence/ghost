package localtrust

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCreatesPrivateTokenAndReusesIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	a, err := Ensure(dir)
	if err != nil || len(a) < 32 {
		t.Fatalf("Ensure: %q, %v", a, err)
	}
	st, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("token file mode %v, want 0600", st.Mode().Perm())
	}
	b, err := Ensure(dir)
	if err != nil || b != a {
		t.Errorf("a second Ensure must return the same token: %q vs %q (%v)", b, a, err)
	}
}

func TestValid(t *testing.T) {
	Accept("")
	r := httptest.NewRequest(http.MethodPost, "/v1/permissions/resolve", nil)
	r.Header.Set(Header, "")
	if Valid(r) {
		t.Fatal("with no token accepted, an empty header must not pass")
	}
	Accept("0123456789abcdef0123456789abcdef")
	if Valid(r) {
		t.Error("a missing token must not pass")
	}
	r.Header.Set(Header, "wrong")
	if Valid(r) {
		t.Error("a wrong token must not pass")
	}
	r.Header.Set(Header, "0123456789abcdef0123456789abcdef")
	if !Valid(r) {
		t.Error("the right token must pass")
	}
}

func TestApplyAndTransport(t *testing.T) {
	dir := t.TempDir()
	tok, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get(Header) }))
	defer srv.Close()
	c := &http.Client{Transport: Transport{Dir: dir}}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set(Header, "spoofed")
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	if got != tok {
		t.Errorf("server saw %q, want the stored token", got)
	}
}
