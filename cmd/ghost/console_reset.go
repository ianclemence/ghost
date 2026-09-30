package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/appliance"
)

// registerConsoleResetRoute lets a paired phone ask for a one-time code that
// resets the web console's password. A normal owner has no terminal on their
// Pod; the phone is already trusted with it, so it can vouch for them. The
// code is shown only to the phone that asked, expires in ten minutes, works
// once, and the owner is told it was requested.
func registerConsoleResetRoute(mux *http.ServeMux, al *agent.AgentLoop) {
	mux.HandleFunc("/v1/console/password-reset", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
			return
		}
		device := principalDevice(r)
		if device == "" {
			jsonError(w, http.StatusForbidden, "forbidden", "a paired device is required")
			return
		}
		dir := os.Getenv("GHOST_DIR")
		if dir == "" {
			dir = appliance.DefaultGhostDir
		}
		code, expires, err := appliance.IssueResetCode(dir, time.Now())
		if err != nil {
			jsonError(w, http.StatusBadRequest, "not_available", err.Error())
			return
		}
		who := "a paired device"
		if device != "owner" {
			who = "your phone"
		}
		al.Announce("console-reset:"+fmt.Sprint(time.Now().Unix()/60),
			"A reset code for the web console password was just requested from "+who+". If that wasn't you, nothing changes unless someone also has the code; it expires in 10 minutes.",
			time.Minute, false)
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok":                true,
			"code":              code,
			"expires_in":        int(time.Until(expires).Seconds()),
			"console_reset_url": "the web console's sign-in page → Forgot your password?",
		})
	}))
}
