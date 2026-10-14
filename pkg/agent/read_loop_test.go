package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

func callMsg(name, args string) providers.Message {
	return providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: name + args, Type: "function", Function: &providers.FunctionCall{Name: name, Arguments: args}}}}
}

func TestReadLoopNoteCatchesTheSameReadTwice(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "q"},
		callMsg("read_file", `{"path":"notes/a.md"}`),
		callMsg("read_file", `{"path":"notes/a.md"}`),
	}
	if note := readLoopNote(msgs, false); !strings.Contains(note, "already made this exact read_file call") {
		t.Fatalf("a repeated read went unnoticed: %q", note)
	}
}

func TestReadLoopNoteTellsAReaderToBuild(t *testing.T) {
	msgs := []providers.Message{{Role: "user", Content: "build a page"}}
	for _, p := range []string{"SKILL.md", "references/a.md", "references/b.md"} {
		msgs = append(msgs, callMsg("read_file", `{"path":"skills/canvas-design/`+p+`"}`))
	}
	if note := readLoopNote(msgs, true); !strings.Contains(note, "Build the page now") {
		t.Fatalf("three skill reads and no page: %q", note)
	}
	if note := readLoopNote(msgs, false); note != "" {
		t.Fatalf("without a canvas to build with there is nothing to say: %q", note)
	}
}

func TestReadLoopNoteLeavesNormalWorkAlone(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "q"},
		callMsg("read_file", `{"path":"skills/canvas-design/SKILL.md"}`),
		callMsg("canvas", `{"title":"x"}`),
		callMsg("read_file", `{"path":"notes/a.md"}`),
	}
	if note := readLoopNote(msgs, true); note != "" {
		t.Fatalf("ordinary work drew a note: %q", note)
	}
}

func TestReadLoopNoteOnlyLooksAtTheCurrentTurn(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "one"},
		callMsg("read_file", `{"path":"a"}`),
		{Role: "user", Content: "two"},
		callMsg("read_file", `{"path":"a"}`),
	}
	if note := readLoopNote(msgs, false); note != "" {
		t.Fatalf("a read in an earlier turn counted: %q", note)
	}
}
