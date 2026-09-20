# Ghost Agent CLI (interactive)

The interactive `ghost agent` experience. It is Ghost's daily terminal surface:
talk to Ghost, watch it work, approve what it asks, and see where each answer
came from. The design is inspired by the pi coding agent's terminal UX, but the
behavior, commands, and status are Ghost's own — trust, memory, routines, and
provenance are first-class here in a way a coding agent does not need.

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
| `/new` | Start a fresh conversation |
| `/sessions` | Show the current session key and turn count |
| `/memory [query]` | Ask Ghost what it remembers (read-only) |
| `/context [name]` | Show or switch topic context (scopes memory + tools) |
| `/rewind` | Put the last message back in the editor to edit and resend |
| `/routines` | List the things Ghost does for you |
| `/clear` | Clear the screen (keeps the session) |
| `/quit` | Exit (also Ctrl+C twice) |

## Logs

Interactive mode owns the screen: log lines are silenced on stderr (JSON
file log still writes). Pass `ghost agent --debug-log <file>` to keep a
debug trail while chatting. One-shot `ghost agent -m` keeps stderr logs.

## Keys

| Key | Action |
|---|---|
| Enter | Send. With the `/` palette open: complete the highlighted command and run it. While working: queue a steering message |
| Ctrl+J | Newline in the composer (grows to 5 rows, then scrolls) |
| Tab | Complete the selected `/command` without running (for adding arguments) |
| Esc | Contextual cancel, like pi and opencode: close dialog → dismiss approval (leaves pending) → dismiss palette → abort the running turn → close the TUI (the session persists, so quitting is safe) |
| Ctrl+C | Clear the editor; twice quits |
| Ctrl+L | Open the model picker |
| Ctrl+O | Toggle tool-detail expansion |
| ↑ / ↓ | `/` palette selection when open (scrolls with `↑/↓ more` edges), else editor history |
| PgUp / PgDn, mouse wheel | Scroll the transcript |

## Layout (opencode-faithful)

```
 ├ transcript (markdown, icon tool trail, spinner+elapsed) ─┤
 │ ┃ /model     switch thinking engine   (autocomplete menu)│
 │ ┃ Ask Ghost anything…  e.g. "Remind me…"  (composer)     │
 │ main • personal                                 │
 │ 3 turns                            (cloud) deepseek-flash │
 │ enter send · esc abort · ctrl+l model · / commands        │
 └───────────────────────────────────────────────────────────┘
```

- **No top header:** like pi and opencode, the transcript owns the full
  height. Model, session and turn state live in the footer — nowhere else.
- **Composer:** opencode's left-`┃`-border panel (slate-violet idle,
  Ghost-violet while working), no `❯` prefix, rotating
  `Ask Ghost anything… "example"` placeholder. Fixed 3-row height, so
  the bar and the editor can never disagree and typing never shifts
  the layout — longer input scrolls inside the box. Multi-line with
  `Ctrl+J`. The working status lives in the footer, not the composer
  chrome.
- **Full conversation, grouped by day:** launching loads the whole shared
  transcript (paged, capped at 500 rows) and opens scrolled to the
  latest turn — iMessage/WhatsApp style, with `Today` / `Yesterday` /
  date dividers wherever the calendar day flips. The welcome card only
  appears for a genuinely new conversation.
- **Messages:** yours render as a left-bar panel with paragraphs preserved
  (never markdown-rendered, like opencode); Ghost's render as opencode
  markdown — concealed markers (no ``` fences, `#`, backticks, or URLs),
  violet bold headings (h1 underlined), orange **bold**, sand *italics*
  and quotes, green code with no background, peach bullets, cyan ordered
  numbers and link text (underlined), green checked tasks.
- **Autocomplete:** opencode rules — `↑/↓` moves (the list scrolls with
  `↑/↓ more` edges past 6 rows), `Enter` completes the highlighted
  command and runs it, `Tab` completes without running (for adding
  arguments), `Esc` hides. Bare `/model` opens the centered model
  picker (`↑↓` move, type to filter, `Enter` picks, `Esc` closes).
- **Ghost palette** (one semantic scheme across composer, menus, modal,
  and approvals — reverse-engineered from pi's `DynamicBorder` selectors
  and opencode's `dialog.select`): violet = brand and selection
  (composer bar while working, dark-on-violet selected rows, modal
  title and border), gold = approvals and warnings only, green =
  success and done, red = errors, blue = info and links, dim = hints
  and metadata. The `/` menu and the model picker share the same
  selected-row language as the approval cursor.
- **Permissions:** opencode's inline block — `┃ △ Permission required`,
  title, risk note, `[1] allow once [2] always allow [3] deny` with
  `←→`+`Enter` or direct `1/2/3` keys. `Esc` leaves the request pending.
- **Tool trail:** collapsed one-liners with opencode's icon language
  (`→` read, `←` write, `✱` search, `%` fetch, `◈` web, `$` shell,
  `⚙` generic), spinner row while running, durations behind `/details`.
  Assistant turns carry `· duration` (opencode `▣ Mode · model · duration`).
- **Footer (3 transparent dim lines):** `session • context`; activity with
  the model in its locality color right-aligned; contextual shortcuts
  naming the escape routes for the current state.
- **Scroll:** mouse wheel + `PgUp/PgDn`; follows the bottom while working.
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
replaced by an inline approval panel (opencode's permission-block pattern):

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

## One shared conversation (terminal + app)

The terminal and the app are two windows onto the same conversation —
opencode's CLI/desktop arrangement, not two chats that happen to share a
database:

- The default session is `main` everywhere: terminal, app, and
  gateway resolve to the same rows. (Pre-unification names
  `mobile:default` and `cli:default` canonicalize onto `main` at the
  gateway edge and in the app, and a database migration folds their
  stored rows — so upgrades never strand history.) `-s` / `/new` still
  open side threads (listed by `/v1/sessions`), but the home
  conversation is shared.
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
