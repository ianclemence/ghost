# Changelog

Newest first. Ghost shows new entries on first launch after an update;
`ghost update --notes` reprints them.

## [0.24.36] - 2026-09-25

- Ghost no longer asks "Which location should I check?" when you already
  said it. "Find a coffee shop near Cebu City" reads the place from the
  message — near, around, close to, not only "in" — and goes straight to
  the nearby-places tool.

## [0.24.35] - 2026-09-25

- Tools and skills now move together. Every capability that has a native
  tool names it at the top of its skill and treats the old shell
  commands as a fallback: weather, air quality, crypto, currency,
  flights, nearby places, maps, calendar, email, Spotify, Notion,
  GitHub, and smart home. The governed path — evidence, approvals,
  honest provider failures — is the default, and a build test keeps the
  pairing from drifting. The full map lives in `docs/TOOL-SKILL-MAP.md`.

## [0.24.34] - 2026-09-25

- Ghost no longer files your questions as facts. "Why do you think my
  name is Ian?" used to be read as if you had declared it — a question
  asserts nothing. Questions are recognized and skipped now.
- Ask "why do you think that?" and Ghost answers with the receipt by
  default: your exact words, where they came from, and how confident it
  is — or an honest "no quote was kept" for older memories.
- The places skill works again. Its request footer carried a
  placeholder contact that OpenStreetMap rejected, so every lookup
  failed with a 403.
- Housekeeping: the mail skill no longer points at documentation that
  doesn't ship, and the self-improvement skill now describes scheduled
  work the way Ghost actually schedules it.

## [0.24.33] - 2026-09-25

- Backups can't be broken by a stray file anymore. A screenshot or a
  folder Ghost doesn't recognise in your workspace used to fail the
  whole weekly snapshot — silently, because the error only lived in
  the service log. Unknown folders are now recorded as skipped, and a
  file you drop at the workspace root travels in the snapshot as your
  own content.
- Release binaries no longer include a leftover development database,
  making them about 6 MB smaller.

## [0.24.32] - 2026-09-25

- Screenshots now use the same working browser engine as everything
  else. The capture command was the one code path that missed the
  browser fix, so a first screenshot on a fresh session could hang on
  a machine where the system browser is broken. It's steered now, and
  the end-to-end test covers navigating, reading the page, and
  capturing a screenshot from a real site.

## [0.24.31] - 2026-09-25

- Follow-through on the stuck-browser fix, in three parts. First: the
  browser cleanup at startup now repeats until no leftovers remain —
  Chromium children can appear only after their parent dies, so one pass
  used to miss them.
- Second: updates refuse to start when the disk is too full, with a
  plain sentence telling you to free space, install the binary
  atomically, and verify it after installing before claiming success.
  A full disk can no longer produce a silent failure or a half-written
  binary.
- Third: when the browser does recover, your phone shows a small card —
  "My browser got stuck — I reset it" — so 90 seconds of quiet is
  followed by an explanation instead of a mystery.

## [0.24.30] - 2026-09-25

- Ghost's browser doesn't get stuck anymore. Root cause found: a
  browser helper left behind by an earlier run kept every page hanging
  until the 90-second timeout, so Ghost blamed the websites. Chelsea
  and the New York Times both load fine — the browser was wedged, not
  the sites. Ghost now clears leftover browser helpers at startup,
  resets itself mid-action if a page times out, and says "my browser
  got stuck" instead of implying the site is down.

## [0.24.29] - 2026-09-25

- Receipts reach your phone. Ask "why do you think that?" and the
  answer arrives with a card right in the chat: the exact words you
  said, how confident Ghost is, when it learned it, and which message
  it came from. Older memories say plainly that no quote was saved,
  instead of pretending.
- Receipt cards never carry buttons on purpose: explaining is
  read-only, and forgetting stays something you do deliberately on
  the Memory screen.

## [0.24.28] - 2026-09-25

- Ask Ghost why it believes something and it shows its work. Every
  memory now keeps the exact words you said, the message they came
  from, how confident Ghost is, and when it learned it. Ask "why do
  you think that?" in chat, or tap **Why?** on the Memory screen.
