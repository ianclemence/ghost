package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/attention"
	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/proactive"
)

// The attention loop: every few minutes Ghost looks at what it knows (dated
// plans in memory, reminders that went unseen, notices from its own
// routines), offers each thing to the attention layer, says the urgent ones,
// and once a morning sends everything else as one message.

var (
	attnMu     sync.Mutex
	attnQueues = map[string]*attention.Queue{}
	// followStores is the workspace's open threads (things the owner said
	// they would do, buy, decide, hear about), opened once like the queue.
	followMu     sync.Mutex
	followStores = map[string]*attention.FollowupStore{}
)

// attentionQueue is the workspace's queue, opened once.
func (al *AgentLoop) attentionQueue() *attention.Queue {
	attnMu.Lock()
	defer attnMu.Unlock()
	q, ok := attnQueues[al.workspace]
	if !ok {
		q = attention.Open(al.workspace)
		attnQueues[al.workspace] = q
	}
	return q
}

// OfferAttention puts one item in front of the attention layer from any
// part of Ghost, and says it now when the layer decides it cannot wait.
func (al *AgentLoop) OfferAttention(it attention.Item) attention.Decision {
	d := al.attentionQueue().Offer(it, time.Now())
	if d == attention.Now {
		al.Announce("attn:"+it.Key, it.Line, 24*time.Hour, true)
	}
	return d
}

// ownerLocation is the owner's zone: their stated one, else the Pod's.
func (al *AgentLoop) ownerLocation() *time.Location {
	return proactive.UserLocation(al.pcStore)
}

// followupStore is the workspace's open-thread store, opened once.
func (al *AgentLoop) followupStore() *attention.FollowupStore {
	followMu.Lock()
	defer followMu.Unlock()
	s, ok := followStores[al.workspace]
	if !ok {
		s = attention.OpenFollowups(al.workspace)
		followStores[al.workspace] = s
	}
	return s
}

// AttentionTick gathers signals, offers them, and sends the morning message
// when it is due. Safe to call often: everything it does is idempotent.
// Returns how many things reached the owner.
func (al *AgentLoop) AttentionTick(now time.Time) int {
	if al == nil || al.workspace == "" {
		return 0
	}
	loc := al.ownerLocation()
	al.offerUpcoming(now, loc)
	al.offerUnseenReminders(now, loc)
	al.offerFollowups(now, loc)
	pol := proactive.Load(al.workspace)
	if !pol.Enabled {
		return 0
	}
	q := al.attentionQueue()
	if !q.DigestDue(now, loc, pol.MorningBriefing) {
		return 0
	}
	items := q.TakeDigest(now, loc, 6)
	if len(items) == 0 {
		return 0
	}
	al.deliverDigest("", items, now, loc)
	return len(items)
}

// MorningMerge folds the morning message into a briefing the heartbeat is
// about to send, so the owner gets one morning message, not two. It returns
// the text to send.
func (al *AgentLoop) MorningMerge(text string) string {
	if al == nil || al.workspace == "" {
		return text
	}
	now := time.Now()
	loc := al.ownerLocation()
	pol := proactive.Load(al.workspace)
	q := al.attentionQueue()
	if !pol.Enabled || !q.DigestDue(now, loc, pol.MorningBriefing) {
		return text
	}
	items := q.TakeDigest(now, loc, 6)
	if len(items) == 0 {
		return text
	}
	return al.deliverDigest(text, items, now, loc)
}

