// Preview tooling: headless Chromium screenshot via CDP (run with bun).
//   bun scripts/preview/shot.mjs URL OUT.png [waitMs] [width] [height] [jsToEvalBeforeShot]
import { spawn } from "node:child_process";
import { writeFileSync } from "node:fs";
const [url, out, waitMs = "6000", w = "1280", h = "800", evalJs = ""] = process.argv.slice(2);
const port = 9300 + Math.floor(Math.random() * 500);
const chrome = spawn("chromium", ["--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars",
  `--remote-debugging-port=${port}`, `--window-size=${w},${h}`, "about:blank"], { stdio: "ignore" });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let target;
for (let i = 0; i < 60 && !target; i++) { await sleep(250); try { target = (await (await fetch(`http://127.0.0.1:${port}/json`)).json()).find((t) => t.type === "page"); } catch {} }
const ws = new WebSocket(target.webSocketDebuggerUrl);
await new Promise((r) => ws.addEventListener("open", r));
let id = 0; const pending = new Map();
ws.addEventListener("message", (e) => { const m = JSON.parse(e.data); if (m.id && pending.has(m.id)) { pending.get(m.id)(m.result ?? m.error); pending.delete(m.id); }
  if (m.method === "Runtime.exceptionThrown") console.log("exception:", (m.params.exceptionDetails.exception?.description ?? m.params.exceptionDetails.text).slice(0, 300)); });
const send = (method, params = {}) => new Promise((r) => { const i = ++id; pending.set(i, r); ws.send(JSON.stringify({ id: i, method, params })); });
await send("Runtime.enable"); await send("Page.enable");
if (process.env.DARK) await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] });
await send("Emulation.setDeviceMetricsOverride", { width: +w, height: +h, deviceScaleFactor: +w > 600 ? 1 : 2, mobile: +w <= 600 });
await send("Page.navigate", { url }); await sleep(+waitMs);
if (evalJs) { await send("Runtime.evaluate", { expression: evalJs, awaitPromise: true }); await sleep(+(process.env.AFTER ?? 7000)); }
// STEPS='[{"js":"...","wait":800,"out":"a.png"},...]' walks a flow, shooting after each step.
if (process.env.STEPS) {
  for (const st of JSON.parse(process.env.STEPS)) {
    const r = await send("Runtime.evaluate", { expression: st.js, awaitPromise: true, returnByValue: true });
    if (r?.result?.value !== undefined && st.log) console.log(st.log, JSON.stringify(r.result.value));
    await sleep(st.wait ?? 800);
    const sh = await send("Page.captureScreenshot", { format: "png" });
    writeFileSync(st.out, Buffer.from(sh.data, "base64")); console.log("wrote", st.out);
  }
  ws.close(); chrome.kill(); process.exit(0);
}
const shot = await send("Page.captureScreenshot", { format: "png" });
writeFileSync(out, Buffer.from(shot.data, "base64")); console.log("wrote", out);
ws.close(); chrome.kill(); process.exit(0);
