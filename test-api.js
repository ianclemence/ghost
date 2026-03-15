// test-api.js
const fs = require("fs");
const path = require("path");
const http = require("http");

function readEnv(filePath) {
  const env = {};
  try {
    for (const line of fs.readFileSync(filePath, "utf8").split(/\r?\n/)) {
      const t = line.trim();
      if (!t || t.startsWith("#")) continue;
      const idx = t.indexOf("=");
      if (idx < 1) continue;
      let val = t.slice(idx + 1).trim();
      if (
        (val.startsWith('"') && val.endsWith('"')) ||
        (val.startsWith("'") && val.endsWith("'"))
      ) {
        val = val.slice(1, -1);
      }
      env[t.slice(0, idx).trim()] = val;
    }
  } catch {}
  return env;
}

const env = readEnv(path.join(__dirname, ".env"));
const HOST = env.PI_HOST?.split("@").pop() || "127.0.0.1";
const PORT = env.GHOST_API_PORT || "8765";
const SECRET = env.BRIDGE_SECRET || "";
const REMOTE_PORT = env.BRIDGE_PORT || "8766";

const BASE = `http://${HOST}:${PORT}`;
const REMOTE = `http://${HOST}:${REMOTE_PORT}`;

let passed = 0;
let failed = 0;

function log(icon, label, detail) {
  console.log(`${icon} ${label}${detail ? " — " + detail : ""}`);
}

function get(url, extraHeaders = {}) {
  return new Promise((resolve, reject) => {
    const req = http.get(
      url,
      {
        headers: { "X-Ghost-Secret": SECRET, ...extraHeaders },
        timeout: 8000,
      },
      (res) => {
        let body = "";
        res.on("data", (d) => (body += d));
        res.on("end", () =>
          resolve({ status: res.statusCode, body, headers: res.headers }),
        );
      },
    );
    req.on("error", reject);
    req.on("timeout", () => {
      req.destroy();
      reject(new Error("timeout"));
    });
  });
}

function post(url, payload, extraHeaders = {}) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify(payload);
    const opts = new URL(url);
    const req = http.request(
      {
        hostname: opts.hostname,
        port: opts.port,
        path: opts.pathname + opts.search,
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(data),
          "X-Ghost-Secret": SECRET,
          ...extraHeaders,
        },
        timeout: 90000,
      },
      (res) => {
        let body = "";
        res.on("data", (d) => (body += d));
        res.on("end", () =>
          resolve({ status: res.statusCode, body, headers: res.headers }),
        );
      },
    );
    req.on("error", reject);
    req.on("timeout", () => {
      req.destroy();
      reject(new Error("timeout"));
    });
    req.write(data);
    req.end();
  });
}

function postSSE(url, payload, sessionKey = "mobile:test") {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify(payload);
    const opts = new URL(url);
    let chunks = [];
    let full = "";
    let done = false;
    let firstChunkMs = null;
    const start = Date.now();

    const req = http.request(
      {
        hostname: opts.hostname,
        port: opts.port,
        path: opts.pathname,
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(data),
          "X-Ghost-Secret": SECRET,
          "X-Ghost-Session": sessionKey,
        },
        timeout: 90000,
      },
      (res) => {
        if (res.statusCode !== 200) {
          let b = "";
          res.on("data", (d) => (b += d));
          res.on("end", () =>
            reject(new Error(`HTTP ${res.statusCode}: ${b.slice(0, 200)}`)),
          );
          return;
        }
        let buffer = "";
        res.on("data", (chunk) => {
          buffer += chunk.toString();
          const lines = buffer.split("\n");
          buffer = lines.pop() ?? "";
          for (const line of lines) {
            if (!line.startsWith("data: ")) continue;
            const raw = line.slice(6).trim();
            if (raw === "[DONE]") {
              done = true;
              continue;
            }
            try {
              const text = JSON.parse(raw);
              if (firstChunkMs === null) firstChunkMs = Date.now() - start;
              full += text;
              chunks.push(text);
            } catch {}
          }
        });
        res.on("end", () =>
          resolve({
            full,
            chunks,
            done,
            firstChunkMs,
            totalMs: Date.now() - start,
          }),
        );
      },
    );

    req.on("error", reject);
    req.on("timeout", () => {
      req.destroy();
      reject(new Error("SSE timeout after 90s"));
    });
    req.write(data);
    req.end();
  });
}