// deliverDigest writes and sends the morning message: an opening line, the
// briefing (when the heartbeat wrote one), the items, and a card whose
// buttons are the items' suggested next steps. With a briefing it returns
// the combined text for the caller to send; alone it sends it itself.
func (al *AgentLoop) deliverDigest(briefing string, items []attention.Item, now time.Time, loc *time.Location) string {
	var b strings.Builder
	if strings.TrimSpace(briefing) != "" {
		b.WriteString(strings.TrimSpace(briefing))
		b.WriteString("\n\nAlso on your plate:\n")
	} else {
		b.WriteString(morningOpening(al.workspace, now.In(loc)))
		b.WriteString("\n")
	}
	for _, it := range items {
		b.WriteString("- " + it.Line + "\n")
	}
	text := strings.TrimSpace(b.String())

	card, err := cards.New(cards.KindDigest, "This morning", "")
	if err == nil {
		block := cards.Block{Type: "list"}
		var sources []string
		for _, it := range items {
			block.Items = append(block.Items, cards.Item{Title: it.Line})
			if it.Reply != "" && len(card.Actions) < 3 {
				label := it.ReplyLabel
				if label == "" {
					label = "Help with this"
				}
				card.Actions = append(card.Actions, cards.Action{
					ID: fmt.Sprintf("item_%d", len(card.Actions)), Label: label, Kind: "reply", Text: it.Reply,
				})
				sources = append(sources, it.Source)
			}
		}
		card.Blocks = []cards.Block{block}
		card.Actions = append(card.Actions, cards.Action{ID: "dismiss", Label: "Got it", Kind: "dismiss"})
		card.Data = map[string]interface{}{"sources": sources}
		card.ExpiresAt = now.Add(20 * time.Hour)
	}

	if briefing == "" {
		al.DeliverToOwner("mobile", "default", text, map[string]interface{}{"announce": "digest"})
	}
	if err == nil {
		cards.Publish(al.bus, nil, "mobile", "default", ownerConversation, card)
	}
	if ev := al.CanonicalEvents(); ev != nil {
		ev.Publish(&cevents.Event{
			Type: cevents.DigestDelivered, GhostID: al.ghostID(), SessionID: ownerConversation, Status: "success",
			Payload: map[string]interface{}{"summary": digestSummary(len(items))},
		})
	}
	logger.InfoCF("agent", "morning message sent", map[string]interface{}{"items": len(items), "merged": briefing != ""})
	return text
}

func digestSummary(n int) string {
	if n == 1 {
		return "1 thing for today"
	}
	return fmt.Sprintf("%d things for today", n)
}

// morningOpening greets the owner by name.
func morningOpening(workspace string, local time.Time) string {
	name := ""
	if id, err := ghoststate.LoadIdentity(workspace); err == nil && id != nil {
		name = strings.TrimSpace(id.OwnerName)
	}
	part := "Good morning"
	if local.Hour() >= 12 {
		part = "Good afternoon"
	}
	if name != "" {
		return part + ", " + name + ". A few things for today:"
	}
	return part + ". A few things for today:"
}

// ForgetAttention drops pending notes whose key starts with prefix.
func (al *AgentLoop) ForgetAttention(prefix string) {
	if al != nil && al.workspace != "" {
		al.attentionQueue().ForgetPrefix(prefix)
	}
}

// AttentionAnswered tells the layer the owner acted on a morning item from
// a source, which is what keeps that kind of thing coming.
func (al *AgentLoop) AttentionAnswered(source string) {
	if al != nil && al.workspace != "" && source != "" {
		al.attentionQueue().Answered(source)
	}
}

