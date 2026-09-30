package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/tools"
	"github.com/ianclemence/ghost/pkg/uploads"
)

// An attachment must be openable by the tool the model is told to use. Before
// uploads had a home, the model was pointed at a temp path its own file tools
// refused.
func TestAttachedFilesAreReadableByTheToolsTheyPointTo(t *testing.T) {
	ws := t.TempDir()
	cb := NewContextBuilder(ws)

	csv, err := uploads.Save(ws, []byte("item,cost\ncoffee,4\ntea,3\n"), "spend.csv", "text/csv", "mobile")
	if err != nil {
		t.Fatal(err)
	}
	tag := cb.attachmentTag(ws+"/"+csv.Path, []byte("item,cost\ncoffee,4\ntea,3\n"))
	if !strings.Contains(tag, "doc_parser") || !strings.Contains(tag, csv.Path) || strings.Contains(tag, ws) {
		t.Fatalf("spreadsheet tag must name doc_parser and the workspace-relative path, got %q", tag)
	}
	res := tools.NewDocParserTool(ws).Execute(context.Background(), map[string]interface{}{"file_path": csv.Path})
	if res.IsError || !strings.Contains(res.ForLLM+res.ForUser, "coffee") {
		t.Fatalf("doc_parser must open the stored upload, got %+v", res)
	}

	txt, _ := uploads.Save(ws, []byte("remember the milk"), "note.txt", "", "mobile")
	if tag := cb.attachmentTag(ws+"/"+txt.Path, []byte("remember the milk")); !strings.Contains(tag, "read_file") {
		t.Fatalf("text tag must name read_file: %q", tag)
	}
	rf := tools.NewReadFileTool(ws, true).Execute(context.Background(), map[string]interface{}{"path": txt.Path})
	if rf.IsError || !strings.Contains(rf.ForLLM, "milk") {
		t.Fatalf("read_file must open the stored upload, got %+v", rf)
	}
}

func TestAudioAndUnknownFilesAreNotClaimedReadable(t *testing.T) {
	cb := NewContextBuilder(t.TempDir())
	if tag := cb.attachmentTag("/x/a.mp3", []byte("ID3\x03\x00\x00\x00\x00\x00\x00")); !strings.Contains(tag, "can't listen") {
		t.Fatalf("audio must be declared unreadable, got %q", tag)
	}
	if tag := cb.attachmentTag("/x/blob.xyz", []byte{0, 1, 2, 3, 4, 5, 6, 7}); !strings.Contains(tag, "no tool") {
		t.Fatalf("unknown files must be declared unreadable, got %q", tag)
	}
}

// A screenshot the owner asked for is kept with their files and never leaks an
// internal path into what the model says.
func TestScreenshotIsDeliveredToTheOwner(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)
	png := filepath.Join(t.TempDir(), "shot.png")
	os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89"), 0o600)

	al.deliverScreenshot("main", png)

	got := uploads.List(ws)
	if len(got) != 1 || got[0].Kind != "image" || got[0].Source != "browser" {
		t.Fatalf("the screenshot must be stored as an image upload, got %+v", got)
	}
}

func TestReminderTextIsShortAndDeterministic(t *testing.T) {
	for in, want := range map[string]string{
		"Stretch.":                 "Reminder: stretch.",
		"stretch. confirm briefly": "Reminder: stretch. confirm briefly.",
		"Remind me to call Jas":    "Reminder: call Jas.",
		"Pay rent!":                "Reminder: pay rent.",
		"NASA call at 3":           "Reminder: NASA call at 3.",
		"":                         "Reminder.",
	} {
		if got := ReminderText(in); got != want {
			t.Errorf("ReminderText(%q) = %q, want %q", in, got, want)
		}
	}
}

