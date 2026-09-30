# Ghost

**The internet in a box, learned around you.**

Ghost is a small computer that lives in your home and keeps learning who you
are. Ask it something and it finds the answer, browses the web for you, keeps
your reminders and routines, remembers the people and plans in your life, and
tells you when something needs you. It runs on hardware you own. Your memory,
your files and your logins stay there.

It is not a chatbot and not someone else's service. It is a way into information,
shaped around one person: you.

- **It runs on your hardware.** A Raspberry Pi 5 "Pod", or any Linux machine.
  A single Go program, no cloud account needed to run it.
- **It learns you.** It keeps a private, editable memory of the people, places,
  habits and plans you tell it about, and uses it in every conversation.
- **It does things, and shows its work.** It browses, reads and writes files,
  signs in to sites you allow, connects to your apps. It asks before anything
  consequential, and what it says it did is backed by evidence.
- **It reaches out when it matters.** Reminders, something wrong with the Pod,
  someone probing your network, a routine's result.

---

## What using it is like

| You say | Ghost does |
|---|---|
| "Search for the cheapest flights from Bangkok to Shenzhen on 15 October." | Uses its browser when a plain search isn't enough, compares the results, cites where each came from, and says which sources blocked it. |
| "Remind me tomorrow at 9 to look at the flights." | Sets a reminder that arrives on time as a notification, even with the app closed and the AI provider down. |
| "Every weekday at 8, remind me to take my vitamins." | Confirms once, then does it. See [Reminders and routines](#reminders-and-routines). |
| "Every Monday at 8, give me a brief of my week and the weather." | Runs it as a routine each Monday and delivers the result. |
| "My friend Sam is a mechanic in Chiang Mai." | Remembers Sam as a person, with the place and the relationship. `/memory` shows exactly what it kept and lets you delete it. |
| "Sign in to my site and check the dashboard." | Uses a login you saved. The password never passes through the model. |
| "Delete the old shenzhen note." | Asks, then deletes it from its own workspace. It will never delete its own files. |

And unprompted: it tells you when the Pod is running hot or nearly out of
storage, when the room gets too damp or cold (with a sensor attached), when a
model provider stops working, when a new version is out, and when something on
your network keeps trying to get in.

---

## Trust is the design

The model is never the authority. A model saying "done" is a claim. Runtime
evidence decides whether something happened.

- **One Permission Broker** decides allow, ask or deny. Unknown risk is denied.
  The model cannot grant itself anything. Approvals offer *once*, *for this
  task* (which ends after ten quiet minutes, or an hour at most), *always*, or
  *deny*.
- **Every reply says where it ran**: on the Pod, or on which cloud model.
- **Secrets are sealed** (AES-256-GCM) and are write-only from every screen.
  Site logins, API keys and tool-server keys are never shown back, never logged,
  and scrubbed from anything that could reach the model or a transcript.
- **Ghost's own files are protected**, from the model and from the shell. Its
  identity, memory, skills and database cannot be deleted or overwritten by a
  request, however it is phrased.
- **Strangers get nothing.** A messaging channel answers only the people you
  list. Everyone else is refused, and you are told they knocked.

Details: [docs/PERMISSION-BROKER.md](docs/PERMISSION-BROKER.md).

---

## Get started

### A Ghost Pod

Plug it in, open `http://ghost.local` from a phone or computer on the same
network, enter the setup code, choose how Ghost thinks, and you are done in about
ten minutes. The whole path, including where the setup code is and what to do if
`ghost.local` doesn't resolve, is in
**[docs/POD-QUICKSTART.md](docs/POD-QUICKSTART.md)**.

### Your own Linux machine

```bash
sudo apt install -y git make golang-go ffmpeg
git clone https://github.com/ianclemence/ghost.git
cd ghost
sudo make install-ghost
sudo reboot
```

`ffmpeg` is required for voice notes and spoken replies. After the reboot open
`http://<the machine's address>` and follow setup. It won't finish until Ghost
has an AI it can actually start with, and a refused attempt leaves nothing behind,
so you can correct it and retry.

Windows: run `setup.bat`. Developers who want an isolated instance that never
touches an installed Ghost: `ghost dev`.

### Connect your phone

Run `ghost pair` on the Pod for a QR code, or use **Devices** in the console.
Scan it in the Ghost app. Each phone gets its own credential, the code works
once and lasts five minutes, and turning on notifications when asked is what lets
Ghost reach you when the app is closed.

---

## Four ways to talk to it, one conversation

| Surface | Good for |
|---|---|
| **The Ghost app** | Daily use: talk, approve, see what Ghost is doing in its browser, browse memory and files. |
| **The web console** | The control plane: Apps, Channels, Devices, Memory, Files, Routines, System, Security. |
| **The terminal** (`ghost`) | Fast and keyboard-first. `/memory`, `/activity`, `/devices`, `/files`, `/attach`. |
| **Channels** | Telegram, Discord, Slack, WhatsApp and email, for the people you allow. |

They are one conversation, not four. Send a message from the terminal and open
the app: the question is there and the answer arrives as it is written. A reminder
that comes due shows up in every open surface, labelled as a reminder.

---

## Reminders and routines

Three things that sound alike, and are different.

| | What it is | How it runs |
|---|---|---|
| **Reminder** | One time. "Remind me tomorrow at 9 to call Sam." | Delivered on time as plain text. No AI call, so it arrives even if the provider is down. If the Pod was off, it arrives late and says so. |
| **Recurring reminder** | A reminder that repeats. "Every weekday at 8, remind me to take my vitamins." | The same deterministic delivery, on every occurrence. |
| **Routine** | A job for Ghost that repeats. "Every Monday at 8, give me a brief of my week." | Ghost does the work each time (it may use tools) and reports the result, labelled as a routine. |

Ghost understands recurrence in the way people say it: *every weekday*,
*weekends*, *Mondays and Thursdays*, *every morning*, *8am every day*, *every
other day*, *every 3 days*, *hourly*, *monthly on the 15th*. It always says the
time back, including one it assumed (a bare "every day" is 9 AM), and asks for a
yes before creating a routine. Manage them in **Routines** in the console or
`ghost tasks` (pause, resume, cancel).

If the Pod was off when a recurring **routine** came due, it is skipped, not
replayed hours late (a morning brief at nine at night is noise), and Ghost tells
you it skipped it. A missed **reminder** is a commitment, so it is still delivered,
with a note saying it was due earlier.

---

## Files and the workspace

Ghost has a workspace of its own. It can create folders, write notes, tidy them
into place, move things, and delete what it made or what you ask it to remove.
It cannot reach outside it, follow a link out of it, or touch its own foundations.

- Screenshots and downloads are kept in the workspace, not in temporary folders.
- Deleting always asks first, and cannot be undone.
- Its default files and folders (its identity and prompt files, `memory`, `skills`,
  `sessions`, `state`, `uploads`, and the rest) can never be deleted or moved,
  through its tools or through the shell.
- Files you send it are listed under **Files** in the app and console, and can be
  opened, previewed or deleted there.

---

## Connecting things

**Apps** (Gmail, Calendar, Outlook and Spotify by browser sign-in; GitHub,
Notion, weather and flight data by key; Home Assistant by address and token).
Keys are tried against the service before Ghost says "connected": a refused key
is explained and not saved, and a service that can't be reached is saved with an
honest note.

**Website logins.** Save a login once under Apps. Ghost signs in with it without
the password passing through the model, and only to the site it belongs to. If a
site asks for a human check (a CAPTCHA, "Verify you are human"), Ghost says so
and stops; it does not try to get around it.

**Tool servers (MCP).** Add an outside server by address and key on the console's
Apps page. The connection is tested before anything is saved, the key is sealed,
and its tools appear at once with no restart. Servers that run a command on the Pod
stay a terminal decision (`ghost mcp add`), because that is running code.

**Channels.** Telegram, Discord, Slack, WhatsApp and email answer only the people
you list under **Channels**. Empty means nobody.

---

## Away from home

At home the phone talks straight to the Pod. Away from home you need a way in:
Tailscale, your own relay, or (planned) **Ghost Connect**. The reasoning, what is
free, and what must be true before anyone is charged are in
[docs/CONNECT.md](docs/CONNECT.md). Push notifications do not need any of it.

---

## Backups, recovery and a forgotten password

```bash
ghost state backup                        # a recovery snapshot (the newest five are kept)
GHOST_MASTER_KEY=<key> ghost state import <file>   # restore onto a fresh install
```

A snapshot is made before every update. A backup on the Pod dies with the Pod,
and it can only be opened with the Pod's key (`GHOST_MASTER_KEY`, in the Ghost
config directory). Copy both somewhere safe, separately. Details in
[docs/USER.md](docs/USER.md).

