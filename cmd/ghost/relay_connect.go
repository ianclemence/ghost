package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/connect"
	"github.com/ianclemence/ghost/pkg/ghoststate"
	"github.com/ianclemence/ghost/pkg/relayclient"
	"github.com/ianclemence/ghost/pkg/relaycrypto"
	"github.com/mdp/qrterminal/v3"
)

// defaultConnectSite is where Ghost Connect lives. It is empty until the site
// has an address; GHOST_CONNECT_SITE or --site overrides it for testing.
const defaultConnectSite = ""

// ensureRelayIdentity returns the Pod's end-to-end key, making and saving one
// the first time. The private half is sealed with the Pod's other secrets.
func ensureRelayIdentity(cfg *config.Config) (*relaycrypto.Identity, error) {
	if cfg.Relay.IdentityKey != "" {
		raw, err := hex.DecodeString(cfg.Relay.IdentityKey)
		if err == nil {
			if id, err := relaycrypto.LoadIdentity(raw); err == nil {
				return id, nil
			}
		}
		return nil, fmt.Errorf("the relay identity key in the secrets file is damaged")
	}
	id, err := relaycrypto.NewIdentity()
	if err != nil {
		return nil, err
	}
	cfg.Relay.IdentityKey = hex.EncodeToString(id.Private())
	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		return nil, fmt.Errorf("couldn't save the relay identity key: %w", err)
	}
	return id, nil
}

// newRelayClient builds the relay client from config, with the Pod's key and,
// for Ghost Connect, the current pass.
func newRelayClient(cfg *config.Config, podID string) (*relayclient.Client, *connect.State, error) {
	id, err := ensureRelayIdentity(cfg)
	if err != nil {
		return nil, nil, err
	}
	st, err := connect.LoadState(cfg.WorkspacePath())
	if err != nil {
		return nil, nil, err
	}
	gatewayURL := cfg.Relay.GatewayURL
	if gatewayURL == "" {
		gatewayURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.Gateway.Port)
	}
	return relayclient.NewClient(relayclient.ClientConfig{
		DeviceID:      podID,
		DeviceSecret:  cfg.Relay.DeviceSecret,
		RelayServer:   cfg.Relay.Server,
		GatewayURL:    gatewayURL,
		ReconnectMin:  cfg.Relay.ReconnectMin,
		ReconnectMax:  cfg.Relay.ReconnectMax,
		Identity:      id,
		RequireSealed: cfg.Relay.RequireSealed,
		Entitlement:   st.Token,
	}), st, nil
}

// runManagedRelay keeps a Ghost Connect Pod on the relay: it holds the tunnel
// open and renews the pass before it runs out. It is what makes Ghost Connect
// need no setup after linking.
func runManagedRelay(ctx context.Context, cfg *config.Config) {
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
	go renewLoop(ctx, cfg, client, st)
	log.Printf("relay: Ghost Connect is on (%s)", cfg.Relay.Server)
	if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("relay: stopped: %v", err)
	}
}

// renewLoop swaps the pass for a fresh one while the subscription lasts. If the
// subscription has ended it stops asking and lets the pass run out; the Pod
// keeps working at home and over any other route.
func renewLoop(ctx context.Context, cfg *config.Config, client *relayclient.Client, st *connect.State) {
	site := connect.NewClient(st.Site)
	check := func() {
		if !st.NeedsRenewal(time.Now()) {
			return
		}
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		pass, err := site.Renew(rctx, st.Token)
		switch {
		case err == nil:
			st.Token, st.ExpiresAt = pass.Token, pass.ExpiresAt
			if pass.Relay != "" {
				st.Relay = pass.Relay
			}
			if err := connect.SaveState(cfg.WorkspacePath(), st); err != nil {
				log.Printf("relay: couldn't save the renewed pass: %v", err)
			}
			client.UpdateEntitlement(pass.Token)
			log.Printf("relay: Ghost Connect renewed until %s", st.Expiry().Format("2 Jan 15:04"))
		case errors.Is(err, connect.ErrNoSubscription):
			log.Printf("relay: no active Ghost Connect subscription; the relay will stop at %s. Your Ghost still works at home.", st.Expiry().Format("2 Jan 15:04"))
		default:
			log.Printf("relay: couldn't renew Ghost Connect yet (%v), will try again", err)
		}
	}
	check()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}