- Forgetting now really forgets. It retracts the memory, records that
  it's gone so nothing can quietly bring it back, and rebuilds the
  notes derived from it. The only thing that can restore it is you
  saying it again — which is you changing your mind, not a bug.
- Evening reflections moved into a private journal that never feeds
  back into Ghost's behavior on its own. Insight gets recorded; it
  doesn't silently rewrite how Ghost acts. A test enforces it.
- Video processing runs sandboxed: no network, read-only system, a
  private temp folder. Untrusted media can't become a way out.
- New docs: the Ghost Pod home specification (Matter/Thread, BLE
  pairing, signed updates, per-device receipts) and the security
  posture — plain answers to "what can leave my machine?"

## [0.24.27] - 2026-09-25

- Progress lines sound human now. While Ghost works, phone and
  terminal show calm phases — "Thinking…", "Searching the web…",
  "Reading the page…" — and never tool names, file names, paths, or
  commands. The backend and the phone app both enforce it, so nothing
  internal can slip through even for a brand-new capability.
- 22 new skills. Money and news: live stock and crypto quotes, RSS
  and Reddit reading. Writing: an AI-speak stripper, a plain-English
  rewriter, and sourced answers with citations. Planning: weekly
  reviews, action items pulled from documents and meetings, inbox
  triage, and a 1-3-1 decision brief. Everyday: maps and routes, GIF
  search, meme maker, ASCII video, songwriting help, and a
  creative-ideation coach.
- Ghost now learns from itself. A new learning loop records only
  verified fixes and mistakes, and promotes proven workflows into
  skills — joined by skill-gardener and a compound memory pipeline
  adapted for Ghost.
- Asking about an app works like a person would expect: say "check my
  Gmail" and Ghost tells you plainly whether it's connected and the
  one next step — on the web console, your phone, or the terminal —
  without ever asking you to paste a secret into chat.
- Tone: ordinary, reversible work just happens; the hard line stays
  exactly where it was for anything consequential.

## [0.24.26] - 2026-09-25

- The Skills screen in the web console and on your phone now shows
  everything Ghost knows how to do. It was reading the wrong folder
  and came up empty even while Ghost was using all 51 skills; the
  same fix restores your saved notes and files on those screens.
  Conversation history never moved.
- Five skills went back to Ghost's own versions after trying the
  community ones: weather, GitHub, Notion, calendar, and smart home.
  The replacements talked to services directly from a terminal,
  which only works for people comfortable with commands. Ghost's
  versions sign in through Connected Apps instead — set up once
  from the web console or your phone, then it just works. The
  smart-home replacement was also flagged by our scan and used the
  wrong settings names, so it's gone.
- Web search setup now has a home in the console: the built-in free
  search already works with nothing to set up, and a Brave key can
  be pasted in under Apps when you want pro results — stored
  encrypted on your device.
- Google Workspace extras now say plainly when they need a
  one-time sign-in instead of failing halfway through, and the
  Excel skill checks for its spreadsheet library up front rather
  than breaking mid-task.

## [0.24.25] - 2026-09-25

- Twelve community skills added: Word documents, Excel spreadsheets,
  PDFs, Google Workspace (Gmail, Calendar, Drive), GitHub, Notion,
  Slack, calendar handling, content writing, a much fuller smart-home
  guide (25 device areas), a proactive-assistant playbook, and
  weather.
- Where a new skill overlapped one Ghost already had — weather,
  GitHub, Notion, calendar, smart home — the fresh version takes
  over; the previous ones stay recoverable from history.
- Nothing was installed blind: every skill was read end to end,
  stripped of third-party branding and store plugs, and passed
  Ghost's own skill checks before shipping. Google Workspace, Notion,
  smart home and Slack will ask for a one-time setup when first used.

## [0.24.24] - 2026-09-25

