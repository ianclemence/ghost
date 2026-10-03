package attention

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

// Follow-ups are things the owner said that a good assistant would come back
// to: a purchase they were planning, something they said they would do, a
// decision they were weighing, something they were waiting on or worried
// about. They are not memories (a keyboard you want to buy is not a fact about
// you); they are open threads. Each is brought up once, at a sensible time,
// through the attention layer, and then let go.

// Followup kinds.
const (
	KindPurchase = "purchase" // planning to buy something
	KindTask     = "task"     // said they would do something
	KindDecision = "decision" // weighing a choice
	KindWaiting  = "waiting"  // waiting on someone or something
	KindWorry    = "worry"    // nervous or worried about something coming up
)

// Followup is one open thread.
type Followup struct {
	ID    string    `json:"id"`
	Kind  string    `json:"kind"`
	What  string    `json:"what"` // "buy a mechanical keyboard", in the owner's frame
	Quote string    `json:"quote,omitempty"`
	Heard time.Time `json:"heard"`
	// Due is the day the owner named for it, if any.
	Due *time.Time `json:"due,omitempty"`
	// AskAt is when it is worth bringing up; Asked marks it brought up.
	AskAt time.Time `json:"ask_at"`
	Asked bool      `json:"asked,omitempty"`
}

// FollowupStore keeps open threads (proactive/followups.json).
type FollowupStore struct {
	mu   sync.Mutex
	path string
	list []Followup
}

// OpenFollowups loads the store for a workspace.
func OpenFollowups(workspace string) *FollowupStore {
	s := &FollowupStore{path: filepath.Join(workspace, "proactive", "followups.json")}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, &s.list)
	}
	return s
}

func (s *FollowupStore) save() {
	_ = os.MkdirAll(filepath.Dir(s.path), 0o700)
	if data, err := json.Marshal(s.list); err == nil {
		tmp := s.path + ".tmp"
		if os.WriteFile(tmp, data, 0o600) == nil {
			_ = os.Rename(tmp, s.path)
		}
	}
}

// Add records new threads, skipping one already open with the same words.
// Returns how many were new.
func (s *FollowupStore) Add(items []Followup) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, it := range items {
		dup := false
		for _, e := range s.list {
			if !e.Asked && normWhat(e.What) == normWhat(it.What) {
				dup = true
				break
			}
		}
		if dup || strings.TrimSpace(it.What) == "" {
			continue
		}
		s.list = append(s.list, it)
		n++
	}
	if n > 0 {
		s.save()
	}
	return n
}

// Due returns threads whose time has come and marks them asked: each is
// brought up once. Threads asked more than 30 days ago are let go.
func (s *FollowupStore) Due(now time.Time) []Followup {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Followup
	kept := s.list[:0]
	for _, f := range s.list {
		if f.Asked && now.Sub(f.AskAt) > 30*24*time.Hour {
			continue
		}
		if !f.Asked && !now.Before(f.AskAt) {
			f.Asked = true
			out = append(out, f)
		}
		kept = append(kept, f)
	}
	s.list = kept
	if len(out) > 0 {
		s.save()
	}
	return out
}

// Close lets a thread go without bringing it up (the owner already said it
// was done).
func (s *FollowupStore) Close(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list[i].Asked = true
		}
	}
	s.save()
}

// All returns a copy (status, tests).
func (s *FollowupStore) All() []Followup {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Followup(nil), s.list...)
}

func normWhat(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.Trim(s, " .!")), " "))
}

// askDelay is how long after hearing it a thread without a date is worth
// bringing up: soon enough to matter, late enough to have happened.
func askDelay(kind string) time.Duration {
	switch kind {
	case KindWorry:
		return 20 * time.Hour // the next morning
	case KindTask, KindWaiting:
		return 3 * 24 * time.Hour
	case KindDecision:
		return 4 * 24 * time.Hour
	case KindPurchase:
		return 6 * 24 * time.Hour
	}
	return 4 * 24 * time.Hour
}

// forwardRE is the cheap test for "this message looks ahead": only these
// messages are worth a model call to find follow-ups.
var forwardRE = regexp.MustCompile(`(?i)\b(i'?ll|i will|i'?m going to|going to|gonna|i need to|i have to|i must|i plan|planning|i want to (?:buy|get|order)|thinking (?:of|about) (?:buying|getting)|buy|order|purchase|waiting (?:for|on)|hope to hear|deciding|decide|can'?t decide|torn between|nervous|worried|anxious|interview|exam|appointment|deadline|tomorrow|next week|this weekend|by (?:monday|tuesday|wednesday|thursday|friday|saturday|sunday|the end))\b`)

// LooksForward reports whether a message may hold a follow-up.
func LooksForward(msg string) bool {
	m := strings.TrimSpace(msg)
	return len(m) >= 12 && forwardRE.MatchString(m)
}

