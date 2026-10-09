package meetings

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitFor(t *testing.T, s *Store, id string, states ...string) Meeting {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		m, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range states {
			if m.State == st {
				return m
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("recording never reached %v", states)
	return Meeting{}
}

func TestARecordingBecomesATranscript(t *testing.T) {
	if Available() != nil {
		t.Skip("ffmpeg is not installed")
	}
	ws := t.TempDir()
	src := filepath.Join(t.TempDir(), "rec.m4a")
	// Four minutes and ten seconds of tone: three two-minute parts.
	if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=250", "-ac", "1", "-ar", "16000", "-c:a", "aac", "-b:a", "32k", src).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	audio, _ := os.ReadFile(src)
	s := For(ws)
	m, err := s.Begin("Weekly standup", "audio/mp4", time.Date(2026, 10, 9, 9, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	half := len(audio) / 2
	if _, err := s.Chunk(m.ID, 0, audio[:half]); err != nil {
		t.Fatal(err)
	}
	// A piece missing: finishing is refused, honestly.
	if _, err := s.Finish(m.ID, 2, nil, nil); err == nil {
		t.Fatal("finished with a piece missing")
	}
	if _, err := s.Chunk(m.ID, 1, audio[half:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chunk(m.ID, 1, audio[half:]); err != nil { // a retried piece
		t.Fatal(err)
	}
	calls := 0
	tr := func(ctx context.Context, b []byte, mime string) (string, error) {
		calls++
		if mime != "audio/wav" || len(b) < 1000 {
			return "", errors.New("not a wav part")
		}
		return "we agreed to ship on Friday", nil
	}
	published := ""
	pub := func(title, summary, path string) (string, error) { published = title + "|" + path; return "art_1", nil }
	if _, err := s.Finish(m.ID, 2, tr, pub); err != nil {
		t.Fatal(err)
	}
	done := waitFor(t, s, m.ID, StateDone, StateFailed)
	if done.State != StateDone || done.Parts != 3 || done.PartDone != 3 || calls != 3 || done.ArtifactID != "art_1" {
		t.Fatalf("done: %+v calls=%d", done, calls)
	}
	if done.Seconds < 249 || done.Seconds > 251 || done.Words != 18 {
		t.Fatalf("length/words: %+v", done)
	}
	text, err := os.ReadFile(filepath.Join(ws, done.Transcript))
	if err != nil || !strings.Contains(string(text), "# Weekly standup") || !strings.Contains(string(text), "**[0:02:00]** we agreed") {
		t.Fatalf("transcript: %v %s", err, text)
	}
	if !strings.HasPrefix(published, "Weekly standup (transcript)|meetings/2026-10-09-weekly-standup.md") {
		t.Fatalf("published: %s", published)
	}
	if left, _ := filepath.Glob(filepath.Join(ws, Dir, m.ID, "part-*")); len(left) != 0 {
		t.Fatalf("pieces left behind: %v", left)
	}
}

func TestSilenceIsSaidHonestly(t *testing.T) {
	if Available() != nil {
		t.Skip("ffmpeg is not installed")
	}
	ws := t.TempDir()
	src := filepath.Join(t.TempDir(), "rec.m4a")
	if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono", "-t", "5", "-c:a", "aac", src).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	audio, _ := os.ReadFile(src)
	s := For(ws)
	m, _ := s.Begin("", "audio/mp4", time.Now())
	if !strings.HasPrefix(m.Title, "Meeting, ") {
		t.Fatalf("untitled: %q", m.Title)
	}
	_, _ = s.Chunk(m.ID, 0, audio)
	_, _ = s.Finish(m.ID, 1, func(context.Context, []byte, string) (string, error) { return "  ", nil }, nil)
	got := waitFor(t, s, m.ID, StateDone, StateFailed)
	if got.State != StateFailed || !strings.Contains(got.Error, "no speech") {
		t.Fatalf("silence: %+v", got)
	}
	if _, err := s.Begin("x", "video/mp4", time.Now()); err == nil {
		t.Fatal("a video was taken as audio")
	}
	if _, err := s.Get("../../etc"); err == nil {
		t.Fatal("an id outside the folder was read")
	}
}
