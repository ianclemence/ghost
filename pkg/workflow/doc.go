// Package workflow is Ghost's thin AgentRun-style routine compiler.
//
// It does NOT vendor AgentRun's TypeScript runtime and it does NOT call
// Jev (TypeSafe System One) at runtime. Jev is a dev-time tuning partner
// only: question wording and thresholds are validated against the Jev API
// from throwaway scripts outside this repo (see /tmp/opencode/jev_probe.sh
// pattern), then compiled into versioned Routine templates + RuleBackend
// gates that run on-Pod with no network dependency.
//
// Eight node types (AgentRun-compatible JSON names): chain, route, judge,
// sift, code, map, loop_until, escalate. Every run appends to an SQLite
// ledger (ledger.go) and human lessons append to a JSONL notes file
// (notes.go). See inbox.go for the first compiled routine (nightly triage).
package workflow
