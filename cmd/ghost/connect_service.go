package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/connect"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/relayclient"
)

// connectService is the daemon's side of Ghost Connect. It holds the relay
// connection, runs the link flow when the owner asks for it in the console,
// and mints the pairing a phone uses to reach the Pod from away. The command
// line does the same things through the same functions.
type connectService struct {
	mu     sync.Mutex
	root   context.Context
	client *relayclient.Client
	stop   context.CancelFunc
	link   *linkAttempt
}

var connectSvc = &connectService{}

// linkAttempt is a link in progress, shown to the owner while they enter the code.
type linkAttempt struct {
	UserCode  string    `json:"user_code"`
	VerifyURL string    `json:"verify_url"`
	ExpiresAt time.Time `json:"expires_at"`
	State     string    `json:"state"` // waiting | linked | expired | needs_plan | error
	Message   string    `json:"message,omitempty"`
}

// begin sets the context the relay and any link run under: the daemon's.
func (s *connectService) begin(ctx context.Context) {
	s.mu.Lock()
	s.root = ctx
	s.mu.Unlock()
}

// startRelay (re)starts the relay connection from the saved config. It is safe
// to call again after linking: the old connection is replaced.
func (s *connectService) startRelay(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		s.stop()
		s.stop, s.client = nil, nil
	}
	if !(cfg.Relay.Managed && cfg.Relay.Enabled && cfg.Relay.Server != "" && cfg.Relay.DeviceSecret != "") {
		return
	}
	ghostID, err := ghoststate.EnsureIdentity(cfg.WorkspacePath())
	if err != nil {
		log.Printf("relay: no identity: %v", err)
		return
	}
	client, st, err := newRelayClient(cfg, ghostID.GhostID)
	if err != nil {
		log.Printf("relay: %v", err)
		return
	}
	parent := s.root
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	s.client, s.stop = client, cancel
	go renewLoop(ctx, cfg, client, st)
	go func() {
		log.Printf("relay: Ghost Connect is on (%s)", cfg.Relay.Server)
		if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("relay: stopped: %v", err)
		}
	}()
}

func (s *connectService) stopRelay() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		s.stop()
		s.stop, s.client = nil, nil
	}
}

// ConnectStatus is what the console shows.
type ConnectStatus struct {
	Available bool         `json:"available"` // a Ghost Connect site is configured
	Site      string       `json:"site,omitempty"`
	Linked    bool         `json:"linked"`
	Relay     string       `json:"relay,omitempty"`
	ExpiresAt int64        `json:"expires_at,omitempty"`
	Tunnel    string       `json:"tunnel,omitempty"` // connected | offline | needs-payment
	Message   string       `json:"message,omitempty"`
	Pending   *linkAttempt `json:"pending,omitempty"`
}

func connectSite(cfg *config.Config) string {
	return firstNonEmpty(os.Getenv("GHOST_CONNECT_SITE"), cfg.Relay.Site, defaultConnectSite)
}

func (s *connectService) status(cfg *config.Config) ConnectStatus {
	st := ConnectStatus{Site: connectSite(cfg)}
	st.Available = st.Site != ""
	if state, err := connect.LoadState(cfg.WorkspacePath()); err == nil && state.Linked() {
		st.Linked, st.Relay, st.ExpiresAt = true, state.Relay, state.ExpiresAt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		cs := s.client.Status()
		st.Tunnel, st.Message = string(cs.State), cs.Message
	}
	if s.link != nil {
		cp := *s.link
		st.Pending = &cp
	}
	return st
}

// startLink asks the site for a code and waits, in the background, for the owner
// to enter it. A second call while one is waiting returns the same code.
func (s *connectService) startLink(cfg *config.Config) (*linkAttempt, error) {
	site := connectSite(cfg)
	if site == "" {
		return nil, errors.New("Ghost Connect isn't open yet")
	}
	s.mu.Lock()
	if s.link != nil && s.link.State == "waiting" && time.Now().Before(s.link.ExpiresAt) {
		cp := *s.link
		s.mu.Unlock()
		return &cp, nil
	}
	root := s.root
	s.mu.Unlock()
	if root == nil {
		root = context.Background()
	}

	ghostID, err := ghoststate.EnsureIdentity(cfg.WorkspacePath())
	if err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	client := connect.NewClient(site)
	start, err := client.Start(root, ghostID.GhostID, hostname)
	if err != nil {
		return nil, err
	}
	at := &linkAttempt{
		UserCode: start.UserCode, VerifyURL: start.VerifyURL, State: "waiting",
		ExpiresAt: time.Now().Add(time.Duration(start.ExpiresIn) * time.Second),
	}
	s.mu.Lock()
	s.link = at
	s.mu.Unlock()

	go s.waitForLink(root, client, start, at, cfg, site, ghostID.GhostID, hostname)
	cp := *at
	return &cp, nil
}

