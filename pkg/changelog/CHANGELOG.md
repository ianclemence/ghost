# Changelog

Newest first. Ghost shows new entries on first launch after an update;
`ghost update --notes` reprints them.

## [0.24.52] - 2026-09-26

- **Ghost notices things.** Ghost now watches the state it already
  keeps — reminders that never reached you, routines that keep failing,
  routines waiting on your approval, goals that went quiet, background
  tasks parked on a human — and turns what it finds into one concrete
  offer: *"Your reminder "send Alex the document" was due 2 hours ago
  and never reached you. Want me to run it now?"* Each one shows the
  exact rows behind it and why it matters now. Nothing is invented:
  if Ghost cannot point at the evidence, there is no suggestion.
- **Ghost asks, then does it, then proves it.** Approving a suggestion
  goes through the same Permission Broker as any other action; Ghost
  then runs it through the same capability path, checks the result
  against real state, and tells you what actually happened — never
  what it hoped happened. Approve, Later, and No thanks all work from
  chat, the phone card, and the Web console.
- **It will not nag.** One live suggestion per situation, a cooldown
  after you dismiss one, a backoff after a failure, and a hard stop
  when the situation changes: if your routine recovers or the reminder
  fires before you answer, the offer is withdrawn instead of executed.
  Quiet hours, a daily limit, per-category controls, and a master
  switch all live in `PROACTIVE_PREFERENCES.md` and are shown on the
  Home screen.
- **Nothing about this costs a model call.** Deciding whether to
  interrupt you is entirely local and deterministic. The model is only
  involved if you approve something that needs judgement — and even
  then, anything consequential still goes through the broker.
- The Ideas screen is now "what needs you": open suggestions with their
  evidence and one action each, followed by what Ghost recently handled.

## [0.24.51] - 2026-09-26

- Ghost no longer asks "Which city should I check?" when the answer is
  already in the conversation. Talking about Phuket and then asking
  about the weather "there" used to bounce straight back into that
  question, because the quick path that handles a weather ask never
  looked at what had just been said. A request that points back at the
  conversation now reaches Ghost with the conversation attached, so the
  place you just named is the place it uses. A plain "what's the
  weather?" with no place anywhere still asks once, as before.
- Section titles are no longer cut short. In a narrow window, every
  heading in a reply came back truncated behind an ellipsis with the
  rest of the line dropped — no way to expand it. Titles now wrap like
  the rest of the text, so nothing is lost.

## [0.24.50] - 2026-09-26

- Fixed Ghost answering with raw web page markup. Approving a read of
  a web page used to hand the fetched bytes straight back as Ghost's
  reply, so an owner who said yes got a wall of HTML and JSON-LD
  streamed and saved as something Ghost said — and never got the
  answer that was promised. The approved run's output now returns to
  the model, which replies in its own words; the raw bytes are the
  evidence for that turn, never the turn itself.
- The approval reply ("always allow") is no longer treated as
  something the owner said: it is not saved as a chat message and is
  not fed to memory, journaling, note capture, or the follow-up
  question tracker.
- The receipt shown when a resumed run cannot be written up is now
  bounded, so a long payload can never be pasted at the owner again.

## [0.24.49] - 2026-09-26

- `docs/` is now exactly the architecture chain and nothing else:
  this README plus the nine layer documents (User → Ghost →
  Intent/Reasoning → Capability → Permission Broker → Execution →
  Evidence → Canonical Event → Memory/Activity/Routines/Artifacts).
  Retired the side documents — agent CLI contract, Desk, golden run
  report, and Pod home — and the code comments that pointed at them
  now stand on their own.
- Removed `ghost eval release-notes`. Its only job was assembling
  `docs/VERIFICATION-<version>.md`, which no longer belongs in the
  docs directory; `verify`, `benchmark`, `golden`, and `replay` are
  unchanged.

## [0.24.48] - 2026-09-26

- Ghost's operating contract is now a complete system prompt.
  `GHOST.md` gained a per-family operating manual — status, web
  reading, live lookups, connected apps, memory, files, commands,
  routines, artifacts, delegation, clarifying, skills — each with
  when to reach for it, how to call it, and what to say on success
  or failure. Plus a dedicated skills section, channel-by-channel
  presentation rules (phone vs console vs CLI), reply-formatting
  guidance, and a "common failures to avoid" list drawn from the
  bugs that actually shipped: date-label leaks, "I can't run
  commands", success claimed without evidence, timezone mistaken
  for location, and permission theater.
- Persona and workspace notes deepened to match: `SOUL.md` now
  carries the values layer (truth, respect, warmth, play) and a
  voice card; `AGENTS.md` the skill, state-boundary, and
  reporting conventions for agentic work.
- Docs cleanup: retired evaluation and audit artifacts removed
  (demo evaluation, interaction audit, live eval, restore drill,
  tool-skill map, verification reports, security posture).

## [0.24.47] - 2026-09-26

- Date-stamp fix reaches the live chat. v0.24.46 stripped the
  label from saved replies, but on a streaming reply the internal
  dump filter still ate the label's opening "[" before the gate
  could recognize it — the stream showed "2026-09-2610:04] …" as a
  bare date fragment. The label gate now runs first and the filter
  sees only what survives, so the live transcript, the saved reply,
  and history reads all agree: no date prefixes, anywhere.

