package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/proactive"
	"github.com/ianclemence/ghost/pkg/providers/flight"
	"github.com/ianclemence/ghost/pkg/watch"
)

// Watches: the bridge from conversation to background observation of the
// world.
//
// When the owner's own words describe something that will change on its
// own ("I'm flying BA123 tomorrow"), the runtime stores a durable watch
// (pkg/watch) instead of letting the sentence die in the transcript. From
// then on a clock — never a model — decides when to check, a diff decides
// whether anything changed, and one notice throat (MaybeNotify) decides
// whether the owner hears about it.
//
// This file owns four halves of that loop:
//
//	extraction  — the turn's deterministic pass (extractWatchesInline)
//	polling     — the bounded, budgeted probe cycle (PollWatches)
//	notification— change → evidence → gated notice (notifyWatchChange)
//	control     — list/cancel/status surfaces the owner can reach
//
// No probe path ever calls a model: polling is a clock over a file or an
// HTTP GET, and every number in a notice comes from stored state.

const (
	// maxWatchProbesPerPoll bounds one poll cycle. A burst of due watches
	// is drained over several cycles, never all at once.
	maxWatchProbesPerPoll = 3
	// watchNoticePriority / watchNoticeConfidence are the noticer inputs
	// for a watch change: a real observed change is high value (≥ the
	// threshold of 7) and as sure as evidence makes it.
	watchNoticePriority   = 7
	watchNoticeConfidence = 0.95
	// watchWakeFloor is the minimum delay before re-arming the poll wake.
	// It keeps a due-but-deferred watch from spinning the eval loop.
	watchWakeFloor = 10 * time.Second
)

// watchRuntime holds the loop's watch-side runtime state: probes, budget,
// metrics, the poll single-flight, and the wake timer. Zero value ready,
// like retrievalRecorder.
type watchRuntime struct {
	stateMu sync.Mutex // guards probes/override/budget init

	probesOnce sync.Once
	probes     *watch.Registry
	override   watch.Probe
	budgetOnce sync.Once
	budget     *watch.Budget
	metrics    *watch.Metrics

	// pollMu makes PollWatches single-flight: a heartbeat tick and an
	// event-driven wake must never probe the same watch twice.
	pollMu sync.Mutex

	wakeMu    sync.Mutex
	wakeTimer *time.Timer
}

// watchStoreFor opens the durable watch ledger for this workspace.
func (al *AgentLoop) watchStoreFor() (*watch.Store, error) {
	if al == nil || al.workspace == "" {
		return nil, fmt.Errorf("no workspace")
	}
	return watch.New(al.workspace)
}

// watchMetrics returns the process-local watch counters (created lazily so
// an unwired loop still works).
func (al *AgentLoop) watchMetrics() *watch.Metrics {
	al.watchRT.stateMu.Lock()
	defer al.watchRT.stateMu.Unlock()
	if al.watchRT.metrics == nil {
		al.watchRT.metrics = watch.NewMetrics()
	}
	return al.watchRT.metrics
}

// WatchMetrics snapshots the counters for diagnostics surfaces and tests.
func (al *AgentLoop) WatchMetrics() watch.MetricsSnapshot {
	if al == nil {
		return watch.MetricsSnapshot{}
	}
	return al.watchMetrics().Snapshot()
}

// watchBudget returns the persisted daily probe budget.
func (al *AgentLoop) watchBudget() *watch.Budget {
	if al == nil || al.workspace == "" {
		return nil
	}
	al.watchRT.stateMu.Lock()
	defer al.watchRT.stateMu.Unlock()
	if al.watchRT.budget == nil {
		if b, err := watch.OpenBudget(al.workspace); err == nil {
			al.watchRT.budget = b
		}
	}
	return al.watchRT.budget
}

// watchProbes builds the probe registry once: the real flight capability
// when a provider key is present, and the local sandbox file source. The
// registry answers "is there any source that could watch this?" — the
// honest refusal gate in front of creation.
func (al *AgentLoop) watchProbes() *watch.Registry {
	al.watchRT.probesOnce.Do(func() {
		reg := watch.NewRegistry()
		if credentials.AviationKey(nil) != "" || credentials.AeroDataBoxKey() != "" {
			svc := flight.New(flight.Config{
				AviationKey:    credentials.AviationKey(nil),
				AeroDataBoxKey: credentials.AeroDataBoxKey(),
			})
			reg.Register(watch.NewFlightProbe(func(ctx context.Context, number string) (flight.Flight, error) {
				_, res := svc.Lookup(ctx, number)
				if res.Err != nil {
					return flight.Flight{}, res.Err
				}
				return res.Value, nil
			}, true))
		}
		reg.Register(watch.NewSandbox(al.workspace))
		al.watchRT.probes = reg
	})
	return al.watchRT.probes
}

