// Package main provides real-world verification tests for Ghost.
// These tests verify that the implementation works correctly in real usage scenarios.
package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/session"
)

// TestMemoryExtraction tests real-world memory extraction scenarios.
func TestMemoryExtraction(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []struct {
			predicate string
			value     string
			kind      personalcontext.Kind
			domain    personalcontext.Domain
		}
	}{
		{
			name:  "Tea preference",
			input: "I prefer tea over coffee.",
			expected: []struct {
				predicate string
				value     string
				kind      personalcontext.Kind
				domain    personalcontext.Domain
			}{
				{
					predicate: "preference/prefers",
					value:     "tea over coffee",
					kind:      personalcontext.KindPreference,
					domain:    personalcontext.DomainLifestyle,
				},
			},
		},
		{
			name:  "Location",
			input: "I live in Bangkok.",
			expected: []struct {
				predicate string
				value     string
				kind      personalcontext.Kind
				domain    personalcontext.Domain
			}{
				{
					predicate: "fact/location",
					value:     "Bangkok",
					kind:      personalcontext.KindFact,
					domain:    personalcontext.DomainLocation,
				},
			},
		},
		{
			name:  "Work - using supported pattern",
			input: "I work at Ghost.",
			expected: []struct {
				predicate string
				value     string
				kind      personalcontext.Kind
				domain    personalcontext.Domain
			}{
				{
					predicate: "fact/work",
					value:     "Ghost",
					kind:      personalcontext.KindFact,
					domain:    personalcontext.DomainWork,
				},
			},
		},
		{
			name:  "Favorite food",
			input: "My favorite food is sushi.",
			expected: []struct {
				predicate string
				value     string
				kind      personalcontext.Kind
				domain    personalcontext.Domain
			}{
				{
					predicate: "preference/favorite_food",
					value:     "sushi",
					kind:      personalcontext.KindPreference,
					domain:    personalcontext.DomainFood,
				},
			},
		},
		{
			name:  "Goal - using supported pattern",
			input: "My goal is to launch Ghost.",
			expected: []struct {
				predicate string
				value     string
				kind      personalcontext.Kind
				domain    personalcontext.Domain
			}{
				{
					predicate: "goal/primary",
					value:     "launch Ghost",
					kind:      personalcontext.KindGoal,
					domain:    personalcontext.DomainWork,
				},
			},
		},
		{
			name:  "Relationship - using supported pattern",
			input: "My partner's name is Sarah.",
			expected: []struct {
				predicate string
				value     string
				kind      personalcontext.Kind
				domain    personalcontext.Domain
			}{
				{
					predicate: "relationship/partner",
					value:     "Sarah",
					kind:      personalcontext.KindRelationship,
					domain:    personalcontext.DomainRelationship,
				},
			},
		},
		{
			name:  "Work on - using supported pattern",
			input: "I'm working on Ghost.",
			expected: []struct {
				predicate string
				value     string
				kind      personalcontext.Kind
				domain    personalcontext.Domain
			}{
				{
					predicate: "fact/work",
					value:     "Ghost",
					kind:      personalcontext.KindFact,
					domain:    personalcontext.DomainWork,
				},
			},
		},
		{
			name:  "Partner is - using supported pattern",
			input: "Sarah is my wife.",
			expected: []struct {
				predicate string
				value     string
				kind      personalcontext.Kind
				domain    personalcontext.Domain
			}{
				{
					predicate: "relationship/partner",
					value:     "Sarah",
					kind:      personalcontext.KindRelationship,
					domain:    personalcontext.DomainRelationship,
				},
			},
		},
		{
			name:  "Work with - using supported pattern",
			input: "I work with Alex.",
			expected: []struct {
				predicate string
				value     string
				kind      personalcontext.Kind
				domain    personalcontext.Domain
			}{
				{
					predicate: "relationship/colleague",
					value:     "Alex",
					kind:      personalcontext.KindRelationship,
					domain:    personalcontext.DomainRelationship,
				},
			},
		},
		{
			name:     "Math question - should NOT extract",
			input:    "What's 20% of 500?",
			expected: nil,
		},
		{
			name:     "Temporary context - should NOT extract",
			input:    "I'm meeting Sarah tomorrow at 3.",
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create input
			input := personalcontext.Input{
				SessionID: "test-session",
				MessageID: fmt.Sprintf("msg-%d", time.Now().UnixNano()),
				Text:      tc.input,
				Timestamp: time.Now().UTC(),
			}

			// Extract
			actions, err := personalcontext.Extract(input)
			if err != nil {
				t.Fatalf("extraction failed: %v", err)
			}

			// Verify
			if tc.expected == nil {
				if len(actions) > 0 {
					t.Errorf("expected no actions, got %d", len(actions))
				}
				return
			}

			if len(actions) != len(tc.expected) {
				t.Errorf("expected %d actions, got %d", len(tc.expected), len(actions))
				return
			}

			for i, action := range actions {
				exp := tc.expected[i]
				if action.Entry.Predicate != exp.predicate {
					t.Errorf("predicate: got %q, want %q", action.Entry.Predicate, exp.predicate)
				}
				// Extract value from json.RawMessage
				var value string
				if err := json.Unmarshal(action.Entry.Value, &value); err != nil {
					t.Fatalf("failed to unmarshal value: %v", err)
				}
				if value != exp.value {
					t.Errorf("value: got %q, want %q", value, exp.value)
				}
				if action.Entry.Kind != exp.kind {
					t.Errorf("kind: got %q, want %q", action.Entry.Kind, exp.kind)
				}

				// Test domain classification
				domain := personalcontext.ClassifyDomain(action.Entry.Predicate)
				if domain != exp.domain {
					t.Errorf("domain: got %q, want %q", domain, exp.domain)
				}
			}
		})
	}
}

