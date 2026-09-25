package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The reflection leash: insight is recorded, but it must never reach the
// prompt context on its own. If this test fails, a private reflection about
// the user is being fed back to the model without anyone deciding it should.
func TestReflectionsAreRecordedButNotHoisted(t *testing.T) {
	ws := t.TempDir()
	ms := NewMemoryStore(ws)
	body := "Pete is terse and dislikes ceremony. He wants things to work."

	path, err := ms.AppendReflection(time.Now(), body)
	if err != nil {
		t.Fatalf("append reflection: %v", err)
	}

	// Recorded, with the honest marker.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reflection not written: %v", err)
	}
	if !strings.Contains(string(data), "prompt_hoisted: false") {
		t.Error("reflection is missing the prompt_hoisted: false marker")
	}
	if !strings.Contains(string(data), body) {
		t.Error("reflection body was not recorded")
	}

	// Never inside the memory/ tree that feeds the prompt.
	if strings.Contains(path, string(filepath.Separator)+"memory"+string(filepath.Separator)) {
		t.Errorf("reflection was written into memory/: %s", path)
	}

	// And never visible through the prompt-facing readers.
	if ctx := ms.GetMemoryContext(); strings.Contains(ctx, body) {
		t.Error("reflection leaked into the prompt memory context")
	}
	if recent := ms.GetRecentDailyNotes(7); strings.Contains(recent, body) {
		t.Error("reflection leaked into recent daily notes")
	}
	if today := ms.ReadToday(); strings.Contains(today, body) {
		t.Error("reflection leaked into today's note")
	}
}

func TestSynthesisCarriesTheSameLeash(t *testing.T) {
	ws := t.TempDir()
	ms := NewMemoryStore(ws)
	const line = "Standing guidance: keep replies short, never chase."

	path, err := ms.WriteSynthesis(line)
	if err != nil {
		t.Fatalf("write synthesis: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "prompt_hoisted: false") {
		t.Error("synthesis is missing the prompt_hoisted: false marker")
	}
	if ctx := ms.GetMemoryContext(); strings.Contains(ctx, line) {
		t.Error("synthesis leaked into the prompt memory context")
	}
}
