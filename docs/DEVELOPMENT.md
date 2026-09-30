# Developing Ghost

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


The Ghost app is a separate repository:
[ghost-app](https://github.com/ianclemence/ghost-app).

Before you change how Ghost decides what it may do, read
[the Permission Broker](PERMISSION-BROKER.md) and [How Ghost works](README.md).
