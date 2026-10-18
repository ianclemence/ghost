package updaterun

import (
	"testing"
	"time"
)

func TestPendingIsReportedOnce(t *testing.T) {
	t.Setenv("GHOST_DIR", t.TempDir())
	if _, ok := TakePending(time.Hour); ok {
		t.Fatal("nothing recorded yet")
	}
	if err := WritePending(Pending{From: "v1", Channel: "mobile", ChatID: "c1"}); err != nil {
		t.Fatal(err)
	}
	p, ok := TakePending(time.Hour)
	if !ok || p.From != "v1" || p.Channel != "mobile" || p.ChatID != "c1" {
		t.Fatalf("got %+v %v", p, ok)
	}
	if _, ok := TakePending(time.Hour); ok {
		t.Fatal("a report must happen once")
	}
}

func TestStalePendingIsDroppedAndClearWorks(t *testing.T) {
	t.Setenv("GHOST_DIR", t.TempDir())
	_ = WritePending(Pending{From: "v1", At: time.Now().Add(-3 * time.Hour)})
	if _, ok := TakePending(time.Hour); ok {
		t.Fatal("an update from hours ago is not waiting on an answer")
	}
	_ = WritePending(Pending{From: "v1"})
	ClearPending()
	if _, ok := TakePending(time.Hour); ok {
		t.Fatal("cleared")
	}
}