// A timer's message must land in the shared conversation, stored and live.
// It used to stay in a private automation session and reach no one.
func TestDeliverToOwnerStoresAndPublishes(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	ch, unsub := al.Bus().SubscribeOutbound("test", false, 8)
	defer unsub()

	al.DeliverToOwner("mobile", "default", "Reminder: stretch.", map[string]interface{}{"reminder": true})

	select {
	case m := <-ch:
		if m.Content != "Reminder: stretch." || m.Metadata["session_id"] != "main" || m.Metadata["reminder"] != true {
			t.Fatalf("unexpected outbound: %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing was published to the owner")
	}
	found := false
	for _, h := range al.sessions.GetHistory("main") {
		if h.Role == "assistant" && h.Content == "Reminder: stretch." {
			found = true
		}
	}
	if !found {
		t.Fatal("the reminder must be stored in the shared conversation so every surface finds it in history")
	}
}

func TestLateNoteOnlyWhenActuallyLate(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Bangkok")
	due := time.Date(2026, 9, 30, 14, 39, 0, 0, loc)
	if n := LateNote(due, due.Add(2*time.Minute), "Asia/Bangkok"); n != "" {
		t.Fatalf("two minutes late is on time, got %q", n)
	}
	if n := LateNote(due, due.Add(3*time.Hour), "Asia/Bangkok"); n != "This was due at 2:39 PM; I was offline then." {
		t.Fatalf("same-day lateness: %q", n)
	}
	if n := LateNote(due, due.Add(20*time.Hour), "Asia/Bangkok"); !strings.Contains(n, "yesterday, 2:39 PM") {
		t.Fatalf("next-morning lateness must say when it was due: %q", n)
	}
	if n := LateNote(due, due.Add(72*time.Hour), "Asia/Bangkok"); !strings.Contains(n, "Sep 30") {
		t.Fatalf("days late must name the date: %q", n)
	}
	if n := LateNote(due, due.Add(time.Hour), "Not/AZone"); n == "" {
		t.Fatal("an unknown zone must still produce a note, in UTC")
	}
}

// A job queued while a drain is running must survive it. The drain used to
// rewrite the queue from a stale snapshot, erasing the new record: Ghost said
// "saved" for a memory it never stored.
func TestDeferredQueueKeepsJobsQueuedDuringADrain(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	al.deferExtraction("main", "r1", "first fact", "cli")
	snapshot := al.readDeferredQueue()
	// A new message arrives while the drain is still working through the snapshot.
	al.deferExtraction("main", "r2", "second fact", "cli")
	// The drain finishes the first job and commits.
	al.commitDeferred(snapshot, nil)
	left := al.readDeferredQueue()
	if len(left) != 1 || left[0].RequestID != "r2" {
		t.Fatalf("the job queued during the drain must remain, got %+v", left)
	}
}

// Ghost tells the owner once and keeps its word about not repeating itself,
// even across a restart.
func TestAnnounceSaysItOnceAndRemembersAcrossRestarts(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)
	ch, unsub := al.Bus().SubscribeOutbound("t", false, 16)
	defer unsub()
	if !al.Announce("storage-critical", "I'm almost out of storage.", time.Hour, true) {
		t.Fatal("the first announcement must be said")
	}
	if al.Announce("storage-critical", "I'm almost out of storage.", time.Hour, true) {
		t.Fatal("the same thing must not be repeated inside its cooldown")
	}
	select {
	case m := <-ch:
		if m.Metadata["announce"] != "storage-critical" || m.Metadata["urgent"] != true || m.Metadata["session_id"] != "main" {
			t.Fatalf("announcement metadata: %+v", m.Metadata)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing published")
	}
	// A new process (same workspace) still remembers.
	al2 := newTestAgentLoop(t, ws)
	if al2.Announce("storage-critical", "I'm almost out of storage.", time.Hour, true) {
		t.Fatal("a restart must not make Ghost repeat itself")
	}
	if !al2.Announce("pod-hot", "The Pod is running hot.", time.Hour, true) {
		t.Fatal("a different matter is announced")
	}
}
