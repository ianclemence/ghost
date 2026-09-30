package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
)

// handlePasswordReset sets a new console password for someone who forgot the
// old one and holds a one-time code from a paired phone (see
// appliance.IssueResetCode). It is public by necessity, so it is throttled
// like a login, checks the new password BEFORE using the code (a weak password
// must not burn a good code), and signs every browser out on success.
func handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Code        string `json:"code"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		http.Error(w, `{"ok":false,"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	ip := clientIP(r)
	if ok, wait := loginThrottle.allowed(ip); !ok {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":       false,
			"error":    fmt.Sprintf("too many failed attempts, try again in %d seconds", int(wait.Seconds())+1),
			"retry_in": int(wait.Seconds()) + 1,
		})
		return
	}
	if err := appliance.ValidatePassword(req.NewPassword); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	if !appliance.RedeemResetCode(fb.GhostDir, req.Code, time.Now()) {
		loginThrottle.recordFailure(ip)
		log.Printf("Failed password-reset attempt from %s", ip)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "That code isn't right, or it has expired. Ask the app for a new one.",
		})
		return
	}
	if err := appliance.SetAdminPassword(fb.GhostDir, req.NewPassword); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": "couldn't save the new password"})
		return
	}
	loginThrottle.recordSuccess(ip)
	sessions.revokeAll()
	log.Printf("Console password reset with a phone code (from %s)", ip)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "message": "Password changed. Sign in with the new one."})
}

// watchBootReset applies a password-reset file left on the SD card's boot
// partition. Checking is a stat of a couple of paths every few seconds.
func watchBootReset() {
	apply := func() {
		changed, err := appliance.ApplyBootReset(fb.GhostDir)
		if changed {
			sessions.revokeAll()
			log.Printf("Console password reset from the SD card")
		} else if err != nil {
			log.Printf("SD card password reset not applied: %v", err)
		}
	}
	apply()
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for range t.C {
			apply()
		}
	}()
}
