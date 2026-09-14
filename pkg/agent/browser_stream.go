package agent

import (
	"context"
	"fmt"

	"github.com/ianclemence/ghost/pkg/browser"
)

// browserStreamBroker is the per-loop screencast ticket broker. Per-loop
// (like the session ledger): a package singleton would leak single-use
// tickets across loops sharing one process.
func (al *AgentLoop) browserStreamBroker() *browser.StreamBroker {
	if al == nil {
		return browser.NewStreamBroker()
	}
	// Lazily reuse the ledger's once-guard domain: a dedicated small mutex
	// would be cleaner, but the loop is already the owner of this state and
	// browser mint/consume are infrequent (one per viewer).
	if al.browserStreamInst == nil {
		al.browserStreamInst = browser.NewStreamBroker()
	}
	return al.browserStreamInst
}

// MintBrowserScreencast mints a single-use viewer ticket for a live browser
// session. The ticket binds the ledger row's owner/context/task at mint, so
// a forged session_id can never widen authority: unknown or expired sessions
// fail closed here, and the WS proxy consumes the ticket exactly once.
func (al *AgentLoop) MintBrowserScreencast(sessionID string) (browser.Ticket, error) {
	if al == nil || sessionID == "" {
		return browser.Ticket{}, fmt.Errorf("browser: session required")
	}
	ledger, err := al.browserSessionLedger()
	if err != nil || ledger == nil {
		return browser.Ticket{}, fmt.Errorf("browser: session ledger unavailable")
	}
	row, err := ledger.Get(sessionID)
	if err != nil || row == nil {
		return browser.Ticket{}, fmt.Errorf("browser: session not found")
	}
	return al.browserStreamBroker().Mint(browser.Session{
		ID: row.ID, Owner: row.Owner, ContextID: row.ContextID, TaskID: row.TaskID,
	})
}

// ConsumeBrowserScreencast validates a viewer ticket Exactly once.
func (al *AgentLoop) ConsumeBrowserScreencast(token string) (browser.Ticket, error) {
	return al.browserStreamBroker().Consume(token)
}

// BrowserStreamURL discovers the agent-browser stream server ([]backed by
// `agent-browser stream enable`, idempotent). Callers map failures to
// 501 SCREENCAST_UNSUPPORTED and fall back to observation stills.
func (al *AgentLoop) BrowserStreamURL(ctx context.Context) (string, error) {
	return al.browserStreamBroker().StreamURL(ctx)
}
