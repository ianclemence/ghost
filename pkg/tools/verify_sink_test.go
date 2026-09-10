package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/ianclemence/ghost/pkg/turnlog"
)

// verifiableStub is a tool whose world-state check the test controls.
type verifiableStub struct {
	stubTool
	verifyErr error
	verified  int
}

func (s *verifiableStub) Verify(ctx context.Context, args map[string]interface{}) error {
	s.verified++
	return s.verifyErr
}

// The verify sink observes every VerifiableTool check with the tool name,
// latency, and outcome — the runtime bridges these to the turn's trajectory.
func TestVerifySinkFiresOnSuccessAndFailure(t *testing.T) {
	type call struct {
		tool string
		ms   int64
		err  error
	}
	var calls []call

	reg := NewToolRegistry()
	reg.Register(&verifiableStub{stubTool: stubTool{name: "w"}})
	reg.SetVerifySink(func(ctx context.Context, tool string, ms int64, verr error) {
		calls = append(calls, call{tool, ms, verr})
	})

	res := reg.Execute(context.Background(), "w", map[string]interface{}{})
	if res.IsError {
		t.Fatalf("expected success, got %s", res.ForLLM)
	}
	if len(calls) != 1 || calls[0].tool != "w" || calls[0].err != nil {
		t.Fatalf("sink must fire once with success, got %+v", calls)
	}
	if calls[0].ms < 0 {
		t.Fatalf("latency must be non-negative, got %d", calls[0].ms)
	}

	// A failing verification surfaces a real failure AND reports it.
	reg2 := NewToolRegistry()
	bad := &verifiableStub{stubTool: stubTool{name: "w2"}, verifyErr: errors.New("not on disk")}
	reg2.Register(bad)
	var calls2 []call
	reg2.SetVerifySink(func(ctx context.Context, tool string, ms int64, verr error) {
		calls2 = append(calls2, call{tool, ms, verr})
	})
	res2 := reg2.Execute(context.Background(), "w2", map[string]interface{}{})
	if !res2.IsError {
		t.Fatal("failed verification must surface an error result")
	}
	if len(calls2) != 1 || calls2[0].err == nil {
		t.Fatalf("sink must report the verification error, got %+v", calls2)
	}

	// Non-verifiable tools never touch the sink.
	reg3 := NewToolRegistry()
	reg3.Register(&stubTool{name: "plain"})
	fired := false
	reg3.SetVerifySink(func(context.Context, string, int64, error) { fired = true })
	reg3.Execute(context.Background(), "plain", map[string]interface{}{})
	if fired {
		t.Fatal("sink must not fire for non-verifiable tools")
	}
}

// The sink receives the calling session and trajectory from the tool
// context (server-set values, never model input).
func TestVerifySinkSeesSessionAndTrajectory(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&verifiableStub{stubTool: stubTool{name: "w"}})
	var sess, trj string
	reg.SetVerifySink(func(ctx context.Context, _ string, _ int64, _ error) {
		sess = SessionKeyFromContext(ctx)
		trj = turnlog.TrajectoryIDFromContext(ctx)
	})
	ctx := turnlog.WithTrajectoryID(WithSessionKey(context.Background(), "sess-9"), "trj_9")
	reg.Execute(ctx, "w", map[string]interface{}{})
	if sess != "sess-9" {
		t.Fatalf("sink must see the session, got %q", sess)
	}
	if trj != "trj_9" {
		t.Fatalf("sink must see the trajectory, got %q", trj)
	}
}