// offerUpcoming turns dated plans in memory into a week-ahead note, a
// day-before note, a note on the day, and a "how did it go" after. Each is
// said once; a plan without a real date (Ghost never guesses "next month")
// produces nothing.
func (al *AgentLoop) offerUpcoming(now time.Time, loc *time.Location) {
	if al.pcStore == nil {
		return
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	q := al.attentionQueue()
	for _, e := range al.pcStore.Current() {
		if e.Subject != "user" && e.Kind != personalcontext.KindEvent {
			continue
		}
		if e.Kind != personalcontext.KindEvent && e.ValidUntil == nil {
			continue
		}
		what := strings.TrimSpace(personalcontext.Value(e))
		sp, ok := attention.FindSpan(what, personalcontext.LearnedAt(e), loc)
		if !ok {
			continue
		}
		days := int(sp.Start.Sub(today).Hours() / 24)
		key := "dated:" + sp.Start.Format("2006-01-02") + ":" + sp.End.Format("2006-01-02")
		subject := upcomingSubject(what)
		travel := strings.EqualFold(e.Domain, "travel") || strings.Contains(strings.ToLower(what), "trip")
		switch {
		case days >= 6 && days <= 8:
			it := attention.Item{Key: key + ":week", Source: "upcoming", Priority: 6,
				Line:    fmt.Sprintf("In a week: %s (%s).", subject, spanWords(sp)),
				Expires: sp.Start}
			if travel {
				it.Reply = fmt.Sprintf("Help me get ready for %s: flights, the weather and anything I should book.", subject)
				it.ReplyLabel = "Help me prepare"
			}
			q.Offer(it, now)
		case days == 1:
			q.Offer(attention.Item{Key: key + ":tomorrow", Source: "upcoming", Priority: 8,
				Line: fmt.Sprintf("Tomorrow: %s.", subject), Expires: sp.Start.AddDate(0, 0, 1)}, now)
		case days == 0:
			q.Offer(attention.Item{Key: key + ":today", Source: "upcoming", Priority: 9,
				Line: fmt.Sprintf("Today: %s.", subject), Expires: sp.Start.AddDate(0, 0, 1)}, now)
		}
		// After a plan that spanned days, ask once how it went.
		after := int(today.Sub(sp.End).Hours() / 24)
		if sp.End.After(sp.Start) && after >= 1 && after <= 3 {
			q.Offer(attention.Item{Key: key + ":after", Source: "followup", Priority: 4,
				Line:    fmt.Sprintf("You're back from %s. How did it go?", subject),
				Expires: sp.End.AddDate(0, 0, 5)}, now)
		}
	}
}

// upcomingSubject turns a memory sentence into the thing itself: "Is planning
// a trip to Shenzhen from around 26 October to 6 November" → "your trip to
// Shenzhen".
func upcomingSubject(v string) string {
	s := strings.TrimSpace(v)
	low := strings.ToLower(s)
	for _, cut := range []string{";", " from around ", " from ", " on ", " — ", " - ", ", "} {
		if i := strings.Index(low, cut); i > 0 {
			s, low = s[:i], low[:i]
		}
	}
	for _, p := range []string{"is planning a ", "is planning ", "is going on a ", "has a ", "their ", "is "} {
		if strings.HasPrefix(low, p) {
			s = "your " + strings.TrimSpace(s[len(p):])
			break
		}
	}
	return strings.TrimSpace(strings.TrimRight(s, "."))
}

// spanWords is "26 Oct" or "26 Oct to 6 Nov".
func spanWords(sp attention.Span) string {
	if sp.End.Equal(sp.Start) {
		return sp.Start.Format("Mon 2 Jan")
	}
	return sp.Start.Format("2 Jan") + " to " + sp.End.Format("2 Jan")
}

// offerUnseenReminders brings up, once, a reminder that went off at least
// three hours ago and was neither seen nor answered: likely missed.
func (al *AgentLoop) offerUnseenReminders(now time.Time, loc *time.Location) {
	if al.schedSvc == nil {
		return
	}
	open, err := al.schedSvc.OpenDeliveries(now.Add(-24*time.Hour), now.Add(-3*time.Hour))
	if err != nil {
		return
	}
	q := al.attentionQueue()
	for _, d := range open {
		when := d.DeliveredAt.In(loc).Format("Mon 3:04 PM")
		q.Offer(attention.Item{
			Key:      "unseen:" + d.ItemID + ":" + d.DeliveredAt.UTC().Format(time.RFC3339),
			Source:   "reminder_unseen",
			Priority: 5,
			Line:     fmt.Sprintf("Still open from %s: %s.", when, strings.TrimRight(strings.TrimSpace(d.Title), ".")),
			Expires:  now.Add(24 * time.Hour),
		}, now)
	}
}

// offerFollowups brings due open threads into the morning message: things
// the owner said in the past that are worth coming back to (a planned
// purchase, something they said they would do, a decision they were
// weighing, something they were waiting on or worried about). Each is
// brought up once — Due marks it asked — and then let go.
func (al *AgentLoop) offerFollowups(now time.Time, loc *time.Location) {
	if al == nil || al.workspace == "" {
		return
	}
	q := al.attentionQueue()
	for _, f := range al.followupStore().Due(now) {
		pri := 5
		if f.Due != nil {
			pri = 7 // the owner named a day: timely
		} else if f.Kind == attention.KindWorry {
			pri = 6
		}
		reply, label := f.Reply()
		q.Offer(attention.Item{
			Key:        "followup:" + f.ID,
			Source:     "followup:" + f.Kind,
			Line:       f.Line(),
			Reply:      reply,
			ReplyLabel: label,
			Priority:   pri,
			Reliable:   true,
		}, now)
	}
}
