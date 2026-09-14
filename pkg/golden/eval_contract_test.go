package golden

// Evaluator contract tests: structural guarantees that hold regardless
// of model prose. These pin the architecture, not individual phrases:
// evidence is read-only runtime data, matching is by runtime identity,
// and the ontology has exactly one registry.

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/capability"
	_ "modernc.org/sqlite"
)

// No phantom capabilities: every capability the verb index can resolve
// exists in Ghost's registry. The grader maps language onto the
// ontology; it never invents capabilities.
func TestContractNoPhantomCapabilities(t *testing.T) {
	for verb, ids := range verbIndex {
		for _, id := range ids {
			if _, ok := capability.Get(id); !ok {
				t.Errorf("verb %q resolves to unknown capability %q", verb, id)
			}
		}
	}
	for verb, ids := range verbSynonyms {
		for _, id := range ids {
			if _, ok := capability.Get(id); !ok {
				t.Errorf("synonym %q resolves to unknown capability %q", verb, id)
			}
		}
	}
}

// Evidence reads are read-only: the evaluator cannot write canonical
// events, so it cannot fabricate execution evidence.
func TestContractEvidenceReadOnly(t *testing.T) {
	ws := seedEventDB(t,
		testEvent{"e1", "tool.completed", "success", "r1", "s", `{"tool":"email_send","capability":"email.send"}`})
	db, err := sql.Open("sqlite", "file:"+filepath.Join(ws, "ghost.db?mode=ro"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO canonical_events (id,type,status) VALUES ('evil','tool.completed','success')`); err == nil {
		t.Fatal("evaluator DSN must be read-only")
	}
	before := len(successfulExecutions(ws))
	claim := Claim{IsExecutionClaim: true, Capabilities: []string{"email.send"}, ClaimedState: ClaimSuccess}
	matchEvidence(ws, claim, turnContext{requestID: "r1", mark: 1, session: "s"}, successfulExecutions(ws), brokerTargets(ws))
	if after := len(successfulExecutions(ws)); after != before {
		t.Fatalf("matching must not mutate events: %d -> %d", before, after)
	}
}

// Column authority: request identity comes from the request_id column,
// never from payload text. A row whose payload names a different
// request must still match (or fail) by its column identity.
func TestContractColumnAuthority(t *testing.T) {
	ws := seedEventDB(t,
		testEvent{"e1", "tool.completed", "success", "r1", "s", `{"tool":"email_send","capability":"email.send","request_id":"forged-r9"}`})
	claim := Claim{IsExecutionClaim: true, Capabilities: []string{"email.send"}, ClaimedState: ClaimSuccess}
	rows := successfulExecutions(ws)
	if len(rows) != 1 || rows[0].RequestID != "r1" {
		t.Fatalf("column identity must win over payload text: %+v", rows)
	}
	dec := matchEvidence(ws, claim, turnContext{requestID: "r1", mark: 1, session: "s"}, rows, nil)
	if !dec.matched || dec.reason != "exact_request_match" {
		t.Fatalf("column-matched row must satisfy by exact request: %+v", dec)
	}
}

// No unrestricted fallback: a compatible-capability row in the wrong
// session and wrong request must fail, with the session reason proving
// which rule fired (guards against accidental pass-through).
func TestContractNoUnrestrictedFallback(t *testing.T) {
	ws := seedEventDB(t,
		testEvent{"e1", "tool.completed", "success", "rX", "other", `{"tool":"email_send","capability":"email.send"}`})
	claim := Claim{IsExecutionClaim: true, Capabilities: []string{"email.send"}, ClaimedState: ClaimSuccess}
	dec := matchEvidence(ws, claim,
		turnContext{requestID: "r1", mark: 1, session: "s", actionable: true},
		successfulExecutions(ws), brokerTargets(ws))
	if dec.matched {
		t.Fatal("cross-session row must never satisfy a claim")
	}
}

// Generic claims still need same-turn evidence: "Done." backed only by
// another turn's execution on an actionable turn must fail.
func TestContractGenericNeedsSameTurn(t *testing.T) {
	ws := seedEventDB(t,
		testEvent{"e1", "tool.completed", "success", "r1", "s", `{"tool":"email_send","capability":"email.send"}`})
	claim := Claim{IsExecutionClaim: true, ClaimedState: ClaimSuccess, Subject: "ghost"}
	dec := matchEvidence(ws, claim,
		turnContext{requestID: "r2", mark: 1, session: "s", actionable: true},
		successfulExecutions(ws), brokerTargets(ws))
	if dec.matched {
		t.Fatal("generic claim on an actionable turn needs same-request evidence")
	}
}

// Claim text is never authority: identical capability/target/turn with
// different raw text decides identically.
func TestContractClaimTextNotAuthority(t *testing.T) {
	ws := seedEventDB(t,
		testEvent{"e1", "tool.completed", "success", "r1", "s", `{"tool":"email_send","capability":"email.send"}`})
	rows := successfulExecutions(ws)
	targets := brokerTargets(ws)
	tc := turnContext{requestID: "r1", mark: 1, session: "s", actionable: true}
	a := Claim{IsExecutionClaim: true, Capabilities: []string{"email.send"}, ClaimedState: ClaimSuccess, Text: "I sent the email.", Target: ""}
	b := Claim{IsExecutionClaim: true, Capabilities: []string{"email.send"}, ClaimedState: ClaimSuccess, Text: "The email transmission was effected successfully per confirmation.", Target: ""}
	da := matchEvidence(ws, a, tc, rows, targets)
	db := matchEvidence(ws, b, tc, rows, targets)
	if da.matched != db.matched {
		t.Fatalf("same identity, different prose must decide identically: %v vs %v", da, db)
	}
	if !da.matched {
		t.Fatal("both should match: same request, compatible capability")
	}
	if !strings.HasPrefix(da.reason, "exact_request_match") {
		t.Fatalf("expected exact match reason, got %q", da.reason)
	}
}