## [0.24.46] - 2026-09-26

- No more stray date stamps. When a model echoed the internal
  history label in a reply — with its characteristic dropped space
  ("[2026-09-2610:04] Approval needed…") — it slipped past the
  stripper and showed up as a visible date prefix on your chat.
  Both label shapes now strip at every output boundary: the live
  stream, the final reply, and history reads.

## [0.24.45] - 2026-09-26

- "Weather in my city" works instead of failing. The place
  extractor was capturing the phrase "my city" literally and handing
  it to a geocoder that could never resolve it — a plain weather read
  came back "That didn't complete. Nothing was changed." Here-phrases
  (my city / my location / my town / near me) now resolve through
  the location Ghost has on file, and a genuinely unknown place says
  so plainly: "I couldn't find a place called …"
- Weather reads speak read language. A failed lookup never claims
  something "was changed" — it says the weather service didn't come
  back with a reading, or that the place couldn't be found.
- Asking for a command offers the command tool. When your message
  explicitly asks Ghost to run something ("run df -h", "run the
  command uname -a", a bare "ls -la"), the command tool is offered
  for that turn. Visibility only — the permission broker still asks
  before anything runs, exactly as before.

## [0.24.44] - 2026-09-26

- The status tool actually reaches the model now. It was registered
  in 0.24.43 but the per-turn intent filter kept it off the offered
  list, so Ghost still said "that tool isn't available in this
  session". It joins the always-offered core set — status phrasings
  vary too much to keyword-gate, and a lost offer is a refused
  question.
- The persona now names `exec` for owner-commanded runs. "Attempt
  the call" wasn't enough without saying which call: hidden means
  unadvertised, not unavailable, so Ghost calls it and the approval
  gate does the rest.

## [0.24.43] - 2026-09-26

- Machine status and free disk space in one call. Ghost now has a
  read-only `system_status` check — temperature, memory, load,
  uptime, and df-style disk rows with real free space — available on
  every surface including subagents. Status questions never needed
  shell, and free space never needed df: it comes from statvfs
  directly.
- Asked to run a command? Ghost attempts it. When you explicitly ask
  for a command (or grant permission for one), the call is made and
  the runtime shows an approval — it runs on your yes. Ghost no
  longer refuses on principle while claiming the capability "isn't
  exposed"; surfaces that truly forbid a tool still say so plainly.
- Direct requests win over narration rules. When you ask for command
  output, a path, or a raw number on your own machine, you get it —
  the "no machinery" style rule is how Ghost narrates its own work,
  never a reason to refuse what you asked for.
- A timezone is not a location. The clock zone is labeled as time
  only in Ghost's context, and the persona rule now forbids
  inferring a city or country from a zone — no more "08:25 in
  Bangkok" from Asia/Bangkok.
- "Weather here / in my city / in my location" resolves from the
  location on file (asked once, remembered), and declarations like
  "my city is Bangkok" are stored as your location too.
- Website sign-ins offer Website logins. Asking Ghost to sign in to
  a site now points at the sealed-login flow (you seal it in
  Connected Apps; the password never passes through chat) instead
  of a flat refusal.

## [0.24.42] - 2026-09-26

- The location you give once now really sticks. Answering "bangkok" to
  "which city should I check?" remembers it, so further "what's the
  weather here" questions answer straight away. (The remember step sat
  on a path that resumed answers deliberately skip; it now lives where
  the answer is resolved, which every location reply passes through.)

## [0.24.41] - 2026-09-26

- "Here" is remembered. Answer "bangkok" once to "which city should I
  check?", and Ghost keeps it — the next "what's the weather here"
  answers straight away instead of asking again. (The first save
  silently did nothing before: the store only knew how to replace an
  existing location, not create one.)
- Weather shows the emoji you expect from wttr. wttr's JSON carries no
  emoji — only its text mode does — so Ghost maps the condition to the
  glyph itself, which works for every provider.

## [0.24.40] - 2026-09-26

- No more "allow once" that gets refused. Shell (`exec`) is hidden by
  default, but the tool list still offered it — so Ghost would ask you to
  approve a shell command from the phone, then the runtime refused it
  ("disabled for this channel/session"). Hidden tools are never offered
  now, a committed skill that needs one promotes it so it actually runs,
  and Ghost says plainly what it will use instead.
- Reading a website goes through web search, page fetch, or the browser —
  never a shell command.

## [0.24.38] - 2026-09-26

- Browser sign-ins now stick. Ghost keeps a per-context browser profile
  (owner-only, and separated so a personal login can never leak into a
  work session), so an account you sign into survives restarts and
  updates. Deleting the profile is the sign-out.
- New: Website logins. Save a site's login once in Ghost settings →
  Apps (never in chat). Ghost signs in for you with the password read
  from the encrypted vault and handed to the browser privately — it is
  never shown to the model, never logged, and you can remove it any
  time. Signing in asks for approval first, like any other consequential
  action.

## [0.24.37] - 2026-09-25

- Weather now runs on wttr.in first — the provider you asked for — with
  Open-Meteo as the automatic keyless fallback and OpenWeather when a key
  is connected. Answers still name their source ("via wttr.in"), the
  skill says the same, and validation, retry, and the breaker are
  unchanged.

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


