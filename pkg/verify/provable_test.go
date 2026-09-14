package verify

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func provableEnv(t *testing.T) *Env {
	t.Helper()
	ws, err := os.MkdirTemp("", "ghost-verify-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	db, err := sql.Open("sqlite", "file:"+filepath.Join(ws, "verify.db")+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	return &Env{Workspace: ws, DB: db, Ctx: context.Background()}
}

func TestProvableChecksPass(t *testing.T) {
	env := provableEnv(t)
	for _, c := range []func(*Env) Check{
		checkBrowserSubmitEvidence, checkScreencastSingleUse,
		checkGoalFanout, checkSubagentCaps,
	} {
		got := guarded(c, env)
		if got.Outcome != Pass {
			t.Errorf("%s/%s: outcome=%s detail=%s", got.Section, got.Name, got.Outcome, got.Detail)
		}
	}
}
