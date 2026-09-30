package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
)

// pairInvitation is the daemon's /v1/pairing/invitations response.
type pairInvitation struct {
	PodID     string `json:"pod_id"`
	Transport string `json:"transport"`
	Host      string `json:"host"`
	Port      string `json:"port"`
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

// pairingURI is the exact URI the Ghost app parses from a scanned QR (the
// same string the web console encodes).
func pairingURI(inv pairInvitation) string {
	q := url.Values{}
	q.Set("v", "1")
	q.Set("pod", inv.PodID)
	q.Set("transport", inv.Transport)
	q.Set("host", inv.Host)
	q.Set("port", inv.Port)
	q.Set("token", inv.Token)
	return "ghost://pair?" + q.Encode()
}

// pairCmd connects a phone to this Pod from the terminal: `ghost pair`.
// It asks the running daemon (loopback is trusted) for a single-use
// invitation and prints it as a QR the phone can scan straight off the
// screen, with the URI and bare token as fallbacks. Nothing here is a
// secret the daemon wouldn't already show in the web console.
func pairCmd() {
	name := "Phone"
	if len(os.Args) > 2 {
		name = strings.Join(os.Args[2:], " ")
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Couldn't read Ghost's config: %v\n", err)
		os.Exit(1)
	}
	port := cfg.Gateway.Port
	if port == 0 {
		port = 8766
	}
	inv, err := requestInvitation(fmt.Sprintf("http://127.0.0.1:%d", port), name)
	if err != nil {
		fmt.Println("Ghost isn't running, so there is nothing to pair with yet.")
		fmt.Println("Start it with:  sudo systemctl start ghost   (or: ghost serve)")
		fmt.Printf("\n(%v)\n", err)
		os.Exit(1)
	}
	uri := pairingURI(*inv)
	fmt.Println()
	fmt.Println("  Scan this with the Ghost app")
	fmt.Println("  Open the app → Connect your Pod → Scan QR code")
	fmt.Println()
	qrterminal.GenerateWithConfig(uri, qrterminal.Config{
		Level:          qrterminal.L,
		Writer:         os.Stdout,
		HalfBlocks:     true,
		BlackChar:      qrterminal.BLACK_BLACK,
		WhiteChar:      qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE,
		WhiteBlackChar: qrterminal.WHITE_BLACK,
		QuietZone:      1,
	})
	fmt.Println()
	fmt.Printf("  Expires in %d minutes, works once.\n", inv.ExpiresIn/60)
	fmt.Println("  Can't scan? In the app choose \"Enter manually\" and use:")
	fmt.Printf("    address  %s:%s\n", inv.Host, inv.Port)
	fmt.Printf("    code     %s\n\n", inv.Token)
}

func requestInvitation(base, name string) (*pairInvitation, error) {
	body, _ := json.Marshal(map[string]string{"display_name": name})
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(base+"/v1/pairing/invitations", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon answered HTTP %d", resp.StatusCode)
	}
	var inv pairInvitation
	if err := json.Unmarshal(raw, &inv); err != nil || inv.Token == "" {
		return nil, fmt.Errorf("unreadable invitation")
	}
	return &inv, nil
}
