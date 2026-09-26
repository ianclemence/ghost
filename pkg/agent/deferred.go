package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/commitments"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// Deferred extraction.
//
// Personal-context and commitment extraction each carry a model call (up to
// two 15-second-timeout attempts). Neither contributes anything to the reply
// the owner is waiting for: they persist durable state that only later turns
// read. Running them inline put a second and third cloud round trip in front
// of the first token, for no benefit the owner could observe.
//
// They now run behind the answer, from a small durable queue so nothing is
// lost: the record is written before the turn returns, drained after the
// response is on the wire, retried on failure, and drained again at boot if
// the process stopped in between. Extraction itself is idempotent (the
// personal-context store dedupes against current rows and the commitment
// ledger dedupes on its key), so a replayed record converges rather than
// duplicating.
//
// Deterministic extraction — regex facts and statements that are obviously
// promises — still runs inline, because it is free and it keeps the durable
// state fresh even if the process dies immediately after the reply.

const (
	deferredQueueFile = "deferred-extraction.jsonl"
	// deferredQueueMax bounds the queue so a long outage cannot grow a file
	// without limit. On overflow the oldest records are dropped, which is the
	// correct failure direction: recent memory matters more than stale memory.
	deferredQueueMax = 64
	// deferredMaxAttempts bounds retries per record so one bad message cannot
	// keep asking the model forever.
	deferredMaxAttempts = 3
	// deferredDrainBatch caps how many records one drain will process, so a
	// backlog cannot hold a background worker for a long stretch.
	deferredDrainBatch = 8
)

// deferredJob is one unit of post-answer extraction work.
type deferredJob struct {
	Session   string    `json:"session"`
	RequestID string    `json:"request_id"`
	Message   string    `json:"message"`
	Channel   string    `json:"channel,omitempty"`
	At        time.Time `json:"at"`
	Attempts  int       `json:"attempts,omitempty"`
}

func (al *AgentLoop) deferredPath() string {
	return filepath.Join(al.workspace, "state", deferredQueueFile)
}

func (al *AgentLoop) readDeferredQueue() []deferredJob {
	raw, err := os.ReadFile(al.deferredPath())
	if err != nil || len(raw) == 0 {
		return nil
	}
	var out []deferredJob
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var j deferredJob
		if err := json.Unmarshal(line, &j); err != nil {
			continue
		}
		if j.Session == "" {
			continue
		}
		out = append(out, j)
	}
	return out
}