func (s *connectService) waitForLink(ctx context.Context, client *connect.Client, start *connect.StartResult, at *linkAttempt, cfg *config.Config, site, podID, hostname string) {
	set := func(state, msg string) {
		s.mu.Lock()
		at.State, at.Message = state, msg
		s.mu.Unlock()
	}
	tick := time.NewTicker(time.Duration(start.Interval) * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		pass, err := client.Poll(ctx, start.PollToken)
		switch {
		case err == nil:
			// Work on a fresh copy of the config: the one handed in may be stale.
			fresh, lerr := loadConfig()
			if lerr != nil {
				set("error", lerr.Error())
				return
			}
			if err := completeLink(ctx, fresh, site, podID, hostname, pass); err != nil {
				set("error", err.Error())
				return
			}
			s.startRelay(fresh)
			set("linked", "")
			return
		case errors.Is(err, connect.ErrPending):
		case errors.Is(err, connect.ErrNoSubscription):
			set("needs_plan", "That account has no active Ghost Connect plan yet.")
			return
		case errors.Is(err, connect.ErrExpired):
			set("expired", "The code expired.")
			return
		default:
			// A flaky connection shouldn't end the attempt; keep waiting until the code expires.
			if time.Now().After(at.ExpiresAt) {
				set("expired", "The code expired.")
				return
			}
		}
	}
}

// unlink forgets Ghost Connect on this Pod. It keeps working at home.
func (s *connectService) unlink(cfg *config.Config) error {
	s.stopRelay()
	if err := connect.ClearState(cfg.WorkspacePath()); err != nil {
		return err
	}
	cfg.Relay.Enabled, cfg.Relay.Managed, cfg.Relay.RequireSealed = false, false, false
	cfg.Relay.Server, cfg.Relay.Site = "", ""
	s.mu.Lock()
	s.link = nil
	s.mu.Unlock()
	return config.SaveConfig(getConfigPath(), cfg)
}

// relayPairingURI makes the link a phone scans to reach this Pod from away. It
// carries what the phone needs and nothing it could use alone: the relay's
// address, the Pod's public key (which the phone pins), a relay credential for
// this phone, and a one-time pairing token that gets the phone its device
// credential through the sealed relay.
func relayPairingURI(cfg *config.Config, podID, pairingToken, clientToken string, podKey string) string {
	q := url.Values{}
	q.Set("v", "1")
	q.Set("pod", podID)
	q.Set("transport", "relay")
	q.Set("relay", connect.HTTPBase(cfg.Relay.Server))
	q.Set("ghost", podID)
	q.Set("token", pairingToken)
	q.Set("client", clientToken)
	q.Set("pk", podKey)
	return "ghost://pair?" + q.Encode()
}

// pairRemote readies a phone to connect through the relay and returns the link
// to scan, valid for a few minutes and once.
func (s *connectService) pairRemote(cfg *config.Config, name string) (string, int, error) {
	if cfg.Relay.Server == "" || !cfg.Relay.Enabled {
		return "", 0, errors.New("the relay isn't set up on this Pod")
	}
	id, err := ensureRelayIdentity(cfg)
	if err != nil {
		return "", 0, err
	}
	ghostID, err := ghoststate.EnsureIdentity(cfg.WorkspacePath())
	if err != nil {
		return "", 0, err
	}
	port := cfg.Gateway.Port
	if port == 0 {
		port = defaultInternalAPIPort
	}
	inv, err := requestRelayInvitation(fmt.Sprintf("http://127.0.0.1:%d", port), name)
	if err != nil {
		return "", 0, fmt.Errorf("Ghost isn't running: %w", err)
	}
	clientToken, err := relayclient.AddClientScoped(ghostID.GhostID, name, "full")
	if err != nil {
		return "", 0, err
	}
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c != nil {
		c.RefreshClients()
	}
	return relayPairingURI(cfg, ghostID.GhostID, inv.Token, clientToken, podKeyParam(id)), inv.ExpiresIn, nil
}
