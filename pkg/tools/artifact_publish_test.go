package tools

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/artifacts"
	_ "modernc.org/sqlite"
)

func testArtifactStore(t *testing.T, ws string) *artifacts.Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := artifacts.NewStore(db, ws)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func ctxWithSession(session string) context.Context {
	return WithSessionKey(context.Background(), session)
}

func TestPublishArtifactToolValidates(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "r.md"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewPublishArtifactTool(testArtifactStore(t, ws), ws)
	res := tool.Execute(ctxWithSession("mobile:default"), map[string]interface{}{
		"title": "R", "kind": "file", "path": "r.md",
	})
	if res.IsError {
		t.Fatalf("valid publish refused: %s", res.ForLLM)
	}
	bad := tool.Execute(ctxWithSession("mobile:default"), map[string]interface{}{
		"title": "R", "kind": "file", "path": "../evil.md",
	})
	if !bad.IsError {
		t.Fatalf("traversal publish must fail")
	}
}

func TestPublishArtifactToolFailsClosed(t *testing.T) {
	tool := NewPublishArtifactTool(nil, t.TempDir())
	res := tool.Execute(ctxWithSession("s"), map[string]interface{}{
		"title": "R", "kind": "text", "text": "hi",
	})
	if !res.IsError {
		t.Fatalf("unwired store must refuse, not publish")
	}
}