- Approvals answer out loud again: after you reply "allow once" or
  "deny" in chat, Ghost's receipt — the list you asked for, the
  confirmation that it ran (or didn't) — now appears right on your
  screen instead of "(no response)".
- Reminders understand how people talk: "at 9pm tonight", "at 8:30 PM
  today" and "tonight at 9" all schedule correctly. A reworded
  request used to bounce off the parser, so the move to 8:30 never
  happened and the reminder stayed at the old time.
- Reminder names are clean: the dinner reminder is titled "Dinner
  with Jas", never the raw sentence with its clock time repeated
  inside. If Ghost can't read a schedule it now asks for a clearer
  time with real examples instead of echoing your words back
  garbled.
- Looking is free: checking your reminder list no longer asks for
  permission — only making, changing or cancelling a reminder does.
- An honest stop first: asking about the system when Home Assistant
  isn't connected now says "connect it first" right away, instead of
  asking for an approval that could only end in a dead end.
- Ghost's replies never begin with an internal date stamp: the date
  labels it keeps for its own reading stay invisible, with a new
  identity rule and protections at every layer — stream, reply,
  save, history read.

## [0.24.23] - 2026-09-25

- Ghost now knows when you said things: every earlier line in a
  conversation carries the date and clock time it was written, so a
  "later today" written last week can never be mistaken for now.
- Summaries stay date-proof: the summarizer is told today's date,
  writes only absolute dates ("Thu, Sep 24, 2026 at 09:00"), and
  stamps every line with when it happened — no more a stale
  "tomorrow" pointing at a day that already passed.
- Open reminders inside summaries show full dates ("Sun 2026-09-27
  07:00") instead of a bare weekday and clock time.
- Ghost re-anchors old context: a new identity rule makes it read
  relative words in older messages, summaries and memory against the
  current clock — "today" in Monday's message means Monday.

## [0.24.22] - 2026-09-25

- Failure replies stay in your language: instant answers no longer
  leak model-only notes like "(completion: failed; do not present
  fabricated data)" — you get the honest sentence, nothing else.
- A weather lookup whose location lookup fails mid-network now says
  so ("The network request failed. I'll try again shortly.")
  instead of blaming the answer ("I got an unexpected response…").
- Scheduled maintenance can actually ask for approval now: heartbeat
  turns carry a request identity, so a due system check opens a
  durable approval you can answer instead of dying with "couldn't
  prepare the approval request".
- "Check my reminders" works: the schedule tool can list pending
  items (soonest first) plus recently completed and failed ones with
  exact times, and fired one-shots are kept as completed history
  (newest 100) instead of being deleted the moment they ran.

## [0.24.21] - 2026-09-25

- Ghost can really drive a browser now: find, wait, screenshot,
  scroll, console, network and accessibility checks join
  navigate/snapshot/click, plus select, check, hover, drag, batch
  form fill, dialogs, upload and download — every action behind
  the same approval gate, and a failed click states the outcome is
  unknown instead of silently retrying.
- Screenshots reach Ghost's eyes: the screenshot action attaches
  the image to that turn only, and the browser playbook loads on
  turns that can actually call the browser (a bare web address in
  your message is enough to open the surface).
- Terminal and mobile name browser work in human words — "Opening
  page: …", "Finding: Log in", "Filling form…" — instead of raw
  tool names.
- A message about a "newsletter" that also says "anything" is no
  longer mistaken for a request for blanket permissions.

## [0.24.20] - 2026-09-24

- Background work reaches every surface: the daemon announces task
  starts and finishes over the live channel, so the mobile
  conversation shows the same running indicator and report-back as
  the terminal. One history note per finish, no double-reporting.

## [0.24.19] - 2026-09-24

- Background tasks report back: delegated subagent work shows a live
  dock line while running, then Ghost tells you what it found —
  failures stated plainly. Max 2 at once, approval unchanged.

## [0.24.18] - 2026-09-24

- Slash palette fixed: the command you type always wins over the
  highlight, and /task stays composed with an arg hint instead of
  erroring. Scoped-models picker removed; /model covers everything.
- Ghost sounds more human: narration is answered, not taskified;
  questions only when the answer changes what happens next; memory
  answers never talk about files.
- Recurring reminders need an explicit ask: describing a habit
  ("every morning I feel groggy") no longer proposes a routine.

## [0.24.17] - 2026-09-23

- Reply headers name Ghost only: the model tag moves out of the
  transcript and stays in the footer status line.

## [0.24.16] - 2026-09-23

- Replies never glue words to clock times: the output path repairs
  "the2:00 PM" style spacing before saving.
- Ghost sounds like a person texting: contractions, plain warmth, an
  emoji only where a human would put one (never on evidence), and an
  honest sentence instead of a dead-end when a turn comes back empty.
- Thin sourcing is flagged unverified AND paired with the
  primary-source next step — never laundered into fact.

## [0.24.15] - 2026-09-23

- Long sent messages no longer fold the pipe onto the text: the user
  bubble wraps to its real width so the bar holds one column.

## [0.24.14] - 2026-09-23

- Approval cards answer to all arrow keys (plus vim keys), not just
  left and right.
- Approved turns always leave a receipt: resumed executions that
  produce no text now say Done (or admit failure) instead of
  replying blank.

## [0.24.13] - 2026-09-23

- Calendar runs on the direct Google Calendar API: agenda, natural
  language quick-add, and delete-by-query with real event evidence.
  gcalcli stays for device-flow-connected users, whose client-bound
  tokens only it can redeem; full removal waits on a Ghost-managed
  OAuth client.

## [0.24.12] - 2026-09-23

Follow-through fixes, same trust story.

- Transient provider blips (timeouts, rate limits, network) retry with
  bounded backoff in model and tool calls; auth and config failures
  still fail fast.
- The browser declares a 90-second bound and closes its session after a
  hung call, so leaked Chromium can no longer starve later turns.
- The Golden grader no longer mistakes distant completion words for
  Ghost's act (p-11 class), and its plural nouns cover third parties.
- Doctor and state suites are hermetic again (PATH pinning, v8-aware
  fixture).
- The web console has an Ideas section; the mobile app stays focused
  (ideas live in conversation).

## [0.24.11] - 2026-09-23

Trust you can read, delight you can feel.

- **Verification pages.** `ghost eval release-notes` assembles
  `docs/VERIFICATION-<version>.md` from Ghost's own checks — verify,
  benchmark, golden results, and limits — committed per release.
- **Tasks surface.** `ghost tasks`, `/tasks`, and `/task` show and manage
  durable routines (pause, resume, cancel) through the same routines
  service the gateway and mobile app use.
- **Ideas with evidence.** `ghost ideas`, `/ideas`, and `/idea` surface
  suggestions that cite their sources, with accept/dismiss receipts.
  `refresh` derives them deterministically; `draft` asks the model and
  verifies every citation, marking failures unverified.

## [0.24.10] - 2026-09-22

- Inline styling no longer leaks when the model breaks a line mid-span.
  Bold, italic, and code split across line breaks now rejoin before
  rendering, on both the live stream and the committed reply — without
  touching fences, tables, headings, quotes, or lists.

## [0.24.9] - 2026-09-22

- Your messages show as `You ┃ text` on one row, with continuation pipes
  aligned beneath.
- The input box now grows while you type a long message (up to its cap,
  then it scrolls) instead of staying one row.

## [0.24.8] - 2026-09-22

- Streaming replies render markdown live, Scout-style: headings, lists,
  quotes, code, and tables format as they arrive instead of raw `##` and
  `**` markers, long lines wrap instead of breaking mid-word, and
  paragraph gaps are preserved. Completion still prints only the
  remaining tail.

## [0.24.7] - 2026-09-22

- Replies now stream line-by-line into the conversation as they arrive,
  opencode style — no more watching a one-row preview and getting the
  whole answer at the end. Completion prints only the remaining tail,
  never the full text again.
- The idle footer is trimmed to `/ commands` (left) and `esc quit`
  (right), matching Scout.

## [0.24.6] - 2026-09-22

- The terminal transcript can no longer silently drop replies. Committed
  lines now print and drain like Scout's transcript (no print cursor left
  to desync), so `/clear`, `/thread`, and history reloads cannot swallow
  later responses.