**Forgot the console password?** Nothing is lost and no terminal is needed.
On the sign-in page choose *Forgot your password?* and either:

- get a one-time code in the app (**Your Pod**, then **Web console**) and enter it
  with a new password, or
- take the SD card out, and on the `boot` drive create a text file named
  `ghost-reset-password` with the new password on its first line.

Or, with a terminal: `sudo ghost reset-password --force`. Your memory, files and
paired phones are never touched.

---

## Updating

```bash
ghost update            # deploy the newest release
ghost update --check    # installed, available, and what is actually running
ghost update --notes    # what changed
```

Ghost tells you when a new version is out and never installs one on its own. You
can also update from **Your Pod** in the app. A recovery snapshot is taken first,
and if it can't be, the update stops and says why.

---

## Commands

| Command | What it does |
|---|---|
| `ghost` | Chat in the terminal |
| `ghost pair [name]` | Show a QR to connect the app |
| `ghost tasks` | Routines: list, pause, resume, cancel |
| `ghost ideas` | Suggestions with the evidence behind them |
| `ghost model [list\|use provider:model]` | See or change the model |
| `ghost mcp list\|add\|edit\|remove\|test` | Tool servers |
| `ghost connector ...` | Portable connectors (validate, install, sign) |
| `ghost skills ...` | List, add, remove, enable, disable skills |
| `ghost state backup\|export\|import\|inspect\|prune` | Snapshots and portable state |
| `ghost update` | Update Ghost |
| `ghost auth reset-password` | Reset the console password |
| `ghost status`, `ghost verify` | Health and the real product checks |
| `ghost reset` | Factory reset (with exclusions) |
| `ghost relay ...` | Your own relay |

