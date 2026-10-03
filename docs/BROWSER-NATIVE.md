# Native Browser Engine (Go)

`pkg/browser/native` is a browser automation engine written in Go that speaks
the Chrome DevTools Protocol directly. It is a deliberate reverse-engineering
of the vercel-labs `agent-browser` daemon (`cli/src/native`, Rust): the same
layered design, reimplemented on Ghost's own terms so the browser layer is
owned rather than rented from a third-party CLI. Nothing here depends on their
code or on Node; the wire protocol is Chrome's.

## Why

Ghost's browser tools currently shell out to the `agent-browser` binary. That
is a real capability, but it is someone else's moving part: a Node/npm install,
their release cadence, their daemon on a socket. For a personal AI whose whole
premise is independence and local-first control, the browser is too important
to outsource. This package is the path to owning it.

## Architecture, mapped from the Rust original

| Concern | Upstream (`cli/src/native`) | Here (`pkg/browser/native`) |
|---|---|---|
| Protocol client | `cdp/client.rs` (tokio-tungstenite) | `cdp.go` (gorilla/websocket) |
| Chrome launch | `cdp/chrome.rs` (`launch_chrome`) | `chrome.go` (`launchChrome`) |
| Refs + snapshot | `snapshot.rs`, `element.rs` (`RefMap`) | `snapshot.go`, `session.go` |
| Interaction | `interaction.rs` (`Input.*`, `DOM.*`) | `session.go` (`Click`, `Fill`, `Type`, `Press`) |
| Daemon / sessions | `daemon.rs` (unix socket) | in-process `Session` |
| Streaming, dashboard, MCP, React, WebMCP, recording | large Rust trees | not ported (out of scope) |

## The core ideas, faithfully kept

1. **One reader, correlated commands.** A single goroutine reads the websocket;
   commands are keyed by id and answered on per-command channels; protocol
   events fan out to subscribers without ever blocking the reader. (`cdp.go`)
2. **Chrome writes its own port.** Launch uses `--remote-debugging-port=0` and
   reads `<user-data-dir>/DevToolsActivePort` for the chosen port and browser
   websocket path, polling until Chrome is up instead of guessing a delay.
3. **The accessibility tree is the interface.** `Accessibility.getFullAXTree`
   is flattened into an outline; interactive roles always get a ref and content
   roles get one when named. Refs are `e1`, `e2`, ... in tree order.
4. **Durable refs.** A ref for a given `backendDOMNodeId` survives re-snapshots
   within the same document (tracked by the top frame's `loaderId`); a new
   document invalidates them all, so a stale ref can never act on a recycled
   node.
5. **Real input, not `element.click()`.** A click scrolls the node into view,
   asks `DOM.getBoxModel` for its center, and dispatches a real
   `Input.dispatchMouseEvent` press/release — the same event path a person
   produces. Fill focuses the node and clears it through a `Runtime` call, then
   `Input.insertText`.
6. **Secrets never touch argv.** `SetValueFromScript` + `Focus` let a value be
   set through the page's own native setter from a script supplied on stdin —
   the hook Ghost's vault-backed multi-step sign-in uses (see
   `pkg/tools/browser_login.go`, `fillVault`).

## What is implemented

`Launch`/`attach`/`Close`, `Navigate`, `Snapshot`, `URL`, `Title`, `Read`,
`Click`, `Fill`, `Type`, `Press`, `Focus`, `SetValueFromScript`, `Wait`,
`Screenshot`.

## What is not (yet)

Streaming/screencast, the dashboard, the MCP server, network interception and
HAR, cookies/storage commands, React introspection, WebMCP, and recording.
These are upstream surfaces, not core automation, and each can be added against
the same CDP client without touching the rest.

## Tests

`pkg/browser/native/native_test.go` runs against a real Chrome, gated by
`GHOST_NATIVE_BROWSER=1`:

```
GHOST_NATIVE_BROWSER=1 go test ./pkg/browser/native/ -run TestNative -v
```

It proves navigate → snapshot → refs → fill → click → observe, durable refs
across snapshots, and the vault-fill value hook.

## Integration status

The package is standalone and verified but not yet wired into
`pkg/tools/browser.go` (which still uses the external CLI). Wiring it in is a
follow-up: an adapter behind a `GHOST_BROWSER_ENGINE=native` switch, so the two
can be compared before the CLI dependency is dropped.
