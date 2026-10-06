// Package cards defines Ghost's rich card payloads: structured,
// client-rendered companions to chat text — suggestions, goal updates,
// carts, checkout sheets, browser views. Every card carries a plain-text
// fallback in the message Content, so channels without rich rendering
// still read fine. Cards are presentation over existing authority:
// approvals still resolve through the permission broker, never the card.
package cards

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
)

// Kind is the card family. Only listed kinds validate; new kinds ship
// with a producer, a renderer, and a fallback — never alone.
type Kind string

const (
	// KindSuggestion is a proactive nudge (topic + message + optional
	// approve/deny bound to a broker request).
	KindSuggestion Kind = "suggestion"
	// KindGoalUpdate reports standing-goal lifecycle (created, progress,
	// paused, resumed, completed).
	KindGoalUpdate Kind = "goal_update"
	// KindCart shows a shopping list/cart (local, no payment).
	KindCart Kind = "cart"
	// KindCheckoutSheet is the pre-payment quote sheet. Reserved for the
	// wallet slice: producing one today is refused (no partner).
	KindCheckoutSheet Kind = "checkout_sheet"
	// KindBrowserView deep-links a live browser surface.
	KindBrowserView Kind = "browser_view"
	// KindMemoryReceipt is the trust card: the verbatim quote, confidence,
	// source message, and lifecycle behind a belief Ghost just explained.
	// Informational only — it carries no actions, because forgetting stays
	// an explicit owner act on the Memory screen, never a card button.
	KindMemoryReceipt Kind = "memory_receipt"
	// KindBrowserRecovery tells the owner their browser was stuck and has
	// been reset. Informational; the recovery already happened.
	KindBrowserRecovery Kind = "browser_recovery"
	// KindPresent is an answer Ghost chose to show rather than write: a
	// weather glance, a flight, a plan, a comparison. It is built from blocks
	// (blocks.go), never from layout the model invents.
	KindPresent Kind = "present"
	// KindReminder is a reminder that went off, with what can be done about
	// it (done, snooze). Its buttons are "act" actions the Pod carries out.
	KindReminder Kind = "reminder"
	// KindDigest is the morning message: several small things Ghost held
	// back so they arrive together instead of as separate pings.
	KindDigest Kind = "digest"
)

// CardVersion is the version of the block vocabulary a card was written in. A
// phone that does not know a newer block type skips it and still shows the rest.
const CardVersion = 1

// Action is one card button. Approve/deny actions carry the broker
// request ID they resolve; the client calls the approvals endpoint.
type Action struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Style     string `json:"style,omitempty"` // primary | secondary | destructive
	RequestID string `json:"request_id,omitempty"`
	// Kind is what tapping does, and it is only ever one of these:
	//   "reply"   send Text to Ghost as if the owner had typed it
	//   "dismiss" put the card away
	//   "act"     the Pod carries out the choice named by ID for this card's
	//             kind (a reminder's Done or Snooze). The phone sends only the
	//             action id; what it does is decided here, never by the card.
	// Empty means the older broker-bound action (RequestID). A card can offer
	// a choice; it can never carry a command, a link or a style of its own.
	Kind string `json:"kind,omitempty"`
	Text string `json:"text,omitempty"`
}

// Resolution is what the owner did with a card, remembered so it stays put
// away on every device and after a restart.
type Resolution struct {
	ActionID string    `json:"action_id"`
	Label    string    `json:"label"`
	At       time.Time `json:"at"`
}

// Card is one rich payload.
type Card struct {
	ID        string                 `json:"id"`
	Kind      Kind                   `json:"kind"`
	Title     string                 `json:"title"`
	Body      string                 `json:"body,omitempty"`
	Topic     string                 `json:"topic,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Actions   []Action               `json:"actions,omitempty"`
	Blocks    []Block                `json:"blocks,omitempty"`
	V         int                    `json:"v,omitempty"`
	Resolved  *Resolution            `json:"resolved,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	ExpiresAt time.Time              `json:"expires_at,omitempty"`
}

func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("card_%d", time.Now().UnixNano())
	}
	return "card_" + hex.EncodeToString(buf)
}

// New builds a validated card (24h default expiry).
func New(kind Kind, title, body string) (Card, error) {
	c := Card{ID: newID(), Kind: kind, Title: strings.TrimSpace(title),
		Body: strings.TrimSpace(body), CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour)}
	if err := c.Validate(); err != nil {
		return Card{}, err
	}
	return c, nil
}

