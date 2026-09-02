package personalcontext

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTitle(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		entry    Entry
		expected string
	}{
		{
			name: "identity name",
			entry: Entry{
				Kind:      KindIdentity,
				Predicate: "identity/name",
				Value:     json.RawMessage(`"Ian"`),
			},
			expected: "Named Ian",
		},
		{
			name: "fact location",
			entry: Entry{
				Kind:      KindFact,
				Predicate: "fact/location",
				Value:     json.RawMessage(`"Bangkok"`),
			},
			expected: "Lives in Bangkok",
		},
		{
			name: "preference likes",
			entry: Entry{
				Kind:      KindPreference,
				Predicate: "preference/likes",
				Value:     json.RawMessage(`"cats"`),
			},
			expected: "Likes cats",
		},
		{
			name: "preference prefers",
			entry: Entry{
				Kind:      KindPreference,
				Predicate: "preference/prefers",
				Value:     json.RawMessage(`"tea over coffee"`),
			},
			expected: "Prefers tea over coffee",
		},
		{
			name: "goal primary",
			entry: Entry{
				Kind:      KindGoal,
				Predicate: "goal/primary",
				Value:     json.RawMessage(`"build a house"`),
			},
			expected: "Goal: build a house",
		},
		{
			name: "relationship partner",
			entry: Entry{
				Kind:      KindRelationship,
				Predicate: "relationship/partner",
				Value:     json.RawMessage(`"Sam"`),
			},
			expected: "Partner: Sam",
		},
		{
			name: "fact work",
			entry: Entry{
				Kind:      KindFact,
				Predicate: "fact/work",
				Value:     json.RawMessage(`"Google"`),
			},
			expected: "Works at Google",
		},
		{
			name: "preference favorite food",
			entry: Entry{
				Kind:      KindPreference,
				Predicate: "preference/favorite_food",
				Value:     json.RawMessage(`"sushi"`),
			},
			expected: "Favorite food: sushi",
		},
		{
			name: "communication style",
			entry: Entry{
				Kind:      KindPreference,
				Predicate: "preference/communication.style",
				Value:     json.RawMessage(`"concise"`),
			},
			expected: "Communication style: concise",
		},
		{
			name: "unknown predicate fallback",
			entry: Entry{
				Kind:      KindFact,
				Predicate: "fact/unknown_thing",
				Value:     json.RawMessage(`"value"`),
			},
			expected: "Unknown thing: value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.entry.ID = "test"
			tt.entry.Subject = "user"
			tt.entry.Status = StatusCurrent
			tt.entry.CreatedAt = now
			tt.entry.UpdatedAt = now
			got := Title(tt.entry)
			if got != tt.expected {
				t.Errorf("Title() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		entry    Entry
		expected string
	}{
		{
			name: "identity name",
			entry: Entry{
				Kind:      KindIdentity,
				Predicate: "identity/name",
				Value:     json.RawMessage(`"Ian"`),
			},
			expected: "Your name is Ian.",
		},
		{
			name: "fact location",
			entry: Entry{
				Kind:      KindFact,
				Predicate: "fact/location",
				Value:     json.RawMessage(`"Bangkok"`),
			},
			expected: "You live in Bangkok.",
		},
		{
			name: "preference likes",
			entry: Entry{
				Kind:      KindPreference,
				Predicate: "preference/likes",
				Value:     json.RawMessage(`"cats"`),
			},
			expected: "You like cats.",
		},
		{
			name: "goal primary",
			entry: Entry{
				Kind:      KindGoal,
				Predicate: "goal/primary",
				Value:     json.RawMessage(`"build a house"`),
			},
			expected: "Your goal is to build a house.",
		},
		{
			name: "relationship partner",
			entry: Entry{
				Kind:      KindRelationship,
				Predicate: "relationship/partner",
				Value:     json.RawMessage(`"Sam"`),
			},
			expected: "Your partner is Sam.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.entry.ID = "test"
			tt.entry.Subject = "user"
			tt.entry.Status = StatusCurrent
			tt.entry.CreatedAt = now
			tt.entry.UpdatedAt = now
			got := Summary(tt.entry)
			if got != tt.expected {
				t.Errorf("Summary() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDomain(t *testing.T) {
	tests := []struct {
		predicate string
		expected  Domain
	}{
		{"identity/name", DomainIdentity},
		{"fact/location", DomainLocation},
		{"fact/work", DomainWork},
		{"preference/favorite_food", DomainFood},
		{"preference/communication.style", DomainCommunication},
		{"goal/primary", DomainWork},
		{"relationship/partner", DomainRelationship},
		{"unknown/predicate", DomainOther},
	}

	for _, tt := range tests {
		t.Run(tt.predicate, func(t *testing.T) {
			got := ClassifyDomain(tt.predicate)
			if got != tt.expected {
				t.Errorf("Domain(%q) = %q, want %q", tt.predicate, got, tt.expected)
			}
		})
	}
}

func TestDomainLabel(t *testing.T) {
	tests := []struct {
		domain   Domain
		expected string
	}{
		{DomainIdentity, "Identity"},
		{DomainFood, "Food"},
		{DomainLocation, "Location"},
		{DomainWork, "Work"},
		{DomainOther, "Other"},
	}

	for _, tt := range tests {
		t.Run(string(tt.domain), func(t *testing.T) {
			got := DomainLabel(tt.domain)
			if got != tt.expected {
				t.Errorf("DomainLabel(%q) = %q, want %q", tt.domain, got, tt.expected)
			}
		})
	}
}

func TestClassifyEntryDomain(t *testing.T) {
	entry := func(predicate, value string) Entry {
		return Entry{Kind: KindPreference, Predicate: predicate, Value: json.RawMessage(`"` + value + `"`)}
	}
	tests := []struct {
		name string
		e    Entry
		want Domain
	}{
		{"specific predicate is preserved", entry("preference/favorite_food", "sushi"), DomainFood},
		{"generic prefer with tea value is food", entry("preference/prefers", "tea over coffee"), DomainFood},
		{"generic favorite with sushi value is food", entry("preference/favorite", "sushi"), DomainFood},
		{"generic likes with games value stays lifestyle", entry("preference/likes", "board games"), DomainLifestyle},
		{"non-food generic stays lifestyle", entry("preference/prefers", "quiet evenings"), DomainLifestyle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyEntryDomain(tt.e)
			if got != tt.want {
				t.Errorf("ClassifyEntryDomain(%q/%q) = %q, want %q", tt.e.Predicate, string(tt.e.Value), got, tt.want)
			}
		})
	}
}

func TestTitleRelationshipEntity(t *testing.T) {
	now := time.Now()
	entry := Entry{
		Kind:      KindRelationship,
		Predicate: "relationship/partner",
		Value:     json.RawMessage(`"Sarah and I are business partners."`),
		ID:        "test",
		Subject:   "user",
		Status:    StatusCurrent,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if got := Title(entry); got != "Partner: Sarah" {
		t.Errorf("Title() = %q, want %q", got, "Partner: Sarah")
	}
	entry.Predicate = "relationship/colleague"
	if got := Title(entry); got != "Colleague: Sarah" {
		t.Errorf("Title() = %q, want %q", got, "Colleague: Sarah")
	}
	if got := Summary(entry); got != "You work with Sarah." {
		t.Errorf("Summary() = %q, want %q", got, "You work with Sarah.")
	}
}