const followupSystem = `You read one message a person sent their personal AI and list the open threads a thoughtful assistant would come back to later. Kinds:
- purchase: they plan or want to buy something ("I want to get a new keyboard", "need to order a sofa").
- task: something they said THEY will do later ("I'll call the bank tomorrow", "I need to renew my passport").
- decision: a choice they are weighing ("can't decide between the Pixel and the iPhone").
- waiting: something they are waiting to hear or receive ("waiting for the visa", "should hear back from them Friday").
- worry: something coming up they are nervous or worried about ("nervous about the interview on Thursday").
Do NOT list: requests to the assistant, things already done, small talk, facts about who they are, anything happening right now.
For each: what = a short phrase in their frame, starting with a verb or noun ("buy a mechanical keyboard", "hear back about the visa", "the job interview"); due = the date it is about as YYYY-MM-DD if one is given or implied (resolve "tomorrow", "Friday" from today), else ""; quote = their exact words, short.
Respond with ONLY JSON: {"followups":[{"kind":"purchase","what":"buy a mechanical keyboard","due":"","quote":"want to buy a mechanical keyboard"}]} or {"followups":[]}`

// ExtractFollowups asks the model for open threads in one message. heard is
// when it was said; loc is the owner's zone.
func ExtractFollowups(ctx context.Context, p providers.LLMProvider, model, msg string, heard time.Time, loc *time.Location) ([]Followup, error) {
	if p == nil || !LooksForward(msg) {
		return nil, nil
	}
	if loc == nil {
		loc = time.Local
	}
	user := fmt.Sprintf("Today is %s.\nMessage: %q", heard.In(loc).Format("Monday 2 January 2006"), msg)
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := p.Chat(cctx, []providers.Message{{Role: "system", Content: followupSystem}, {Role: "user", Content: user}},
		nil, model, map[string]interface{}{"temperature": 0.0, "max_tokens": 400})
	if err != nil {
		return nil, err
	}
	return ParseFollowups(resp.Content, msg, heard, loc), nil
}

// ParseFollowups turns the model's JSON into threads, keeping only known
// kinds, quotes that really are in the message, and sane dates.
func ParseFollowups(raw, msg string, heard time.Time, loc *time.Location) []Followup {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if j := strings.LastIndex(raw, "}"); j >= 0 {
		raw = raw[:j+1]
	}
	var out struct {
		Followups []struct {
			Kind, What, Due, Quote string
		} `json:"followups"`
	}
	if json.Unmarshal([]byte(raw), &out) != nil {
		return nil
	}
	var list []Followup
	for i, f := range out.Followups {
		kind := strings.ToLower(strings.TrimSpace(f.Kind))
		switch kind {
		case KindPurchase, KindTask, KindDecision, KindWaiting, KindWorry:
		default:
			continue
		}
		what := strings.TrimSpace(f.What)
		if what == "" || len(what) > 120 {
			continue
		}
		quote := strings.TrimSpace(f.Quote)
		if quote != "" && !strings.Contains(strings.ToLower(msg), strings.ToLower(quote)) {
			quote = "" // only the owner's actual words count as a receipt
		}
		fu := Followup{ID: fmt.Sprintf("fu-%d-%d", heard.UnixNano(), i), Kind: kind, What: what, Quote: quote, Heard: heard}
		if d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(f.Due), loc); err == nil {
			if !d.Before(heard.In(loc).AddDate(0, 0, -1)) && d.Before(heard.AddDate(1, 0, 0)) {
				fu.Due = &d
			}
		}
		fu.AskAt = AskAt(fu, loc)
		list = append(list, fu)
	}
	return list
}

// AskAt is when a thread is worth bringing up. With a date: that morning for
// a task or something awaited; the day after for a worry ("how did it go");
// otherwise a kind-specific delay after it was said.
func AskAt(f Followup, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	if f.Due != nil {
		d := f.Due.In(loc)
		morning := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
		if f.Kind == KindWorry || f.Kind == KindWaiting {
			return morning.AddDate(0, 0, 1)
		}
		return morning
	}
	return f.Heard.Add(askDelay(f.Kind))
}

// Line is how a thread reads in the morning message, and Reply is the next
// step the owner can tap (a purchase gets help finding the best price: Ghost
// shops for its owner, not for a store).
func (f Followup) Line() string {
	w := strings.TrimRight(f.What, ".")
	switch f.Kind {
	case KindPurchase:
		return fmt.Sprintf("You were planning to %s. Still on?", strings.TrimPrefix(w, "to "))
	case KindTask:
		if f.Due != nil {
			return fmt.Sprintf("Today you planned to %s.", strings.TrimPrefix(w, "to "))
		}
		return fmt.Sprintf("You said you'd %s. Done?", strings.TrimPrefix(w, "to "))
	case KindDecision:
		return fmt.Sprintf("You were deciding on %s. Settled it?", w)
	case KindWaiting:
		return fmt.Sprintf("You were waiting to %s. Any news?", strings.TrimPrefix(w, "to "))
	case KindWorry:
		return fmt.Sprintf("How did %s go?", w)
	}
	return w
}

// Reply is the suggested next step, if there is a useful one.
func (f Followup) Reply() (text, label string) {
	w := strings.TrimPrefix(strings.TrimRight(f.What, "."), "to ")
	switch f.Kind {
	case KindPurchase:
		return "Help me " + w + " at the best price: compare a few places and tell me where it's cheapest.", "Find the best price"
	case KindDecision:
		return "Help me decide on " + w + ".", "Help me decide"
	}
	return "", ""
}
