# Ghost terminal (interactive)

Run `ghost` to chat in the terminal — Ghost's daily surface, like `pi` or
`opencode`. (`ghost agent` is the same command.) Talk to Ghost, watch it work,
approve what it asks, and see where each answer came from. The behavior,
commands, and status are Ghost's own — trust, memory, routines, and provenance
are first-class here.

## Principles

- **Streaming, not waiting.** Tokens render as they arrive in place (with a
  cursor); each tool shows as one collapsed row (`✓ label`, spinner while
  active) so Ghost never looks frozen and raw tool output never floods chat.
  The final response wins verbatim over the stream preview.
- **Where it ran is always visible.** Every turn reports whether it was served
  locally, by the Pod, or by cloud.
- **Approvals are first-class.** A permission request is a prompt in the flow,
  not something you discover in another app.
- **Never block the user.** While Ghost works, you can still type. Enter queues
  a steering message (injected into the running turn); Esc aborts.
- **Slash commands for everything.** No hidden flags; `/` opens the palette.
- **Calm rendering.** No flicker, readable in any terminal.

## Commands

| Command | Purpose |
|---|---|
| `/help` | List commands and keybindings |
| `/session` | Where this terminal is, the model, and turn count |
| `/model [name]` | Show or switch the active model (presets, connections, keyed providers) |
| `/context [name]` | Show or switch topic context (scopes memory + tools) |
| `/memory [query]` | Ask Ghost, in a turn, what it remembers |
| `/routines` | Ask Ghost, in a turn, what it has scheduled |
| `/thread` | Open a **side thread** — a tangent, not the shared conversation |
| `/main` | Return to the shared conversation (reloads its rows) |
| `/rewind` | Put the last message back in the editor to edit and resend |
| `/details` | Toggle tool step details (durations) |
| `/clear` | Clear the screen (keeps the conversation) |
| `/quit` | Exit (also Ctrl+C twice) |

The palette shows the commands in this same order — discovery and state
first, then memory and behavior, then navigation, then display, with
housekeeping and exit last.

`/memory` and `/routines` are **turns**: they ask Ghost (a model call),
not a local read, and they render the answer in the transcript. Everything
else is instant and local.

## One conversation, many surfaces

Ghost has **one conversation**. Every surface — the terminal, the mobile
app, the web console, and the owner's messaging channels (Telegram, Slack,
WhatsApp, SMS, Discord, voice, …) — reads and writes the same thread. The
surface a turn came from is **provenance**, not identity: it is recorded on
the turn and on the message (`source_channel`), and the read history
carries a `channel` field so a client can note "via Telegram", but it never
splits the conversation.

`/thread` opens a *side thread* for a tangent; `/main` returns to the shared
conversation. Side threads are visible to any surface that asks for them,
but the home conversation is the one every surface lands on by default.

## Logs

Interactive mode owns the screen: log lines are silenced on stderr (JSON
file log still writes). Pass `ghost --debug-log <file>` to keep a
debug trail while chatting. One-shot `ghost -m` keeps stderr logs.

## Keys

| Key | Action |
|---|---|
| Enter | Send. With the `/` palette open: complete the highlighted command and run it. While working: queue a steering message |
| Tab | Complete the selected `/command` without running (for adding arguments) |
| Esc | Contextual cancel: close picker → dismiss approval (leaves pending) → dismiss palette → abort the running turn → close the TUI (the session persists, so quitting is safe) |
| Ctrl+C | Clear the editor; twice quits |
| Ctrl+L | Open the model picker |
| Ctrl+O | Toggle tool-detail expansion |
| ↑ / ↓ | `/` palette selection when open (wraps top↔bottom, scroll footer while long), else editor history |
| PgUp / PgDn, mouse wheel | Scroll the transcript |

## Layout

```
 transcript (markdown, streaming reply)

  You
  ┃ What do you remember about me?

  👻 Ghost · deepseek-flash
  Here's what I keep about you...

── ▘ Searching: weather in Bangkok · 4s · 2 tools ────────

────────────────────────────────────────────────────────────
3 turns                                  (cloud) deepseek-flash
/ commands · tab complete · ctrl+l model · esc quit
```

- **No top header:** the transcript owns the full height. State and model
  live in the footer — nowhere else.