## [0.24.5] - 2026-09-22

- The terminal no longer loses Ghost replies after `/clear`, `/thread`,
  or returning with `/main`. Replacing the transcript used to leave the
  scrollback cursor behind, silently swallowing later replies; the cursor
  now restarts with the transcript.

## [0.24.4] - 2026-09-22

- `ghost update` no longer prints scope and sudo notes on the Deploying
  line.

## [0.24.3] - 2026-09-22

Evaluation-driven fixes (Golden Suite 58/59 → 59/59, confirmed by external
JEV evaluation before and after).

- **Memory recall no longer falsely reports absence.** Asking what you
  like or prefer could answer "I don't have that stored yet" even when the
  preference was stored, because the fast recall path only looked up two
  predicates while stored beliefs use a wider vocabulary. The fast path now
  covers the liking family and falls through to full retrieval instead of
  asserting an absence it cannot prove.
- **Computer actions report dispatch honestly.** Control actions that were
  dispatched but not independently screen-verified used to report "executor
  reported false", which read as failure and contradicted the recorded
  success evidence. They now report "dispatched but not independently
  screen-verified".

## [0.24.2] - 2026-09-21

Reasoning-hygiene release.

- **Reasoning never surfaces.** Provider chain-of-thought is identified and
  discarded at the provider boundary: the OpenAI-compatible streaming and
  non-streaming paths read the full reasoning allowlist (`reasoning_content`,
  `reasoning`, `reasoning_text`) into the separate `ReasoningContent` field and
  never into answer content.