// SetWatchProbeOverride installs a probe that answers every fetch instead
// of the registered ones (tests, golden fixtures, live demonstrations).
// A nil override restores the real probes. It never changes which sources
// are considered available at creation time — only what a fetch reads.
func (al *AgentLoop) SetWatchProbeOverride(p watch.Probe) {
	if al == nil {
		return
	}
	al.watchRT.stateMu.Lock()
	al.watchRT.override = p
	al.watchRT.stateMu.Unlock()
}

// resolveWatchProbe returns the probe that will answer a fetch for a
// watch's source.
func (al *AgentLoop) resolveWatchProbe(source string) (watch.Probe, bool) {
	al.watchRT.stateMu.Lock()
	ov := al.watchRT.override
	al.watchRT.stateMu.Unlock()
	if ov != nil && ov.Available() {
		return ov, true
	}
	return al.watchProbes().Get(source)
}

// watchPolicy assembles the deterministic creation gate from the runtime:
// available sources from the registry, the master switch and probe budget
// from PROACTIVE_PREFERENCES.md.
func (al *AgentLoop) watchPolicy() watch.Policy {
	p := watch.Default()
	p.Sources = al.watchProbes().AvailableNames()
	if al.workspace != "" {
		pp := proactive.Load(al.workspace)
		p.Enabled = pp.Enabled
		if pp.MaxWatchChecksPerDay > 0 {
			p.BudgetPerDay = pp.MaxWatchChecksPerDay
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// Extraction: conversation → durable watch
// ---------------------------------------------------------------------------

// extractWatchesInline is the deterministic half of watch creation: the
// pattern pass runs inside the turn so the durable record exists even if
// the process stops right after the reply. The gate (policy) decides; the
// model is never consulted about whether a watch exists.
func (al *AgentLoop) extractWatchesInline(opts processOptions) {
	if al.workspace == "" || isMachineTurn(opts.SessionKey) || isAutomationIntent(opts.UserMessage) {
		return
	}
	now := time.Now().UTC()
	cands := watch.Detect(opts.UserMessage, now, al.scheduleTimezone())
	if len(cands) == 0 {
		return
	}
	store, err := al.watchStoreFor()
	if err != nil {
		return
	}
	existing, err := store.List()
	if err != nil {
		return
	}
	msgID := opts.RequestID
	if msgID == "" {
		msgID = fmt.Sprintf("msg-%d", now.UnixNano())
	}
	reg := al.watchProbes()
	pol := al.watchPolicy()
	for _, c := range cands {
		source, _ := reg.Resolve(c.Kind)
		if v := pol.Allow(c, existing, now, source); !v.Allow {
			logger.InfoCF("agent", "watch refused by policy", map[string]interface{}{
				"entity": c.Entity, "kind": string(c.Kind), "reason": string(v.Reason),
			})
			continue
		}
		w := c.ToWatch(opts.SessionKey, msgID, now)
		w.Source = source
		created, err := store.Create(w)
		if err != nil {
			logger.InfoCF("agent", "watch not stored", map[string]interface{}{"error": err.Error()})
			continue
		}
		al.watchMetrics().RecordCreated()
		al.publishWatch(cevents.WatchCreated, created, "noticed something worth watching")
		logger.InfoCF("agent", "watch created", map[string]interface{}{
			"id": created.ID, "kind": string(created.Kind), "entity": created.Entity,
			"source": created.Source,
		})
		// A new watch wants its baseline immediately.
		al.RequestProactiveEvaluation()
	}
}

// ---------------------------------------------------------------------------
// Polling: clock → probe → diff → notice
// ---------------------------------------------------------------------------

// PollWatches runs one bounded poll cycle: sweep expired watches, probe
// each due watch at most once (max 3, daily budget respected), diff the
// observed state against the baseline, and route meaningful changes
// through the noticer. It returns how many notices reached the owner.
//
// It never calls a model and never blocks on one: the only I/O is one file
// read (sandbox) or one HTTP GET (flight), each under a 10s timeout.
func (al *AgentLoop) PollWatches(now time.Time) int {
	if al == nil || al.workspace == "" {
		return 0
	}
	// Single-flight: overlapping cycles would probe the same state twice
	// and race the fingerprint dedupe.
	if !al.watchRT.pollMu.TryLock() {
		return 0
	}
	defer al.watchRT.pollMu.Unlock()

	store, err := al.watchStoreFor()
	if err != nil {
		return 0
	}
	al.sweepWatchExpiry(store, now)

	due, err := store.Due(now)
	if err != nil {
		return 0
	}
	delivered := 0
	probed := 0
	if len(due) > 0 {
		pol := al.watchPolicy()
		budget := al.watchBudget()
		for _, w := range due {
			if probed >= maxWatchProbesPerPoll {
				break
			}
			if budget != nil && !budget.Allow(now, pol.BudgetPerDay) {
				al.watchMetrics().RecordDeferred()
				logger.InfoCF("agent", "watch poll deferred: daily budget spent",
					map[string]interface{}{"used": budget.Used(now), "max": pol.BudgetPerDay})
				break
			}
			probed++
			delivered += al.probeWatch(store, w, now)
		}
	}
	al.scheduleWatchWake(now)
	return delivered
}

// probeWatch performs one probe of one watch: fetch, observe (diff), and
// notify. Failures back off honestly and never invent state.
func (al *AgentLoop) probeWatch(store *watch.Store, w watch.Watch, now time.Time) int {
	probe, ok := al.resolveWatchProbe(w.Source)
	if !ok {
		return al.failWatch(store, w, fmt.Errorf("source %q is not available", w.Source), now)
	}
	ctx, cancel := context.WithTimeout(context.Background(), watch.Timeout)
	defer cancel()
	state, excerpt, err := probe.Fetch(ctx, w)
	if err != nil {
		return al.failWatch(store, w, err, now)
	}
	ev := watch.Evidence{At: now, Source: probe.Name(), Detail: excerpt}
	updated, changes, err := store.Observe(w.ID, state, ev, watch.NextCheckAt(w, now).UTC())
	if err != nil {
		logger.InfoCF("agent", "watch observe failed", map[string]interface{}{"error": err.Error()})
		return 0
	}
	al.watchMetrics().RecordProbe(now)
	logger.InfoCF("agent", "watch checked", map[string]interface{}{
		"id": w.ID, "entity": w.Entity, "source": probe.Name(),
		"changes": len(changes), "excerpt": truncateReason(excerpt, 160),
	})
	if len(changes) == 0 {
		return 0
	}
	al.watchMetrics().RecordChanges(len(changes))
	al.publishWatch(cevents.WatchChanged, updated, fmt.Sprintf("%d change(s)", len(changes)))
	return al.notifyWatchChange(store, updated, changes)
}

// failWatch records an honest probe failure. The watch retries with the
// normal cadence until its failure budget runs out, at which point it says
// it stopped watching instead of pretending.
func (al *AgentLoop) failWatch(store *watch.Store, w watch.Watch, err error, now time.Time) int {
	al.watchMetrics().RecordProbeFailure(err.Error())
	next := watch.NextCheckAt(w, now).UTC()
	failed, ferr := store.RecordFailure(w.ID, err.Error(), next)
	if ferr != nil {
		return 0
	}
	logger.InfoCF("agent", "watch probe failed", map[string]interface{}{
		"id": w.ID, "entity": w.Entity, "failures": failed.Failures,
		"class": string(watch.ClassOf(err)), "error": truncateReason(err.Error(), 160),
	})
	al.publishWatch(cevents.WatchFailed, failed, truncateReason(err.Error(), 160))
	if failed.Status != watch.StatusFailed {
		return 0
	}
	// Terminal failure: tell the owner once, honestly.
	al.watchMetrics().RecordFailed()
	nt := Notice{
		Topic:      "watch:" + w.ID + ":failed",
		Priority:   watchNoticePriority,
		Confidence: 0.9,
		DedupeKey:  "watch:" + w.ID + ":failed",
		Message:    watch.RenderFailure(failed),
	}
	if al.MaybeNotify(nt) == DecisionNotify {
		// An approved notice is a canonical fact: record it exactly as a
		// change notice is recorded, so the stream never misses one.
		al.watchMetrics().RecordNoticed()
		al.watchMetrics().RecordDelivered()
		al.publishWatch(cevents.WatchNotified, failed, nt.Message)
		return 1
	}
	return 0
}

// notifyWatchChange routes one set of changes through the single notice
// throat. The fingerprint of each change is recorded only when the noticer
// approves, so a held or dropped notice can be re-proposed while an
// approved one can never repeat — even across a restart.
func (al *AgentLoop) notifyWatchChange(store *watch.Store, w watch.Watch, changes []watch.Change) int {
	pending := make([]watch.Change, 0, len(changes))
	for _, c := range changes {
		if !w.AlreadyNotified(c.Fingerprint(w.ID)) {
			pending = append(pending, c)
		}
	}
	if len(pending) == 0 {
		// Everything here was already delivered: the reversal case is the
		// only repeat that may speak, and it carries a new fingerprint.
		return 0
	}
	// Topic keys on the first change's field+value so a reversal (gate
	// A→B then B→A) is not cooldown-blocked by its own prior notice.
	first := pending[0]
	nt := Notice{
		Topic:      fmt.Sprintf("watch:%s:%s:%s", w.ID, first.Field, first.To),
		Priority:   watchNoticePriority,
		Confidence: watchNoticeConfidence,
		DedupeKey:  first.Fingerprint(w.ID),
		Urgency:    watchUrgent(pending),
		Message:    watch.RenderNotice(w, pending),
	}
	decision := al.MaybeNotify(nt)
	if decision != DecisionNotify {
		reason := watchSuppressionReason(decision)
		_, _ = store.RecordSuppressed(w.ID, reason)
		al.watchMetrics().RecordSuppressed(reason)
		al.publishWatch(cevents.WatchSuppressed, w, reason)
		logger.InfoCF("agent", "watch notice held", map[string]interface{}{
			"id": w.ID, "reason": reason, "decision": string(decision),
		})
		return 0
	}
	for _, c := range pending {
		if _, _, err := store.MarkNotified(w.ID, c.Fingerprint(w.ID)); err != nil {
			logger.InfoCF("agent", "watch fingerprint not recorded", map[string]interface{}{"error": err.Error()})
		}
	}
	al.watchMetrics().RecordNoticed()
	al.watchMetrics().RecordDelivered()
	al.publishWatch(cevents.WatchNotified, w, nt.Message)
	return 1
}

// watchUrgent reports whether a change is a cancellation: the owner must
// know their flight was cancelled even in quiet hours.
func watchUrgent(changes []watch.Change) bool {
	for _, c := range changes {
		if c.Field != "status" {
			continue
		}
		to := strings.ToLower(c.To)
		if strings.Contains(to, "cancel") {
			return true
		}
	}
	return false
}

// watchSuppressionReason maps a gate decision to a durable reason name.
func watchSuppressionReason(d Decision) string {
	switch d {
	case DecisionDuplicate:
		return "dedupe"
	case DecisionCoolingDown:
		return "cooldown"
	case DecisionBudgetExhausted:
		return "budget"
	case DecisionLowConfidence:
		return "confidence"
	case DecisionLowPriority:
		return "low_priority"
	default:
		return "unknown"
	}
}

// sweepWatchExpiry expires watches past their horizon, announcing each one
// exactly once. Explicit watches (the owner asked for them by name) get a
// farewell notice; automatic ones end silently.
func (al *AgentLoop) sweepWatchExpiry(store *watch.Store, now time.Time) int {
	expired, err := store.Sweep(now)
	if err != nil || len(expired) == 0 {
		return 0
	}
	for _, w := range expired {
		al.watchMetrics().RecordExpired()
		al.publishWatch(cevents.WatchExpired, w, "horizon passed")
		logger.InfoCF("agent", "watch expired", map[string]interface{}{
			"id": w.ID, "entity": w.Entity,
		})
		if !w.Explicit {
			continue
		}
		nt := Notice{
			Topic:      "watch:" + w.ID + ":expired",
			Priority:   watchNoticePriority,
			Confidence: watchNoticeConfidence,
			DedupeKey:  "watch:" + w.ID + ":expired",
			Message:    watch.RenderExpired(w),
		}
		if al.MaybeNotify(nt) == DecisionNotify {
			if _, _, err := store.MarkNotified(w.ID, "watch:"+w.ID+":expired"); err == nil {
				// Same rule as a change notice: approval is recorded in
				// the canonical stream, deduped by the ledger.
				al.watchMetrics().RecordNoticed()
				al.publishWatch(cevents.WatchNotified, w, nt.Message)
				return 1
			}
		}
	}
	return 0
}

// scheduleWatchWake arms a timer for the next due watch so a change is
// noticed on time without a hot loop or a fixed-rate poll. The wake asks
// the existing evaluation loop to run; it never probes directly (so the
// single-flight and the interactive-yield rules still apply).
func (al *AgentLoop) scheduleWatchWake(now time.Time) {
	if al == nil || al.proactiveWake == nil || al.workspace == "" {
		return
	}
	store, err := al.watchStoreFor()
	if err != nil {
		return
	}
	due, err := store.Due(now)
	if err != nil || len(due) == 0 {
		return
	}
	when := now.Add(watchWakeFloor)
	if due[0].NextCheck != nil && due[0].NextCheck.After(when) {
		when = *due[0].NextCheck
	}
	delay := when.Sub(now)
	if delay < watchWakeFloor {
		delay = watchWakeFloor
	}
	al.watchRT.wakeMu.Lock()
	defer al.watchRT.wakeMu.Unlock()
	if al.watchRT.wakeTimer != nil {
		al.watchRT.wakeTimer.Stop()
	}
	al.watchRT.wakeTimer = time.AfterFunc(delay, func() {
		al.RequestProactiveEvaluation()
	})
}

// ---------------------------------------------------------------------------
// Control surface: list, cancel, status
// ---------------------------------------------------------------------------

// Watches returns the owner's watchlist, newest first.
func (al *AgentLoop) Watches() ([]watch.Watch, error) {
	store, err := al.watchStoreFor()
	if err != nil {
		return nil, err
	}
	return store.List()
}

// ActiveWatchCount reports how many watches are being polled — for the
// proactive status surface. A missing store reports zero rather than
// failing the surface.
func (al *AgentLoop) ActiveWatchCount() int {
	store, err := al.watchStoreFor()
	if err != nil {
		return 0
	}
	list, err := store.Active()
	if err != nil {
		return 0
	}
	return len(list)
}

// CancelWatch closes one watch by owner decision. It never touches any
// state in the world; it records that the owner no longer wants Ghost
// watching.
func (al *AgentLoop) CancelWatch(id, note string) (watch.Watch, error) {
	store, err := al.watchStoreFor()
	if err != nil {
		return watch.Watch{}, err
	}
	w, err := store.Cancel(id, note)
	if err != nil {
		return watch.Watch{}, err
	}
	al.watchMetrics().RecordCancelled()
	al.publishWatch(cevents.WatchCancelled, w, note)
	return w, nil
}

// CancelWatchesByEntity closes every live watch whose entity matches the
// owner's phrase ("stop watching my flight" → BA123). Matching is
// normalized and case-insensitive; it returns how many were closed.
func (al *AgentLoop) CancelWatchesByEntity(phrase, note string) (int, []watch.Watch) {
	store, err := al.watchStoreFor()
	if err != nil {
		return 0, nil
	}
	all, err := store.List()
	if err != nil {
		return 0, nil
	}
	needle := strings.ToLower(strings.TrimSpace(phrase))
	n := 0
	var closed []watch.Watch
	for _, w := range all {
		if !w.Live() {
			continue
		}
		if needle == "" ||
			strings.Contains(strings.ToLower(w.Entity), needle) ||
			strings.Contains(strings.ToLower(w.Label), needle) ||
			strings.Contains(needle, strings.ToLower(w.Entity)) {
			cw, err := store.Cancel(w.ID, note)
			if err != nil {
				continue
			}
			n++
			closed = append(closed, cw)
			al.watchMetrics().RecordCancelled()
			al.publishWatch(cevents.WatchCancelled, cw, note)
		}
	}
	return n, closed
}

// WatchCountByStatus reports lifecycle counts for status surfaces.
func (al *AgentLoop) WatchCountByStatus() map[watch.Status]int {
	store, err := al.watchStoreFor()
	if err != nil {
		return map[watch.Status]int{}
	}
	counts, err := store.Counts()
	if err != nil {
		return map[watch.Status]int{}
	}
	return counts
}

// ---------------------------------------------------------------------------
// Observability
// ---------------------------------------------------------------------------

// publishWatch records the watch lifecycle in the canonical stream. The
// payload carries identifiers and observed values only — never the
// owner's prose beyond the bounded provenance quote already stored in the
// ledger (which is deliberately NOT copied here; the ledger holds it).
func (al *AgentLoop) publishWatch(typ cevents.Type, w watch.Watch, note string) {
	if al == nil || al.governance == nil || al.governance.Events == nil {
		return
	}
	payload := map[string]interface{}{
		"watch_id": w.ID,
		"kind":     string(w.Kind),
		"entity":   w.Entity,
		"status":   string(w.Status),
		"source":   w.Source,
	}
	if len(w.Current) > 0 {
		if ex := watch.Excerpt(w.Current); ex != "" {
			payload["observed"] = truncateReason(ex, 200)
		}
	}
	if note != "" {
		payload["note"] = truncateReason(note, 200)
	}
	al.governance.Events.Publish(&cevents.Event{
		Type: typ, SessionID: proposalSession,
		GhostID: al.governance.GhostID, AgentID: al.governance.AgentID,
		Status: string(w.Status), Payload: payload,
	})
}
