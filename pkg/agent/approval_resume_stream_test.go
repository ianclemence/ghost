package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/permissions"
)

// openProbeApproval parks a durable approval whose tool does not exist
// in the harness registry: the resume still re-executes (through the
// registry), fails closed, and reports a receipt — which is exactly the
// text that must reach the LIVE transcript.
func openProbeApproval(t *testing.T, h *gateHarness, requestID, session string) {
	t.Helper()
	res := h.loop.governance.AuthorizeStandalone(requestID, session, "device.control", "ghost_probe", map[string]interface{}{}, permissions.RiskConsequential)
	if res.Allowed || res.PendingID == "" {
		t.Fatalf("want a durable approval, got %+v", res)
	}
}

// An approved resume must report to the live transcript, not only to the
// database: the receipt used to be saved and never streamed, and the
// owner's screen showed "(no response)" while Ghost claimed it acted.
func TestApprovalResumeStreamsReceipt(t *testing.T) {
	h := newGateHarness(t)
	openProbeApproval(t, h, "req-rs", "sess-rs")

	var chunks []string
	resp, err := h.loop.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat-1",
		SessionKey: "sess-rs",
		Content:    "allow once",
	}, func(s string) { chunks = append(chunks, s) }, nil)
	if err != nil {
		t.Fatalf("processMessage: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("resume receipt must stream to the transcript")
	}
	streamed := strings.Join(chunks, "")
	if !strings.Contains(streamed, "That didn't work") {
		t.Errorf("streamed receipt = %q", streamed)
	}
	if !strings.Contains(resp, "That didn't work") {
		t.Errorf("returned receipt = %q", resp)
	}
}

// A denial reports the same way — the owner's "no" deserves a visible
// confirmation, not silence.
func TestApprovalDenyStreamsMessage(t *testing.T) {
	h := newGateHarness(t)
	openProbeApproval(t, h, "req-dn", "sess-dn")

	var chunks []string
	resp, err := h.loop.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat-1",
		SessionKey: "sess-dn",
		Content:    "deny",
	}, func(s string) { chunks = append(chunks, s) }, nil)
	if err != nil {
		t.Fatalf("processMessage: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("denial message must stream to the transcript")
	}
	if !strings.Contains(strings.Join(chunks, ""), "didn't run it") {
		t.Errorf("streamed denial = %q", strings.Join(chunks, ""))
	}
	if !strings.Contains(resp, "didn't run it") {
		t.Errorf("returned denial = %q", resp)
	}
}
