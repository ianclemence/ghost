package life

import (
	"strings"
	"testing"
	"time"
)

func TestKnowledgeFollowsWhatTheOwnerReads(t *testing.T) {
	k := OpenKnowledge(t.TempDir())
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	said := Source{Kind: "conversation"}
	l, created, err := k.Save(LearningInput{Title: "Sapiens", Author: "Yuval Noah Harari", Current: -1, Total: 443, Source: said}, now)
	if err != nil || !created || l.Status != "want" || l.Total != 443 || l.Unit != "page" {
		t.Fatalf("new: %+v %v", l, err)
	}
	// Saying where they are makes it active, and starts it.
	l, created, _ = k.Save(LearningInput{Title: "sapiens", Current: 140, Total: -1, Source: said}, now.Add(time.Hour))
	if created || l.Status != "active" || l.Current != 140 || l.Started != "2026-10-09" || l.Progress() != "page 140 of 443" {
		t.Fatalf("progress: %+v", l)
	}
	l, _, _ = k.Save(LearningInput{Title: "Sapiens", Current: -1, Total: -1, Note: "Gossip let us cooperate in large groups.", Source: said}, now.Add(2*time.Hour))
	l, _, _ = k.Save(LearningInput{Title: "Sapiens", Current: -1, Total: -1, Note: "History is something very few people have been doing.", Quote: true, Where: "p. 112", Source: said}, now.Add(3*time.Hour))
	if len(l.Notes) != 2 || !l.Notes[1].Quote || l.Notes[1].Where != "p. 112" {
		t.Fatalf("notes: %+v", l.Notes)
	}
	// Reaching the end finishes it.
	l, _, _ = k.Save(LearningInput{Title: "Sapiens", Current: 443, Total: -1, Source: said}, now.Add(48*time.Hour))
	if l.Status != "done" || l.Finished != "2026-10-11" {
		t.Fatalf("done: %+v", l)
	}
	// A course in lessons; a subject with an exam.
	_, _, _ = k.Save(LearningInput{Title: "CPA Section 1", Kind: "subject", Goal: "Pass in March", Due: "2027-03-15", Current: -1, Total: -1, Status: "active", Source: said}, now)
	c, _, _ := k.Save(LearningInput{Title: "Intro to Go", Kind: "course", Current: 3, Total: 12, Source: said}, now)
	if c.Unit != "lesson" || c.Progress() != "lesson 3 of 12" {
		t.Fatalf("course: %+v", c)
	}
	all, _ := k.All()
	if all[0].Status != "active" || all[len(all)-1].Title != "Sapiens" {
		t.Fatalf("order: %v %v", all[0].Title, all[len(all)-1].Title)
	}
	if q := k.Quiet(now.Add(30*24*time.Hour), 14*24*time.Hour); len(q) != 2 {
		t.Fatalf("quiet: %d", len(q))
	}
	if _, _, err := k.Save(LearningInput{Title: "x", Kind: "movie", Current: -1, Total: -1, Source: said}, now); err == nil || !strings.Contains(err.Error(), "book") {
		t.Fatalf("kind: %v", err)
	}
	if _, _, err := k.Save(LearningInput{Title: "x", Unit: "%", Current: 140, Total: -1, Source: said}, now); err == nil {
		t.Fatal("percent over 100")
	}
	found, err := k.Find("intro to")
	if err != nil || found.ID != c.ID {
		t.Fatalf("find: %v", err)
	}
	e, err := k.Edit(c.ID, LearningInput{Title: "Intro to Go", Kind: "course", Unit: "lesson", Current: 5, Total: 12, Status: "paused"}, nil, now)
	if err != nil || e.Status != "paused" || e.Current != 5 {
		t.Fatalf("edit: %+v %v", e, err)
	}
	if err := k.Forget(c.ID); err != nil {
		t.Fatal(err)
	}
}
