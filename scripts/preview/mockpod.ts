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
const files = [
  { id: "f1a2b3c4d5e6", name: "Lisbon rooftops.jpg", kind: "image", mime: "image/jpeg", size: 2412000, path: "uploads/f1.jpg", source: "the app", created_at: iso(now - 2 * H) },
  { id: "a1b2c3d4e5f6", name: "Boarding pass TP1352.pdf", kind: "document", mime: "application/pdf", size: 184000, path: "uploads/a1.pdf", source: "the app", created_at: iso(now - 5 * H) },
  { id: "b2c3d4e5f6a1", name: "Q3 expenses.xlsx", kind: "spreadsheet", mime: "application/vnd.ms-excel", size: 61000, path: "uploads/b2.xlsx", source: "the terminal", created_at: iso(now - D) },
  { id: "c3d4e5f6a1b2", name: "Hotel confirmation.png", kind: "image", mime: "image/png", size: 903000, path: "uploads/c3.png", source: "the app", created_at: iso(now - D - 3 * H) },
  { id: "d4e5f6a1b2c3", name: "meeting-notes.txt", kind: "text", mime: "text/plain", size: 3200, path: "uploads/d4.txt", source: "the terminal", created_at: iso(now - 3 * D) },
  { id: "e5f6a1b2c3d4", name: "Voice memo.m4a", kind: "audio", mime: "audio/mp4", size: 1840000, path: "uploads/e5.m4a", source: "the app", created_at: iso(now - 4 * D) },
  { id: "f6a1b2c3d4e5", name: "Passport scan.jpg", kind: "image", mime: "image/jpeg", size: 1320000, path: "uploads/f6.jpg", source: "the app", created_at: iso(now - 6 * D) },
  { id: "0a1b2c3d4e5f", name: "project-archive.zip", kind: "archive", mime: "application/zip", size: 48200000, path: "uploads/0a.zip", source: "the terminal", created_at: iso(now - 9 * D) },
];
const hues: Record<string, [string, string, string]> = {
  f1a2b3c4d5e6: ["#f6c177", "#e57f84", "#5a5aa8"], c3d4e5f6a1b2: ["#cfe3d8", "#8fb8a8", "#3d5a6c"], f6a1b2c3d4e5: ["#d9d4ea", "#9a94c9", "#4a456f"],
};
function scene(id: string, w = 640, h = 480) {
  const [a, b, c] = hues[id] ?? ["#ddd", "#aaa", "#666"];
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${a}"/><stop offset=".55" stop-color="${b}"/><stop offset="1" stop-color="${c}"/></linearGradient></defs><rect width="${w}" height="${h}" fill="url(#g)"/><circle cx="${w * 0.72}" cy="${h * 0.3}" r="${h * 0.11}" fill="#fff" opacity=".8"/><path d="M0 ${h * 0.78} Q ${w * 0.25} ${h * 0.55} ${w * 0.5} ${h * 0.72} T ${w} ${h * 0.66} V ${h} H0Z" fill="#0b0b18" opacity=".35"/></svg>`;
}
const routes: Record<string, unknown> = {
  "/v1/files": { ok: true, files, retention_days: 30 },
  "/v1/health": { status: "ok", uptime_s: 3 * D },
  "/v1/identity": { ghost: { ghost_id: "g1", name: "Ghost", owner: "Maya" } },
  "/v1/permissions/grants": { grants: [
    { capability: "exec.shell", action: "exec", scope: "owner", created_at: iso(now - 3 * D), expires_at: "" },
    { capability: "schedule.create", action: "schedule", scope: "owner", created_at: iso(now - D), expires_at: "" },
    { capability: "email.read", action: "email_search", scope: "session:main", created_at: iso(now - H), expires_at: iso(now + 2 * D) },
    { capability: "calendar.modify", action: "create", scope: "contact:Sam Rivera-Thompson", created_at: iso(now - H), expires_at: "" },
  ] },
  "/v1/permissions/requests": { requests: [{ id: "ap2", request_id: "r8", session_key: "main", capability: "schedule.create", action: "schedule", status: "pending", created_at: iso(now - 5 * 60), expires_at: iso(now + 10 * 60), card: { title: "Schedule this?", description: "Ghost wants to set this up to run later, as you asked.", risk: "consequential", actions: [{ id: "allow_once", label: "Allow once", style: "primary" }, { id: "allow_task", label: "Allow for this task", style: "secondary" }, { id: "allow_always", label: "Always allow", style: "secondary" }, { id: "deny", label: "Deny", style: "danger" }] } }, { id: "ap1", request_id: "r7", session_key: "main", capability: "email.send", action: "send", status: "pending", created_at: iso(now - 39 * 60), expires_at: iso(now + 3 * H), card: { title: "Send email to Sam", description: "“Hi Sam, my flight shifted by 45 minutes…” from your Gmail.", risk: "consequential", actions: [{ id: "allow_once", label: "Send", style: "primary" }, { id: "deny", label: "Don't send", style: "danger" }] } }] },
  "/v1/activity": { activity }, "/v1/routinefeed": { routines }, "/v1/routines": { routines },
  "/v1/goals": { goals: [{ id: "g1", text: "Run a half marathon in March", status: "active" }] },
  "/v1/memory/self": memory, "/v1/memory/files": { files: [{ name: "MEMORY.md" }] },
  "/v1/proactive": { proactive: { quiet: false, budget_used: 1, budget_max: 6, waiting: 0 } },
  "/v1/doctor": { status: "ok", checks: [{ name: "Model provider", status: "ok", message: "deepseek reachable" }, { name: "Memory", status: "ok", message: "3 memories" }, { name: "Scheduler", status: "ok", message: "3 routines" }] },
  "/v1/channels/status": { channels: [{ name: "telegram", enabled: false }, { name: "whatsapp", enabled: false }] },
  "/v1/pairing/devices": { devices: [] }, "/v1/model": { active: "deepseek:deepseek-flash", provider: "deepseek", presets: [] },
  "/v1/skills": JSON.parse(await Bun.file(new URL("./fixtures/skills.json", import.meta.url)).text()),
  "/v1/artifacts": { artifacts: [] }, "/v1/cards": { cards: [] },
};
Bun.serve({ port: Number(process.env.PORT ?? 8392), fetch(req) {
  const url = new URL(req.url);
  const cors = { "Access-Control-Allow-Origin": "*", "Access-Control-Allow-Headers": "*" };
  if (req.method === "OPTIONS") return new Response(null, { headers: cors });
  const fm = /^\/v1\/files\/([^/]+)\/(thumb|preview|content)$/.exec(url.pathname);
  if (fm) {
    const f = files.find((x) => x.id === fm[1]);
    if (!f) return new Response("{}", { status: 404, headers: cors });
    if (fm[2] === "thumb") return f.kind === "image" ? new Response(scene(f.id), { headers: { ...cors, "Content-Type": "image/svg+xml" } }) : new Response("{}", { status: 404, headers: cors });
    if (fm[2] === "preview") {
      if (f.kind === "image") return Response.json({ ok: true, previewable: true, kind: "image", mime: "image/svg+xml", name: f.name, image_base64: Buffer.from(scene(f.id, 1200, 800)).toString("base64") }, { headers: cors });
      if (f.kind === "text") return Response.json({ ok: true, previewable: true, kind: "text", content: "Meeting notes, Tuesday\n\n- Sam confirmed the flight lands at 08:25.\n- Book the hotel near Alfama before Friday.\n- Ask Maya about the dinner on the 14th.\n", name: f.name }, { headers: cors });
      if (f.kind === "document") return Response.json({ ok: true, previewable: true, extracted: true, kind: "document", content: "BOARDING PASS\nTP 1352  LIS to LHR\nDeparture 08:25  Gate 21\nPassenger: Maya Okafor\nSeat 14A\n", name: f.name }, { headers: cors });
      return Response.json({ ok: true, previewable: false, reason: "There is no preview for this kind of file. Download it to open it.", kind: f.kind, name: f.name }, { headers: cors });
    }
    return Response.json({ ok: true, name: f.name, mime: f.mime, size: 12, base64: Buffer.from("preview file").toString("base64") }, { headers: cors });
  }
  const body = url.pathname === "/v1/history" ? { messages: [], total: 0 } : (routes[url.pathname] ?? {});
  return Response.json(body, { headers: cors });
} });
console.log("mock pod up");
