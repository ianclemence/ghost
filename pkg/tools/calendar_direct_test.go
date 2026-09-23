package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	calprov "github.com/ianclemence/ghost/pkg/providers/calendar"
)

func testCalendarSvc(handler http.HandlerFunc) (*calprov.Service, *httptest.Server) {
	srv := httptest.NewServer(handler)
	svc := calprov.New(calprov.Config{
		HTTPClient: srv.Client(),
		Base:       srv.URL,
		TokenSource: func(ctx context.Context, needWrite bool) (string, error) {
			return "test-token", nil
		},
		Connected: func() bool { return true },
	})
	return svc, srv
}

func TestCalendarToolDirectAPI(t *testing.T) {
	svc, srv := testCalendarSvc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"id":"e1","summary":"Standup","start":{"dateTime":"2026-09-24T09:00:00Z"}}]}`))
	})
	defer srv.Close()
	tool := NewCalendarTool(t.TempDir())
	tool.newSvc = func() *calprov.Service { return svc }

	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if res.IsError {
		t.Fatalf("list failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Standup") {
		t.Fatalf("list must show events, got %q", res.ForLLM)
	}
	if len(res.Evidence) != 0 {
		t.Fatalf("read must not require evidence, got %v", res.Evidence)
	}
}

func TestCalendarToolDirectCreateEvidence(t *testing.T) {
	svc, srv := testCalendarSvc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"e9","summary":"Dentist tomorrow at 2pm"}`))
	})
	defer srv.Close()
	tool := NewCalendarTool(t.TempDir())
	tool.newSvc = func() *calprov.Service { return svc }

	res := tool.Execute(context.Background(), map[string]interface{}{"action": "create", "title": "Dentist", "when": "tomorrow at 2pm"})
	if res.IsError {
		t.Fatalf("create failed: %s", res.ForLLM)
	}
	if res.Evidence["operation"] != "create" || res.Evidence["event_id"] != "e9" {
		t.Fatalf("create must carry event evidence, got %v", res.Evidence)
	}
}

func TestCalendarToolLegacyFallback(t *testing.T) {
	// Direct API unconfigured; stubbed gcalcli answers instead.
	tool := NewCalendarTool(t.TempDir())
	var sawArgs []string
	tool.run = func(ctx context.Context, args []string) (string, error) {
		sawArgs = args
		return "ok-output", nil
	}
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if res.IsError {
		t.Fatalf("legacy fallback failed: %s", res.ForLLM)
	}
	if !strings.Contains(strings.Join(sawArgs, " "), "agenda") {
		t.Fatalf("fallback must invoke agenda, got %v", sawArgs)
	}
}

func TestCalendarToolFallbackDisabled(t *testing.T) {
	t.Setenv("GHOST_CALENDAR_GCALCLI", "0")
	tool := NewCalendarTool(t.TempDir())
	tool.run = func(ctx context.Context, args []string) (string, error) {
		return "ok-output", nil
	}
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if !res.IsError {
		t.Fatal("disabled fallback must surface the honest error, not success")
	}
	if !strings.Contains(res.ForLLM, "Calendar isn't connected") {
		t.Fatalf("must be honest product language, got %q", res.ForLLM)
	}
}
