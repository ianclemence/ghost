// Package golden implements the Ghost Golden Conversation Suite: a
// model-agnostic, repeatable evaluation of realistic natural-language
// interactions against the REAL Ghost runtime.
//
// Two guarantees are asserted per conversation:
//  1. Runtime correctness — what Ghost claims actually happened (memory,
//     permissions, routines, events, evidence).
//  2. Model behavior quality — the model understands the user, does not
//     fabricate tool results, respects Ghost's semantics, and stays
//     truthful.
//
// Every conversation runs from a fresh, isolated workspace through the
// same AgentLoop/ProcessDirect path a real user exercises. Nothing here
// bypasses the runtime, the permission broker, or capability dispatch.
//
// Qwen is a SUPPORTED target but is intentionally NOT run by this task
// (too slow on the development appliance); selecting it reports
// SUPPORTED/NOT RUN rather than a pass or fail.
package golden

// SuiteVersion identifies the expected semantics of the conversations.
// Bump when a case's expected behavior intentionally changes so a
// regression can be distinguished from a spec change.
const SuiteVersion = 1

// Category groups golden conversations.
type Category string

const (
	CatConversation     Category = "conversation"
	CatMemory           Category = "memory"
	CatCorrection       Category = "correction"
	CatAmbiguity        Category = "ambiguity"
	CatPermission       Category = "permission"
	CatDenial           Category = "denial"
	CatRoutines         Category = "routines"
	CatOffline          Category = "offline"
	CatToolFailure      Category = "tool_failure"
	CatProvider         Category = "provider"
	CatContradiction    Category = "contradiction"
	CatTruthfulness     Category = "truthfulness"
	CatContextIsolation Category = "context_isolation"
	CatCrossUser        Category = "cross_user"
)

// SupportedCategories lists categories the suite covers.
var SupportedCategories = []Category{
	CatConversation, CatMemory, CatCorrection, CatAmbiguity, CatPermission,
	CatDenial, CatRoutines, CatOffline, CatToolFailure, CatProvider,
	CatContradiction, CatTruthfulness, CatContextIsolation, CatCrossUser,
}

// Fixture selects a simulated provider/tool for a conversation so no
// destructive real-world side effect can occur while the runtime contract
// (tool dispatch, permission, evidence, events) is fully preserved.
type Fixture string

const (
	FixtureNone        Fixture = ""
	FixtureWeatherOK   Fixture = "weather:ok"
	FixtureWeatherFail Fixture = "weather:fail"
	FixtureWeatherBad  Fixture = "weather:malformed"
	FixtureLight       Fixture = "skill:light" // consequential device fixture
)

// MemorySeed pre-seeds a memory in the person's fresh store.
type MemorySeed struct {
	Kind      string
	Predicate string
	Value     string
	Scopes    []string
}

// Turn is one natural-language user message.
type Turn struct {
	User string
}

// Person is one user script within a conversation. Each person runs in
// its own fresh workspace (or shares one when Case.SharedWorkspace), so
// cross-user cases share no state by default.
type Person struct {
	Name         string
	Session      string
	Context      string // context id for the session; default "personal"
	SeedMemories []MemorySeed
	Turns        []Turn
}

// Match is a loose memory-row match (substring on predicate+value).
type Match struct {
	Predicate string
	Value     string
}

// Expect holds the semantic/behavioral assertions for a conversation.
type Expect struct {
	// LastResponseContains / NotContains: assessed on the FINAL turn of
	// the last person (most conversations are single-person).
	LastResponseContains    []string
	LastResponseNotContains []string
	// AnyResponseContains: true if any turn's response matched all terms.
	AnyResponseContains []string
	// AskClarification: one of the responses was a clarifying question and
	// did NOT execute an action.
	AskClarification bool
	// MemoryPresent: rows exist (predicate/value substrings) after run.
	MemoryPresent []Match
	// MemorySuperseded: a superseded row exists for predicate/value.
	MemorySuperseded []Match
	// MemoryAbsent: no current row matches.
	MemoryAbsent []Match
	// RequireMemoryPersist: if the model claims it remembered something,
	// a matching memory row MUST exist (memory claim truthfulness hard gate).
	RequireMemoryPersist bool
	// MemoryValueCurrent: final current value for a predicate must contain.
	MemoryValueCurrent []Match
	// RoutineCount is the expected number of routines after the run.
	RoutineCount    int
	RoutineCountSet bool
	// Grant / deny expectations (permission DB).
	ExpectGrant     bool
	GrantCapability string
	GrantAction     string
	GrantScope      string
	ExpectDenied    bool
	// RequiredCanonicalEvents: each type must appear in the run's DB.
	RequiredEvents []string
	// RequiredNoEvents: none of these types may appear.
	NoEvents []string
	// ClarifyResumedExactlyOnce: clarification ask -> resume produced
	// exactly one execution and no duplicate pending.
	ClarifyResumedExactlyOnce bool
	// DuplicateRoutineRejected: the suite's duplicate routine request must
	// NOT increase routine count beyond the original.
	DuplicateRoutineRejected bool
	// NoFalseSuccess (HARD): assistant never claims an action completed
	// without execution evidence in that person's store.
	NoFalseSuccess bool
	// NoUnauthorizedExec (HARD): no consequential tool ran without an
	// approved permission record.
	NoUnauthorizedExec bool
	// CrossUserAbsentValues: after all people run, the LAST person's store
	// must not contain these seed values (stored by an earlier person).
	CrossUserAbsentValues []string
}

// Conversation is one canonical golden conversation.
type Conversation struct {
	ID       string
	Category Category
	Title    string
	Severity string // "high" cases gate overall on hard-fail categories
	Fixture  Fixture
	Offline  bool
	// SharedWorkspace runs all People against the SAME fresh workspace
	// (different sessions/contexts), for context-isolation scenarios.
	SharedWorkspace bool
	People          []Person
	Expect          Expect
}

// DefaultPerson returns a single-person conversation scaffold.
func onePerson(name, session string, turns ...Turn) []Person {
	return []Person{{Name: name, Session: session, Turns: turns}}
}

// mem builds a MemorySeed for a "user" subject fact/preference.
func mem(kind, predicate, value string) MemorySeed {
	return MemorySeed{Kind: kind, Predicate: predicate, Value: value}
}

// WithScope returns a copy scoped to a context (e.g. "context:work").
func (m MemorySeed) WithScope(scope string) MemorySeed {
	m.Scopes = append(m.Scopes, scope)
	return m
}

// pref is a shorthand for a user preference memory.
func pref(predicate, value string) MemorySeed {
	return mem("preference", predicate, value)
}

// turn is shorthand for a Turn.
func turn(u string) Turn { return Turn{User: u} }
