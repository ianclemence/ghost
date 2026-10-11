// Behavioral test for the PDF branch of web/js/sections/files.js.
// Run: bun cmd/ghost-web/web/js/sections/files.test.mjs
// (cmd/ghost-web runs it from Go when bun exists.)
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(here, "files.js"), "utf8");

// The section only touches these globals at load (registerSection); the
// viewer paths under test do not run here.
globalThis.GhostApp = { registerSection: () => {} };
new Function(src + "; globalThis.__F = GhostFiles;")();
const F = globalThis.__F;

const failures = [];
const check = (name, cond, detail) => { if (!cond) failures.push(name + (detail ? " — " + detail : "")); };

// A PDF reads as its pages, never as extracted text.
check("mime wins", F.isPdfPreview({ mime: "application/pdf", name: "x.bin" }, { name: "x.bin" }) === true);
check("extension fallback", F.isPdfPreview({ mime: "text/plain", name: "report.pdf" }, { name: "report.pdf" }) === true);
check("case-insensitive", F.isPdfPreview({}, { name: "REPORT.PDF" }) === true);
// Anything else keeps the old behavior.
check("text stays text", F.isPdfPreview({ mime: "text/plain", name: "notes.txt" }, { name: "notes.txt" }) === false);
check("photo stays photo", F.isPdfPreview({ mime: "image/jpeg", name: "img.jpg" }, { name: "img.jpg" }) === false);
check("empty stays false", F.isPdfPreview({}, {}) === false);
check("null stays false", F.isPdfPreview(null, null) === false);

if (failures.length > 0) {
  console.error("FAIL:\n  " + failures.join("\n  "));
  process.exit(1);
}
console.log("files section PDF detection: all checks passed");