- **Composer:** two full-width `─` rules with the text between them — no
  side borders, no `❯` prefix, no placeholder. Transparent inside: only
  the cursor is drawn, no line background. It is **responsive**: it opens
  at one text row and grows as the sentence wraps onto new rows, up to a
  cap of 30% of the terminal height (at least five rows), after which it
  scrolls inside the box. It is single-line input — there is no manual
  newline key — so growth comes from wrapping. While Ghost works, the live
  status embeds in the **top rule** — the only place the live state appears,
  never repeated in the transcript. A turning cube (`▖▘▝▗`) leads the
  status, then what Ghost is doing right now (`thinking`, or the active step
  such as `Searching the web…`), then elapsed time and tool count:

  ```
  ── ▖ thinking · 4s ──────────────────────────────────────
  ── ▘ Searching the web… · 4s · 1 tool ───────────────────
  ```

  The `─` rule lines keep their idle slate-violet colour at all times —
  only the status text is accented — so the prompt chrome never changes
  colour while Ghost works. The cube always animates: the tick restarts
  with each turn.
- **Full conversation, grouped by day:** launching loads the whole shared
  transcript (paged, capped at 500 rows) and opens scrolled to the latest
  turn — iMessage/WhatsApp style, with `Today` / `Yesterday` / date
  dividers wherever the calendar day flips.
- **Messages:** yours render as a name line and a single purple bar with
  the raw text preserved (never markdown-rendered, no panel background
  or side bars). Ghost's render as Ghost markdown — concealed
  markers (no ``` fences, `#`, backticks, or URLs), violet bold headings
  (h1 underlined), orange **bold**, sand *italics* and quotes, green code
  with no background, peach bullets, cyan ordered numbers and link text
  (underlined), green checked tasks.

  **Tables** render as a box-drawn grid, like the opencode CLI: a
  top/middle/bottom border, a violet bold header row, a separator between
  rows, and inline styling inside cells. Columns size to their content and
  shrink to fit the terminal, wrapping long cells; a table too narrow to
  render cleanly falls back to raw markdown rather than a broken box.

  ```
  ┌─────────┬───────────────────────────────┬────────┐
  │ Command │ Purpose                       │ Status │
  ├─────────┼───────────────────────────────┼────────┤
  │ /help   │ List commands and keybindings │ done   │
  └─────────┴───────────────────────────────┴────────┘
  ```
- **Autocomplete:** the `/` menu opens **below** the composer — bare rows
  with a `→ ` pointer on the selected row (no highlight background), a
  padded command column, a dim description, and a `(n/total)` scroll
  footer past five rows. `↑/↓` wraps top↔bottom, `Enter` completes the
  highlighted command and runs it, `Tab` completes without running (for
  adding arguments), `Esc` hides. Bare `/model` opens the model picker in
  the same below-box position (`↑↓` move, type to filter, `Enter` picks,
  `Esc` closes) — Ghost floats no modal over the transcript, and its
  selected row carries the same pointer, not a filled block.
- **Ghost palette** (one semantic scheme across the composer, menus, and
  approvals): violet = brand and selection (working composer rules,
  palette pointer text, picker title), gold = approvals and warnings
  only, green = success and done, red = errors, blue = info and links,
  dim = hints and metadata. The `/` menu and the model picker share the
  same selected-row language as the approval cursor.
- **Permissions:** the inline block — `┃ △ Permission required`, title,
  risk note, `[1] allow once [2] always allow [3] deny` with `←→`+`Enter`
  or direct `1/2/3` keys. `Esc` leaves the request pending. Its height is
  measured, so the dock never jumps between the wide one-row options and
  the narrow stacked options.
- **Activity lives in the prompt rule, not the transcript:** while a turn
  runs, the composer's top rule names what Ghost is doing right now — the
  active step (`Searching: weather in Bangkok`, `Reading notes.md`) when a
  tool runs, otherwise `thinking` — with elapsed time and tool count. The
  transcript is not littered with `searching…`/`reading…` rows under the
  user's message. The full icon tool trail (`→` read, `←` write, `✱`
  search, `%` fetch, `◈` web, `$` shell, `⚙` generic) is available on
  demand behind `/details` (Ctrl+O). Assistant turns carry `· duration`.
- **Footer (2 transparent dim lines):** a session digest (turns,
  contexts, queued) with the model in its locality color right-aligned;
  contextual shortcuts ordered by frequency — the command surface first
  and the exit route last (`/ commands · tab complete · ctrl+l model ·
  esc quit`).
- **Welcome card:** the empty state, centered — the Ghost banner, the
  Ghost tagline (`Your AI. Your Memory. Your Machine.`), and a command
  cheat-sheet. It only appears for a genuinely new conversation; the
  tagline lives here, not in the footer.
