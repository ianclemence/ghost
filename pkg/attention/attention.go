// Package attention decides what deserves the owner's attention, and when.
//
// Every proactive signal (an upcoming trip, a reminder that went unseen, a
// routine that keeps failing) becomes an Item and goes through one place.
// Each item gets a decision, not a message:
//
//	Now     it cannot wait and is reliable: say it now (quiet hours still
//	        hold anything that is not urgent)
//	Digest  it can wait: it joins the morning message, so five small things
//	        arrive once, together, instead of as five pings
//	Drop    already said, already handled, stale, or a kind of thing the
//	        owner keeps ignoring
//
// The layer is deterministic: no model decides whether to interrupt. It also
// learns from what the owner does: a source whose items are ignored in three
// morning messages in a row is quiet for two weeks.
package attention

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Decision is what happens to an item.
type Decision string

const (
	Now    Decision = "now"
	Digest Decision = "digest"
	Drop   Decision = "drop"
)

// Item is one thing that might deserve the owner's attention.
type Item struct {
	// Key identifies the thing itself ("dated:<entry>:week"); the same key is
	// never surfaced twice.
	Key string `json:"key"`
	// Source names the kind of signal ("upcoming", "reminder_unseen"), which
	// is what the owner's responses are learned against.
	Source string `json:"source"`
	// Line is the one sentence the owner reads.
	Line string `json:"line"`
	// Reply, when set, is a suggested next step the owner can tap: sent to
	// Ghost as if they typed it. ReplyLabel is the button's words.
	Reply      string `json:"reply,omitempty"`
	ReplyLabel string `json:"reply_label,omitempty"`
	// Priority orders the morning message, 1 (least) to 10.
	Priority int `json:"priority"`
	// Urgent and Reliable together are the only way to interrupt.
	Urgent   bool `json:"urgent,omitempty"`
	Reliable bool `json:"reliable,omitempty"`
	// NotBefore holds an item back until a time; Expires drops it after.
	NotBefore time.Time `json:"not_before,omitempty"`
	Expires   time.Time `json:"expires,omitempty"`
	Added     time.Time `json:"added"`
}

// state is the durable form.
type state struct {
	Pending []Item               `json:"pending"`
	Said    map[string]time.Time `json:"said"`
	// LastDigest is the local date (YYYY-MM-DD) the last morning message went.
	LastDigest string `json:"last_digest,omitempty"`
	// Unanswered is the items of the last morning message, by source, the
	// owner has not acted on yet; Ignored counts consecutive digests a
	// source went unanswered; QuietUntil silences a source.
	Unanswered map[string]int       `json:"unanswered,omitempty"`
	Ignored    map[string]int       `json:"ignored,omitempty"`
	QuietUntil map[string]time.Time `json:"quiet_until,omitempty"`
}

// Queue is the attention layer's memory: what is waiting, what was said.
type Queue struct {
	mu   sync.Mutex
	path string
	st   state
}

// ignoreLimit is how many morning messages in a row a source may go
// unanswered before it is quiet for quietFor.
const (
	ignoreLimit = 3
	quietFor    = 14 * 24 * time.Hour
	// sayMemory is how long a key stays "already said".
	sayMemory = 60 * 24 * time.Hour
)

// Open loads the queue for a workspace (proactive/attention.json).
func Open(workspace string) *Queue {
	q := &Queue{path: filepath.Join(workspace, "proactive", "attention.json")}
	if data, err := os.ReadFile(q.path); err == nil {
		_ = json.Unmarshal(data, &q.st)
	}
	q.init()
	return q
}

func (q *Queue) init() {
	if q.st.Said == nil {
		q.st.Said = map[string]time.Time{}
	}
	if q.st.Unanswered == nil {
		q.st.Unanswered = map[string]int{}
	}
	if q.st.Ignored == nil {
		q.st.Ignored = map[string]int{}
	}
	if q.st.QuietUntil == nil {
		q.st.QuietUntil = map[string]time.Time{}
	}
}

func (q *Queue) save() {
	if q.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(q.path), 0o700)
	if data, err := json.Marshal(q.st); err == nil {
		tmp := q.path + ".tmp"
		if os.WriteFile(tmp, data, 0o600) == nil {
			_ = os.Rename(tmp, q.path)
		}
	}
}

