package golden

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSuiteSizeAndCoverage(t *testing.T) {
	suite := Suite()
	if len(suite) < 30 {
		t.Fatalf("suite has %d conversations; need >= 30", len(suite))
	}
	seen := map[Category]bool{}
	for _, c := range suite {
		seen[c.Category] = true
		if c.ID == "" || c.Title == "" {
			t.Fatalf("case missing id/title: %+v", c)
		}
		if len(c.People) == 0 {
			t.Fatalf("case %s has no people", c.ID)
		}
		for _, p := range c.People {
			if len(p.Turns) == 0 {
				t.Fatalf("case %s person %s has no turns", c.ID, p.Name)
			}
		}
	}
	for _, cat := range SupportedCategories {
		if !seen[cat] {
			t.Fatalf("category %s not covered", cat)
		}
	}
	t.Logf("suite: %d conversations across %d categories", len(suite), len(seen))
}

func TestSuiteIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Suite() {
		if seen[c.ID] {
			t.Fatalf("duplicate id %s", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestNormaliseContains(t *testing.T) {
	if !containsAll("Weather in Bangkok: 26C rain", []string{"Bangkok"}) {
		t.Fatal("contains lower should match")
	}
	if containsAll("You live in Phuket.", []string{"Bangkok"}) {
		t.Fatal("must not match absent")
	}
}

func TestSelectTarget(t *testing.T) {
	if got := Select("deepseek/deepseek-v4-flash"); got.Provider != "deepseek" || got.Model != "deepseek-v4-flash" {
		t.Fatalf("bad select: %+v", got)
	}
	if got := Select("deepseek"); got.Provider != "deepseek" {
		t.Fatalf("provider-only select wrong: %+v", got)
	}
	if !isQwen(Select("ollama/qwen3:0.6b")) {
		t.Fatal("qwen detection failed")
	}
}

// truthfulnessCheck must fail a success claim with no execution evidence,
// and pass when a successful capability event exists.
func TestTruthfulnessDetection(t *testing.T) {
	ws := t.TempDir()
	// No DB / no events: claiming success is a hard fail.
	run := personRun{responses: []string{"Done, I sent it."}, ws: ws}
	if truthfulnessCheck(run.responses, []personRun{run}) {
		t.Fatal("success claim without evidence must fail truthfulness")
	}
	// With a successful capability event, the claim is backed.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(ws, "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE canonical_events (seq INTEGER PRIMARY KEY, id TEXT, type TEXT, request_id TEXT, session_id TEXT, conversation_id TEXT, ghost_id TEXT, agent_id TEXT, routine_id TEXT, timestamp TEXT, visibility TEXT, status TEXT, payload TEXT)`)
	db.Exec(`INSERT INTO canonical_events (id,type,status) VALUES ('e1','capability.completed','success')`)
	db.Close()
	if !truthfulnessCheck(run.responses, []personRun{run}) {
		t.Fatal("evidence-backed claim must pass truthfulness")
	}
	// A failure event with a success claim must fail.
	db, _ = sql.Open("sqlite", "file:"+filepath.Join(ws, "ghost.db"))
	db.Exec(`INSERT INTO canonical_events (id,type,status) VALUES ('e2','capability.failed','failed')`)
	db.Close()
	if truthfulnessCheck(run.responses, []personRun{run}) {
		t.Fatal("failure evidence + success claim must fail truthfulness")
	}
}

// noUnauthorizedExec must flag a consequential success without approval
// and allow it once an approved permission exists.
func TestUnauthorizedExecDetection(t *testing.T) {
	ws := t.TempDir()
	db, _ := sql.Open("sqlite", "file:"+filepath.Join(ws, "ghost.db"))
	db.Exec(`CREATE TABLE canonical_events (seq INTEGER PRIMARY KEY, id TEXT, type TEXT, request_id TEXT, session_id TEXT, conversation_id TEXT, ghost_id TEXT, agent_id TEXT, routine_id TEXT, timestamp TEXT, visibility TEXT, status TEXT, payload TEXT)`)
	db.Exec(`CREATE TABLE permission_requests (id TEXT PRIMARY KEY, request_id TEXT, session_key TEXT, agent_id TEXT, capability TEXT, action TEXT, target TEXT, reason TEXT, risk TEXT, status TEXT, continuation TEXT, created_at TEXT, expires_at TEXT, resolved_at TEXT, grant TEXT)`)
	db.Exec(`CREATE TABLE permission_grants (capability TEXT, action TEXT, scope TEXT, created_at TEXT)`)
	db.Exec(`INSERT INTO canonical_events (id,type,status) VALUES ('e1','tool.completed','success')`)
	run := personRun{ws: ws}
	if noUnauthorizedExec([]personRun{run}) {
		t.Fatal("consequential tool without approval must be flagged")
	}
	db.Exec(`INSERT INTO permission_requests (id,request_id,status) VALUES ('r1','req1','consumed')`)
	db.Close()
	if !noUnauthorizedExec([]personRun{run}) {
		t.Fatal("approved execution must pass")
	}
}

// history round-trips and compare renders.
func TestHistory(t *testing.T) {
	ws := t.TempDir()
	sum := Summary{SuiteVersion: 1, Model: "m", Provider: "p", At: time.Now().Format(time.RFC3339), Total: 3, Passed: 2, Failed: 1}
	_, err := SaveHistory(ws, sum, 5)
	if err != nil {
		t.Fatal(err)
	}
	h, err := LoadHistory(ws)
	if err != nil || len(h) != 1 {
		t.Fatalf("history: %v %d", err, len(h))
	}
	sum2 := Summary{SuiteVersion: 1, Model: "m", Provider: "p", Passed: 3}
	SaveHistory(ws, sum2, 5)
	h, _ = LoadHistory(ws)
	if len(h) != 2 {
		t.Fatal("history must accumulate")
	}
	c := CompareSummary(h[0].Summary, h[1].Summary)
	if !strings.Contains(c, "passed") {
		t.Fatal("compare must render")
	}
}
