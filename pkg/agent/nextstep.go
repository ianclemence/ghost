package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

// Next-step offers for instant answers.
//
// An answer the runtime gives straight from stored state or a provider has no
// model turn behind it, so the prompt rule about offering a useful next step
// cannot apply. These offers close that gap. Each one is derived from the data
// in the answer (a failed reminder, rain in the report, a flight nobody is
// watching), never from the words of the question, is a single short line the
// owner can ignore, and is rate-limited so it does not become a tic. A "yes"
// reaches the model with the offer in the transcript, so it knows what was
// meant.

const offerCooldown = 6 * time.Hour

var (
	offerMu   sync.Mutex
	offerSeen = map[string]time.Time{}
)

// offerDue reports whether this topic may be offered again in this session,
// and records the offer when it may.
func offerDue(session, topic string, now time.Time) bool {
	offerMu.Lock()
	defer offerMu.Unlock()
	key := session + "\x00" + topic
	if last, ok := offerSeen[key]; ok && now.Sub(last) < offerCooldown {
		return false
	}
	offerSeen[key] = now
	return true
}

// withNextStep appends offer to answer as its own short line, once per
// cooldown. An empty offer returns the answer unchanged.
func withNextStep(session, topic, answer, offer string) string {
	offer = strings.TrimSpace(offer)
	if offer == "" || strings.TrimSpace(answer) == "" || !offerDue(session, topic, time.Now()) {
		return answer
	}
	return strings.TrimRight(answer, "\n") + "\n\n" + offer
}

// weatherOffer reads the provider's report. Rain and heat are things a
// reminder can act on; plain good weather offers nothing.
func weatherOffer(report string) string {
	lower := strings.ToLower(report)
	for _, w := range []string{"rain", "shower", "drizzle", "thunder", "storm"} {
		if strings.Contains(lower, w) {
			return "Rain is around. Want a reminder to take an umbrella?"
		}
	}
	var temp float64
	if i := strings.Index(report, "°C"); i > 0 {
		j := i
		for j > 0 && (report[j-1] == '.' || report[j-1] == '-' || (report[j-1] >= '0' && report[j-1] <= '9')) {
			j--
		}
		fmt.Sscanf(report[j:i], "%f", &temp)
	}
	if temp >= 35 {
		return "That's properly hot. Want a reminder to drink water, or to plan around the afternoon?"
	}
	return ""
}

// remindersOffer reads the schedule. Something that failed to send comes
// first; otherwise something due within a day.
func (al *AgentLoop) remindersOffer(now time.Time) string {
	if al.schedSvc == nil {
		return ""
	}
	if failed, err := al.schedSvc.ListItems("", scheduled.StateFailed, 5); err == nil && len(failed) > 0 {
		if len(failed) == 1 {
			return "One reminder failed to send. Want me to try it again?"
		}
		return fmt.Sprintf("%d reminders failed to send. Want me to try them again?", len(failed))
	}
	pending, err := al.schedSvc.ListItems("", scheduled.StateScheduled, 100)
	if err != nil {
		return ""
	}
	var next *scheduled.ScheduledItem
	for _, it := range pending {
		if it.Schedule.Kind != scheduled.ScheduleAt || it.NextRunAt == nil || it.NextRunAt.Before(now) || it.NextRunAt.After(now.Add(24*time.Hour)) {
			continue
		}
		if next == nil || it.NextRunAt.Before(*next.NextRunAt) {
			next = it
		}
	}
	if next == nil {
		return ""
	}
	return fmt.Sprintf("%q is the next one up. Want me to move it, or add a nudge before it?", strings.TrimSpace(next.Title))
}

// flightOffer suggests watching a flight nobody is watching yet.
func (al *AgentLoop) flightOffer(flightNumber string) string {
	flightNumber = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(flightNumber), " ", ""))
	if flightNumber == "" || al.workspace == "" {
		return ""
	}
	if store, err := al.watchStoreFor(); err == nil {
		if list, err := store.List(); err == nil {
			for _, w := range list {
				if w.Live() && strings.EqualFold(w.Entity, flightNumber) {
					return ""
				}
			}
		}
	}
	return fmt.Sprintf("Want me to keep watching %s and tell you if its gate, time or status changes?", flightNumber)
}
