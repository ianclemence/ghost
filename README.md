# Ghost

**Your AI. Your Memory. Your Machine.**

Ghost is a personal AI that lives on a small computer in your home, called a
Pod. It remembers the people and plans in your life, does things for you (it
browses the web, keeps your reminders and routines, works with your files and
apps), and speaks up when something needs you. You reach it from your phone, a
browser or a terminal, and it is the same Ghost everywhere.

It is yours in a way a service can't be. Your memory, your files and your logins
stay on the Pod. You choose which AI model it thinks with, in the cloud or on the
machine itself, and you can change your mind without losing anything.

---

## What makes it Ghost

- **It stays.** One Ghost, with one memory, across days, devices, restarts and
  changes of model. It isn't a conversation you start over.
- **It learns you.** It keeps a private, editable memory of your people, places,
  habits and plans, and uses it in every conversation. You can read all of it and
  delete any of it.
- **It acts, and shows its work.** It searches and browses, signs in to sites you
  allow, reads and writes files, connects to your apps. What it says it did is
  backed by evidence, not by the model's word.
- **It reaches out.** A reminder, a routine's result, something wrong with the
  Pod, someone probing your network.
- **You decide.** Anything consequential waits for your yes.

---

## A day with Ghost

| You say | Ghost |
|---|---|
| "Find the cheapest flights from Bangkok to Shenzhen on 15 October." | Uses its browser when a plain search isn't enough, compares what it finds, and tells you where each answer came from and which sites blocked it. |
| "Remind me tomorrow at 9 to look at the flights." | Sends it on time, even with the app closed and the AI provider down. |
| "Every Monday at 8, give me a brief of my week and the weather." | Does it each Monday and delivers the result. |
| "My friend Sam is a mechanic in Chiang Mai." | Remembers Sam, with the place and the relationship, and shows you exactly what it kept. |
| "Sign in to my site and check the dashboard." | Uses the login you saved. The password never passes through the model. If the site wants a human check, it stops and lets you do that step yourself. |

And unprompted: it tells you when the Pod is running hot or short of storage,
when a new version is out, and when something on your network keeps trying to get in.

---

## You stay in control

A model saying "done" is only a claim. Ghost decides what happened from evidence,
and it decides what is allowed with one rule: a single Permission Broker allows,
asks or denies, and the model cannot grant itself anything. Your secrets are
sealed on the Pod and are never shown back or sent to the model. Ghost's own
identity and memory files can't be deleted by a request, however it is phrased.

More in [the Permission Broker](docs/PERMISSION-BROKER.md).

---

## Get started

**With a Pod.** Plug it in, open `http://ghost.local` on the same network, enter
the setup code, choose how Ghost thinks, and you are talking to it in about ten
minutes. The whole path is in [Your Pod, from the box to your first
conversation](docs/POD-QUICKSTART.md). What to buy or reuse is in [What a Pod
needs](docs/HARDWARE.md).

**On your own Linux machine.**

```bash
sudo apt install -y git make golang-go ffmpeg
git clone https://github.com/ianclemence/ghost.git
cd ghost
sudo make install-ghost
sudo reboot
```

Then open `http://<the machine's address>` and follow setup. (`ffmpeg` is for
voice notes and spoken replies. On Windows, run `setup.bat`.)

**On your phone.** Run `ghost pair` on the Pod for a QR code, and scan it in the
[Ghost app](https://github.com/ianclemence/ghost-app).

---

## Where to go next

| If you want to | Read |
|---|---|
| Use Ghost day to day: reminders, files, connecting apps, updating, backups | [Using Ghost](docs/GUIDE.md) |
| Know what hardware a Pod needs | [What a Pod needs](docs/HARDWARE.md) |
| Reach Ghost when you are away from home | [Away from home](docs/CONNECT.md) |
| Look up a command or a setting | [Commands and configuration](docs/COMMANDS.md) |
| Understand how Ghost works and why it can be trusted | [How Ghost works](docs/README.md) |
| See what changed | [Changelog](pkg/changelog/CHANGELOG.md) |
| Work on Ghost | [Developing Ghost](docs/DEVELOPMENT.md) |

The phone app lives in its own repository:
[ghost-app](https://github.com/ianclemence/ghost-app).

## License

See [LICENSE](LICENSE).
