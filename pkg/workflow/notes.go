package workflow

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// Lesson is one 1-3 sentence reusable note from a learning run:
// not about the case, about the job (where time went, what to skip next).
type Lesson struct {
	At     time.Time `json:"at"`
	Job    string    `json:"job"`
	Text   string    `json:"text"`
	Digest string    `json:"digest"`
}

// Notes is a deduplicated JSONL lesson store beside the ledger.
type Notes struct {
	mu   sync.Mutex
	path string
	seen map[string]bool
}

// OpenNotes loads existing digests for dedup.
func OpenNotes(path string) (*Notes, error) {
	n := &Notes{path: path, seen: map[string]bool{}}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return n, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var l Lesson
		if json.Unmarshal(sc.Bytes(), &l) == nil && l.Digest != "" {
			n.seen[l.Digest] = true
		}
	}
	return n, sc.Err()
}

// Add appends a lesson unless identical text was seen. Enforces 1-3
// sentences by rejecting empty or >600 char notes.
func (n *Notes) Add(job, text string) (bool, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 600 {
		return false, nil
	}
	sum := sha256.Sum256([]byte(job + "\x00" + text))
	d := hex.EncodeToString(sum[:12])
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.seen[d] {
		return false, nil
	}
	l := Lesson{At: time.Now().UTC(), Job: job, Text: text, Digest: d}
	raw, _ := json.Marshal(l)
	f, err := os.OpenFile(n.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return false, err
	}
	n.seen[d] = true
	return true, nil
}
