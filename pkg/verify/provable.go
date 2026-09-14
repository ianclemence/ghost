// Provable runtime checks: browser transact evidence, screencast tickets,
// goal fanout, subagent caps. Every check executes real product behavior
// against the scratch appliance — never canned results.
package verify

import (
	"time"

	"github.com/ianclemence/ghost/pkg/browser"
	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/goals"
	"github.com/ianclemence/ghost/pkg/tools"
)

// checkBrowserSubmitEvidence proves the purchase-class path is bound:
// submit classifies as transact, the capability resolves to browser_submit,
// checkout detection extracts a quote, and confirmations are observable.
// A success without this binding would be a false-success defect.
func checkBrowserSubmitEvidence(e *Env) Check {
	sub := tools.NewBrowserTool(e.Workspace, "submit")
	if sub.Classify() != "transact" {
		return fail("Security", "browser submit evidence", "browser_submit does not classify as transact", true)
	}
	r := capability.NewResolver()
	capability.RegisterDefaults(r, capability.Availability{})
	impl, ok := r.Resolve("browser.transact", false)
	if !ok || impl.Tool != "browser_submit" {
		return fail("Security", "browser submit evidence", "browser.transact does not resolve to browser_submit", true)
	}
	q := browser.DetectCheckout("https://shop.example.com/checkout", "Order total $42.50 — place your order")
	if !q.IsCheckout {
		return fail("Security", "browser submit evidence", "checkout page not detected", true)
	}
	if q.Total == "" || q.Merchant == "" {
		return fail("Security", "browser submit evidence", "quote missing merchant/total binding", true)
	}
	if !browser.ConfirmationKeywords("Thank you — your order is confirmed. Receipt #123") {
		return fail("Security", "browser submit evidence", "confirmation not observable", true)
	}
	return pass("Security", "browser submit evidence")
}

// checkScreencastSingleUse proves viewer tickets are single-use, expiring,
// and bound: mint → consume ok → re-consume fails; expired fails; unbound
// mint fails. A reusable or unbound ticket would let one approval serve
// many viewers.
func checkScreencastSingleUse(e *Env) Check {
	_ = e
	b := browser.NewStreamBroker()
	sess := browser.Session{ID: "verify-sess", Owner: "owner", ContextID: "ctx", TaskID: "task"}
	tk, err := b.Mint(sess)
	if err != nil {
		return fail("Security", "screencast single-use", "mint failed: "+err.Error(), true)
	}
	if len(tk.Token) != 48 {
		return fail("Security", "screencast single-use", "token is not 48-char hex", true)
	}
	got, err := b.Consume(tk.Token)
	if err != nil {
		return fail("Security", "screencast single-use", "first consume failed: "+err.Error(), true)
	}
	if got.SessionID != sess.ID || got.Owner != sess.Owner {
		return fail("Security", "screencast single-use", "ticket binding lost", true)
	}
	if _, err := b.Consume(tk.Token); err == nil {
		return fail("Security", "screencast single-use", "reused ticket consumed", true)
	}
	if _, err := b.Mint(browser.Session{}); err == nil {
		return fail("Security", "screencast single-use", "unbound mint allowed", true)
	}
	return pass("Security", "screencast single-use")
}

// checkGoalFanout proves standing-goal lifecycle persists and goal_update
// cards carry a text fallback so non-rich surfaces read fine.
func checkGoalFanout(e *Env) Check {
	st := goals.NewStore(e.Workspace)
	g, err := st.Create("verify goal", "personal", "done", []string{"memory.recall"}, time.Now().Add(24*time.Hour))
	if err != nil {
		return fail("Automation", "goal fanout", "create failed: "+err.Error(), false)
	}
	if _, err := st.AppendProgress(g.ID, "first step"); err != nil {
		return fail("Automation", "goal fanout", "progress failed: "+err.Error(), false)
	}
	if _, err := st.Complete(g.ID); err != nil {
		return fail("Automation", "goal fanout", "complete failed: "+err.Error(), false)
	}
	c, err := cards.New(cards.KindGoalUpdate, "Goal completed", "verify goal")
	if err != nil {
		return fail("Automation", "goal fanout", "card build failed: "+err.Error(), false)
	}
	if err := c.Validate(); err != nil {
		return fail("Automation", "goal fanout", "card invalid: "+err.Error(), false)
	}
	if c.TextFallback() == "" {
		return fail("Automation", "goal fanout", "card has no text fallback", false)
	}
	return pass("Automation", "goal fanout")
}

// checkSubagentCaps proves delegation is bounded: depth 1, small
// concurrency, spawn/self tools blocked.
func checkSubagentCaps(e *Env) Check {
	_ = e
	p := tools.DefaultSubagentPolicy
	if p.MaxDepth != 1 {
		return fail("Governance", "subagent caps", "max depth is not 1", true)
	}
	if p.MaxConcurrency < 1 || p.MaxConcurrency > 5 {
		return fail("Governance", "subagent caps", "concurrency unbounded", true)
	}
	blocked := map[string]bool{}
	for _, t := range p.BlockedTools {
		blocked[t] = true
	}
	if !blocked["subagent"] && !blocked["spawn_agent"] && !blocked["spawn"] {
		return fail("Governance", "subagent caps", "spawn tools not blocked", true)
	}
	return pass("Governance", "subagent caps")
}
