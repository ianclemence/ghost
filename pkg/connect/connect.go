// Package connect is the Pod's side of Ghost Connect, the hosted relay.
//
// A Pod links itself to the Ghost site the way a TV links to a streaming
// account: it asks the site for a short code, the owner types the code into the
// site while signed in, and the Pod is handed an entitlement, a signed pass the
// relay checks. The site never learns anything about the Pod's contents and the
// relay never learns who the owner is. See docs/CONNECT.md.
package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RenewBefore is how long before expiry the Pod asks for a fresh pass. Passes
// last three days and are renewed daily, so the site can be unreachable for two
// days without anyone noticing.
const RenewBefore = 48 * time.Hour

var (
	// ErrPending means the owner has not entered the code yet.
	ErrPending = errors.New("connect: waiting for the code to be entered")
	// ErrExpired means the code ran out before it was used.
	ErrExpired = errors.New("connect: the code expired, start again")
	// ErrNoSubscription means the account has no active Ghost Connect plan.
	ErrNoSubscription = errors.New("connect: no active Ghost Connect subscription")
	// ErrRejected means the site did not accept the Pod's pass at all.
	ErrRejected = errors.New("connect: the site did not accept this Pod's pass")
)

// State is what a linked Pod keeps. It is not secret in the way a password is
// (the pass is bound to this Pod and signed) but it is kept 0600 all the same.
type State struct {
	Site      string `json:"site"`
	Relay     string `json:"relay"` // where the relay is, as the site last said
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	LinkedAt  string `json:"linked_at,omitempty"`
}

// Linked reports whether there is a pass at all.
func (s *State) Linked() bool { return s != nil && s.Token != "" }

// Expiry is when the current pass ends.
func (s *State) Expiry() time.Time { return time.Unix(s.ExpiresAt, 0) }

// NeedsRenewal reports whether the pass is due to be replaced.
func (s *State) NeedsRenewal(now time.Time) bool {
	return s.Linked() && s.Expiry().Sub(now) < RenewBefore
}

func statePath(workspace string) string {
	return filepath.Join(workspace, "state", "connect.json")
}

// LoadState reads the link state; a Pod that was never linked has none.
func LoadState(workspace string) (*State, error) {
	b, err := os.ReadFile(statePath(workspace))
	if os.IsNotExist(err) {
		return &State{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveState writes the link state atomically with owner-only permissions.
func SaveState(workspace string, s *State) error {
	path := statePath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".connect-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ClearState forgets the link. The Pod keeps working at home and over any other
// route; only the hosted relay is lost.
func ClearState(workspace string) error {
	err := os.Remove(statePath(workspace))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Client talks to the Ghost site.
type Client struct {
	Site string // e.g. https://ghost.example, no trailing slash
	HTTP *http.Client
}

// NewClient normalises the site address.
func NewClient(site string) *Client {
	return &Client{Site: strings.TrimRight(site, "/"), HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// StartResult is what the site gives a Pod that wants to link.
type StartResult struct {
	UserCode  string `json:"user_code"`  // what the owner types, e.g. GHOST-4F7K
	VerifyURL string `json:"verify_url"` // where, with the code filled in
	PollToken string `json:"poll_token"` // secret the Pod polls with
	ExpiresIn int    `json:"expires_in"` // seconds the code lasts
	Interval  int    `json:"interval"`   // seconds between polls
}

// Pass is a granted or renewed entitlement.
type Pass struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Relay     string `json:"relay"`
}

// Start asks the site for a link code for this Pod.
func (c *Client) Start(ctx context.Context, podID, name string) (*StartResult, error) {
	var out StartResult
	status, err := c.post(ctx, "/api/connect/start", "", map[string]string{"pod_id": podID, "name": name}, &out)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || out.UserCode == "" || out.PollToken == "" {
		return nil, fmt.Errorf("connect: the site answered %d", status)
	}
	if out.Interval <= 0 {
		out.Interval = 3
	}
	return &out, nil
}

// Poll checks whether the owner has entered the code. It returns ErrPending
// until they have.
func (c *Client) Poll(ctx context.Context, pollToken string) (*Pass, error) {
	var out struct {
		Status string `json:"status"`
		Pass
	}
	status, err := c.post(ctx, "/api/connect/poll", "", map[string]string{"poll_token": pollToken}, &out)
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusGone:
		return nil, ErrExpired
	case status == http.StatusAccepted || out.Status == "pending":
		return nil, ErrPending
	case status == http.StatusPaymentRequired || out.Status == "needs_subscription":
		return nil, ErrNoSubscription
	case status == http.StatusOK && out.Token != "":
		return &out.Pass, nil
	}
	return nil, fmt.Errorf("connect: the site answered %d", status)
}

// Renew swaps the current pass for a fresh one while the subscription is
// active. An expired pass is still accepted by the site for a while, so a Pod
// that was switched off for a few days can recover on its own.
func (c *Client) Renew(ctx context.Context, token string) (*Pass, error) {
	var out Pass
	status, err := c.post(ctx, "/api/connect/renew", token, struct{}{}, &out)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		if out.Token == "" {
			return nil, fmt.Errorf("connect: the site sent no pass")
		}
		return &out, nil
	case http.StatusPaymentRequired:
		return nil, ErrNoSubscription
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrRejected
	}
	return nil, fmt.Errorf("connect: the site answered %d", status)
}

func (c *Client) post(ctx context.Context, path, bearer string, in, out any) (int, error) {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Site+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("connect: couldn't reach %s: %w", hostOf(c.Site), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if out != nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, out)
	}
	return resp.StatusCode, nil
}

func hostOf(site string) string {
	if u, err := url.Parse(site); err == nil && u.Host != "" {
		return u.Host
	}
	return site
}

// Enroll registers the Pod with the relay, presenting the pass instead of an
// operator's admin token. The relay checks that the pass is for this very Pod.
func Enroll(ctx context.Context, relayHTTP, podID, deviceSecret, name, token string) error {
	body, _ := json.Marshal(map[string]string{
		"device_id": podID, "device_secret": deviceSecret, "entitlement": token, "name": name,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(relayHTTP, "/")+"/v1/enroll", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("connect: couldn't reach the relay: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("connect: the relay refused to enroll this Pod (%d %s)", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// HTTPBase turns a relay address in any of its spellings into the http(s)
// address the enroll call needs.
func HTTPBase(relay string) string {
	r := strings.TrimRight(relay, "/")
	switch {
	case strings.HasPrefix(r, "wss://"):
		return "https://" + strings.TrimPrefix(r, "wss://")
	case strings.HasPrefix(r, "ws://"):
		return "http://" + strings.TrimPrefix(r, "ws://")
	}
	return r
}
