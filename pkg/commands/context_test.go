package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

func newContextStore(t *testing.T) *personalcontext.Store {
	t.Helper()
	s, err := personalcontext.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open personal context: %v", err)
	}
	return s
}

func ctxValue(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	raw, err := personalcontext.RawValue(v)
	if err != nil {
		t.Fatalf("RawValue(%v): %v", v, err)
	}
	return raw
}

func ctxSource() personalcontext.Source {
	return personalcontext.Source{
		Type:      personalcontext.SourceConversation,
		Kind:      personalcontext.SourceUserDeclared,
		Ref:       "telegram:42:msg-9",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// ctxEntry builds a valid current entry with deterministic provenance.
func ctxEntry(id, kind, subject, predicate string, value interface{}) personalcontext.Entry {
	raw, _ := personalcontext.RawValue(value)
	return personalcontext.Entry{
		ID:         id,
		Kind:       personalcontext.Kind(kind),
		Subject:    subject,
		Predicate:  predicate,
		Value:      raw,
		Status:     personalcontext.StatusCurrent,
		Confidence: 1,
		Sources:    []personalcontext.Source{ctxSource()},
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func runContextHandler(t *testing.T, store *personalcontext.Store, text string) string {
	t.Helper()
	var out string
	rt := &Runtime{PersonalContext: store}
	req := Request{
		Text:       text,
		Channel:    "cli",
		ChatID:     "direct",
		SessionKey: "s1",
		Reply: func(s string) error {
			out = s
			return nil
		},
	}
	if err := contextHandler(context.Background(), req, rt); err != nil {
		t.Fatalf("contextHandler(%q): %v", text, err)
	}
	return out
}

// A. Empty context produces the empty response.
func TestContextCommandEmpty(t *testing.T) {
	store := newContextStore(t)
	out := runContextHandler(t, store, "/context")
	if !strings.Contains(out, "Personal Context is empty.") {
		t.Fatalf("expected empty response, got: %q", out)
	}
}

// B. Current entries are listed grouped by kind, compactly.
func TestContextCommandCurrentEntries(t *testing.T) {
	store := newContextStore(t)
	mustCreate(t, store, ctxEntry("e1", "identity", "user", "identity/name", "Ian"))
	mustCreate(t, store, ctxEntry("e2", "preference", "user", "preference/favorite_color", "green"))
	mustCreate(t, store, ctxEntry("e3", "fact", "user", "fact/location", "Bangkok"))
	mustCreate(t, store, ctxEntry("e4", "goal", "user", "goal/primary", "build a router"))

	out := runContextHandler(t, store, "/context")

	for _, want := range []string{
		"### Personal Context",
		"**Identity**",
		"- name: Ian",
		"**Preferences**",
		"- favorite_color: green",
		"**Facts**",
		"- location: Bangkok",
		"**Goals**",
		"- primary: build a router",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Compact output must not leak internal fields.
	for _, leaked := range []string{"id:", "created_at", "updated_at", "sources:", "confidence:"} {
		if strings.Contains(out, leaked) {
			t.Errorf("compact output leaked internal field %q:\n%s", leaked, out)
		}
	}
}

// C. Supersession: blue -> green shows only green as current.
func TestContextCommandSupersession(t *testing.T) {
	store := newContextStore(t)
	mustCreate(t, store, ctxEntry("e1", "preference", "user", "preference/favorite_color", "blue"))
	green := ctxEntry("e2", "preference", "user", "preference/favorite_color", "green")
	if _, err := store.Supersede("user", "preference/favorite_color", green); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	out := runContextHandler(t, store, "/context")
	if !strings.Contains(out, "- favorite_color: green") {
		t.Fatalf("expected green as current, got:\n%s", out)
	}
	if strings.Contains(out, "blue") {
		t.Fatalf("superseded blue leaked into current context:\n%s", out)
	}
}

// D. A forgotten entry is excluded from the current view.
func TestContextCommandForgotten(t *testing.T) {
	store := newContextStore(t)
	mustCreate(t, store, ctxEntry("e1", "preference", "user", "preference/favorite_color", "blue"))
	if err := store.Forget("e1"); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	out := runContextHandler(t, store, "/context")
	if !strings.Contains(out, "Personal Context is empty.") {
		t.Fatalf("expected empty response after forget, got:\n%s", out)
	}
	if strings.Contains(out, "blue") {
		t.Fatalf("forgotten entry leaked into output:\n%s", out)
	}
}

// E. An expired entry (valid_until in the past) is excluded.
func TestContextCommandExpired(t *testing.T) {
	store := newContextStore(t)
	e := ctxEntry("e1", "fact", "user", "fact/location", "Old City")
	until := time.Now().Add(-time.Hour)
	e.ValidUntil = &until
	mustCreate(t, store, e)

	out := runContextHandler(t, store, "/context")
	if !strings.Contains(out, "Personal Context is empty.") {
		t.Fatalf("expected empty response for expired entry, got:\n%s", out)
	}
}

// F. A future entry (valid_from in the future) is excluded.
func TestContextCommandFuture(t *testing.T) {
	store := newContextStore(t)
	e := ctxEntry("e1", "fact", "user", "fact/location", "Future City")
	from := time.Now().Add(time.Hour)
	e.ValidFrom = &from
	mustCreate(t, store, e)

	out := runContextHandler(t, store, "/context")
	if !strings.Contains(out, "Personal Context is empty.") {
		t.Fatalf("expected empty response for future entry, got:\n%s", out)
	}
}

// G. Conflicting entries surface as unresolved, never as current truth.
func TestContextCommandConflict(t *testing.T) {
	store := newContextStore(t)
	mustCreate(t, store, ctxEntry("e1", "preference", "user", "preference/favorite_color", "blue"))
	mustCreate(t, store, ctxEntry("e2", "preference", "user", "preference/favorite_color", "green"))
	if err := store.DeclareConflict("user", "preference/favorite_color", "e1", "e2"); err != nil {
		t.Fatalf("DeclareConflict: %v", err)
	}

	out := runContextHandler(t, store, "/context")

	if !strings.Contains(out, "No current beliefs.") {
		t.Fatalf("conflict must not produce current beliefs:\n%s", out)
	}
	if !strings.Contains(out, "**Unresolved**") {
		t.Fatalf("unresolved section missing:\n%s", out)
	}
	if !strings.Contains(out, "- preference/favorite_color") {
		t.Fatalf("unresolved predicate missing:\n%s", out)
	}
	for _, v := range []string{"  - blue", "  - green"} {
		if !strings.Contains(out, v) {
			t.Fatalf("unresolved candidate %q missing:\n%s", v, out)
		}
	}
	if !strings.Contains(out, "has not resolved") {
		t.Fatalf("conflict explanation missing:\n%s", out)
	}
	if strings.Contains(out, "- favorite_color: green") || strings.Contains(out, "- favorite_color: blue") {
		t.Fatalf("a conflicting candidate was presented as current:\n%s", out)
	}
}

// H. Filtering by kind, predicate, and subject returns only matching entries.
func TestContextCommandFiltering(t *testing.T) {
	store := newContextStore(t)
	mustCreate(t, store, ctxEntry("e1", "identity", "user", "identity/name", "Ian"))
	mustCreate(t, store, ctxEntry("e2", "preference", "user", "preference/favorite_color", "green"))
	mustCreate(t, store, ctxEntry("e3", "fact", "user", "fact/location", "Bangkok"))
	mustCreate(t, store, ctxEntry("e4", "fact", "home", "fact/home", "Sukhumvit"))

	cases := []struct {
		text    string
		present []string
		absent  []string
	}{
		{"/context preference", []string{"- favorite_color: green"}, []string{"location", "name", "Sukhumvit"}},
		{"/context fact/location", []string{"- location: Bangkok"}, []string{"favorite_color", "name"}},
		{"/context identity", []string{"- name: Ian"}, []string{"favorite_color", "location"}},
		{"/context user", []string{"- name: Ian", "- favorite_color: green", "- location: Bangkok"}, []string{"Sukhumvit"}},
		{"/context home", []string{"- home: Sukhumvit"}, []string{"- name: Ian"}},
	}
	for _, c := range cases {
		out := runContextHandler(t, store, c.text)
		for _, want := range c.present {
			if !strings.Contains(out, want) {
				t.Errorf("%s: output missing %q:\n%s", c.text, want, out)
			}
		}
		for _, absent := range c.absent {
			if strings.Contains(out, absent) {
				t.Errorf("%s: output unexpectedly contains %q:\n%s", c.text, absent, out)
			}
		}
	}
}

// I. Verbose output includes full provenance.
func TestContextCommandVerbose(t *testing.T) {
	store := newContextStore(t)
	mustCreate(t, store, ctxEntry("e1", "preference", "user", "preference/favorite_color", "green"))

	out := runContextHandler(t, store, "/context --verbose")

	for _, want := range []string{
		"- preference/favorite_color",
		"id: e1",
		"kind: preference",
		"subject: user",
		"predicate: preference/favorite_color",
		"value: green",
		"status: current",
		"confidence:",
		"created_at:",
		"updated_at:",
		"sources:",
		"type: conversation",
		"kind: user_declared",
		"ref: telegram:42:msg-9",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose output missing %q:\n%s", want, out)
		}
	}
}

// J. Store unavailable fails gracefully without panicking.
func TestContextCommandStoreUnavailable(t *testing.T) {
	var out string
	rt := &Runtime{} // PersonalContext is nil
	req := Request{
		Text:    "/context",
		Channel: "cli",
		Reply: func(s string) error {
			out = s
			return nil
		},
	}
	if err := contextHandler(context.Background(), req, rt); err != nil {
		t.Fatalf("contextHandler: %v", err)
	}
	if !strings.Contains(out, "Personal Context is unavailable.") {
		t.Fatalf("expected availability error, got: %q", out)
	}
}

// K. The command executes with no LLM/provider at all (pure store read).
func TestContextCommandNoLLM(t *testing.T) {
	store := newContextStore(t)
	mustCreate(t, store, ctxEntry("e1", "preference", "user", "preference/favorite_color", "green"))
	out := runContextHandler(t, store, "/context")
	if !strings.Contains(out, "- favorite_color: green") {
		t.Fatalf("expected favorite color in output:\n%s", out)
	}
}

func mustCreate(t *testing.T, store *personalcontext.Store, e personalcontext.Entry) {
	t.Helper()
	if _, err := store.Create(e); err != nil {
		t.Fatalf("Create(%s): %v", e.ID, err)
	}
}
