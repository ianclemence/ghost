package agent

import (
	"sort"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

// Every tool name reachable in a live agent turn must be classified by the
// governance audit as exactly one of:
//
//  1. browser_*            → the browser gate (operation taxonomy + broker)
//  2. on the standalone consequential table (pkg/tools.FreeConsequentialTools)
//     → the broker standalone gate (main + subagent)
//  3. an explicitly audited allowed-core tool (bounded by product semantics;
//     rationale must be recorded below — none is a privileged/external/
//     process/device/communication escape hatch around the broker).
//
// A new tool that is not classified here fails the audit loudly instead of
// silently shipping an ungoverned side-effect surface.
func TestRegistryGovernanceAudit(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	names := al.tools.RegisteredNames()
	// schedule is appended to the live loop registry by the gateway
	// (cmd/ghost), not by NewAgentLoop; it is still model-reachable so the
	// audit must cover it.
	names = append(names, "schedule")
	sort.Strings(names)

	var unclassified []string
	for _, name := range names {
		if isBrowserTool(name) || isComputerTool(name) {
			continue
		}
		if tools.IsFreeConsequentialTool(name) {
			continue
		}
		if allowedCoreAudit[name] {
			continue
		}
		unclassified = append(unclassified, name)
	}
	if len(unclassified) > 0 {
		t.Fatalf("ungoverned tool(s) reachable from a turn must be classified: %v", unclassified)
	}
}

// allowedCoreAudit lists every model-reachable tool that is neither a
// browser operation nor on the standalone-consequential table, together
// with the product reason it is safe to expose ungoverned. Adding a tool
// here requires an explicit rationale — the audit is the gate.
//
// Read-only / retrieval:
//   - read_file, list_dir: bounded read of the user's own workspace. The
//     memory estate (memory journal, per-context curated notes, runtime
//     state) is additionally protected by the session scope guard, so raw
//     file access can never bypass context memory isolation — memory is
//     reached only through the scope-filtered memory tools.
//   - context_get, session_search, memory_recall, memory_curate, remember:
//     read/write Ghost's own memory/context. Memory writes are low-risk,
//     user-invoked, and bounded to Ghost's durable memory stores (never an
//     external/privileged side effect). Durable-memory truthfulness is
//     asserted separately by memory_present / RequireMemoryPersist.
//   - web_search, web_fetch: read-only external retrieval (GET / search
//     providers) with URL safety + secret checks. web_fetch is GET-only.
//   - networking: LAN/service discovery, read-only enumeration.
//   - system_status: read-only machine health (thermal zone, /proc
//     mem/load/uptime, statvfs disk sizing). It changes nothing, reaches
//     nothing external, and exists so status questions — free disk space
//     above all — are answerable on every surface without shell.
//   - vision, video_frames, doc_parser: read local media/files.
//   - weather_now, flight_status, aqi_now, currency_convert, crypto_price,
//     places_nearby: read-only lookups against provider APIs.
//   - email_search, code_search, docs_search: read-only lookups against
//     connected apps (Gmail/Outlook, GitHub, Notion). No bodies are
//     fetched beyond snippets/hits; writes use separate governed verbs
//     (email_send). Unconnected → honest refusal, never fabricated data.
//   - connections: read-only status of the connected-app catalog plus the
//     one next setup step. Never returns, stores, or asks for secrets; the
//     owner pastes secrets only into the secure screen or a CLI prompt.
//   - memory_explain: read-only receipt for one belief (verbatim quote,
//     source message id, confidence, lifecycle). Never writes memory and
//     never fabricates a quote for an older belief.
//
// Workspace / product core (no external or privileged effect):
//   - write_file, append_file, edit_file: filesystem writes confined to the
//     workspace/project root (validatePath enforces the boundary); the
//     user's own notes/artifacts.
//   - todo, schedule/cron are GOVERNED (see table); reminders via runtime
//     are not model tool calls.
//   - canvas: display-only canvas state for the console.
//   - tts: local speech synthesis out (no external side effect).
//   - compact_context, compaction, switch_lane/lane, voice_wake, profile:
//     internal runtime/context controls, no external side effect.
//   - skill_manage: edits skill files inside the skills directory. Any
//     capability a skill defines is STILL broker-gated at execution time
//     (unknown capabilities default to consequential → ask), so skill
//     creation cannot become an execution bypass.
//   - update/image_generate/exec/i2c/spi/hass/message/schedule/cron are
//     on the standalone-consequential table and governed.
//   - subagent, subagent_*, spawn, batch_delegate: delegate to a sub-loop
//     that now enforces the same consequential governance (browser gate +
//     standalone broker hook); not themselves external side effects.
var allowedCoreAudit = map[string]bool{
	// read / retrieval
	"read_file": true, "list_dir": true, "context_get": true,
	"session_search": true, "memory_recall": true, "memory_curate": true, "remember": true,
	"oracle":     true, // read-only workspace context bundling
	"web_search": true, "web_fetch": true, "networking": true,
	"vision": true, "video_frames": true, "doc_parser": true,
	"weather_now": true, "flight_status": true, "aqi_now": true,
	"currency_convert": true, "crypto_price": true, "places_nearby": true,
	"email_search": true, "code_search": true, "docs_search": true,
	"connections":    true,
	"system_status":  true,
	"memory_explain": true,
	// workspace / product core
	"write_file": true, "append_file": true, "edit_file": true,
	"todo": true, "canvas": true, "tts": true,
	"compact_context": true, "compaction": true, "switch_lane": true,
	"lane": true, "voice_wake": true, "profile": true,
	"skill_manage": true, "clarify": true,
	// subagent delegation (governance propagates)
	"subagent": true, "subagent_list": true, "subagent_read": true,
	"subagent_stop": true, "spawn": true, "batch_delegate": true,
}

var _ = strings.TrimSpace // keep strings import if assertions change
