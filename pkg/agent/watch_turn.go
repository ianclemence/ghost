package agent

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/watch"
)

// Deterministic watch control.
//
// "track my flight BA123", "stop watching my package", "what am I watching"
// are commands over durable state the runtime already owns — sending them
// through a model buys a ~0.9 s cloud round trip and a chance of the model
// misreporting what is stored. This fast path runs them with zero model
// calls, and every reply is read back from the store, never constructed
// from memory of the request.
//
// It runs AFTER the state-query turn (which owns the read-only list
// questions) and BEFORE the readiness fast paths, which would otherwise
// answer "track my flight" with a capability's "not connected" reply
// instead of creating the watch the owner asked for.

var (
	// watchStartRE matches the owner asking Ghost to watch something.
	watchStartRE = regexp.MustCompile(`(?i)\b(?:track|watch|follow|monitor|keep\s+(?:me\s+)?(?:an\s+)?eye\s+on|stay\s+on\s+top\s+of|check\s+up\s+on)\b`)
	// watchStopRE matches the owner ending a watch. Checked first: a stop
	// phrase contains the start verb.
	watchStopRE = regexp.MustCompile(`(?i)\b(?:stop|cancel|end|drop|untrack|quit)\b[^.?!]{0,40}\b(?:watch(?:ing)?|track(?:ing)?)\b|\b(?:stop|cancel)\s+(?:watching|tracking)\b`)
	// watchEntityRE is the same flight-number shape detection uses, so
	// "stop watching BA123" cancels exactly what "track BA123" created.
	// watchDirectiveRE requires the watch verb to open the sentence, after
	// any greeting or politeness.
	watchDirectiveRE = regexp.MustCompile(`(?i)^(?:(?:hey|hi|ok|okay|so|and|also|now|just|please|pls|ghost|can you|could you|would you|will you|i want you to|i'd like you to|go ahead and)[,\s]+)*(?:track|watch|follow|monitor|keep|stay|check up|stop|cancel|end|drop|untrack|quit)\b`)
	// watchStopAllRE is the one stop phrase that names no flight and still
	// means every watch.
	watchStopAllRE = regexp.MustCompile(`(?i)\b(?:stop|cancel|end|drop|untrack|quit)\s+(?:all\s+(?:of\s+)?(?:my\s+|the\s+)?)?(?:watching|tracking|watches|trackers?)(?:\s+(?:everything|it|that|them|all(?:\s+of\s+them)?|(?:my\s+|the\s+)?flights?))?`)
	watchEntityRE  = regexp.MustCompile(`\b([A-Z]{2}\s?\d{1,4})\b`)
)

// tryWatchTurn runs the owner's watch commands deterministically. It
// returns handled=false for anything that is not a watch command, so the
// rest of the fast-path chain is untouched.
func (al *AgentLoop) tryWatchTurn(msg, session string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" || strings.HasPrefix(lower, "/") {
		return "", false
	}
	// A watch command is a directive: its verb opens the sentence (after any
	// politeness). "I'm going to watch the game", "follow up on my email" and
	// "cancel the watch party" mention the same words and are none of this.
	if !watchDirectiveRE.MatchString(lower) {
		return "", false
	}
	if watchStopRE.MatchString(lower) || watchStopRE.MatchString(msg) {
		// Stopping closes what the owner named. With no flight number named,
		// only a bare "stop watching" (or "…everything") means all of them;
		// "stop tracking my expenses" names something else and goes to the
		// model rather than closing every watch.
		if watchEntityRE.MatchString(msg) || wholeAsk(lower, watchStopAllRE) {
			return al.stopWatchTurn(msg), true
		}
		return "", false
	}
	if !watchStartRE.MatchString(lower) {
		return "", false
	}
	// Starting needs something detectable to watch; otherwise the sentence was
	// ordinary talk and the model reads it.
	if len(watch.Detect(msg, time.Now().UTC(), al.scheduleTimezone())) == 0 {
		return "", false
	}
	return al.startWatchTurn(msg, session), true
}

// startWatchTurn creates a watch the owner asked for by name. The request
// is parsed by the same deterministic detector the automatic path uses, so
// one phrase means one thing; the policy still gates it (source, horizon,
// provenance) and its honest refusal text is what the owner hears.
func (al *AgentLoop) startWatchTurn(msg, session string) string {
	if al.workspace == "" {
		return "I don't have a workspace to keep a watch in right now."
	}
	now := time.Now().UTC()
	cands := watch.Detect(msg, now, al.scheduleTimezone())
	if len(cands) == 0 {
		return "I couldn't tell what to watch. Tell me the flight number, or what's happening and when."
	}
	store, err := al.watchStoreFor()
	if err != nil {
		return "I can't open my watch list right now, so I didn't start anything."
	}
	existing, err := store.List()
	if err != nil {
		return "I can't read my watch list right now, so I didn't start anything."
	}
	reg := al.watchProbes()
	pol := al.watchPolicy()
	msgID := fmt.Sprintf("msg-%d", now.UnixNano())
	for _, c := range cands {
		source, _ := reg.Resolve(c.Kind)
		v := pol.Allow(c, existing, now, source)
		if !v.Allow {
			if v.Reason == watch.ReasonDuplicate {
				return "I'm already watching that."
			}
			return watch.ReasonText(v)
		}
		w := c.ToWatch(session, msgID, now)
		w.Source = source
		w.Explicit = true
		created, err := store.Create(w)
		if err != nil {
			return "I couldn't start that watch — nothing was changed."
		}
		al.watchMetrics().RecordCreated()
		al.publishWatch(cevents.WatchCreated, created, "owner asked for this watch")
		logger.InfoCF("agent", "watch created (explicit)", map[string]interface{}{
			"id": created.ID, "entity": created.Entity, "source": created.Source,
		})
		al.RequestProactiveEvaluation()
		return watchStartReply(created)
	}
	return "I couldn't tell what to watch."
}

// watchStartReply confirms what is being watched, read back from the store,
// and says exactly how the watching happens — including when it is a local
// source rather than a connected provider.
func watchStartReply(w watch.Watch) string {
	switch w.Kind {
	case watch.KindFlight:
		msg := fmt.Sprintf("Watching %s. I'll check it in the background and tell you the moment its status, gate or terminal changes.", watch.SubjectPhrase(w))
		if w.Source == "sandbox" {
			msg += " (Using your local watch source — no flight provider is connected.)"
		}
		return msg
	default:
		return fmt.Sprintf("Watching %s. I'll check it in the background and tell you when anything about it changes.", watch.SubjectPhrase(w))
	}
}

// stopWatchTurn cancels the watches the owner named (or all of them when
// they named none). The reply counts what was actually closed, read back
// from the store.
func (al *AgentLoop) stopWatchTurn(msg string) string {
	if al.workspace == "" {
		return "I don't have a watch list right now."
	}
	target := ""
	if m := watchEntityRE.FindStringSubmatch(msg); m != nil {
		target = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(m[1]), " ", ""))
	}
	n, closed := al.CancelWatchesByEntity(target, "owner stopped watching")
	if n == 0 {
		return watch.RenderNone()
	}
	if n == 1 {
		return fmt.Sprintf("Stopped watching %s.", watch.SubjectPhrase(closed[0]))
	}
	return fmt.Sprintf("Stopped watching %d things.", n)
}
