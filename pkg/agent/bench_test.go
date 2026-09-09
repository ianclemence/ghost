package agent

import (
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// BenchmarkContextFilteredMemoryAssembly measures building the model-visible
// prompt from the scope-filtered memory digest for a session (the path the
// p-02 privacy invariant protects). Personal-context assembly must stay
// cheap enough to run every turn.
func BenchmarkContextFilteredMemoryAssembly(b *testing.B) {
	ws := b.TempDir()
	gid, err := ghoststate.EnsureIdentity(ws)
	if err != nil {
		b.Fatal(err)
	}
	cs, err := contexts.Open(ws, gid.GhostID)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := cs.Create(contexts.KindWork, "work"); err != nil {
		b.Fatal(err)
	}
	st, err := personalcontext.Open(ws)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		raw, _ := personalcontext.RawValue("sample value " + string(rune('a'+i%26)))
		entry := personalcontext.Entry{
			ID: "b-" + itoaTest(i), Kind: personalcontext.KindFact,
			Subject: "user", Predicate: "fact/item" + itoaTest(i), Value: raw,
			Status: personalcontext.StatusCurrent, Confidence: 0.9,
			Sources: []personalcontext.Source{{Type: personalcontext.SourceImport,
				Kind: personalcontext.SourceUserDeclared, Ref: "bench", Timestamp: time.Now().UTC()}},
		}
		if i%2 == 1 {
			entry.Scopes = []string{"context:work"}
		}
		if _, err := st.Create(entry); err != nil {
			b.Fatal(err)
		}
	}
	if err := cs.SetSessionContext("bench::work", "work"); err != nil {
		b.Fatal(err)
	}
	if err := cs.SetSessionContext("bench::home", "personal"); err != nil {
		b.Fatal(err)
	}
	cb := NewContextBuilder(ws)
	cb.SetPersonalContext(st)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scopes := cs.ScopesForSession("bench::home")
		if got := cb.BuildSystemPrompt(scopes); got == "" {
			b.Fatal("empty prompt")
		}
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
