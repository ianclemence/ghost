// Behavioral test for web/js/wording.js. Run: bun cmd/ghost-web/web/js/wording.test.mjs
// (cmd/ghost-web runs it from Go when bun exists.)
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const src = readFileSync(join(here, "wording.js"), "utf8");
new Function(src + "; globalThis.__W = GhostWording;")();
const W = globalThis.__W;

const failures = [];
const check = (name, cond, detail) => { if (!cond) failures.push(name + (detail ? " — " + detail : "")); };
const eq = (name, got, want) => check(name, JSON.stringify(got) === JSON.stringify(want), `got ${JSON.stringify(got)} want ${JSON.stringify(want)}`);

// The same words twice are shown once.
eq("identical", W.routine("Take my vitamins", "Take my vitamins"), { title: "Take my vitamins", sub: "" });
eq("case differs", W.routine("Book the Shenzhen flights, departing Friday 16 October", "book the Shenzhen flights, departing Friday 16 October"),
  { title: "Book the Shenzhen flights, departing Friday 16 October", sub: "" });
eq("closing full stop differs", W.routine("Pay rent", "Pay rent."), { title: "Pay rent", sub: "" });
eq("spacing differs", W.routine("Give me a short brief of my week", "Give me a  short brief of my week"), { title: "Give me a short brief of my week", sub: "" });

// A clipped title is replaced by the full wording.
eq("clipped title",
  W.routine("Chelsea vs Bournemouth is tomorrow — Saturday 10 October, 15:00 UK / 21:00 Ban…",
            "Chelsea vs Bournemouth is tomorrow — Saturday 10 October, 15:00 UK / 21:00 Bangkok."),
  { title: "Chelsea vs Bournemouth is tomorrow — Saturday 10 October, 15:00 UK / 21:00 Bangkok.", sub: "" });

// A second line that adds something is kept.
eq("adds detail", W.routine("Monday brief", "Calendar, weather and anything waiting on you"),
  { title: "Monday brief", sub: "Calendar, weather and anything waiting on you" });
eq("longer than title, not clipped", W.routine("Book flights", "Book flights and email Sam the dates"),
  { title: "Book flights", sub: "Book flights and email Sam the dates" });

// Missing pieces.
eq("no description", W.routine("Pay rent", ""), { title: "Pay rent", sub: "" });
eq("no title", W.routine("", "Pay rent on the 1st"), { title: "Pay rent on the 1st", sub: "" });
eq("neither", W.routine("", ""), { title: "Untitled", sub: "" });

if (failures.length) {
  console.error("FAILED:\n - " + failures.join("\n - "));
  process.exit(1);
}
console.log("all checks passed");
