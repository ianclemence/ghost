# Ghost Agent CLI (interactive)

The interactive `ghost agent` experience. It is Ghost's daily terminal surface:
talk to Ghost, watch it work, approve what it asks, and see where each answer
came from. The behavior, commands, and status are Ghost's own — trust, memory,
routines, and provenance are first-class here.

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
| `/model [name]` | Show or switch the active model (presets, connections, keyed providers) |
| `/details` | Toggle tool step details (durations) |
| `/new` | Open a **side thread** — a tangent, not the shared conversation |
| `/main` | Return to the shared conversation (reloads its rows) |
| `/session` | Where this terminal is, the model, and turn count |
| `/memory [query]` | Ask Ghost, in a turn, what it remembers |
| `/context [name]` | Show or switch topic context (scopes memory + tools) |
| `/rewind` | Put the last message back in the editor to edit and resend |
| `/routines` | Ask Ghost, in a turn, what it has scheduled |
| `/clear` | Clear the screen (keeps the conversation) |
| `/quit` | Exit (also Ctrl+C twice) |

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

`/new` opens a *side thread* for a tangent; `/main` returns to the shared
conversation. Side threads are visible to any surface that asks for them,
but the home conversation is the one every surface lands on by default.

## Logs

Interactive mode owns the screen: log lines are silenced on stderr (JSON
file log still writes). Pass `ghost agent --debug-log <file>` to keep a
debug trail while chatting. One-shot `ghost agent -m` keeps stderr logs.

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
 transcript (markdown, icon tool trail, spinner + elapsed)

  What do you remember about me?

  👻 Ghost · deepseek-flash
  Here's what I keep about you...

── ⠋ working · 4s · 2 tools ───────────────────────────────

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
  status (`spinner · elapsed · tools · queued`) embeds in the **top rule**
  and the rules take the Ghost-violet working accent (slate-violet at
  idle).
- **Full conversation, grouped by day:** launching loads the whole shared
  transcript (paged, capped at 500 rows) and opens scrolled to the latest
  turn — iMessage/WhatsApp style, with `Today` / `Yesterday` / date
  dividers wherever the calendar day flips.
- **Messages:** yours render as a full-width background panel with the
  text inset by one cell and paragraphs preserved (never
  markdown-rendered). Ghost's render as Ghost markdown — concealed
  markers (no ``` fences, `#`, backticks, or URLs), violet bold headings
  (h1 underlined), orange **bold**, sand *italics* and quotes, green code
  with no background, peach bullets, cyan ordered numbers and link text
  (underlined), green checked tasks.
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
- **Tool trail:** collapsed one-liners with the Ghost icon language
  (`→` read, `←` write, `✱` search, `%` fetch, `◈` web, `$` shell,
  `⚙` generic), spinner row while running, durations behind `/details`.
  Assistant turns carry `· duration`.
- **Footer (2 transparent dim lines):** a session digest (turns,
  contexts, queued) with the model in its locality color right-aligned;
  contextual shortcuts ordered by frequency — the command surface first
  and the exit route last (`/ commands · tab complete · ctrl+l model ·
  esc quit`).
- **Welcome card:** the empty state, centered — the Ghost banner, the
  Ghost tagline (`Your AI. Your Memory. Your Machine.`), and a command
  cheat-sheet. It only appears for a genuinely new conversation; the
  tagline lives here, not in the footer.
- **Scroll:** mouse wheel + `PgUp/PgDn`; follows the bottom while working.
  Every region is edge to edge on the same canvas, with only text inset
  by one cell, so messages, the rules, and the footer all align.
- **Terminal hygiene (terminal-ui skill):** the TUI owns the whole frame —
  stderr is parked during the run and logger output silenced, so no
  dependency's stray line can paint over the alt-screen (the one
  historical offender, a mid-turn `fmt.Printf` in the web tool, now goes
  through the logger). `--debug-log` keeps a file trail; non-TTY stdin
  refuses interactive mode with the `-m` remedy instead of hanging;
  quitting prints a session outro.

## Status line

`model · where it runs · session · state` — for example:

```
deepseek-flash · cloud · main · ready
```

While working: `… · working · 3 tools`. On a held approval: `waiting for you`.

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
  rows — so upgrades never strand history.) `-s` / `/new` still open side
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

`ghost agent -m "..."` stays one-shot and machine-friendly; the TUI is used
only when no message is passed and stdin is a terminal.
