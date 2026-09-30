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
