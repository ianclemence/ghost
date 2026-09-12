package agent

import (
	"net"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
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

func TestIsSchedulerEcho(t *testing.T) {
	echo := map[string]bool{"send ian a short poem": true}
	// Verbatim automation content is echo even without intent phrasing.
	if !isSchedulerEcho("Send Ian a short poem", echo) {
		t.Error("automation action content not detected as echo")
	}
	if !isSchedulerEcho("Send Ian a short poem.", echo) {
		t.Error("echo with trailing punctuation not detected")
	}
	// Scheduling-intent phrasing is echo without any item match.
	if !isSchedulerEcho("Send me a poem every 5 seconds", map[string]bool{}) {
		t.Error("scheduling intent not detected as echo")
	}
	// Genuine user facts are never echo.
	for _, fact := range []string{"Ian", "Chelsea", "I would like to go to Bangkok some time"} {
		if isSchedulerEcho(fact, echo) {
			t.Errorf("genuine fact %q wrongly detected as echo", fact)
		}
	}
}

func TestPickEmbedProviderPrefersReachableOllama(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback listener available")
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	cfg := config.DefaultConfig()
	cfg.Providers.Ollama.APIBase = "http://" + ln.Addr().String()
	chat := &mockProvider{}
	got := pickEmbedProvider(cfg, chat)
	if got == providers.LLMProvider(chat) {
		t.Error("reachable Ollama must win over the chat provider for embeddings")
	}
}

func TestPickEmbedProviderFallsBackWhenOllamaDown(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Providers.Ollama.APIBase = "http://127.0.0.1:1"
	chat := &mockProvider{}
	got := pickEmbedProvider(cfg, chat)
	if got != providers.LLMProvider(chat) {
		t.Error("unreachable Ollama must fall back to the chat provider")
	}
}

func TestStripScheduleFooters(t *testing.T) {
	in := "The user likes Chelsea.\n[Open schedules @11:04 — from scheduler rows, authoritative over chat text:]\n- Today at 9 PM → Sat 21:00\n- Every 2 minutes → Sat 11:06 — Send Ian a short poem\nMore prose here."
	got := stripScheduleFooters(in)
	if strings.Contains(got, "[Open schedules @") || strings.Contains(got, "Today at 9 PM") {
		t.Errorf("old footer survived stripping: %q", got)
	}
	if !strings.Contains(got, "The user likes Chelsea.") || !strings.Contains(got, "More prose here.") {
		t.Errorf("prose lost in stripping: %q", got)
	}
	if stripScheduleFooters("plain summary, no footer") != "plain summary, no footer" {
		t.Error("footer-free summary altered")
	}
	if stripScheduleFooters("") != "" {
		t.Error("empty summary altered")
	}
}
