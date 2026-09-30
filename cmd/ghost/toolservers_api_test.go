package main

import "testing"

func TestToolServerAddressesAreWebAddressesOnly(t *testing.T) {
	good := []string{"https://mcp.example.com/mcp", "https://api.thing.io/sse", "http://192.168.0.20:3000/mcp", "http://homeserver.local/mcp", "http://localhost:8080/mcp"}
	for _, u := range good {
		if _, ok := toolServerURLOK(u); !ok {
			t.Errorf("%s should be accepted", u)
		}
	}
	bad := []string{"", "mcp.example.com", "http://evil.example.com/mcp", "ftp://x/y", "file:///etc/passwd", "npx -y some-server", "javascript:alert(1)", "http://8.8.8.8/mcp"}
	for _, u := range bad {
		if _, ok := toolServerURLOK(u); ok {
			t.Errorf("%q must be refused: plain http off the home network, or not a web address", u)
		}
	}
}

func TestToolServerNames(t *testing.T) {
	for _, n := range []string{"notion", "my-server_2", "a"} {
		if !toolServerNameRE.MatchString(n) {
			t.Errorf("%q is a fine name", n)
		}
	}
	for _, n := range []string{"", "Has Space", "../etc", "-lead", "toolongtoolongtoolongtoolongtoolongxxxxx"} {
		if toolServerNameRE.MatchString(n) {
			t.Errorf("%q must be refused", n)
		}
	}
}

func TestAKeyNeverAppearsInAnErrorMessage(t *testing.T) {
	if got := scrubKey("dial failed for Bearer sk-live-123", "sk-live-123"); got != "dial failed for Bearer «key»" {
		t.Fatalf("got %q", got)
	}
}