// TestMemoryPresentation tests memory title and domain presentation.
func TestMemoryPresentation(t *testing.T) {
	testCases := []struct {
		name           string
		entry          personalcontext.Entry
		expectedTitle  string
		expectedDomain personalcontext.Domain
		expectedLabel  string
	}{
		{
			name: "Name",
			entry: personalcontext.Entry{
				Kind:      personalcontext.KindIdentity,
				Predicate: "identity/name",
				Value:     json.RawMessage(`"Ian"`),
			},
			expectedTitle:  "Named Ian",
			expectedDomain: personalcontext.DomainIdentity,
			expectedLabel:  "Identity",
		},
		{
			name: "Location",
			entry: personalcontext.Entry{
				Kind:      personalcontext.KindFact,
				Predicate: "fact/location",
				Value:     json.RawMessage(`"Bangkok"`),
			},
			expectedTitle:  "Lives in Bangkok",
			expectedDomain: personalcontext.DomainLocation,
			expectedLabel:  "Location",
		},
		{
			name: "Work",
			entry: personalcontext.Entry{
				Kind:      personalcontext.KindFact,
				Predicate: "fact/work",
				Value:     json.RawMessage(`"Ghost"`),
			},
			expectedTitle:  "Works at Ghost",
			expectedDomain: personalcontext.DomainWork,
			expectedLabel:  "Work",
		},
		{
			name: "Preference",
			entry: personalcontext.Entry{
				Kind:      personalcontext.KindPreference,
				Predicate: "preference/prefers",
				Value:     json.RawMessage(`"tea over coffee"`),
			},
			expectedTitle:  "Prefers tea over coffee",
			expectedDomain: personalcontext.DomainLifestyle,
			expectedLabel:  "Lifestyle",
		},
		{
			name: "Goal",
			entry: personalcontext.Entry{
				Kind:      personalcontext.KindGoal,
				Predicate: "goal/primary",
				Value:     json.RawMessage(`"launch Ghost"`),
			},
			expectedTitle:  "Goal: launch Ghost",
			expectedDomain: personalcontext.DomainWork,
			expectedLabel:  "Work",
		},
		{
			name: "Relationship",
			entry: personalcontext.Entry{
				Kind:      personalcontext.KindRelationship,
				Predicate: "relationship/partner",
				Value:     json.RawMessage(`"Sarah"`),
			},
			expectedTitle:  "Partner: Sarah",
			expectedDomain: personalcontext.DomainRelationship,
			expectedLabel:  "Relationship",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test title
			title := personalcontext.Title(tc.entry)
			if title != tc.expectedTitle {
				t.Errorf("title: got %q, want %q", title, tc.expectedTitle)
			}

			// Test domain
			domain := personalcontext.ClassifyDomain(tc.entry.Predicate)
			if domain != tc.expectedDomain {
				t.Errorf("domain: got %q, want %q", domain, tc.expectedDomain)
			}

			// Test domain label
			label := personalcontext.DomainLabel(domain)
			if label != tc.expectedLabel {
				t.Errorf("label: got %q, want %q", label, tc.expectedLabel)
			}
		})
	}
}

