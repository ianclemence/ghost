// Preview tooling: a synthetic Ghost gateway (no real person or account) so
// the console and app can be inspected. `bun scripts/preview/mockpod.ts`
const now = Math.floor(Date.now() / 1000), H = 3600, D = 86400;
const iso = (t: number) => new Date(t * 1000).toISOString();
const activity = [
  { id: "a1", event_id: "e1", seq: 5, title: "Drafted an email to Sam", kind: "draft", state: "waiting", timestamp: iso(now - 39 * 60), why: "You asked for a note about the later landing." },
  { id: "a2", event_id: "e2", seq: 4, title: "Checked TP 1352", kind: "watch", state: "changed", timestamp: iso(now - 3 * H), summary: "Departure 07:40 → 08:25", why: "You asked me to keep an eye on this flight." },
  { id: "a3", event_id: "e3", seq: 3, title: "Checked TP 1352", kind: "watch", state: "unchanged", timestamp: iso(now - 9 * H), summary: "No change" },
  { id: "a4", event_id: "e4", seq: 2, title: "Prepared your Monday brief", kind: "routine", state: "done", timestamp: iso(now - D - 8 * H) },
];
const routines = [
  { id: "ro1", title: "Monday brief", what: "Calendar, weather and anything waiting on you", kind: "routine", state: "active", schedule: "Mondays at 08:00", next_run_at: iso(now + 4 * D), run_count: 12, source: "chat", created_at: iso(now - 90 * D), updated_at: iso(now - D) },
  { id: "ro2", title: "Watch TP 1352", what: "Gate, time and status until you land", kind: "watch", state: "active", schedule: "Every 30 min until Friday", next_run_at: iso(now + 20 * 60), run_count: 18, source: "chat", created_at: iso(now - D), updated_at: iso(now - 3 * H) },
  { id: "ro3", title: "Pay rent", what: "Reminder on the 1st", kind: "reminder", state: "active", schedule: "Monthly on the 1st", next_run_at: iso(now + 2 * D), run_count: 5, source: "chat", created_at: iso(now - 150 * D), updated_at: iso(now - 28 * D) },
];
const memory = { entries: [
  { id: "m1", kind: "fact", label: "Home airport", title: "Home airport", summary: "Flies from Heathrow", domain: "travel", domain_label: "Travel", value: "Heathrow (LHR)", created_at: iso(now - 60 * D), reinforce_count: 4 },
  { id: "m2", kind: "person", label: "Sam", title: "Sam", summary: "Friend in Lisbon", domain: "people", domain_label: "People", value: "Friend in Lisbon", created_at: iso(now - 20 * D) },
  { id: "m3", kind: "preference", label: "Mornings", title: "Mornings", summary: "Prefers messages after 07:00", domain: "preferences", domain_label: "Preferences", value: "No messages before 07:00", created_at: iso(now - 40 * D) },
], notes: [], you: ["Maya"] };
const routes: Record<string, unknown> = {
  "/v1/health": { status: "ok", uptime_s: 3 * D },
  "/v1/identity": { ghost: { ghost_id: "g1", name: "Ghost", owner: "Maya" } },
  "/v1/permissions/requests": { requests: [{ id: "ap1", request_id: "r7", session_key: "main", capability: "email.send", action: "send", status: "pending", created_at: iso(now - 39 * 60), expires_at: iso(now + 3 * H), card: { title: "Send email to Sam", description: "“Hi Sam, my flight shifted by 45 minutes…” from your Gmail.", risk: "consequential", actions: [{ id: "allow_once", label: "Send", style: "primary" }, { id: "deny", label: "Don't send", style: "danger" }] } }] },
  "/v1/activity": { activity }, "/v1/routinefeed": { routines }, "/v1/routines": { routines },
  "/v1/goals": { goals: [{ id: "g1", text: "Run a half marathon in March", status: "active" }] },
  "/v1/memory/self": memory, "/v1/memory/files": { files: [{ name: "MEMORY.md" }] },
  "/v1/proactive": { proactive: { quiet: false, budget_used: 1, budget_max: 6, waiting: 0 } },
  "/v1/doctor": { status: "ok", checks: [{ name: "Model provider", status: "ok", message: "deepseek reachable" }, { name: "Memory", status: "ok", message: "3 memories" }, { name: "Scheduler", status: "ok", message: "3 routines" }] },
  "/v1/channels/status": { channels: [{ name: "telegram", enabled: false }, { name: "whatsapp", enabled: false }] },
  "/v1/pairing/devices": { devices: [] }, "/v1/model": { active: "deepseek:deepseek-flash", provider: "deepseek", presets: [] },
  "/v1/artifacts": { artifacts: [] }, "/v1/cards": { cards: [] },
};
Bun.serve({ port: Number(process.env.PORT ?? 8392), fetch(req) {
  const url = new URL(req.url);
  const cors = { "Access-Control-Allow-Origin": "*", "Access-Control-Allow-Headers": "*" };
  if (req.method === "OPTIONS") return new Response(null, { headers: cors });
  const body = url.pathname === "/v1/history" ? { messages: [], total: 0 } : (routes[url.pathname] ?? {});
  return Response.json(body, { headers: cors });
} });
console.log("mock pod up");
