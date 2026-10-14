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
	if note := readLoopNote(msgs, true); !strings.Contains(note, "Call canvas now") {
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

func TestAPageAskedForAndOnlyDescribedIsCaught(t *testing.T) {
	ask := "Build me a landing page for a bakery called Sunrise Loaf in Chiang Mai."
	claim := "Here's your landing page for Sunrise Loaf. It's written as a single self-contained HTML file."
	if !claimsBuildWithoutBuilding(ask, claim, true, []string{"read_file"}) {
		t.Fatal("a described page with no canvas call must be caught")
	}
	if claimsBuildWithoutBuilding(ask, claim, true, []string{"read_file", "canvas"}) {
		t.Fatal("a built page is not a false claim")
	}
	if claimsBuildWithoutBuilding(ask, claim, false, nil) {
		t.Fatal("without a canvas to build with there is nothing to insist on")
	}
	if claimsBuildWithoutBuilding(ask, "What name should be on the sign, and what do you sell?", true, nil) {
		t.Fatal("a question is not a claim")
	}
	if claimsBuildWithoutBuilding("what's the weather", claim, true, nil) {
		t.Fatal("only build requests are held to this")
	}
	if !claimsBuildWithoutBuilding("สวัสดี ช่วยสร้างหน้าเว็บร้านกาแฟชื่อ Doi Chang ให้หน่อย", "ผมทำหน้าเว็บร้านกาแฟ Doi Chang ให้แล้ว", true, nil) {
		t.Fatal("the same shape in Thai must be caught")
	}
}