Inside a chat: `/memory`, `/activity`, `/devices`, `/files`, `/attach <path>`,
`/model`, `/context`, `/routines`, `/tasks`, `/ideas`, `/thread`, `/main`, `/rewind`,
`/details`, `/clear`, `/help`.

---

## Configuration

Precedence, highest first: environment variables, then `.secrets.json` (sealed;
API keys and tokens), then `config.json` (model, providers, channels), then
defaults. Secrets are set from the console or app, never by editing files. `.env`
holds only system settings (`GHOST_API_PORT`, `TZ`).

| Where | What |
|---|---|
| `/var/ghost/config/` | `config.json`, sealed secrets, the master key. Mode `0700`. |
| `/var/lib/ghost/workspace/` | Identity, memory, skills, sessions, files. Owned by whoever installed Ghost. |
| `/var/lib/ghost/backups/` | Recovery snapshots. |

The services run as root; every file they create in your workspace and config is
handed back to the account that installed Ghost, so your own terminal can always
read them.

---

## Development

```bash
make build               # both binaries
go vet ./... && go test ./...
ghost dev                # an isolated instance: own directory and port
```

| Tool | Use |
|---|---|
| `scripts/tui-pty.py COLS ROWS steps.json` | Drive the terminal in a real PTY and see what a user would see |
| `scripts/phone-sim.py --host <ip>` | A simulated phone: pairs, checks auth, chat, files, push, the live connection, updates |
| `scripts/preview/console.sh start` | The console on synthetic data, with screenshots |
| `scripts/fresh-install.sh` | A clean-room install in a sandbox |

The Ghost app is a separate repository: [ghost-app](https://github.com/ianclemence/ghost-app).

## Documentation

| | |
|---|---|
| [docs/README.md](docs/README.md) | How Ghost works, one document per layer |
| [docs/POD-QUICKSTART.md](docs/POD-QUICKSTART.md) | From the box to the first conversation |
| [docs/USER.md](docs/USER.md) | Using Ghost day to day, backups and recovery |
| [docs/CONNECT.md](docs/CONNECT.md) | Reaching Ghost away from home; the relay decision |
| [docs/PERMISSION-BROKER.md](docs/PERMISSION-BROKER.md) | The approval model |
| [docs/MOBILE-API.md](docs/MOBILE-API.md) | How the app connects, and the gateway API |
| [pkg/changelog/CHANGELOG.md](pkg/changelog/CHANGELOG.md) | What changed in every release |

## License

See [LICENSE](LICENSE).
