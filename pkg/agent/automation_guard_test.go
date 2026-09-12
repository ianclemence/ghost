package agent

import (
	"testing"
)

func TestIsAutomationSession(t *testing.T) {
	for _, key := range []string{"automation:abc-123", "routine:poem-daily"} {
		if !isAutomationSession(key) {
			t.Errorf("isAutomationSession(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"mobile:default", "cli:default", "", "heartbeat", "cron-morning"} {
		if isAutomationSession(key) {
			t.Errorf("isAutomationSession(%q) = true, want false", key)
		}
	}
}

func TestIsAutomationIntent(t *testing.T) {
	scheduling := []string{
		"Remind me at 9pm that Chelsea is playing",
		"Send me a poem every 5 seconds",
		"Every 2 minutes",
		"remind me tomorrow at 8 AM to drink water",
		"wake me up at 7",
		"set an alarm for 6am",
	}
	for _, msg := range scheduling {
		if !isAutomationIntent(msg) {
			t.Errorf("isAutomationIntent(%q) = false, want true", msg)
		}
	}
	conversational := []string{
		"My name is Ian",
		"My favorite team is Chelsea",
		"My girlfriend's name is Jasmine",
		"What is the weather in Phuket",
		"I would like to go to Bangkok some time",
		"The lion is a fierce animal",
		"Which team do you support?",
	}
	for _, msg := range conversational {
		if isAutomationIntent(msg) {
			t.Errorf("isAutomationIntent(%q) = true, want false", msg)
		}
	}
}

// A journal entry restating the note's tail is redundant; a new topic is not.
func TestJournalRedundant(t *testing.T) {
	today := "# 2026-09-12\n\n- [11:02] (journal) Chelsea play Hull City at home, kick-off 14:00 London time, 20:00 Bangkok. The user's reminder moved to 19:45 Bangkok time."
	restatement := "Chelsea versus Hull City kicks off at 14:00 London time which is 20:00 in Bangkok, and the reminder is now set for 19:45 Bangkok time before the match."
	if !journalRedundant(today, restatement) {
		t.Error("restatement not detected as redundant")
	}
	newTopic := "The user asked about Phuket ferry schedules and decided to book the morning boat to Phi Phi on Tuesday with Jasmine."
	if journalRedundant(today, newTopic) {
		t.Error("new topic wrongly detected as redundant")
	}
	if journalRedundant(today, "ok noted") {
		t.Error("short note should never count as redundant")
	}
	if journalRedundant("", restatement) {
		t.Error("empty note cannot be redundant")
	}
}

func TestContentWordsIgnoresNoise(t *testing.T) {
	words := contentWords("The Chelsea Reminder for the User at the Match")
	for _, noise := range []string{"the", "for", "at", "user"} {
		if words[noise] {
			t.Errorf("noise word %q kept", noise)
		}
	}
	if !words["chelsea"] || !words["reminder"] || !words["match"] {
		t.Errorf("content words lost: %v", words)
	}
}
