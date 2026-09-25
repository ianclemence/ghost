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
	"errors"
	"fmt"
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
)

// Action is one card button. Approve/deny actions carry the broker
// request ID they resolve; the client calls the approvals endpoint.
type Action struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Style     string `json:"style,omitempty"` // primary | secondary | destructive
	RequestID string `json:"request_id,omitempty"`
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
	case KindSuggestion, KindGoalUpdate, KindCart, KindBrowserView, KindMemoryReceipt:
		// producible today
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
		"created_at": c.CreatedAt.Unix(), "expires_at": c.ExpiresAt.Unix(),
	}
}

// Store keeps recent cards per channel for GET /v1/cards fetch-on-open
// (phones miss pushes). Bounded: 20 per channel, expired pruned on write.
type Store struct {
	mu    sync.Mutex
	items map[string][]Card
}

var DefaultStore = &Store{items: map[string][]Card{}}

const storeCap = 20

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