// relayLinkCmd links this Pod to Ghost Connect: `ghost relay link`.
func relayLinkCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Couldn't read Ghost's config: %v\n", err)
		os.Exit(1)
	}
	site := firstNonEmpty(flagValue("--site"), os.Getenv("GHOST_CONNECT_SITE"), cfg.Relay.Site, defaultConnectSite)
	if site == "" {
		fmt.Println("Ghost Connect isn't open yet, so there is nothing to link to.")
		fmt.Println("Away from home today, use Tailscale or your own relay (docs/CONNECT.md).")
		os.Exit(1)
	}
	ghostID, err := ghoststate.EnsureIdentity(cfg.WorkspacePath())
	if err != nil {
		fmt.Printf("Couldn't load this Pod's identity: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	hostname, _ := os.Hostname()
	client := connect.NewClient(site)
	start, err := client.Start(ctx, ghostID.GhostID, hostname)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("  Link this Pod to Ghost Connect")
	fmt.Println()
	fmt.Printf("  1. Open  %s\n", start.VerifyURL)
	fmt.Println("     (or scan this)")
	fmt.Println()
	qrterminal.GenerateWithConfig(start.VerifyURL, qrterminal.Config{
		Level: qrterminal.L, Writer: os.Stdout, HalfBlocks: true,
		BlackChar: qrterminal.BLACK_BLACK, WhiteChar: qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE, WhiteBlackChar: qrterminal.WHITE_BLACK, QuietZone: 1,
	})
	fmt.Println()
	fmt.Printf("  2. Check the code reads   %s\n", start.UserCode)
	fmt.Printf("\n  Waiting… the code works for %d minutes. Ctrl-C to cancel.\n", start.ExpiresIn/60)

	var pass *connect.Pass
	tick := time.NewTicker(time.Duration(start.Interval) * time.Second)
	defer tick.Stop()
	for pass == nil {
		select {
		case <-ctx.Done():
			fmt.Println("\n  Cancelled. Nothing was changed.")
			os.Exit(1)
		case <-tick.C:
		}
		p, err := client.Poll(ctx, start.PollToken)
		switch {
		case err == nil:
			pass = p
		case errors.Is(err, connect.ErrPending):
		case errors.Is(err, connect.ErrNoSubscription):
			fmt.Println("\n  That account has no active Ghost Connect plan yet. Choose one on the site, then run this again.")
			os.Exit(1)
		case errors.Is(err, connect.ErrExpired):
			fmt.Println("\n  The code expired. Run `ghost relay link` for a new one.")
			os.Exit(1)
		default:
			fmt.Printf("\n  %v\n", err)
			os.Exit(1)
		}
	}

	if cfg.Relay.DeviceSecret == "" {
		secret, err := relayclient.GenerateToken()
		if err != nil {
			fmt.Printf("Couldn't make a device secret: %v\n", err)
			os.Exit(1)
		}
		cfg.Relay.DeviceSecret = secret
	}
	if _, err := ensureRelayIdentity(cfg); err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
	if err := connect.Enroll(ctx, connect.HTTPBase(pass.Relay), ghostID.GhostID, cfg.Relay.DeviceSecret, hostname, pass.Token); err != nil {
		fmt.Printf("\n  %v\n", err)
		os.Exit(1)
	}
	st := &connect.State{Site: site, Relay: pass.Relay, Token: pass.Token, ExpiresAt: pass.ExpiresAt, LinkedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := connect.SaveState(cfg.WorkspacePath(), st); err != nil {
		fmt.Printf("Couldn't save the link: %v\n", err)
		os.Exit(1)
	}
	cfg.Relay.Enabled, cfg.Relay.Managed, cfg.Relay.RequireSealed = true, true, true
	cfg.Relay.Server, cfg.Relay.Site = pass.Relay, site
	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Couldn't save the config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n  Linked. Your Pod is on Ghost Connect.")
	fmt.Println("  Restart Ghost so it connects:   sudo systemctl restart ghost")
	fmt.Println("  Then pair your phone for away-from-home use:   ghost relay pair")
}

// relayStatusCmd says whether Ghost Connect is set up and for how long.
func relayStatusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Couldn't read Ghost's config: %v\n", err)
		os.Exit(1)
	}
	st, err := connect.LoadState(cfg.WorkspacePath())
	if err != nil {
		fmt.Printf("Couldn't read the link: %v\n", err)
		os.Exit(1)
	}
	if !st.Linked() {
		fmt.Println("Ghost Connect: not linked.  (ghost relay link)")
		if cfg.Relay.Enabled {
			fmt.Printf("Relay: your own, %s\n", cfg.Relay.Server)
		}
		return
	}
	left := time.Until(st.Expiry()).Round(time.Hour)
	fmt.Printf("Ghost Connect: linked to %s\n", strings.TrimPrefix(strings.TrimPrefix(st.Site, "https://"), "http://"))
	fmt.Printf("Relay:         %s\n", st.Relay)
	if left > 0 {
		fmt.Printf("Pass:          valid for %s, renewed automatically while your plan is active\n", left)
	} else {
		fmt.Println("Pass:          ended. Your plan may have lapsed; Ghost still works at home.")
	}
}

// relayUnlinkCmd forgets Ghost Connect on this Pod.
func relayUnlinkCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Couldn't read Ghost's config: %v\n", err)
		os.Exit(1)
	}
	if err := connect.ClearState(cfg.WorkspacePath()); err != nil {
		fmt.Printf("Couldn't remove the link: %v\n", err)
		os.Exit(1)
	}
	cfg.Relay.Enabled, cfg.Relay.Managed, cfg.Relay.RequireSealed = false, false, false
	cfg.Relay.Server, cfg.Relay.Site = "", ""
	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Couldn't save the config: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Unlinked. Ghost works at home and over any other route you set up.")
	fmt.Println("Restart Ghost to close the relay connection:   sudo systemctl restart ghost")
}

// podKeyParam is the Pod's public key as it travels in a pairing link.
func podKeyParam(id *relaycrypto.Identity) string {
	return base64.RawURLEncoding.EncodeToString(id.Public())
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// flagValue reads --name value or --name=value from the arguments after the
// subcommand, without a flag set, because the relay subcommands share os.Args.
func flagValue(name string) string {
	for i, a := range os.Args {
		if a == name && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}