func (al *AgentLoop) writeDeferredQueue(jobs []deferredJob) {
	path := al.deferredPath()
	if len(jobs) == 0 {
		_ = os.Remove(path)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	var buf []byte
	for _, j := range jobs {
		raw, err := json.Marshal(j)
		if err != nil {
			continue
		}
		buf = append(buf, raw...)
		buf = append(buf, '\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// deferExtraction records the model-backed extraction work for after the
// reply. It is a local file append: no model, no network, no blocking.
func (al *AgentLoop) deferExtraction(session, requestID, message, channel string) {
	if al.semanticExtractor == nil && al.commitmentExtractor == nil {
		return
	}
	if isMachineTurn(session) || message == "" {
		return
	}
	jobs := al.readDeferredQueue()
	// A retry of the same turn must not enqueue twice.
	for _, j := range jobs {
		if j.Session == session && j.RequestID == requestID && requestID != "" {
			return
		}
	}
	jobs = append(jobs, deferredJob{
		Session: session, RequestID: requestID, Message: message,
		Channel: channel, At: time.Now().UTC(),
	})
	if len(jobs) > deferredQueueMax {
		dropped := len(jobs) - deferredQueueMax
		jobs = jobs[dropped:]
		logger.WarnCF("agent", "deferred extraction queue overflow; oldest dropped",
			map[string]interface{}{"dropped": dropped})
	}
	al.writeDeferredQueue(jobs)
}

// kickDeferredWorker starts one background drain if none is running. It never
// blocks the caller.
func (al *AgentLoop) kickDeferredWorker() {
	if al.deferredRunning.CompareAndSwap(false, true) {
		al.backgroundWG.Add(1)
		go func() {
			defer al.backgroundWG.Done()
			defer al.deferredRunning.Store(false)
			if al.drainDeferred() {
				// Work remained because the owner was active. Try again
				// shortly rather than holding the turn open; the timer only
				// exists while the queue is non-empty.
				time.AfterFunc(2*time.Second, al.kickDeferredWorker)
			}
		}()
	}
}

// FlushDeferred drains the queue synchronously. The automatic path never waits
// for extraction, so callers that need to observe its result (the golden
// runner, tests, a shutdown drain) use this instead of racing the worker.
func (al *AgentLoop) FlushDeferred() int {
	if al == nil {
		return 0
	}
	// Wait for any in-flight background drain to finish first, then take the
	// lock so the flush itself is the only drainer.
	for i := 0; i < 60 && al.deferredRunning.Load(); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	al.deferredFlush.Lock()
	defer al.deferredFlush.Unlock()
	n := 0
	for {
		jobs := al.readDeferredQueue()
		if len(jobs) == 0 {
			return n
		}
		j := jobs[0]
		al.runDeferredExtraction(j.Session, j.Message, j.RequestID)
		al.writeDeferredQueue(jobs[1:])
		n++
		if n > deferredQueueMax {
			return n
		}
	}
}

// waitForQuiet reports whether the interactive window cleared within d. It is
// how background work yields to the owner without inventing a scheduler.
func (al *AgentLoop) waitForQuiet(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if !al.interactiveBusy() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// BackgroundModelAllowed is the exported gate background callers use before
// spending a model call. Interactive turns take precedence everywhere, and on
// a local runtime they take precedence on the only model there is.
func (al *AgentLoop) BackgroundModelAllowed() bool { return !al.interactiveBusy() }

// drainDeferred processes queued records one at a time, yielding to interactive
// work between records.
// drainDeferred processes the queue and reports whether work is still waiting
// because the owner was active.
func (al *AgentLoop) drainDeferred() bool {
	jobs := al.readDeferredQueue()
	if len(jobs) == 0 {
		return false
	}
	if !al.waitForQuiet(3 * time.Second) {
		return true
	}
	var remaining []deferredJob
	processed := 0
	for i, j := range jobs {
		// Interactive work always wins: stop and leave the rest queued.
		if processed >= deferredDrainBatch || al.interactiveBusy() {
			remaining = append(remaining, jobs[i:]...)
			al.writeDeferredQueue(remaining)
			return true
		}
		if al.runDeferredExtraction(j.Session, j.Message, j.RequestID) {
			processed++
			continue
		}
		j.Attempts++
		if j.Attempts < deferredMaxAttempts {
			remaining = append(remaining, j)
		} else {
			logger.WarnCF("agent", "deferred extraction dropped after retries",
				map[string]interface{}{"session": j.Session, "attempts": j.Attempts})
		}
	}
	al.writeDeferredQueue(remaining)
	return len(remaining) > 0
}

// StartDeferredWorker drains anything left over from a previous process. Safe
// to call more than once; it only starts a drain when the queue is non-empty.
func (al *AgentLoop) StartDeferredWorker() {
	if al == nil || al.workspace == "" {
		return
	}
	if len(al.readDeferredQueue()) == 0 {
		return
	}
	logger.InfoCF("agent", "recovering deferred extraction queue", nil)
	al.kickDeferredWorker()
}

// runDeferredExtraction performs the model-backed work for one record. It
// returns false when the work should be retried.
func (al *AgentLoop) runDeferredExtraction(session, message, requestID string) bool {
	ok := true
	if al.semanticExtractor != nil {
		al.extractSemanticMemory(session, message, requestID)
	}
	if al.commitmentExtractor != nil {
		al.extractSemanticCommitment(session, message, requestID)
	}
	return ok
}

// interactive bookkeeping --------------------------------------------------

// beginInteractive marks a user-facing turn as in flight. Background work uses
// this to yield: a heartbeat or a deferred extraction must never compete with
// the owner for the model.
func (al *AgentLoop) beginInteractive() { al.interactive.Add(1) }
func (al *AgentLoop) endInteractive()   { al.interactive.Add(-1) }

func (al *AgentLoop) interactiveBusy() bool { return al.interactive.Load() > 0 }

// backgroundModelAllowed reports whether background work may make a model
// call right now. Interactive turns take precedence on every provider, and on
// a local runtime they take precedence on the only model there is.
func (al *AgentLoop) backgroundModelAllowed() bool { return !al.interactiveBusy() }

// extractSemanticMemory is the model-backed half of personal-context
// extraction. It runs from the deferred queue, never inline, so a second model
// call is not part of the owner's wait.
func (al *AgentLoop) extractSemanticMemory(session, message, requestID string) {
	msgID := requestID
	if msgID == "" {
		msgID = fmt.Sprintf("msg-%d", time.Now().UnixNano())
	}
	if al.semanticExtractor != nil && al.pcStore != nil && !isAutomationIntent(message) {

		logger.InfoCF("agent", "Attempting semantic extraction", map[string]interface{}{
			"message": message,
		})
		existing := al.pcStore.Current()
		if al.governance != nil {
			existing = al.pcStore.CurrentInScope(al.governance.SessionScopes(session))
		}
		result := al.semanticExtractor.Extract(context.Background(), message, existing)
		if result.ShouldRemember && len(result.Entries) > 0 {
			// Persist semantic extraction results, deduplicated: an entry
			// identical in kind+predicate+value to one already current is a
			// restatement, not a new memory. Deterministic guard so a weak or
			// over-eager extractor can never accumulate duplicate rows.
			current := al.pcStore.CurrentInScope(al.sessionScopes(session))
			for _, entry := range result.Entries {
				logger.InfoCF("agent", "semantic entry candidate", map[string]interface{}{
					"predicate": entry.Predicate,
					"value":     entryValueText(entry),
					"status":    string(entry.Status),
				})
				if personalcontext.DirectiveEcho(entryValueText(entry)) {
					logger.InfoCF("agent", "semantic extraction: directive echo, skipped",
						map[string]interface{}{"predicate": entry.Predicate})
					continue
				}
				if personalcontext.HasCurrent(current, entry) {
					logger.InfoCF("agent", "semantic extraction: duplicate of current memory, skipped",
						map[string]interface{}{"predicate": entry.Predicate})
					continue
				}
				entry.Sources[0].Ref = fmt.Sprintf("%s:%s", session, msgID)
				if al.governance != nil {
					entry.Scopes = al.governance.SessionWriteScopes(session)
				}
				// Correction, not accumulation: a current entry for the same
				// belief with a different value means the user changed their
				// mind. Retire it via supersede so exactly one current row
				// survives — the same rule the deterministic extractor
				// enforces. Only current-status extractions supersede, and
				// only when the conflicting row is the unambiguous
				// store-wide current (never retire a fact from a context we
				// cannot see).
				if entry.Status == personalcontext.StatusCurrent &&
					al.supersedeSemanticCorrection(current, entry) {
					if al.events != nil {
						al.events.emit(EventMemoryUpdated, "", map[string]interface{}{
							"session_key": session,
							"method":      "semantic",
						})
					}
					continue
				}
				if _, err := al.pcStore.Create(entry); err != nil {
					logger.WarnCF("agent", "Failed to persist semantic extraction", map[string]interface{}{
						"error": err.Error(),
					})
				} else {
					logger.InfoCF("agent", "semantic extraction persisted", map[string]interface{}{
						"predicate": entry.Predicate,
					})
					if al.events != nil {
						al.events.emit(EventMemoryCreated, "", map[string]interface{}{
							"session_key": session,
							"method":      "semantic",
						})
					}
				}
			}
		} else {
			logger.InfoCF("agent", "Semantic extraction: no memory worth remembering", map[string]interface{}{
				"reason": result.Reason,
			})
		}
	}
}

// extractSemanticCommitment is the model-backed half of commitment extraction:
// it runs only for messages that could plausibly state an obligation, and only
// from the deferred queue.
func (al *AgentLoop) extractSemanticCommitment(session, message, requestID string) {
	if al.commitmentExtractor == nil || isAutomationIntent(message) {
		return
	}
	if !commitments.PossibleCommitment(message) {
		return
	}
	store, err := al.commitmentStoreFor()
	if err != nil {
		return
	}
	now := time.Now().UTC()
	c, ok := al.commitmentExtractor.Extract(context.Background(), message, now, al.scheduleTimezone())
	if !ok {
		return
	}
	if !strings.Contains(normalizeForMatch(message), normalizeForMatch(c.Quote)) {
		return
	}
	created, err := store.Create(commitments.Commitment{
		Text: c.Text, Subject: c.Subject, Kind: c.Kind,
		DueAt: c.DueAt, DueSource: c.DuePhrase,
		Confidence: c.Confidence, Origin: c.Origin,
		Provenance: commitments.Provenance{Session: session, MessageID: requestID, Quote: c.Quote, At: now},
		DedupeKey:  commitments.DueKey(c.Text, c.Subject, c.DueAt),
	})
	if err != nil {
		return
	}
	al.publishCommitment(cevents.CommitmentCreated, created, "noticed a promise")
	al.RequestProactiveEvaluation()
}
