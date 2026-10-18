package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/film"
)

func filmWorkspace(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "workspace", filepath.FromSlash(film.SkillDir))
	if _, err := os.Stat(src); err != nil {
		t.Skip("the skill is not in this checkout")
	}
	ws := t.TempDir()
	dst := filepath.Join(ws, filepath.FromSlash(film.SkillDir))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-r", src, dst).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return ws
}

type nopPub struct{ got []artifacts.Input }

func (p *nopPub) Publish(in artifacts.Input) (*artifacts.Artifact, error) {
	p.got = append(p.got, in)
	return &artifacts.Artifact{ID: "a1"}, nil
}

func TestFilmToolStartsAProjectAndRefusesTheRest(t *testing.T) {
	ws := filmWorkspace(t)
	tool := NewFilmTool(ws, film.NewJobs())
	ctx := context.Background()

	res := tool.Execute(ctx, map[string]interface{}{"action": "new", "project": "Paper Boat"})
	if res.IsError || !strings.Contains(res.ForLLM, "films/paper-boat/paper-boat.html") {
		t.Fatalf("new: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(ws, "films", "paper-boat", "core.js")); err != nil {
		t.Error("the engine must sit beside the film")
	}
	if again := tool.Execute(ctx, map[string]interface{}{"action": "new", "project": "Paper Boat"}); !again.IsError {
		t.Error("a project must not be overwritten")
	}
	if bad := tool.Execute(ctx, map[string]interface{}{"action": "new", "project": "X", "template": "nope"}); !bad.IsError {
		t.Error("an unknown template must be refused")
	}
	if missing := tool.Execute(ctx, map[string]interface{}{"action": "check", "project": "ghost-film"}); !missing.IsError || !strings.Contains(missing.ForLLM, "action new") {
		t.Errorf("an unknown film should say how to start one: %+v", missing)
	}
	if esc := tool.Execute(ctx, map[string]interface{}{"action": "check", "project": "../../etc"}); !esc.IsError {
		t.Error("a path out of the workspace must be refused")
	}
	if st := tool.Execute(ctx, map[string]interface{}{"action": "status", "project": "paper-boat"}); st.IsError || !strings.Contains(st.ForLLM, "No render") {
		t.Errorf("status before any render: %+v", st)
	}
	if odd := tool.Execute(ctx, map[string]interface{}{"action": "dance", "project": "paper-boat"}); !odd.IsError {
		t.Error("an unknown action must be refused")
	}
	if strip := tool.Execute(ctx, map[string]interface{}{"action": "preview", "project": "paper-boat", "strip": "10,99"}); !strip.IsError {
		t.Error("a strip longer than 24 frames must be refused")
	}
}

func TestFilmToolFinishPublishesTheVideoAndTellsTheOwner(t *testing.T) {
	tool := NewFilmTool(t.TempDir(), film.NewJobs())
	pub := &nopPub{}
	tool.SetPublisher(pub)
	var told film.Job
	tool.OnVideo(func(_ context.Context, _ string, j film.Job) { told = j })
	tool.finish("sess", "Paper boat", film.Job{State: "done", Video: "films/paper-boat/out/paper-boat.mp4", Contact: "films/paper-boat/out/paper-boat-contact.jpg", Seconds: 6})
	if len(pub.got) != 2 || pub.got[0].Path != "films/paper-boat/out/paper-boat.mp4" || pub.got[0].SessionKey != "sess" {
		t.Fatalf("published %+v", pub.got)
	}
	if told.State != "done" {
		t.Error("the owner must be told")
	}
	pub.got = nil
	tool.finish("sess", "Paper boat", film.Job{State: "failed", Error: "boom"})
	if len(pub.got) != 0 || told.State != "failed" {
		t.Error("a failed render publishes nothing and is still reported")
	}
}
