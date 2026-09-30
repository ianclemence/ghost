// Package push sends the phone a notification when Ghost needs it and the app
// isn't running to hear about it.
//
// The app's live connection only exists while the app is alive, so a reminder,
// a question or "I finished" sent while it was closed reached no one. Push
// closes that gap through Expo's push service. The notification carries fixed
// product copy only: never message content, tool arguments or memory. The
// conversation remains the source of truth once the owner opens it.
package push

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Category decides the wording and where a tap lands.
type Category string

const (
	Approval Category = "approval" // needs a decision
	Question Category = "question" // needs an answer
	Update   Category = "update"   // needs nothing
	// Reminder is something the owner asked to be told at this time. It is
	// never held for quiet hours: they set it, for this moment.
	Reminder Category = "reminder"
)

var copyFor = map[Category]struct{ body, anchor string }{
	Approval: {"Ghost needs your OK.", "approvals"},
	Question: {"Ghost has a question for you.", "thread"},
	Update:   {"Ghost has an update for you.", "thread"},
	Reminder: {"Ghost has a reminder for you.", "thread"},
}

// minGap coalesces bursts: several messages in a minute are one nudge, not a
// buzzing phone.
var minGap = map[Category]time.Duration{
	Approval: 20 * time.Second,
	Question: 20 * time.Second,
	Update:   60 * time.Second,
	Reminder: 15 * time.Second,
}

const defaultURL = "https://exp.host/--/api/v2/push/send"

var tokenRe = regexp.MustCompile(`^Expo(?:nent)?PushToken\[[A-Za-z0-9_\-]{8,}\]$`)

// Store keeps each paired device's push token.
type Store struct{ db *sql.DB }

// NewStore opens (creating if needed) the token table.
func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("push: no database")
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS push_tokens (
		device_id TEXT PRIMARY KEY, token TEXT NOT NULL, platform TEXT, updated_at TEXT)`); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Register records a device's token, replacing its previous one.
func (s *Store) Register(deviceID, token, platform string) error {
	token = strings.TrimSpace(token)
	if strings.TrimSpace(deviceID) == "" {
		return errors.New("a device is required")
	}
	if !tokenRe.MatchString(token) {
		return errors.New("that isn't a valid push token")
	}
	_, err := s.db.Exec(`INSERT INTO push_tokens (device_id, token, platform, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(device_id) DO UPDATE SET token=excluded.token, platform=excluded.platform, updated_at=excluded.updated_at`,
		deviceID, token, platform, time.Now().UTC().Format(time.RFC3339))
	return err
}

// Remove forgets a device's token (the phone turned notifications off or was
// unpaired).
func (s *Store) Remove(deviceID string) error {
	_, err := s.db.Exec(`DELETE FROM push_tokens WHERE device_id=?`, deviceID)
	return err
}

// Tokens lists every registered token.
func (s *Store) Tokens() []string {
	rows, err := s.db.Query(`SELECT token FROM push_tokens ORDER BY updated_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			out = append(out, t)
		}
	}
	return out
}

func (s *Store) removeToken(token string) {
	_, _ = s.db.Exec(`DELETE FROM push_tokens WHERE token=?`, token)
}

// Notifier sends notifications to every registered device.
type Notifier struct {
	store  *Store
	url    string
	client *http.Client
	now    func() time.Time

	mu   sync.Mutex
	last map[Category]time.Time
}

// NewNotifier builds a notifier. GHOST_EXPO_PUSH_URL overrides the service
// address (used by tests and self-hosted relays).
func NewNotifier(store *Store) *Notifier {
	url := strings.TrimSpace(os.Getenv("GHOST_EXPO_PUSH_URL"))
	if url == "" {
		url = defaultURL
	}
	return &Notifier{store: store, url: url, client: &http.Client{Timeout: 10 * time.Second}, now: time.Now, last: map[Category]time.Time{}}
}

// Notify sends one notification of the category to every device, unless one
// of the same category went out a moment ago. It returns how many devices the
// push service accepted.
func (n *Notifier) Notify(ctx context.Context, cat Category) (int, error) {
	tokens := n.store.Tokens()
	if len(tokens) == 0 {
		return 0, nil
	}
	n.mu.Lock()
	if t, ok := n.last[cat]; ok && n.now().Sub(t) < minGap[cat] {
		n.mu.Unlock()
		return 0, nil
	}
	n.last[cat] = n.now()
	n.mu.Unlock()

	c := copyFor[cat]
	msgs := make([]map[string]interface{}, 0, len(tokens))
	for _, t := range tokens {
		msgs = append(msgs, map[string]interface{}{
			"to": t, "title": "Ghost", "body": c.body, "sound": "default", "priority": "high",
			"ttl": 3600, "channelId": "default",
			"data": map[string]string{"category": string(cat), "anchor": c.anchor},
		})
	}
	raw, _ := json.Marshal(msgs)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("push service unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("push service answered %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Details struct {
				Error string `json:"error"`
			} `json:"details"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &out) != nil {
		return 0, errors.New("push service sent an unreadable answer")
	}
	sent := 0
	for i, tk := range out.Data {
		if i >= len(tokens) {
			break
		}
		switch {
		case tk.Status == "ok":
			sent++
		case tk.Details.Error == "DeviceNotRegistered":
			// The phone uninstalled the app or revoked notifications: stop trying.
			n.store.removeToken(tokens[i])
		}
	}
	return sent, nil
}