- **Scroll: the terminal's own scrollback.** Ghost runs in the terminal's
  main screen — no alternate screen and no mouse capture — and prints each
  committed message into the scrollback as it happens. Only the live
  region (the in-progress stream preview, the composer, the palette, the
  footer) is repainted in place. So scrolling history works exactly like
  the opencode CLI: your terminal's mouse wheel, scrollbar, and
  PageUp/PageDown reach every previous message, and nothing is ever
  ``hidden'' behind an app-owned viewport. This is the same model the
  opencode/pi CLI uses in its regular (non-fullscreen) mode.
- **Terminal hygiene:** stderr is parked during the run and logger output
  silenced, so a dependency's stray line cannot paint over the live
  composer (the one historical offender, a mid-turn `fmt.Printf` in the
  web tool, now goes through the logger). `--debug-log` keeps a file trail;
  non-TTY stdin refuses interactive mode with the `-m` remedy instead of
  hanging; quitting prints a session outro.

## Status line

`model · where it runs · session · state` — for example:

```
deepseek-flash · cloud · main · ready
```

While thinking: `… · thinking · 3 tools`. On a held approval: `waiting for you`.

## Approvals

When Ghost needs permission for something consequential, the composer is
replaced by an inline approval panel:

```
┃ △ Permission required   ◆ consequential
┃ Send this email?
┃ This acts on your behalf, so Ghost asks before doing it.
┃ [1] allow once    [2] always allow    [3] deny   ←→ select · enter confirm
```

The choice sends the recognized grant phrase as a normal turn, so it travels
through the **same governed resume path** the console and mobile use — the CLI
never authorizes around the broker. Dismissing the card (Esc) leaves the
request pending; nothing is claimed to have run.

## Contexts, not branches

`/context` switches the session into a topic space that scopes memory and
tools (for example `work`). This is Ghost's answer to keeping complex topics
separate. It deliberately does **not** fork the conversation: Ghost keeps one
durable, reconciled memory, so what Ghost knows stays a single truth rather
than a tree of conflicting branches.

## Models: everything usable is visible

The picker (`/model`), `Ctrl+L`, and `ghost model list` all read the same
switchable set: **named presets** first, then **named connections**, then
every **provider with a configured key** (e.g. deepseek appears the moment
its key exists — no preset entry required). Unkeyed presets still show,
marked with why they can't serve. Switching accepts a preset name, a
connection name, or `provider:model`.

## One shared conversation (terminal + app + channels)

The terminal, the app, the web console, and the owner's messaging channels
are all windows onto the same conversation, not separate chats that happen
to share a database. A message the owner sends on Telegram lands in `main`
next to a message typed in the terminal; a reply Ghost produced for the
terminal is visible to the app, tagged with its surface. Channel is
provenance; the conversation is one.

- The default session is `main` everywhere: terminal, app, web, and
  every owner messaging channel resolve to the same rows. (Pre-unification
  names `mobile:default` and `cli:default` canonicalize onto `main` at the
  gateway edge and in the app, and a database migration folds their stored
  rows — so upgrades never strand history.) `-s` / `/thread` still open side
  threads (listed by `/v1/sessions`), but the home conversation is shared.
- An allowlisted channel sender talks into `main`; the inbound message
  keeps its `channel`, `chat_id`, and `sender_id` so the reply routes back
  to the right chat, and the turn records the surface as provenance.
- The live app stream forwards a turn when it belongs to the shared
  conversation (by session), not merely when it arrived on the app's own
  channel — so a reply Ghost produced for another surface still reaches the
  app live, labelled with where it came from.
- Starting the TUI backfills the latest shared transcript (merging any
  pre-migration legacy rows chronologically), so it opens where the app
  left off — the welcome card only appears for a genuinely new `main`.
- When the gateway daemon runs, `ghost agent` is its thin client over the
  same HTTP+SSE surface as the app: same turns, same permission broker
  (approvals raised anywhere answer anywhere), same model and contexts,
  questions answerable in-band. Starting the TUI backfills the latest
  shared transcript, so it opens where the app left off.
- When the daemon is down, the CLI runs its embedded loop instead
  (offline-capable) and says so on stderr. That mode is honest local
  work: it rejoins the shared conversation only through the store, with
  no shared live state. Start the daemon (`ghost serve`) for full sync.

## Turning it off

`ghost -m "..."` stays one-shot and machine-friendly; the TUI is used
only when no message is passed and stdin is a terminal.
