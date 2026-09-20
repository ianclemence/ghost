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
| `/model [name]` | Show or switch the active model (presets first) |
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
| Enter | Send. While Ghost is working: queue a steering message |
| Tab | Complete the selected `/command` |
| Esc | Abort the current turn; queued text returns to the editor |
| Ctrl+C | Clear the editor; twice quits |
| Ctrl+L | Open the model picker |
| Ctrl+O | Toggle tool-detail expansion |
| ↑ / ↓ | `/` palette selection when open, else editor history |
| PgUp / PgDn | Scroll the transcript |

## Layout (pi-style)

```
 ├ transcript (markdown, tool trail, spinner+elapsed) ─┤
 │ /palette when typing / (↑/↓ + Tab)                  │
 │ ╭── ⠋ working · 4s · 2 tools ─────────────────╮   │
 │ │ ❯ …                                           │   │
 │ ╰───────────────────────────────────────────────╯   │
 │ cli:default • personal                              │
 │ ready · 3 turns              (cloud) deepseek-flash │
 └─────────────────────────────────────────────────────┘
```

- **No top header:** like pi, the transcript owns the full height. Model,
  session and turn state live in the footer — nowhere else.
- **Prompt box:** rounded border, no label above it. While a turn runs the
  working status is embedded in the top border
  (`── ⠋ working · 4s · 2 tools ──`, pi's `CustomEditor` pattern) and the
  border takes the accent color. The placeholder carries the hints.
- **Footer (2 dim lines, pi's `FooterComponent` pattern):**
  `session • context`, then left activity (`ready`, `N turns`,
  `⠋ working · 4s · 2 tools`, `waiting for you`) with
  `(locality) model` right-aligned.

## Status line

`model · where it runs · session · state` — for example:

```
deepseek-flash · cloud · cli:default · ready
```

While working: `… · working · 3 tools`. On a held approval: `waiting for you`.

## Approvals

When Ghost needs permission for something consequential, the editor is
replaced by an inline approval card:

```
⚑ Send this email?
  This acts on your behalf, so Ghost asks before doing it.
  [1] allow once   [2] always allow   [3] deny
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

## Turning it off

`ghost agent -m "..."` stays one-shot and machine-friendly; the TUI is used
only when no message is passed and stdin is a terminal.
