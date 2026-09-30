#!/usr/bin/env python3
"""Drive the Ghost terminal in a real PTY and print what a user would see.

Renders the byte stream through a terminal emulator (pyte), including
scrollback, so layout bugs (duplicate docks, vanished rows, stale frames)
are visible exactly as in a real terminal.

    pip install pyte pexpect
    scripts/tui-pty.py COLS ROWS steps.json [-- ghost-binary args...]

steps.json is a list of:
    ["type", "text"]   send text char by char
    ["keys", "text"]   send text at once (a paste)
    ["key",  "<enter>"] named key: <enter> <esc> <tab> <up> <down> <bs> <ctrl-c>
    ["sleep", 1.0]
    ["snap", "label"]  print the visible screen + last scrollback lines
"""
import json, os, sys, time
import pexpect, pyte

cols, rows = int(sys.argv[1]), int(sys.argv[2])
steps = json.load(open(sys.argv[3]))
cmd = sys.argv[5:] if len(sys.argv) > 4 and sys.argv[4] == "--" else ["ghost"]
screen = pyte.HistoryScreen(cols, rows, history=5000)
stream = pyte.ByteStream(screen)
env = dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor")
p = pexpect.spawn(cmd[0], cmd[1:], dimensions=(rows, cols), env=env)
KEYS = {"<enter>": "\r", "<esc>": "\x1b", "<tab>": "\t", "<down>": "\x1b[B",
        "<up>": "\x1b[A", "<bs>": "\x7f", "<ctrl-c>": "\x03"}

def pump(t):
    end = time.time() + t
    while time.time() < end:
        try:
            stream.feed(p.read_nonblocking(65536, timeout=0.05))
        except pexpect.TIMEOUT:
            pass
        except pexpect.EOF:
            break

def snap(label):
    hist = ["".join(line[x].data for x in range(cols)).rstrip() for line in screen.history.top]
    print(f"===== {label} (scrollback {len(hist)} lines) =====")
    for l in (hist if os.environ.get("FULL") else hist[-6:]):
        print("H|" + l)
    for i, l in enumerate(screen.display):
        print(f"{i:02d}|" + l.rstrip())
    print(f"cursor: row={screen.cursor.y} col={screen.cursor.x}")

pump(3.0)
for kind, arg in steps:
    if kind == "type":
        for ch in arg:
            p.send(ch); pump(0.02)
        pump(0.5)
    elif kind == "keys":
        p.send(arg); pump(0.6)
    elif kind == "key":
        p.send(KEYS[arg]); pump(0.5)
    elif kind == "sleep":
        pump(float(arg))
    elif kind == "snap":
        pump(0.4); snap(arg)
p.terminate(force=True)