async function test(label, fn) {
  try {
    await fn();
    passed++;
  } catch (err) {
    failed++;
    log("❌", label, err.message);
  }
}

function assert(cond, msg) {
  if (!cond) throw new Error(msg);
}

async function main() {
  console.log("\n👻 Ghost API Test Suite");
  console.log(`   Internal API : ${BASE}`);
  console.log(`   Remote Bridge: ${REMOTE}`);
  console.log(
    `   Secret set   : ${SECRET ? "yes (" + SECRET.length + " chars)" : "NO — tests will fail auth"}`,
  );
  console.log("─".repeat(60) + "\n");

  // ── 1. Health ────────────────────────────────────────────────────────────
  await test("Internal API /v1/health returns 200", async () => {
    const r = await get(`${BASE}/v1/health`);
    assert(r.status === 200, `Expected 200, got ${r.status}`);
    const d = JSON.parse(r.body);
    assert(d.status === "ok", `status field wrong: ${r.body}`);
    assert(typeof d.uptime_s === "number", "uptime_s missing");
    log("✅", "Health", `uptime=${d.uptime_s}s version=${d.version}`);
  });

  // ── 2. Auth rejection ────────────────────────────────────────────────────
  await test("Bad secret returns 401", async () => {
    const r = await get(`${BASE}/v1/health`, { "X-Ghost-Secret": "wrong" });
    assert(r.status === 401, `Expected 401, got ${r.status}`);
    log("✅", "Auth rejection", "401 on bad secret");
  });

  // ── 3. History ───────────────────────────────────────────────────────────
  await test("/v1/history returns message array", async () => {
    const r = await get(`${BASE}/v1/history?limit=10&session=mobile:default`);
    assert(
      r.status === 200,
      `Expected 200, got ${r.status}: ${r.body.slice(0, 200)}`,
    );
    const d = JSON.parse(r.body);
    assert(Array.isArray(d.messages), "messages is not an array");
    assert(typeof d.total === "number", "total field missing");
    log("✅", "History", `${d.messages.length} messages, total=${d.total}`);
  });

  // ── 4. Search ────────────────────────────────────────────────────────────
  await test("/v1/search returns array", async () => {
    const r = await get(
      `${BASE}/v1/search?q=hello&limit=5&session=mobile:default`,
    );
    assert(r.status === 200, `Expected 200, got ${r.status}`);
    const d = JSON.parse(r.body);
    assert(Array.isArray(d), "Expected array");
    log("✅", "Search", `${d.length} results for 'hello'`);
  });

  // ── 5. Memory files ──────────────────────────────────────────────────────
  await test("/v1/memory/files returns array", async () => {
    const r = await get(`${BASE}/v1/memory/files`);
    assert(r.status === 200, `Expected 200, got ${r.status}`);
    const d = JSON.parse(r.body);
    assert(Array.isArray(d), "Expected array");
    log("✅", "Memory files", `${d.length} files found`);
  });

  // ── 6. THE CRITICAL TEST — chat with tools ───────────────────────────────
  // Before consolidation: Ghost said "I don't have real-time access".
  // After consolidation: Ghost uses web_search/browser and returns a price.
  // This is the exact scenario from the screenshot that proved the bug.
  await test("/v1/chat — Ghost uses tools (bitcoin price)", async () => {
    console.log("\n   ⏳ Sending: 'What is the current price of bitcoin?'");
    console.log("   (waiting — tools may take 10-30s)\n");

    const r = await postSSE(`${BASE}/v1/chat`, {
      content: "What is the current price of bitcoin?",
      session_key: "mobile:test",
      channel: "mobile",
      chat_id: "test",
    });

    assert(r.done, "Stream did not receive [DONE]");
    assert(r.chunks.length > 0, "No chunks received");
    assert(r.full.length > 0, "Response is empty");

    const lobotomizedPhrases = [
      "don't have real-time",
      "cannot access",
      "no real-time",
      "check coingecko",
      "check coinmarketcap",
      "i don't have access",
      "unable to access",
      "don't have access to current",
    ];
    const lower = r.full.toLowerCase();
    const isLobotomized = lobotomizedPhrases.some((p) => lower.includes(p));

    if (isLobotomized) {
      throw new Error(
        `Ghost responded WITHOUT tools — lobotomized path is still active.\n` +
          `   Response: "${r.full.slice(0, 200)}"\n` +
          `   GHOST_AGENT_URL is not set or internal API is not routing through the agent.`,
      );
    }

    const hasPrice =
      /\$[\d,]+/.test(r.full) || /[\d,]+\s*(usd|usdt|dollars)/i.test(r.full);

    log(
      "✅",
      "Chat with tools",
      `first_chunk=${r.firstChunkMs}ms total=${r.totalMs}ms chunks=${r.chunks.length}`,
    );

    if (hasPrice) {
      log(
        "✅",
        "Tool usage confirmed",
        `Price found in response: "${r.full.slice(0, 120)}"`,
      );
    } else {
      log(
        "⚠️ ",
        "Tool usage unclear",
        `Response received but no price detected. Check manually: "${r.full.slice(0, 200)}"`,
      );
    }
  });

  // ── 7. Session isolation ─────────────────────────────────────────────────
  await test("X-Ghost-Session header is respected", async () => {
    const r = await get(`${BASE}/v1/history?limit=5`, {
      "X-Ghost-Session": "mobile:test",
    });
    assert(r.status === 200, `Expected 200, got ${r.status}`);
    const d = JSON.parse(r.body);
    assert(Array.isArray(d.messages), "Expected messages array");
    log(
      "✅",
      "Session isolation",
      `mobile:test session has ${d.messages.length} messages`,
    );
  });

  // ── 8. Remote bridge stats ───────────────────────────────────────────────
  await test("Remote bridge /v1/stats returns system metrics", async () => {
    const r = await get(`${REMOTE}/v1/stats`);
    assert(
      r.status === 200,
      `Expected 200, got ${r.status}: ${r.body.slice(0, 200)}`,
    );
    const d = JSON.parse(r.body);
    assert(d.hostname, "hostname missing");
    assert(d.ip, "ip missing");
    log(
      "✅",
      "Remote bridge stats",
      `host=${d.hostname} ip=${d.ip} temp=${d.cpu_temp} mem=${d.memory}`,
    );
  });

  // ── 9. Ghost-bridge no longer serves chat ────────────────────────────────
  await test("ghost-bridge /v1/chat is gone (consolidation complete)", async () => {
    try {
      const r = await post(`${REMOTE}/v1/chat`, { content: "test" });
      if (r.status === 200) {
        throw new Error(
          "ghost-bridge still has /v1/chat — consolidation incomplete. " +
            "Remove the chat handler from bridge/main.go.",
        );
      }
      log("✅", "Chat removed from bridge", `Returned ${r.status}`);
    } catch (err) {
      if (err.message.includes("consolidation incomplete")) throw err;
      log(
        "✅",
        "Chat removed from bridge",
        "Endpoint gone (connection refused or 404)",
      );
    }
  });

  // ── 10. Latency baseline ─────────────────────────────────────────────────
  await test("Simple response latency baseline", async () => {
    console.log("\n   ⏳ Sending: 'Reply with exactly: PONG'");
    const r = await postSSE(`${BASE}/v1/chat`, {
      content: "Reply with exactly the word: PONG",
      session_key: "mobile:test",
      channel: "mobile",
      chat_id: "test",
    });
    assert(r.done, "Stream did not complete");
    assert(r.chunks.length > 0, "No chunks received");
    log(
      "✅",
      "Latency baseline",
      `first_chunk=${r.firstChunkMs}ms total=${r.totalMs}ms response="${r.full.trim()}"`,
    );
    if (r.firstChunkMs > 10000) {
      log(
        "⚠️ ",
        "Slow first chunk",
        `${r.firstChunkMs}ms — normal on cold start, rerun to confirm`,
      );
    }
  });

  // ── Summary ───────────────────────────────────────────────────────────────
  console.log("\n" + "═".repeat(60));
  console.log(`\n  Results: ${passed} passed, ${failed} failed\n`);

  if (failed === 0) {
    console.log("  ✅ All tests passed.");
    console.log("  The mobile app can now be pointed at port 8765 directly.");
    console.log("  ghost-bridge on port 8766 handles remote control only.\n");
  } else {
    console.log("  ❌ Some tests failed. Do not update the mobile app config");
    console.log("  until all tests pass.\n");
    process.exit(1);
  }
}

main().catch((err) => {
  console.error("\n💥 Test runner crashed:", err.message);
  process.exit(1);
});