// TestConversationTitles tests conversation title generation.
func TestConversationTitles(t *testing.T) {
	testCases := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "Trip planning",
			message:  "Help me plan a trip to Japan.",
			expected: "Help me plan a trip to Japan",
		},
		{
			name:     "With greeting",
			message:  "Hey Ghost, help me plan a trip to Japan.",
			expected: "Help me plan a trip to Japan",
		},
		{
			name:     "Long message",
			message:  "Help me plan a very long trip to Japan that involves visiting multiple cities and doing many activities over several weeks",
			expected: "Help me plan a very long trip to Japan that involves",
		},
		{
			name:     "With punctuation",
			message:  "Help me plan a trip to Japan!",
			expected: "Help me plan a trip to Japan",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			title := session.GenerateTitle(tc.message)
			if title != tc.expected {
				t.Errorf("title: got %q, want %q", title, tc.expected)
			}
		})
	}
}

// TestScheduledItemTypes tests scheduled item type constants.
func TestScheduledItemTypes(t *testing.T) {
	types := []scheduled.ItemType{scheduled.TypeReminder, scheduled.TypeEvent, scheduled.TypeAutomation, scheduled.TypeTask}
	for _, typ := range types {
		if !scheduled.ValidTypes[typ] {
			t.Errorf("expected %s to be a valid type", typ)
		}
	}
}

// TestScheduledItemStates tests scheduled item state constants.
func TestScheduledItemStates(t *testing.T) {
	states := []scheduled.ItemState{
		scheduled.StateScheduled, scheduled.StateDue, scheduled.StateRunning, scheduled.StateCompleted,
		scheduled.StateFailed, scheduled.StateCancelled, scheduled.StateMissed, scheduled.StatePaused,
	}
	for _, state := range states {
		if !scheduled.ValidStates[state] {
			t.Errorf("expected %s to be a valid state", state)
		}
	}
}

// TestScheduledItemHumanSchedule tests human-readable schedule formatting.
func TestScheduledItemHumanSchedule(t *testing.T) {
	cases := []struct {
		schedule scheduled.Schedule
		want     string
	}{
		{scheduled.Schedule{Kind: scheduled.ScheduleAt, At: timePtr(time.Date(2025, 9, 8, 8, 0, 0, 0, time.UTC))}, "Monday, September 8 at 8:00 AM"},
		{scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: 24 * time.Hour}, "Every day"},
		{scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: time.Hour}, "Every hour"},
		{scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: "0 8 * * 1"}, "Every Monday at 8:00 AM"},
		{scheduled.Schedule{Kind: scheduled.ScheduleNone}, "Manual"},
	}
	for _, tc := range cases {
		item := &scheduled.ScheduledItem{Schedule: tc.schedule}
		if got := item.HumanSchedule(); got != tc.want {
			t.Errorf("HumanSchedule() = %q, want %q", got, tc.want)
		}
	}
}

// Helper function
func timePtr(t time.Time) *time.Time {
	return &t
}
