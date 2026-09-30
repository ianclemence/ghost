package push

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const tokA = "ExponentPushToken[aaaaaaaaaaaaaaaaaaaaaa]"
const tokB = "ExponentPushToken[bbbbbbbbbbbbbbbbbbbbbb]"

func TestRegisterValidatesAndReplaces(t *testing.T) {
	s := testStore(t)
	if err := s.Register("phone", "not-a-token", "ios"); err == nil {
		t.Fatal("a malformed token must be refused")
	}
	if err := s.Register("", tokA, "ios"); err == nil {
		t.Fatal("a token needs a device")
	}
	s.Register("phone", tokA, "ios")
	s.Register("phone", tokB, "ios")
	if got := s.Tokens(); len(got) != 1 || got[0] != tokB {
		t.Fatalf("a device keeps one token, the newest: %v", got)
	}
	s.Remove("phone")
	if len(s.Tokens()) != 0 {
		t.Fatal("removing a device forgets its token")
	}
}

type expo struct {
	mu   sync.Mutex
	reqs []map[string]interface{}
	resp string
}

func (e *expo) server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var msgs []map[string]interface{}
		json.Unmarshal(b, &msgs)
		e.mu.Lock()
		e.reqs = append(e.reqs, msgs...)
		e.mu.Unlock()
		w.Write([]byte(e.resp))
	}))
}

func TestNotifySendsFixedCopyOnly(t *testing.T) {
	s := testStore(t)
	s.Register("phone", tokA, "ios")
	e := &expo{resp: `{"data":[{"status":"ok","id":"1"}]}`}
	srv := e.server(t)
	defer srv.Close()
	t.Setenv("GHOST_EXPO_PUSH_URL", srv.URL)
	n := NewNotifier(s)

	sent, err := n.Notify(context.Background(), Approval)
	if err != nil || sent != 1 {
		t.Fatalf("sent=%d err=%v", sent, err)
	}
	m := e.reqs[0]
	if m["to"] != tokA || m["body"] != "Ghost needs your OK." || m["title"] != "Ghost" {
		t.Fatalf("unexpected payload: %v", m)
	}
	data := m["data"].(map[string]interface{})
	if data["anchor"] != "approvals" || len(data) != 2 {
		t.Fatalf("data must carry only the category and where a tap lands: %v", data)
	}
	raw, _ := json.Marshal(m)
	if strings.Contains(string(raw), "content") || strings.Contains(string(raw), "message") {
		t.Fatalf("a notification must never carry message content: %s", raw)
	}
}

func TestBurstsCoalesceAndCategoriesAreIndependent(t *testing.T) {
	s := testStore(t)
	s.Register("phone", tokA, "ios")
	e := &expo{resp: `{"data":[{"status":"ok"}]}`}
	srv := e.server(t)
	defer srv.Close()
	t.Setenv("GHOST_EXPO_PUSH_URL", srv.URL)
	n := NewNotifier(s)
	now := time.Now()
	n.now = func() time.Time { return now }

	n.Notify(context.Background(), Update)
	n.Notify(context.Background(), Update)
	n.Notify(context.Background(), Question)
	if len(e.reqs) != 2 {
		t.Fatalf("two updates in a moment are one nudge; a question is separate; got %d pushes", len(e.reqs))
	}
	now = now.Add(2 * time.Minute)
	n.Notify(context.Background(), Update)
	if len(e.reqs) != 3 {
		t.Fatal("after the gap, the next update goes out")
	}
}

func TestDeadTokensArePruned(t *testing.T) {
	s := testStore(t)
	s.Register("gone", tokA, "ios")
	s.Register("alive", tokB, "android")
	e := &expo{resp: `{"data":[{"status":"error","message":"x","details":{"error":"DeviceNotRegistered"}},{"status":"ok"}]}`}
	srv := e.server(t)
	defer srv.Close()
	t.Setenv("GHOST_EXPO_PUSH_URL", srv.URL)
	sent, _ := NewNotifier(s).Notify(context.Background(), Question)
	if sent != 1 {
		t.Fatalf("one device accepted, got %d", sent)
	}
	if got := s.Tokens(); len(got) != 1 || got[0] != tokB {
		t.Fatalf("an uninstalled app's token must be dropped, got %v", got)
	}
}

func TestNoDevicesNoTraffic(t *testing.T) {
	s := testStore(t)
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	t.Setenv("GHOST_EXPO_PUSH_URL", srv.URL)
	if n, _ := NewNotifier(s).Notify(context.Background(), Update); n != 0 || hit {
		t.Fatal("with no registered device nothing may be sent")
	}
}

func TestServiceErrorsAreReportedNotSwallowed(t *testing.T) {
	s := testStore(t)
	s.Register("phone", tokA, "ios")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	t.Setenv("GHOST_EXPO_PUSH_URL", srv.URL)
	if _, err := NewNotifier(s).Notify(context.Background(), Update); err == nil {
		t.Fatal("an outage must surface as an error")
	}
}

func TestReminderCopyAndCategory(t *testing.T) {
	s := testStore(t)
	s.Register("phone", tokA, "ios")
	e := &expo{resp: `{"data":[{"status":"ok"}]}`}
	srv := e.server(t)
	defer srv.Close()
	t.Setenv("GHOST_EXPO_PUSH_URL", srv.URL)
	if n, _ := NewNotifier(s).Notify(context.Background(), Reminder); n != 1 {
		t.Fatal("a reminder must be sent")
	}
	if e.reqs[0]["body"] != "Ghost has a reminder for you." {
		t.Fatalf("reminder copy: %v", e.reqs[0]["body"])
	}
}
