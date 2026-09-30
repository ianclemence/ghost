# What a Pod needs

A Pod is the always-on machine Ghost lives on. It does the remembering, the
scheduling and the browsing. The thinking is done by the model you chose, which
by default runs in the cloud, so a Pod does not need a graphics card.

## Specs

| | Minimum | Recommended | For a local model |
|---|---|---|---|
| Processor | 4 cores | 4 cores or more | 8 cores, or a GPU or NPU |
| Memory | 4 GB | **8 GB** | 16 GB or more |
| Storage | 64 GB SSD | **256 GB SSD** | 512 GB SSD |
| Power draw | under 15 W | 5 to 10 W | depends on the accelerator |
| Network | wired or Wi-Fi | wired | wired |

- **Memory** is spent mostly on the browser. Ghost runs one browser job at a
  time so a busy Pod stays responsive. Below 4 GB, web tasks get slow.
- **Storage** should be a solid-state drive, not a memory card. Ghost writes all
  day (memory, files, backups, screenshots), which wears cards out, and a power
  cut is when a worn card corrupts. Ghost tells you if it finds itself on one.
- **A local model** is optional and is the only reason to want more. It is
  private and works offline, but on modest hardware it is slower and less
  capable than a cloud model. Most owners should start in the cloud.

For an example, the Pod Ghost is developed on has 4 cores, 8 GB of memory and an
SSD. That is a working example, not a requirement.

## Always on

A Pod has to be a quiet appliance, not a computer you look after:

- **Low power.** Aim for 5 to 10 W, about the cost of a nightlight.
- **Cool and silent.** No loud fan. If the Pod gets hot (80°C) Ghost tells you
  to check airflow. Give it space and keep it off the carpet.
- **Starts itself.** Ghost starts by itself whenever the Pod powers on, with no
  one at a keyboard. Choose hardware that powers on by itself when power returns.

## Power cuts

Pods lose power. Ghost is built so that does not cost you anything:

- The database is journaled, so a cut in the middle of a write leaves the last
  complete state, not a half-written one.
- On every start Ghost checks the database and refuses to serve one that is
  damaged, pointing you to the last backup instead of serving wrong memories.
- Ghost notices when it was cut off instead of stopped (it keeps a marker while
  running and removes it on a clean stop). When that happens it tells you: it
  restarted after being cut off, and your memory checked out.
- Backups are taken before every update and on a schedule (see
  [USER.md](USER.md)).

If cuts are common where you live, a small battery backup (a UPS, or a power bank
that can power the Pod while charging) is the fix. Ghost's message says so.
It does not yet count how often cuts happen.

## When the Pod is too small

Ghost watches its own memory, storage, temperature and drive type, and speaks
early, at the first warning and not when it is already slow. Each message says
what is wrong and what to do:

| It says | Meaning | What to do |
|---|---|---|
| Storage is getting low | Under 15% or 3 GB free | Clear old downloads and screenshots in Files. Later, a bigger drive. |
| I'm almost out of storage | Under 7% or 1 GB free (urgent) | Clear space now, or move to a bigger drive. |
| Memory is getting tight | Under 15% or 700 MB free | Nothing yet. Ghost does one browser job at a time. |
| Short on memory | Under 8% or 350 MB free | Let a running browser task finish. |
| Short on memory for a while | Half an hour of low memory | The Pod is too small for how you use it. Move to 8 GB or more. |
| Too little memory or storage | Under the minimums above | Said once, up front. |
| Running from a memory card | The system drive is an SD or eMMC card | Move to an SSD. |
| Restarted after being cut off | The last run did not stop cleanly | Check the power. Consider a battery backup. |

Each message is repeated at most every few days, so a Pod that is a little short
does not nag.
