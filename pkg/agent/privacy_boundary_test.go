package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/tools"
)

// TestPrivacyNoCrossContextPromptAssembly is the deterministic, model-free
// half of the p-02 privacy invariant. A work-scoped fact must never be
// assembled into a model-visible prompt for a personal-context session,
// while remaining visible to the work session. This guards the prompt
// assembly path (digest + scoped retrieval) without depending on model
// behavior.
func TestPrivacyNoCrossContextPromptAssembly(t *testing.T) {
	ws := t.TempDir()
	gid, err := ghoststate.EnsureIdentity(ws)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := contexts.Open(ws, gid.GhostID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Create(contexts.KindWork, "work"); err != nil {
		t.Fatal(err)
	}
	workSession := "privacy::work"
	personalSession := "privacy::home"
	if err := cs.SetSessionContext(workSession, "work"); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetSessionContext(personalSession, "personal"); err != nil {
		t.Fatal(err)
	}

	st, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	mustSeed := func(id, pred, val string, scopes []string) {
		t.Helper()
		raw, err := personalcontext.RawValue(val)
		if err != nil {
			t.Fatal(err)
		}
		_, err = st.Create(personalcontext.Entry{
			ID: "seed-" + id, Kind: personalcontext.KindFact,
			Subject: "user", Predicate: pred, Value: raw,
			Status: personalcontext.StatusCurrent, Confidence: 0.95,
			Scopes: scopes,
			Sources: []personalcontext.Source{{
				Type: personalcontext.SourceImport, Kind: personalcontext.SourceUserDeclared,
				Ref: "seed:1", Timestamp: time.Now().UTC(),
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Work-scoped restricted fact + a global (shared) fact.
	mustSeed("s1", "fact/salary", "220000", []string{"context:work"})
	mustSeed("s2", "fact/color", "teal", nil)

	cb := NewContextBuilder(ws)
	cb.SetPersonalContext(st)

	personalPrompt := cb.BuildSystemPrompt(cs.ScopesForSession(personalSession))
	workPrompt := cb.BuildSystemPrompt(cs.ScopesForSession(workSession))

	if strings.Contains(personalPrompt, "220000") {
		t.Fatal("privacy breach: work-scoped salary reached the personal-context system prompt")
	}
	if !strings.Contains(personalPrompt, "teal") {
		t.Fatal("global (shared) memory must remain visible in the personal context")
	}
	if !strings.Contains(workPrompt, "220000") {
		t.Fatal("work session must still see its own scoped salary")
	}

	// Store-level scoping is the predicate every retrieval path uses.
	for _, e := range st.CurrentInScope(cs.ScopesForSession(personalSession)) {
		if personalcontext.Value(e) == "220000" {
			t.Fatal("CurrentInScope leaked work-scoped fact into personal scopes")
		}
	}
}

// TestPrivacyFileGuardProtectsMemoryStore verifies the raw file-tool
// boundary: a model session cannot read the memory journal or another
// context's curated notes through file tools, but can read its own context
// notes and normal files. This is the enforcement that closed the p-02
// leak channel (the model reading entries.jsonl directly).
func TestPrivacyFileGuardProtectsMemoryStore(t *testing.T) {
	ws := t.TempDir()
	gid, err := ghoststate.EnsureIdentity(ws)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := contexts.Open(ws, gid.GhostID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Create(contexts.KindWork, "work"); err != nil {
		t.Fatal(err)
	}
	workSession := "guard::work"
	personalSession := "guard::home"
	if err := cs.SetSessionContext(workSession, "work"); err != nil {
		t.Fatal(err)
	}
	cs.SetSessionContext(personalSession, "personal")

	guard := tools.ScopeGuard{Workspace: ws, ContextOf: cs.SessionContext}

	writeFile(ws, "personal-context/entries.jsonl", `{"id":"e1","subject":"user","predicate":"fact/salary","value":"220000","status":"current"}`+"\n")
	writeFile(ws, "knowledge/self/contexts/work/user-profile.md", "salary 220000\n")
	writeFile(ws, "knowledge/self/user-profile.md", "global profile\n")
	writeFile(ws, "notes/plan.md", "some plan\n")

	read := func(session, path string) (string, bool) {
		rt := tools.NewReadFileTool(ws, false)
		rt.SetScopeGuard(guard)
		res := rt.Execute(tools.WithSessionKey(context.Background(), session), map[string]interface{}{"path": path})
		return res.ForLLM, res.IsError
	}
	list := func(session, path string) bool {
		lt := tools.NewListDirTool(ws, false)
		lt.SetScopeGuard(guard)
		return lt.Execute(tools.WithSessionKey(context.Background(), session), map[string]interface{}{"path": path}).IsError
	}

	// Personal session must not read the memory journal (holds work facts).
	if _, err := read(personalSession, "personal-context/entries.jsonl"); !err {
		t.Fatal("personal session read the protected memory journal")
	}
	// Personal session must not read another context's curated notes.
	if _, err := read(personalSession, "knowledge/self/contexts/work/user-profile.md"); !err {
		t.Fatal("personal session read work-context curated notes")
	}
	// Work session MAY read its own curated notes and the global profile.
	if _, err := read(workSession, "knowledge/self/contexts/work/user-profile.md"); err {
		t.Fatal("work session could not read its own curated notes")
	}
	if _, err := read(workSession, "knowledge/self/user-profile.md"); err {
		t.Fatal("global profile must stay readable")
	}
	// Normal files are unaffected for everyone.
	if _, err := read(personalSession, "notes/plan.md"); err {
		t.Fatal("ordinary workspace files must remain readable")
	}
	// Protected stores cannot even be listed.
	if !list(personalSession, "personal-context") {
		t.Fatal("list_dir of the protected journal must be refused")
	}
}

// writeFile writes a file relative to the workspace under test.
func writeFile(ws, rel, content string) {
	p := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		panic(err)
	}
}