// Validate enforces the card contract.
func (c Card) Validate() error {
	switch c.Kind {
	case KindSuggestion, KindGoalUpdate, KindCart, KindBrowserView, KindMemoryReceipt, KindBrowserRecovery, KindReminder, KindDigest:
		// producible today
	case KindPresent:
		if len(c.Blocks) == 0 {
			return errors.New("a presented card needs at least one block")
		}
	case KindCheckoutSheet:
		return errors.New("checkout_sheet has no payment partner yet")
	default:
		return fmt.Errorf("unknown card kind %q", c.Kind)
	}
	if c.Title == "" {
		return errors.New("card title is required")
	}
	if len(c.Actions) > 4 {
		return errors.New("card carries at most 4 actions")
	}
	if len(c.Blocks) > MaxBlocks {
		return fmt.Errorf("card carries at most %d blocks", MaxBlocks)
	}
	return nil
}

// TextFallback renders the plain-text companion carried in message
// Content for channels without rich rendering.
func (c Card) TextFallback() string {
	var sb strings.Builder
	sb.WriteString(c.Title)
	if c.Body != "" {
		sb.WriteString("\n" + c.Body)
	}
	for _, b := range c.Blocks {
		if t := blockText(b); t != "" {
			sb.WriteString("\n" + t)
		}
	}
	for _, a := range c.Actions {
		sb.WriteString("\n[" + a.Label + "]")
	}
	return sb.String()
}

// Payload is the wire form attached to bus metadata.
func (c Card) Payload() map[string]interface{} {
	return map[string]interface{}{
		"card_id": c.ID, "card_kind": string(c.Kind),
		"title": c.Title, "body": c.Body, "topic": c.Topic,
		"request_id": c.RequestID, "data": c.Data, "actions": c.Actions,
		"blocks": c.Blocks, "v": c.V, "resolved": c.Resolved,
		"created_at": c.CreatedAt.Unix(), "expires_at": c.ExpiresAt.Unix(),
	}
}

// Store keeps recent cards per channel for GET /v1/cards fetch-on-open
// (phones miss pushes) and so a conversation keeps its cards where they were
// shown. Bounded: 100 per channel, expired pruned on write. Persist makes it
// survive a restart; without it the store lives in memory only (tests).
type Store struct {
	mu    sync.Mutex
	items map[string][]Card
	path  string
}

var DefaultStore = &Store{items: map[string][]Card{}}

const storeCap = 100

// Persist loads cards from path (if the file exists) and keeps writing there.
func (s *Store) Persist(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = path
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var loaded map[string][]Card
	if err := json.Unmarshal(b, &loaded); err != nil {
		return err
	}
	if s.items == nil {
		s.items = map[string][]Card{}
	}
	for ch, list := range loaded {
		s.items[ch] = list
	}
	return nil
}

// saveLocked writes the store atomically. Callers hold s.mu. Best effort: a
// failed write never breaks a turn; the in-memory copy stays authoritative.
func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	b, err := json.Marshal(s.items)
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

// Add stores a card for channel (mobile, telegram, ...).
func (s *Store) Add(channel string, c Card) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[string][]Card{}
	}
	now := time.Now()
	kept := make([]Card, 0, storeCap)
	for _, e := range s.items[channel] {
		if e.ExpiresAt.IsZero() || now.Before(e.ExpiresAt) {
			kept = append(kept, e)
		}
	}
	kept = append(kept, c)
	if len(kept) > storeCap {
		kept = kept[len(kept)-storeCap:]
	}
	s.items[channel] = kept
	s.saveLocked()
}

// List returns unexpired cards for channel, oldest first.
func (s *Store) List(channel string) []Card {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := []Card{}
	for _, e := range s.items[channel] {
		if e.ExpiresAt.IsZero() || now.Before(e.ExpiresAt) {
			out = append(out, e)
		}
	}
	return out
}

