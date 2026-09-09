package main

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/deviceops"
)

// Device runners perform the actual restart/update. They are package-level
// so tests can substitute them; production defaults refuse when no system
// command is configured, so an operation can never pretend success.
var (
	errDeviceRestartUnavailable = errors.New("restart_unavailable: no restart mechanism is configured on this host")
	errDeviceUpdateUnavailable  = errors.New("update_unavailable: no update mechanism is configured on this host")
	deviceProcessStart          = time.Now()
)

func defaultRestartRunner() error {
	cmd := strings.TrimSpace(os.Getenv("GHOST_DEVICE_RESTART_CMD"))
	if cmd == "" {
		return errDeviceRestartUnavailable
	}
	return exec.Command("sh", "-c", cmd).Run()
}

func defaultUpdateRunner() error {
	cmd := strings.TrimSpace(os.Getenv("GHOST_DEVICE_UPDATE_CMD"))
	if cmd == "" {
		return errDeviceUpdateUnavailable
	}
	return exec.Command("sh", "-c", cmd).Run()
}

var deviceRestart = defaultRestartRunner
var deviceUpdate = defaultUpdateRunner

// deviceAPIHandler serves the mobile-safe device surface. requireAuth is the
// device-authentication wrapper; operations are durable and never accept
// caller-supplied commands.
func deviceAPIHandler(st *deviceops.Store, requireAuth func(http.HandlerFunc) http.HandlerFunc) http.Handler {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/device"), "/")
		switch {
		case path == "":
			if r.Method != http.MethodGet {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"ok": true,
				"device": map[string]interface{}{
					"version":  version,
					"status":   "ok",
					"uptime_s": int(time.Since(deviceProcessStart).Seconds()),
				},
			})
		case path == "restart":
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
				return
			}
			runDeviceOp(w, st, r, "restart")
		case path == "update":
			switch r.Method {
			case http.MethodGet:
				ops, err := st.List()
				if err != nil {
					jsonError(w, http.StatusInternalServerError, "unavailable", "operations unavailable")
					return
				}
				jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "operations": ops})
			case http.MethodPost:
				runDeviceOp(w, st, r, "update")
			default:
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET or POST")
			}
		case path == "operations":
			if r.Method != http.MethodGet {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
				return
			}
			ops, err := st.List()
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "unavailable", "operations unavailable")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "operations": ops})
		case strings.HasPrefix(path, "operations/"):
			if r.Method != http.MethodGet {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
				return
			}
			id := strings.TrimPrefix(path, "operations/")
			op, err := st.Get(id)
			if err != nil || op == nil {
				jsonError(w, http.StatusNotFound, "operation_not_found", "operation not found")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "operation": op})
		default:
			jsonError(w, http.StatusNotFound, "not_found", "unknown device endpoint")
		}
	})
	if requireAuth == nil {
		return h
	}
	return requireAuth(h)
}

// runDeviceOp creates a durable operation, transitions it through the real
// runner, and returns the resulting state. A runner that is unavailable
// produces an honest failed state — never a fake success.
func runDeviceOp(w http.ResponseWriter, st *deviceops.Store, r *http.Request, action string) {
	device := strings.TrimSpace(r.Header.Get("X-Ghost-Device-ID"))
	if device == "" {
		device = "owner"
	}
	op, err := st.Create(action, device)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "op_failed", "could not create operation")
		return
	}
	runner := deviceRestart
	if action == "update" {
		runner = deviceUpdate
	}
	if _, err := st.Transition(op.ID, deviceops.StateStarting, ""); err != nil {
		jsonError(w, http.StatusInternalServerError, "op_failed", err.Error())
		return
	}
	if err := runner(); err != nil {
		_, _ = st.Transition(op.ID, deviceops.StateFailed, err.Error())
		final, _ := st.Get(op.ID)
		// Product-language error: the operation is durable and inspectable.
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "operation": final})
		return
	}
	if action == "update" {
		_, _ = st.Transition(op.ID, deviceops.StateCompleted, "update completed")
	} else {
		_, _ = st.Transition(op.ID, deviceops.StateRebooting, "restart initiated; device will reconcile on return")
	}
	final, _ := st.Get(op.ID)
	jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "operation": final})
}

// registerDeviceRoutes mounts the device API behind device auth and
// reconciles any pre-restart operation once the device is back.
func registerDeviceRoutes(mux *http.ServeMux) {
	dir := filepath.Join(apiWorkspaceDir, "state", "device-ops")
	st, err := deviceops.New(dir)
	if err != nil {
		return
	}
	_, _ = st.Reconcile(deviceProcessStart, time.Now())
	mux.Handle("/v1/device/", deviceAPIHandler(st, authMiddleware))
}
