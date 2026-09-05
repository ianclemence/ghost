package skills

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVerificationPacketNoSecrets(t *testing.T) {
	p := VerificationPacketFor([]string{"https://relay.example.com/oauth/calendar/callback"})
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	// No top-level (or nested data_handling) key may carry credential
	// material. Descriptive prose about redaction is fine; key names
	// that would hold secrets are not.
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch tv := v.(type) {
		case map[string]interface{}:
			for k, val := range tv {
				lk := strings.ToLower(k)
				if lk == "client_secret" || lk == "refresh_token" || lk == "access_token" || lk == "authorization_code" {
					t.Fatalf("packet carries secret key %q", k)
				}
				walk(val)
			}
		case []interface{}:
			for _, e := range tv {
				walk(e)
			}
		}
	}
	walk(decoded)
	if len(p.Scopes) != 2 || len(p.Checklist) == 0 {
		t.Fatal("packet must carry scopes + checklist")
	}
	if len(p.RedirectURIs) != 1 {
		t.Fatal("redirect URIs must pass through")
	}
}

func TestVerificationPacketNoFabricatedURIs(t *testing.T) {
	p := VerificationPacketFor(nil)
	if len(p.RedirectURIs) != 0 {
		t.Fatal("must not fabricate redirect URIs")
	}
}
