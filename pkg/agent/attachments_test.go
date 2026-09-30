package agent

import (
	"context"
	"strings"
	"testing"

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