- **Inline `<think>` tags are stripped** from content with a streaming-safe
  filter that survives token-boundary splits, so a model that emits reasoning
  inline cannot leak it to the agent loop, the TUI, or the SSE bridge to the
  app. Anthropic, Moonshot, and Claude CLI content is sanitized at the same
  boundary.
- Thinking remains off by default; this is the hard guarantee that holds even
  when a caller opts a reasoning model in.

## [0.24.1] - 2026-09-21

Updater parity release.

- `ghost update` / `ghost update --check` print **what's new** on top of "Already current" and after an install, matching Scout.
- Embedded changelog + last-seen marker; `ghost update --notes` prints the full changelog.
- Version comparison is base-version aware, so a `-dirty` or dev-suffixed installed binary is correctly recognised as current instead of looking different.

## [0.24.0] - 2026-09-21

Updater release.

### One updater, no sudo
Ghost's updater ran entirely under sudo and rewrote an appliance-wide install.
It now prefers a per-user layout that needs no root and escalates only the
steps that genuinely require it.

- **`ghost update`** runs as you: `--check`, `--notes`, `--force`, `--dry-run`, `--channel release|dev`.
- **User scope needs no sudo**: binary in `~/.local/bin`, service via `systemctl --user` (`make install-user`).
- **System scope** escalates only the binary swap and service restart.
- **Verified artifacts**: the release channel resolves from GitHub Releases, verifies sha256 (+ Ed25519 when `GHOST_RELEASE_PUBKEY` is set), smoke-tests, and atomically installs.
- **`ghost update --check`** reports installed vs available and the detected scope.

### Retired
The standalone `ghost-update-daemon` is gone — it was a second, divergent
updater. `ghost auto-update` now ticks the same release-channel updater.


## [0.23.45] - 2026-09-21

# Ghost v0.23.45 — "Thinking" reads with a capital T

The composer's top rule now reads **Thinking** (capital T) when Ghost is
reasoning without a tool, matching the tool labels it alternates with
("Searching…", "Reading…"):

```
── ▖ Thinking · 4s ────────────────────────
── ▖ Searching the web… · 4s ─────────────
```

Docs updated. Build clean; `cmd/ghost` suite green.


## [0.23.44] - 2026-09-21

# Ghost v0.23.44 — no tool count in the prompt status

The composer's top rule used to read `⠋ thinking · 4s · 2 tools`. The tool
count is gone — the prompt now names the live activity and elapsed time only:

```
── ▖ thinking · 4s ────────────────────────
── ▖ Searching the web… · 4s ─────────────
── ▖ Reading notes.md · 3s ───────────────
```

Tool detail is still available in the `/details` trail (Ctrl+O).

Also removed a dead status helper and its unused styles that still rendered a
tool count. Build clean; `cmd/ghost` suite green.


