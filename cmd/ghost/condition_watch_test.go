package main

import "testing"

func TestConditionWatchResolvesAfterARunOfClearChecks(t *testing.T) {
	w := newConditionWatch()
	var got []string
	res := func(k string) { got = append(got, k) }

	w.observe(map[string]bool{"storage-critical": true}, res)
	w.observe(map[string]bool{"storage-critical": false}, res)
	if len(got) != 0 {
		t.Fatalf("resolved after one clear check: %v", got)
	}
	w.observe(map[string]bool{"storage-critical": false}, res)
	if len(got) != 1 || got[0] != "storage-critical" {
		t.Fatalf("not resolved after two: %v", got)
	}
	w.observe(map[string]bool{"storage-critical": false}, res)
	if len(got) != 1 {
		t.Fatalf("resolved twice: %v", got)
	}
}

func TestConditionWatchIgnoresAReadingThatFlapsBack(t *testing.T) {
	w := newConditionWatch()
	var got []string
	res := func(k string) { got = append(got, k) }
	w.observe(map[string]bool{"pod-hot": false}, res)
	w.observe(map[string]bool{"pod-hot": true}, res) // back before the run completed
	w.observe(map[string]bool{"pod-hot": false}, res)
	if len(got) != 0 {
		t.Fatalf("flapping reading resolved: %v", got)
	}
}

func TestConditionWatchLeavesUnreadConditionsAlone(t *testing.T) {
	w := newConditionWatch()
	var got []string
	res := func(k string) { got = append(got, k) }
	for i := 0; i < 5; i++ {
		w.observe(map[string]bool{}, res) // sensor missing: nothing known
	}
	if len(got) != 0 {
		t.Fatalf("resolved an unread condition: %v", got)
	}
}

func TestTUIDrawsASettledAlertAsResolved(t *testing.T) {
	if got := tuiTagFor(historyEntry{Kind: "alert"}); got != tagAlert {
		t.Fatalf("open alert tag: %q", got)
	}
	if got := tuiTagFor(historyEntry{Kind: "alert", Resolved: true}); got != tagResolved {
		t.Fatalf("settled alert tag: %q", got)
	}
	if got := tuiTagFor(historyEntry{Kind: "reminder", Resolved: true}); got != "reminder" {
		t.Fatalf("only alerts and notices settle: %q", got)
	}
}
