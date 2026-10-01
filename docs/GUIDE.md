# Using Ghost

Day-to-day use, in the order you are likely to need it. If you are still setting
up, start with [Your Pod, from the box to your first conversation](POD-QUICKSTART.md).

## Talking to Ghost

Ghost is one conversation that you can reach four ways.

| Surface | Good for |
|---|---|
| **The Ghost app** | Daily use: talk, approve, see what Ghost is doing in its browser, browse memory and files. |
| **The web console** | The control plane: Apps, Channels, Devices, Memory, Files, Routines, System, Security. |
| **The terminal** (`ghost`) | Fast and keyboard-first. `/memory`, `/activity`, `/devices`, `/files`, `/attach`. |
| **Channels** | Telegram, Discord, Slack, WhatsApp and email, for the people you allow. |

They are one conversation, not four. Send a message from the terminal and open
the app: the question is there and the answer arrives as it is written. A reminder
that comes due shows up in every open surface, labelled as a reminder.

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

## Connecting your apps

Ghost works with your other apps in four ways. Each is set up in the console
under **Apps**, or in the app under **Connected apps**.

**By key** (GitHub, Notion, weather and flight data). Paste the key. It is tried
against the service before Ghost says "connected": a refused key is explained and
not saved, and a service that can't be reached is saved with an honest note.

**By address and token** (Home Assistant).

**By signing in** (Google Calendar, Gmail, Outlook, Spotify). These four only let
an app you have registered with them sign in, and Ghost does not ship one, so the
first time you connect one the console walks you through registering your own.
It takes a few minutes, is free, and is done once per provider (Google Calendar
and Gmail share one). You paste the app's ID and secret, which are stored sealed
on the Pod, then approve access in your browser. That last page says it can't be
reached: that is expected, and it means the sign-in worked. Copy its address and
paste it back into the console.

Two things to know. With Google, choose *Publish app* on the consent screen, or
Google disconnects Ghost every seven days. And a one-click sign-in, with none of
this, needs a Ghost-owned app and a public address, which is what
[Ghost Connect](CONNECT.md) is for.

**Website logins.** Save a login once under Apps. Ghost signs in with it without
the password passing through the model, and only to the site it belongs to. If a
site asks for a human check (a CAPTCHA, "Verify you are human", a code sent to
your phone), Ghost says so and stops; it does not try to get around it. Open its
browser card in the app, tap **Take over and steer**, do that step yourself, and
tap **Done**. Ghost carries on from where you left it.

**Tool servers (MCP).** Add an outside server by address and key on the console's
Apps page. The connection is tested before anything is saved, the key is sealed,
and its tools appear at once with no restart. Servers that run a command on the
Pod stay a terminal decision (`ghost mcp add`), because that is running code.

**Channels.** Telegram, Discord, Slack, WhatsApp and email answer only the people
you list under **Channels**. Empty means nobody.

## Away from home

At home the phone talks straight to the Pod. Away from home you need a way in:
Tailscale, your own relay, or **Ghost Connect**, the hosted relay, which is in
early access. The reasoning, what is free, and what must be true before anyone is
charged are in [docs/CONNECT.md](docs/CONNECT.md). Push notifications do not need
any of it.

**Ghost Connect, from the console.** Open **Devices**, and under **Away from home**
choose **Link to Ghost Connect**. It shows a short code and an address: open the
address, sign in, check the code matches, and choose a plan. The Pod links itself and
connects, with no restart. Then choose **Connect a phone for away from home** and scan
the QR in the app. Everything a phone sends through the relay is sealed on the phone
and opened only by your Pod, so the relay carries it and can't read it. If the plan
ever lapses, only the relay stops; the Pod keeps working at home.

The same from a terminal: `ghost relay link` (links), `ghost relay status`,
`ghost relay pair` (a QR for a phone) and `ghost relay unlink`.

## Looking after your Ghost

### Updating

```bash
ghost update            # deploy the newest release
ghost update --check    # installed, available, and what is actually running
ghost update --notes    # what changed
```

Ghost tells you when a new version is out and never installs one on its own. You
can also update from **Your Pod** in the app. A recovery snapshot is taken first,
and if it can't be, the update stops and says why.

### Backups and a forgotten password

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

### Health

Ghost watches its own Pod and tells you early: low storage or memory, a hot
processor, running from a memory card, a restart after a power cut. What it says
and what to do is in [What a Pod needs](HARDWARE.md).
