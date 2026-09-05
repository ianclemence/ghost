package redact

import (
	"strings"
	"testing"
)

func TestMapKeys(t *testing.T) {
	in := map[string]string{
		"api_key": "sk-proj-abc123", "location": "Bangkok", "keyboard": "qwerty",
		"X-Auth-Token": "tok123456789", "count": "3",
	}
	out := Map(in)
	if out["location"] != "Bangkok" || out["keyboard"] != "qwerty" || out["count"] != "3" {
		t.Fatalf("innocent values rewritten: %v", out)
	}
	for _, k := range []string{"api_key", "X-Auth-Token"} {
		if out[k] == in[k] || !strings.HasPrefix(out[k], "«redacted") {
			t.Fatalf("secret %s not masked: %q", k, out[k])
		}
	}
}

func TestIdempotent(t *testing.T) {
	once := Text("key sk-proj-abcdefghijklmnop here")
	twice := Text(once)
	if once != twice {
		t.Fatalf("redaction not stable:\n%s\n%s", once, twice)
	}
	m := Map(map[string]string{"token": "abc"})
	if Map(m)["token"] != m["token"] {
		t.Fatal("map redaction not stable")
	}
}

func TestShapes(t *testing.T) {
	cases := []string{
		"uses sk-ant-abcdefghijklmnopqrst now",
		"token ghp_abcdefghijklmnopqrstuvwx here",
		"Bearer abcdefghijklmnop end",
		"api_key=SECRETVALUE123 ok",
		"my password: hunter2hunter ok",
		"-----BEGIN PRIVATE KEY-----\nMIIBSECRET\n-----END PRIVATE KEY-----",
	}
	for _, c := range cases {
		got := Text(c)
		if got == c {
			t.Fatalf("shape not masked: %q", c)
		}
	}
	// Prose untouched.
	prose := "Remember that I prefer tea over coffee in the morning."
	if Text(prose) != prose {
		t.Fatalf("prose rewritten: %q", Text(prose))
	}
	code := "keyboard := qwerty; keys := map[string]int{}"
	if Text(code) != code {
		t.Fatalf("code rewritten: %q", Text(code))
	}
}

func TestNested(t *testing.T) {
	v := Any(map[string]interface{}{
		"headers": map[string]interface{}{"authorization": "Bearer abcdefghijklmnop", "content-type": "text"},
		"items":   []interface{}{map[string]interface{}{"api_key": "k1234567890"}},
	})
	m := v.(map[string]interface{})
	h := m["headers"].(map[string]interface{})
	if h["content-type"] != "text" || !strings.HasPrefix(h["authorization"].(string), "«redacted") {
		t.Fatalf("nested redaction wrong: %v", m)
	}
}
