package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func testServer() *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "test"}, nil)
	type in struct{}
	type out struct {
		Pong string `json:"pong"`
	}
	sdk.AddTool(s, &sdk.Tool{Name: "ping", Description: "pings"}, func(ctx context.Context, r *sdk.CallToolRequest, _ in) (*sdk.CallToolResult, out, error) {
		return nil, out{Pong: "pong"}, nil
	})
	return s
}

func tools(t *testing.T, sess *sdk.ClientSession) []string {
	t.Helper()
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	return names
}

// Both dialects are reached from a plain address, whichever the server speaks.
func TestConnectHTTPSpeaksBothDialects(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/mcp", sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return testServer() }, nil))
	mux.Handle("/sse", sdk.NewSSEHandler(func(*http.Request) *sdk.Server { return testServer() }, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, path := range []string{"/mcp", "/sse"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, sess, err := connectHTTP(ctx, srv.URL+path, nil)
		if err != nil {
			cancel()
			t.Fatalf("%s: %v", path, err)
		}
		if got := tools(t, sess); len(got) != 1 || got[0] != "ping" {
			t.Fatalf("%s: tools %v", path, got)
		}
		sess.Close()
		cancel()
	}
}

// An API key kept in the vault is sent to the server, and the config only ever
// holds a reference to it.
func TestVaultReferencedHeaderIsResolvedAndSent(t *testing.T) {
	gotAuth := ""
	mux := http.NewServeMux()
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return testServer() }, nil)
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if a := r.Header.Get("Authorization"); a != "" {
			gotAuth = a
		}
		h.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	old := HeaderSecretResolver
	HeaderSecretResolver = func(id string) string {
		if id == "mcp:demo" {
			return "Bearer s3cret-key"
		}
		return ""
	}
	defer func() { HeaderSecretResolver = old }()

	cfgHeaders := map[string]string{"Authorization": VaultRef("mcp:demo"), "X-Plain": "ok", "X-Missing": VaultRef("nope")}
	resolved := ResolveHeaders(cfgHeaders)
	if resolved["Authorization"] != "Bearer s3cret-key" || resolved["X-Plain"] != "ok" {
		t.Fatalf("resolved: %v", resolved)
	}
	if _, present := resolved["X-Missing"]; present {
		t.Fatal("an unresolvable reference must be dropped, never sent literally")
	}
	if cfgHeaders["Authorization"] != VaultRef("mcp:demo") {
		t.Fatal("resolving must not change the stored config")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, sess, err := connectHTTP(ctx, srv.URL+"/mcp", resolved)
	if err != nil {
		t.Fatal(err)
	}
	sess.Close()
	if gotAuth != "Bearer s3cret-key" {
		t.Fatalf("the server must receive the key, got %q", gotAuth)
	}
}
