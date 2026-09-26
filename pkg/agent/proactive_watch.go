package agent

import (
	"time"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/logger"
)

// Event-driven awareness.
//
// The heartbeat is a reconciliation fallback, not the primary source of
// awareness: an assistant whose value depends on noticing change cannot wait
// half an hour for a routine to fail. This watcher subscribes to the canonical
// stream and asks for an evaluation the moment something relevant happens.
//
// Three properties keep it safe on a Raspberry Pi:
//
//  1. It is a plain live subscription. The durable consumer exists in
//     pkg/cevents, but its claim table is never pruned, so subscribing the
//     whole firehose durably would grow a table without bound. Restart safety
//     comes from the heartbeat reconciliation plus the fact that evaluation is
//     idempotent — repeated runs converge on the same proposals.
//  2. Evaluation is coalesced: a burst of events produces one evaluation, not
//     one per event, with a floor between runs.
//  3. Evaluation itself is deterministic and never calls a model, so an event
//     cannot turn into inference.

// proactiveEvalFloor is the minimum gap between two event-driven evaluations.
// Events arrive in bursts (a routine emits several), and the work is the same
// regardless.
const proactiveEvalFloor = 3 * time.Second

// awarenessEvent reports whether an event can change what Ghost should notice.
// Everything else is ignored before any work happens.
func awarenessEvent(t cevents.Type) bool {
	switch t {
	case cevents.CommitmentCreated, cevents.CommitmentCompleted, cevents.CommitmentFailed, cevents.CommitmentBlocked,
		cevents.RoutineFailed, cevents.RoutineWaiting, cevents.RoutineCompleted, cevents.RoutineCreated,
		cevents.TaskFailed, cevents.TaskWaiting, cevents.TaskCompleted, cevents.TaskCancelled,
		cevents.MessageCreated, cevents.PermissionApproved, cevents.PermissionDenied,
		cevents.PermissionExpired, cevents.ToolFailed, cevents.OperationFailed:
		return true
	}
	return false
}

// StartProactiveWatcher begins event-driven awareness. It is safe to call
// once; later calls are no-ops. The watcher stops with the loop's shutdown
// context, and a nil governance (unwired/test runtime) simply leaves the
// heartbeat as the only trigger.
func (al *AgentLoop) StartProactiveWatcher() {
	if al == nil || al.governance == nil || al.governance.Events == nil {
		return
	}
	al.proactiveWatchOnce.Do(func() {
		al.proactiveWake = make(chan struct{}, 1)
		al.proactiveStop = make(chan struct{})
		go al.proactiveEvalLoop()
		al.governance.Events.Subscribe(cevents.Filter{}, func(e *cevents.Event) {
			if e == nil || !awarenessEvent(e.Type) {
				return
			}
			al.RequestProactiveEvaluation()
		})
		if al.shutdownCtx != nil {
			// Stop the worker when the loop shuts down.
			go func() {
				<-al.shutdownCtx.Done()
				select {
				case <-al.proactiveStop:
				default:
					close(al.proactiveStop)
				}
			}()
		}
		logger.InfoCF("agent", "proactive watcher started", nil)
	})
}

// RequestProactiveEvaluation asks for an evaluation soon. It never blocks and
// never queues more than one pending run: a flood of events collapses into one
// evaluation, which is all the work requires.
func (al *AgentLoop) RequestProactiveEvaluation() {
	if al == nil || al.proactiveWake == nil {
		// The watcher is not running (tests, or an unwired runtime). Callers
		// that need evaluation call EvaluateProposals directly.
		return
	}
	select {
	case al.proactiveWake <- struct{}{}:
	default:
	}
}

// proactiveEvalLoop drains wake-ups with a floor between evaluations.
func (al *AgentLoop) proactiveEvalLoop() {
	var last time.Time
	for {
		select {
		case <-al.proactiveStop:
			return
		case <-al.proactiveWake:
			if wait := proactiveEvalFloor - time.Since(last); wait > 0 {
				select {
				case <-time.After(wait):
				case <-al.proactiveStop:
					return
				}
			}
			// Drain any wake-ups that arrived while waiting: they are one
			// evaluation's worth of news.
			select {
			case <-al.proactiveWake:
			default:
			}
			last = time.Now()
			al.EvaluateProposals(time.Now())
		}
	}
}
