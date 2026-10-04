package tools

import "testing"

// Acceptance criteria for "context assembly" (see the capability spec):
//
//  1. Given a question or task, Ghost gathers relevant material from every
//     surface it has access to (memory, notes, files, the schedule, the
//     web), and names what it pulled from.
//  2. It reports what it could NOT reach. A relevant source outside its
//     access is named as unreachable — silently omitting it is how context
//     assembly lies.
//  3. Relevance is judged, not dumped: it returns what changes the decision.
//
// STATUS: NOT BUILT. context_get reads one surface (structured personal
// context) and returns entries plus unresolved conflicts. It names no
// source and never reports an inaccessible surface, so criteria 1 and 2
// cannot be proven. This test is skipped rather than deleted so the gap is
// recorded in the suite; remove the Skip once the capability exists.
func TestAcceptance_ContextAssemblyNamesSourcesAndUnreachable(t *testing.T) {
	t.Skip("context assembly is not built: no multi-surface gather, no source attribution, no unreachable reporting")

	// Once built, this must fail unless all three criteria hold:
	//
	//   - the assembled answer names every surface it consulted;
	//   - a source deliberately made inaccessible (e.g. a connector that is
	//     not connected) appears as unreachable, not omitted;
	//   - the output contains the material from the reachable sources.
}
