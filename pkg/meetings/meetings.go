// Package meetings turns a conversation the owner recorded (a meeting, a
// lecture, a doctor's visit) into a transcript on their Pod: the phone sends
// the recording in pieces, the Pod cuts it into two-minute parts and
// transcribes each with the owner's own speech engine (local first), and
// keeps the transcript as a document in the workspace. What to make of it (a
// summary, decisions, action items) is then an ordinary turn of the
// conversation, so it lands where everything else Ghost says does.
package meetings

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Dir is the workspace folder meetings live in.
const Dir = "meetings"

// Limits.
const (
	MaxChunkBytes = 2 << 20   // one piece of a recording
	MaxAudioBytes = 120 << 20 // a whole recording (about 8 hours at 32 kbps)
	SegmentSecs   = 120
	MaxMinutes    = 240
)

// States a recording goes through.
const (
	StateReceiving    = "receiving"
	StateTranscribing = "transcribing"
	StateDone         = "done"
	StateFailed       = "failed"
)

// Meeting is one recording and what became of it.
type Meeting struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Mime     string `json:"mime"`
	Chunks   int    `json:"chunks"`
	Bytes    int64  `json:"bytes"`
	Seconds  int    `json:"seconds,omitempty"`
	Parts    int    `json:"parts,omitempty"`
	PartDone int    `json:"part_done,omitempty"`
	Words    int    `json:"words,omitempty"`
	// Transcript is the workspace path of the transcript, once done.
	Transcript string    `json:"transcript,omitempty"`
	ArtifactID string    `json:"artifact_id,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Transcriber turns a short piece of audio into words.
type Transcriber func(ctx context.Context, audio []byte, mime string) (string, error)

// Publisher puts the finished transcript in the conversation as something
// Ghost made (an artifact); it returns its id.
type Publisher func(title, summary, path string) (string, error)

// Store is the workspace's recordings.
type Store struct {
	mu        sync.Mutex
	workspace string
	running   map[string]bool
}

var (
	storesMu sync.Mutex
	stores   = map[string]*Store{}
)

// For is the meetings store of a workspace (one per workspace).
func For(workspace string) *Store {
	storesMu.Lock()
	defer storesMu.Unlock()
	if s, ok := stores[workspace]; ok {
		return s
	}
	s := &Store{workspace: workspace, running: map[string]bool{}}
	stores[workspace] = s
	return s
}

func (s *Store) dir(id string) string { return filepath.Join(s.workspace, Dir, id) }

var idRE = regexp.MustCompile(`^mtg_[0-9a-f]{12}$`)

func (s *Store) load(id string) (Meeting, error) {
	if !idRE.MatchString(id) {
		return Meeting{}, errors.New("no such recording")
	}
	b, err := os.ReadFile(filepath.Join(s.dir(id), "meeting.json"))
	if err != nil {
		return Meeting{}, errors.New("no such recording")
	}
	var m Meeting
	if err := json.Unmarshal(b, &m); err != nil {
		return Meeting{}, err
	}
	return m, nil
}

func (s *Store) save(m Meeting) error {
	m.UpdatedAt = time.Now().UTC()
	b, _ := json.MarshalIndent(m, "", " ")
	tmp := filepath.Join(s.dir(m.ID), "meeting.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir(m.ID), "meeting.json"))
}

var mimeExt = map[string]string{"audio/mp4": "m4a", "audio/m4a": "m4a", "audio/x-m4a": "m4a", "audio/aac": "aac", "audio/mpeg": "mp3", "audio/webm": "webm", "audio/ogg": "ogg", "audio/wav": "wav", "audio/3gpp": "3gp", "audio/amr": "amr"}

// Begin starts receiving a recording.
func (s *Store) Begin(title, mime string, now time.Time) (Meeting, error) {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if _, ok := mimeExt[mime]; !ok {
		return Meeting{}, fmt.Errorf("%q is not an audio format Ghost reads", mime)
	}
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		title = "Meeting, " + now.Format("2 Jan 15:04")
	}
	if len([]rune(title)) > 80 {
		title = string([]rune(title)[:80])
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	m := Meeting{ID: "mtg_" + hex.EncodeToString(b), Title: title, State: StateReceiving, Mime: mime, CreatedAt: now.UTC()}
	if err := os.MkdirAll(s.dir(m.ID), 0o700); err != nil {
		return Meeting{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return m, s.save(m)
}

// Chunk keeps piece seq (from 0) of a recording. Pieces may arrive again (a
// retry): the same piece is simply written again.
func (s *Store) Chunk(id string, seq int, data []byte) (Meeting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load(id)
	if err != nil {
		return Meeting{}, err
	}
	if m.State != StateReceiving {
		return m, errors.New("this recording is no longer being received")
	}
	if seq < 0 || seq > 100000 || len(data) == 0 || len(data) > MaxChunkBytes {
		return m, fmt.Errorf("a piece is 1 byte to %d MB", MaxChunkBytes>>20)
	}
	if m.Bytes+int64(len(data)) > MaxAudioBytes {
		return m, errors.New("the recording is too long")
	}
	path := filepath.Join(s.dir(id), fmt.Sprintf("part-%06d", seq))
	old, _ := os.Stat(path)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return m, err
	}
	if old != nil {
		m.Bytes -= old.Size()
	} else {
		m.Chunks++
	}
	m.Bytes += int64(len(data))
	return m, s.save(m)
}

// Finish closes a recording of n pieces and transcribes it in the background.
// It refuses if a piece is missing.
func (s *Store) Finish(id string, n int, tr Transcriber, pub Publisher) (Meeting, error) {
	s.mu.Lock()
	m, err := s.load(id)
	if err != nil {
		s.mu.Unlock()
		return Meeting{}, err
	}
	if m.State != StateReceiving {
		s.mu.Unlock()
		return m, nil
	}
	for i := 0; i < n; i++ {
		if _, err := os.Stat(filepath.Join(s.dir(id), fmt.Sprintf("part-%06d", i))); err != nil {
			s.mu.Unlock()
			return m, fmt.Errorf("piece %d of %d has not arrived", i+1, n)
		}
	}
	if n == 0 || n != m.Chunks {
		s.mu.Unlock()
		return m, fmt.Errorf("expected %d pieces, have %d", n, m.Chunks)
	}
	m.State = StateTranscribing
	if err := s.save(m); err != nil {
		s.mu.Unlock()
		return m, err
	}
	s.running[id] = true
	s.mu.Unlock()
	go s.process(m, n, tr, pub)
	return m, nil
}

// Get returns a recording.
func (s *Store) Get(id string) (Meeting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load(id)
}

// List returns recordings, newest first.
func (s *Store) List() ([]Meeting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, _ := os.ReadDir(filepath.Join(s.workspace, Dir))
	var out []Meeting
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if m, err := s.load(e.Name()); err == nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Resume restarts transcription of recordings a restart interrupted.
func (s *Store) Resume(tr Transcriber, pub Publisher) int {
	list, _ := s.List()
	n := 0
	for _, m := range list {
		s.mu.Lock()
		busy := s.running[m.ID]
		s.mu.Unlock()
		if m.State == StateTranscribing && !busy {
			s.mu.Lock()
			s.running[m.ID] = true
			s.mu.Unlock()
			go s.process(m, m.Chunks, tr, pub)
			n++
		}
	}
	return n
}

func (s *Store) update(id string, fn func(m *Meeting)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, err := s.load(id); err == nil {
		fn(&m)
		_ = s.save(m)
	}
}

func (s *Store) fail(id string, err error) {
	s.update(id, func(m *Meeting) {
		m.State = StateFailed
		m.Error = err.Error()
	})
}

// process joins the pieces, cuts the audio into parts, transcribes each and
// writes the transcript. Every failure is kept on the recording, honestly.
func (s *Store) process(m Meeting, n int, tr Transcriber, pub Publisher) {
	defer func() {
		s.mu.Lock()
		delete(s.running, m.ID)
		s.mu.Unlock()
	}()
	dir := s.dir(m.ID)
	full := filepath.Join(dir, "audio."+mimeExt[m.Mime])
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("part-%06d", i)))
		if err != nil {
			s.fail(m.ID, fmt.Errorf("piece %d is missing", i+1))
			return
		}
		buf.Write(b)
	}
	if err := os.WriteFile(full, buf.Bytes(), 0o600); err != nil {
		s.fail(m.ID, err)
		return
	}
	for i := 0; i < n; i++ {
		_ = os.Remove(filepath.Join(dir, fmt.Sprintf("part-%06d", i)))
	}
	secs, _ := duration(full)
	if secs > MaxMinutes*60 {
		s.fail(m.ID, fmt.Errorf("the recording is longer than %d hours", MaxMinutes/60))
		return
	}
	segDir := filepath.Join(dir, "parts")
	_ = os.RemoveAll(segDir)
	_ = os.MkdirAll(segDir, 0o700)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", full,
		"-f", "segment", "-segment_time", strconv.Itoa(SegmentSecs), "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le",
		filepath.Join(segDir, "seg%04d.wav"))
	var errb bytes.Buffer
	cmd.Stderr = &errb
	err := cmd.Run()
	cancel()
	if err != nil {
		s.fail(m.ID, fmt.Errorf("couldn't read the recording: %s", firstLine(errb.String(), err)))
		return
	}
	segs, _ := filepath.Glob(filepath.Join(segDir, "seg*.wav"))
	sort.Strings(segs)
	if len(segs) == 0 {
		s.fail(m.ID, errors.New("the recording was empty"))
		return
	}
	s.update(m.ID, func(x *Meeting) { x.Seconds, x.Parts, x.PartDone = secs, len(segs), 0 })
	var sb strings.Builder
	words := 0
	for i, seg := range segs {
		audio, err := os.ReadFile(seg)
		if err != nil {
			s.fail(m.ID, err)
			return
		}
		var text string
		var terr error
		for attempt := 0; attempt < 2; attempt++ {
			tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Minute)
			text, terr = tr(tctx, audio, "audio/wav")
			tcancel()
			if terr == nil {
				break
			}
		}
		if terr != nil {
			s.fail(m.ID, fmt.Errorf("couldn't transcribe part %d of %d: %v", i+1, len(segs), terr))
			return
		}
		text = strings.TrimSpace(text)
		if text != "" {
			at := i * SegmentSecs
			fmt.Fprintf(&sb, "**[%d:%02d:%02d]** %s\n\n", at/3600, (at%3600)/60, at%60, text)
			words += len(strings.Fields(text))
		}
		s.update(m.ID, func(x *Meeting) { x.PartDone = i + 1 })
	}
	_ = os.RemoveAll(segDir)
	if words == 0 {
		s.fail(m.ID, errors.New("no speech could be heard in the recording"))
		return
	}
	slug := slugOf(m.Title)
	name := fmt.Sprintf("%s-%s.md", m.CreatedAt.Local().Format("2006-01-02"), slug)
	rel := Dir + "/" + name
	head := fmt.Sprintf("# %s\n\nRecorded %s · %d min · %d words. Transcribed on your Pod.\n\n", m.Title, m.CreatedAt.Local().Format("Monday 2 January 2006, 15:04"), (secs+59)/60, words)
	if err := os.WriteFile(filepath.Join(s.workspace, rel), []byte(head+sb.String()), 0o600); err != nil {
		s.fail(m.ID, err)
		return
	}
	artifact := ""
	if pub != nil {
		if id, err := pub(m.Title+" (transcript)", fmt.Sprintf("%d min · %d words", (secs+59)/60, words), rel); err == nil {
			artifact = id
		}
	}
	s.update(m.ID, func(x *Meeting) {
		x.State, x.Transcript, x.Words, x.ArtifactID, x.Error = StateDone, rel, words, artifact, ""
	})
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugOf(s string) string {
	out := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if out == "" {
		return "meeting"
	}
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	return out
}

// duration reads a recording's length in seconds.
func duration(path string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, err
	}
	return int(f + 0.5), nil
}

func firstLine(s string, err error) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 160 {
				l = l[:160]
			}
			return l
		}
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}

// Available reports what this Pod lacks to transcribe recordings, or nil.
func Available() error {
	for _, p := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(p); err != nil {
			return fmt.Errorf("%s is not installed on this Pod", p)
		}
	}
	return nil
}