// Offer puts an item in front of the layer and returns what will happen to
// it. A Now item is returned to the caller to deliver (and recorded as said);
// a Digest item waits in the queue.
func (q *Queue) Offer(it Item, now time.Time) Decision {
	q.mu.Lock()
	defer q.mu.Unlock()
	it.Key = strings.TrimSpace(it.Key)
	if it.Key == "" || strings.TrimSpace(it.Line) == "" {
		return Drop
	}
	if _, said := q.st.Said[it.Key]; said {
		return Drop
	}
	for _, p := range q.st.Pending {
		if p.Key == it.Key {
			return Digest
		}
	}
	if !it.Expires.IsZero() && !now.Before(it.Expires) {
		return Drop
	}
	if until, ok := q.st.QuietUntil[it.Source]; ok && now.Before(until) && !it.Urgent {
		return Drop
	}
	if it.Urgent && it.Reliable {
		q.st.Said[it.Key] = now
		q.save()
		return Now
	}
	if it.Added.IsZero() {
		it.Added = now
	}
	q.st.Pending = append(q.st.Pending, it)
	q.save()
	return Digest
}

// Forget removes a pending item that no longer applies (the reminder was
// done, the trip was cancelled), so a stale thing never reaches the morning.
func (q *Queue) Forget(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.st.Pending[:0]
	for _, p := range q.st.Pending {
		if p.Key != key {
			kept = append(kept, p)
		}
	}
	q.st.Pending = kept
	q.save()
}

// ForgetPrefix removes pending items whose key starts with prefix: every
// note about one reminder once the owner closed it.
func (q *Queue) ForgetPrefix(prefix string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.st.Pending[:0]
	for _, p := range q.st.Pending {
		if !strings.HasPrefix(p.Key, prefix) {
			kept = append(kept, p)
		}
	}
	q.st.Pending = kept
	q.save()
}

// DigestDue reports whether the morning message should go now: after the
// morning time in the owner's zone, not yet sent today.
func (q *Queue) DigestDue(now time.Time, loc *time.Location, morning string) bool {
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	at, err := time.ParseInLocation("15:04", morning, loc)
	if err != nil {
		at, _ = time.ParseInLocation("15:04", "08:00", loc)
	}
	start := time.Date(local.Year(), local.Month(), local.Day(), at.Hour(), at.Minute(), 0, 0, loc)
	q.mu.Lock()
	defer q.mu.Unlock()
	return !local.Before(start) && q.st.LastDigest != local.Format("2006-01-02")
}

// TakeDigest returns the items for this morning's message, best first, at
// most max, marks them said, and records the digest as sent for today. It
// also settles learning: sources whose items in the previous message went
// unanswered count one more ignore. It returns nil (and still marks today as
// done) when there is nothing worth saying: no empty "good morning".
func (q *Queue) TakeDigest(now time.Time, loc *time.Location, max int) []Item {
	if loc == nil {
		loc = time.Local
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.st.LastDigest = now.In(loc).Format("2006-01-02")

	// Learning from the previous message.
	for src, n := range q.st.Unanswered {
		if n <= 0 {
			continue
		}
		q.st.Ignored[src]++
		if q.st.Ignored[src] >= ignoreLimit {
			q.st.QuietUntil[src] = now.Add(quietFor)
			q.st.Ignored[src] = 0
		}
	}
	q.st.Unanswered = map[string]int{}

	var ready, waiting []Item
	for _, it := range q.st.Pending {
		switch {
		case !it.Expires.IsZero() && !now.Before(it.Expires):
			// stale: let it go
		case !it.NotBefore.IsZero() && now.Before(it.NotBefore):
			waiting = append(waiting, it)
		default:
			if until, ok := q.st.QuietUntil[it.Source]; ok && now.Before(until) {
				continue
			}
			ready = append(ready, it)
		}
	}
	sort.SliceStable(ready, func(i, j int) bool {
		if ready[i].Priority != ready[j].Priority {
			return ready[i].Priority > ready[j].Priority
		}
		return ready[i].Added.Before(ready[j].Added)
	})
	if max > 0 && len(ready) > max {
		// What does not fit waits for tomorrow rather than being lost.
		waiting = append(waiting, ready[max:]...)
		ready = ready[:max]
	}
	for _, it := range ready {
		q.st.Said[it.Key] = now
		q.st.Unanswered[it.Source]++
	}
	for k, t := range q.st.Said {
		if now.Sub(t) > sayMemory {
			delete(q.st.Said, k)
		}
	}
	q.st.Pending = waiting
	q.save()
	return ready
}

// Answered records that the owner acted on something from a source (tapped
// its suggestion, or answered it), which resets that source's ignore count.
func (q *Queue) Answered(source string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.st.Unanswered, source)
	q.st.Ignored[source] = 0
	q.save()
}

// Pending returns a copy of what is waiting (for status and tests).
func (q *Queue) Pending() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]Item(nil), q.st.Pending...)
}
