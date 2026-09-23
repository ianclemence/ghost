package calendar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
)

func testService(handler http.HandlerFunc) (*Service, *httptest.Server) {
	srv := httptest.NewServer(handler)
	svc := New(Config{
		HTTPClient: srv.Client(),
		Base:       srv.URL,
		TokenSource: func(ctx context.Context, needWrite bool) (string, error) {
			return "test-token", nil
		},
		Connected: func() bool { return true },
	})
	return svc, srv
}

func TestAgendaListsEvents(t *testing.T) {
	svc, srv := testService(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/calendars/primary/events") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing bearer token")
		}
		w.Write([]byte(`{"items":[{"id":"e1","summary":"Standup","start":{"dateTime":"2026-09-24T09:00:00Z"},"end":{"dateTime":"2026-09-24T09:30:00Z"},"htmlLink":"https://x/e1"}]}`))
	})
	defer srv.Close()
	events, res := svc.Agenda(context.Background(), 5)
	if res.Err != nil {
		t.Fatalf("agenda: %v", res.Err)
	}
	if len(events) != 1 || events[0].ID != "e1" || events[0].Summary != "Standup" {
		t.Fatalf("agenda wrong: %+v", events)
	}
	if events[0].Start != "2026-09-24T09:00:00Z" || events[0].Link != "https://x/e1" {
		t.Fatalf("event fields wrong: %+v", events[0])
	}
}

func TestAgendaUnauthorizedMapsAuth(t *testing.T) {
	svc, srv := testService(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	})
	defer srv.Close()
	_, res := svc.Agenda(context.Background(), 5)
	if res.Failure != provider.FailAuth {
		t.Fatalf("401 must map to auth, got %v", res.Failure)
	}
}

func TestAgendaUnconfiguredHonest(t *testing.T) {
	svc := New(Config{
		TokenSource: func(ctx context.Context, needWrite bool) (string, error) { return "", nil },
		Connected:   func() bool { return false },
	})
	_, res := svc.Agenda(context.Background(), 5)
	if res.Failure != provider.FailNotConfigured {
		t.Fatalf("unconnected must map to not-configured, got %v", res.Failure)
	}
}

func TestQuickAddCreatesEvent(t *testing.T) {
	svc, srv := testService(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "quickAdd") || r.URL.Query().Get("text") == "" {
			t.Errorf("quickAdd must POST with text, got %s %s", r.Method, r.URL)
		}
		w.Write([]byte(`{"id":"e9","summary":"Dentist tomorrow at 2pm","start":{"dateTime":"2026-09-24T14:00:00Z"}}`))
	})
	defer srv.Close()
	ev, res := svc.QuickAdd(context.Background(), "Dentist tomorrow at 2pm")
	if res.Err != nil {
		t.Fatalf("quickAdd: %v", res.Err)
	}
	if ev.ID != "e9" {
		t.Fatalf("created event wrong: %+v", ev)
	}
	if _, res := svc.QuickAdd(context.Background(), "  "); res.Failure != provider.FailInvalid {
		t.Fatalf("empty text must be invalid, got %v", res.Failure)
	}
}

func TestDeleteByQueryRemovesFirstMatch(t *testing.T) {
	var deleted string
	svc, srv := testService(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET":
			w.Write([]byte(`{"items":[{"id":"e7","summary":"Old meeting","start":{"date":"2026-09-20"}}]}`))
		case r.Method == "DELETE":
			deleted = r.URL.Path
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	ev, res := svc.DeleteByQuery(context.Background(), "old meeting")
	if res.Err != nil {
		t.Fatalf("delete: %v", res.Err)
	}
	if ev.ID != "e7" || !strings.HasSuffix(deleted, "/e7") {
		t.Fatalf("must delete matched id, got %+v path %q", ev, deleted)
	}
}

func TestDeleteByQueryNoMatchHonest(t *testing.T) {
	svc, srv := testService(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[]}`))
	})
	defer srv.Close()
	_, res := svc.DeleteByQuery(context.Background(), "nothing like this")
	if res.Failure != provider.FailInvalid {
		t.Fatalf("no match must be invalid (not success), got %v", res.Failure)
	}
}

func TestServiceTimeoutsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	svc := New(Config{
		HTTPClient: srv.Client(), Base: srv.URL,
		TokenSource: func(ctx context.Context, needWrite bool) (string, error) { return "t", nil },
		Connected:   func() bool { return true },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, res := svc.Agenda(ctx, 5)
	if res.Err == nil {
		t.Fatal("hung provider must surface an error")
	}
}