// Resolve records what the owner did with a card, so it stays put away on
// every device. actionID must be one the card offered (or "dismiss"). It
// returns the updated card, or false when there is no such card, it was
// already resolved, or the action is not one it offered.
func (s *Store) Resolve(channel, id, actionID string) (Card, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.items[channel]
	for i := range list {
		if list[i].ID != id {
			continue
		}
		if list[i].Resolved != nil {
			return list[i], false
		}
		label := ""
		for _, a := range list[i].Actions {
			if a.ID == actionID {
				label = a.Label
			}
		}
		if label == "" && actionID == "dismiss" {
			label = "Dismissed"
		}
		if label == "" {
			return list[i], false
		}
		list[i].Resolved = &Resolution{ActionID: actionID, Label: label, At: time.Now().UTC()}
		s.items[channel] = list
		s.saveLocked()
		return list[i], true
	}
	return Card{}, false
}

// Publish stores a card and emits it on the bus with card_update metadata.
// A nil bus stores only (fetch path). Content always carries the text
// fallback so non-rich surfaces read fine.
func Publish(b *bus.MessageBus, store *Store, channel, chatID, sessionID string, c Card) {
	if store == nil {
		store = DefaultStore
	}
	store.Add(channel, c)
	if b == nil {
		return
	}
	meta := c.Payload()
	meta["type"] = "card_update"
	if sessionID != "" {
		meta["session_id"] = sessionID
	}
	b.PublishOutbound(bus.OutboundMessage{
		Channel: channel, ChatID: chatID,
		Content: c.TextFallback(), Metadata: meta,
	})
}

// ResolveHandler carries out an "act" choice on a card of one kind. It
// returns the words the card should show once resolved ("Snoozed until 9:40
// PM"); an error leaves the card open.
type ResolveHandler func(c Card, actionID string) (string, error)

var (
	handlersMu sync.Mutex
	handlers   = map[Kind]ResolveHandler{}
)

// OnResolve registers the handler for a kind's "act" choices.
func OnResolve(kind Kind, h ResolveHandler) {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	handlers[kind] = h
}

// HandlerFor returns the registered handler for a kind, if any.
func HandlerFor(kind Kind) (ResolveHandler, bool) {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	h, ok := handlers[kind]
	return h, ok
}

// ResolveByData resolves every open card in any channel whose Data[key]
// equals value on the default store (e.g. the companion cards of a decided
// idea) and returns the resolved cards.
func ResolveByData(key, value, actionID string) []Card {
	return DefaultStore.ResolveByData(key, value, actionID)
}

// ResolveByData (store method) resolves every open card in any channel whose Data[key]
// equals value (e.g. the companion cards of a decided idea) and returns the
// resolved cards. An action the card offered wins; otherwise the generic
// "dismiss" receipt is recorded so the card still stays put away.
func (s *Store) ResolveByData(key, value, actionID string) []Card {
	type target struct {
		channel string
		id      string
		action  string
	}
	s.mu.Lock()
	var targets []target
	for channel, list := range s.items {
		for i := range list {
			c := list[i]
			if c.Resolved != nil {
				continue
			}
			v, _ := c.Data[key].(string)
			if v == "" || v != value {
				continue
			}
			action := actionID
			offered := false
			for _, a := range c.Actions {
				if a.ID == action {
					offered = true
					break
				}
			}
			if !offered {
				action = "dismiss"
			}
			targets = append(targets, target{channel, c.ID, action})
		}
	}
	s.mu.Unlock()
	var done []Card
	for _, t := range targets {
		if c, ok := s.Resolve(t.channel, t.id, t.action); ok {
			done = append(done, c)
		}
	}
	return done
}

// Find returns a stored card by id.
func (s *Store) Find(channel, id string) (Card, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.items[channel] {
		if c.ID == id {
			return c, true
		}
	}
	return Card{}, false
}

// FindByData returns the newest open card of a kind whose Data[key] equals
// value: the reminder card for a reminder id.
func (s *Store) FindByData(channel string, kind Kind, key, value string) (Card, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.items[channel]
	for i := len(list) - 1; i >= 0; i-- {
		c := list[i]
		if c.Kind == kind && c.Resolved == nil {
			if v, _ := c.Data[key].(string); v == value {
				return c, true
			}
		}
	}
	return Card{}, false
}

// Relabel sets the words a resolved card shows.
func (s *Store) Relabel(channel, id, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.items[channel]
	for i := range list {
		if list[i].ID == id && list[i].Resolved != nil {
			list[i].Resolved.Label = label
			s.items[channel] = list
			s.saveLocked()
			return
		}
	}
}
