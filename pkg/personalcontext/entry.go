// Package personalcontext is the storage layer for Personal Context: a
// derived, portable, correctable view of what Ghost currently believes about
// its person, grounded in conversations which remain the authoritative
// evidence.
//
// The store is an append-only JSONL log (personal-context/entries.jsonl).
// Entries are never rewritten in place: every change to an entry appends a new
// revision record with the same id, and loading the log reconstructs the
// current state by taking the last record per id. Nothing in this package ever
// reads, writes, or deletes conversation evidence, requires a model, or
// touches RAG.
package personalcontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Kind classifies what kind of belief an entry records.
type Kind string

const (
	KindIdentity     Kind = "identity"
	KindFact         Kind = "fact"
	KindPreference   Kind = "preference"
	KindRelationship Kind = "relationship"
	KindGoal         Kind = "goal"
	KindDecision     Kind = "decision"
	KindConsent      Kind = "consent"
	KindRoutine      Kind = "routine"
	KindProject      Kind = "project"
	KindConstraint   Kind = "constraint"
	KindInterest     Kind = "interest"
)

// Status is the lifecycle state of an entry. Only StatusCurrent entries are
// returned by the current-context query; superseded, conflicting, uncertain,
// and rejected entries remain inspectable through the history queries.
type Status string

const (
	StatusCurrent     Status = "current"
	StatusSuperseded  Status = "superseded"
	StatusConflicting Status = "conflicting"
	StatusUncertain   Status = "uncertain"
	StatusRejected    Status = "rejected"
)

// SourceType is the class of origin of a source.
type SourceType string

const (
	SourceConversation   SourceType = "conversation"
	SourceCommand        SourceType = "command"
	SourceDocument       SourceType = "document"
	SourceWorkflow       SourceType = "workflow"
	SourceImport         SourceType = "import"
	SourceManualEdit     SourceType = "manual_edit"
	SourceAgentInference SourceType = "agent_inference"
)

// SourceKind describes how the evidence established the entry.
type SourceKind string

const (
	SourceUserDeclared  SourceKind = "user_declared"
	SourceUserCorrected SourceKind = "user_corrected"
	SourceInferred      SourceKind = "inferred"
	SourceImported      SourceKind = "imported"
	SourceManual        SourceKind = "manual"
	SourceKindWorkflow  SourceKind = "workflow"
)

// Source is an immutable piece of historical evidence attached to an entry.
// Sources are never erased: a revision keeps the sources of the record it was
// created from, and a correction adds a new source on the new entry.
type Source struct {
	Type      SourceType `json:"type"`
	Kind      SourceKind `json:"kind"`
	Ref       string     `json:"ref,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
}

// Entry is one belief in Personal Context. It is the canonical unit of the
// store: an identity, fact, preference, relationship, goal, decision, consent,
// or routine, with its provenance and lifecycle status.
//
// Value is a JSON value preserved byte-for-byte (compacted at write time).
// Every change to an entry appends a new record with the same ID; the final
// record for an ID is that entry's current state.
type Entry struct {
	ID        string          `json:"id"`
	Kind      Kind            `json:"kind"`
	Subject   string          `json:"subject"`
	Predicate string          `json:"predicate"`
	Value     json.RawMessage `json:"value"`
	Status    Status          `json:"status"`
	// Scopes limits visibility to contexts (e.g. ["context:work"]).
	// Empty = global memory, visible from every context. Additive and
	// backward compatible: old entries without scopes stay global.
	Scopes       []string   `json:"scopes,omitempty"`
	Confidence   float64    `json:"confidence"`
	ValidFrom    *time.Time `json:"valid_from,omitempty"`
	ValidUntil   *time.Time `json:"valid_until,omitempty"`
	SupersededBy *string    `json:"superseded_by,omitempty"`
	Sources      []Source   `json:"sources"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// ReinforceCount is how many times this belief was restated after creation
	// (nudge-style reinforcement). ReinforcedAt is the last time it was restated,
	// so a consolidated memory can answer "when was this last reinforced".
	ReinforceCount int        `json:"reinforce_count,omitempty"`
	ReinforcedAt   *time.Time `json:"reinforced_at,omitempty"`
}

// RawValue returns a json.RawMessage for an arbitrary Go value. It is the
// intended way to build the Value field deterministically.
func RawValue(v interface{}) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return nil, err
	}
	return json.RawMessage(buf.Bytes()), nil
}

// ValueInto unmarshals the entry's value into v.
func (e Entry) ValueInto(v interface{}) error {
	return json.Unmarshal(e.Value, v)
}

// ValidKind reports whether k is a known kind.
func ValidKind(k Kind) bool {
	switch k {
	case KindIdentity, KindFact, KindPreference, KindRelationship,
		KindGoal, KindDecision, KindConsent, KindRoutine:
		return true
	}
	return false
}

// ValidStatus reports whether s is a known status.
func ValidStatus(s Status) bool {
	switch s {
	case StatusCurrent, StatusSuperseded, StatusConflicting,
		StatusUncertain, StatusRejected:
		return true
	}
	return false
}

// ValidSourceType reports whether t is a known source type.
func ValidSourceType(t SourceType) bool {
	switch t {
	case SourceConversation, SourceCommand, SourceDocument, SourceWorkflow,
		SourceImport, SourceManualEdit, SourceAgentInference:
		return true
	}
	return false
}

// ValidSourceKind reports whether k is a known source kind.
func ValidSourceKind(k SourceKind) bool {
	switch k {
	case SourceUserDeclared, SourceUserCorrected, SourceInferred,
		SourceImported, SourceManual, SourceKindWorkflow:
		return true
	}
	return false
}

// Validate checks that an entry is well-formed enough to append. ID, subject,
// and predicate are required; kind and status must be known; confidence must
// be within 0..1; value must be present valid JSON; temporal validity must not
// be inverted; and every supplied source must be well-formed with an explicit
// timestamp. Provenance is never invented: an entry with no sources validates
// cleanly.
func (e Entry) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if strings.TrimSpace(e.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if strings.TrimSpace(e.Predicate) == "" {
		return fmt.Errorf("predicate is required")
	}
	if !ValidKind(e.Kind) {
		return fmt.Errorf("invalid kind %q", e.Kind)
	}
	if !ValidStatus(e.Status) {
		return fmt.Errorf("invalid status %q", e.Status)
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return fmt.Errorf("confidence %v must be within 0..1", e.Confidence)
	}
	if len(e.Value) == 0 {
		return fmt.Errorf("value is required")
	}
	if !json.Valid(e.Value) {
		return fmt.Errorf("value is not valid JSON: %q", e.Value)
	}
	if e.ValidFrom != nil && e.ValidUntil != nil && e.ValidUntil.Before(*e.ValidFrom) {
		return fmt.Errorf("valid_until %s precedes valid_from %s", e.ValidUntil, e.ValidFrom)
	}
	for i, src := range e.Sources {
		if !ValidSourceType(src.Type) {
			return fmt.Errorf("source %d: invalid type %q", i, src.Type)
		}
		if !ValidSourceKind(src.Kind) {
			return fmt.Errorf("source %d: invalid kind %q", i, src.Kind)
		}
		if src.Timestamp.IsZero() {
			return fmt.Errorf("source %d: timestamp is required", i)
		}
	}
	return nil
}
