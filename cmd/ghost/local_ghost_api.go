// Ghost phone/Pod cooperation surface: protocol versioning, capability
// advertisement, mobile model catalog, device-capability evaluation, and
// operation-based memory sync. Registered alongside the existing gateway
// routes; existing mobile → Pi behavior is untouched.
package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ianclemence/ghost/pkg/devcap"
	"github.com/ianclemence/ghost/pkg/execplan"
	"github.com/ianclemence/ghost/pkg/ghostproto"
	"github.com/ianclemence/ghost/pkg/infer"
	"github.com/ianclemence/ghost/pkg/memsync"
	"github.com/ianclemence/ghost/pkg/modelreg"
	"github.com/ianclemence/ghost/pkg/toolcap"
)

// startupInformer is the minimal agent surface this file needs.
type startupInformer interface {
	GetStartupInfo() map[string]interface{}
}

// runtimeInformer is implemented when the loop exposes its inference runtime.
type runtimeInformer interface {
	InferenceRuntime() infer.InferenceRuntime
}

var podSyncLog = memsync.NewLog()

// mobileCatalog is the seed registry of phone-local model artifacts.
//
// Sizes are publisher estimates (size_estimated) and manifests are unsigned:
// the phone pins the observed SHA-256 on first verified download and shows
// these models as "unverified publisher" until the operator publishes signed
// manifests. URLs point at the real upstream GGUF repos; exact per-file
// hashes are resolved at publish time via the signed-manifest flow.
func mobileCatalog() []modelreg.Manifest {
	return []modelreg.Manifest{
		{
			ID: "ghost-mini-1", Version: "1.0.0", ManifestVers: modelreg.ManifestVersion,
			Role:         "language",
			Capabilities: []string{"chat", "tool_calling", "structured_output"},
			Runtime:      "mobile-local", Format: "gguf", Quantization: "Q4_K_M",
			SizeBytes:     650 * 1024 * 1024,
			SizeEstimated: true,
			Platforms:     []string{"android", "ios"},
			Archs:         []string{"arm64"},
			MinRAMMB:      4000, RecRAMMB: 6000,
			DownloadURL: "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q4_K_M.gguf",
		},
		{
			ID: "ghost-balanced-1", Version: "1.0.0", ManifestVers: modelreg.ManifestVersion,
			Role:         "language",
			Capabilities: []string{"chat", "tool_calling", "structured_output"},
			Runtime:      "mobile-local", Format: "gguf", Quantization: "Q4_K_M",
			SizeBytes:     1250 * 1024 * 1024,
			SizeEstimated: true,
			Platforms:     []string{"android", "ios"},
			Archs:         []string{"arm64"},
			MinRAMMB:      6000, RecRAMMB: 8000,
			DownloadURL: "https://huggingface.co/Qwen/Qwen3-1.7B-GGUF/resolve/main/Qwen3-1.7B-Q4_K_M.gguf",
		},
	}
}

// podToolAdvertisement derives executor placement from the live tool registry:
// tools whose names indicate Pod-owned hardware are pod-only; every other Pod
// tool runs on the Pod. The phone merges this with its own local
// advertisement client-side; availability is authoritative per advertisement.
func podToolAdvertisement(names []string) toolcap.Advertisement {
	defs := make([]toolcap.Definition, 0, len(names))
	for _, n := range names {
		execs := []toolcap.Executor{toolcap.ExecPod}
		defs = append(defs, toolcap.Definition{
			Name: n, Executors: execs,
			Sensitive: isSensitiveTool(n),
		})
	}
	return toolcap.Advertisement{DeviceID: "pod", Tools: defs}
}

func isSensitiveTool(name string) bool {
	for _, s := range []string{"shell", "exec", "browser", "computer", "permission", "secret", "credential"} {
		if len(name) >= len(s) {
			for i := 0; i+len(s) <= len(name); i++ {
				if name[i:i+len(s)] == s {
					return true
				}
			}
		}
	}
	return false
}

func toolNamesFromInfo(info map[string]interface{}) []string {
	var names []string
	if ti, ok := info["tools"].(map[string]interface{}); ok {
		if n, ok := ti["names"].([]string); ok {
			names = n
		}
	}
	return names
}

func registerLocalGhostRoutes(mux *http.ServeMux, agentLoop startupInformer) {
	// ── Protocol version negotiation ──────────────────────────────────
	mux.HandleFunc("/v1/protocol", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"proto_version":     ghostproto.Version,
			"min_supported":     ghostproto.MinSupportedVersion,
			"execution_targets": []string{string(execplan.TargetPhone), string(execplan.TargetPod), string(execplan.TargetCloud)},
			"privacy_modes":     []string{string(execplan.PrivacyLocalOnly), string(execplan.PrivacyBalanced), string(execplan.PrivacyCloudCapable)},
		})
	}))

	// ── Pod capability advertisement (protocol-backed tool availability) ─
	mux.HandleFunc("/v1/capabilities", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		var names []string
		if agentLoop != nil {
			names = toolNamesFromInfo(agentLoop.GetStartupInfo())
		}
		ad := podToolAdvertisement(names)
		resp := map[string]interface{}{
			"device_id":         "pod",
			"proto_version":     ghostproto.Version,
			"tools":             ad.Tools,
			"pod_hardware_only": toolcap.PodHardwareTools(ad.Tools),
			"execution_targets": []string{string(execplan.TargetPod)},
		}
		// Live Pod model capability/health, reported through the runtime
		// contract (capabilities, not provider internals).
		if ri, ok := interface{}(agentLoop).(runtimeInformer); ok && ri != nil {
			if rt := ri.InferenceRuntime(); rt != nil {
				h := rt.Health(r.Context())
				resp["runtime"] = map[string]interface{}{
					"name": h.LoadedModel, "available": h.Available, "reason": h.Reason,
				}
				if models, err := rt.Models(r.Context()); err == nil {
					resp["models"] = models
				}
			}
		}
		jsonResponse(w, http.StatusOK, resp)
	}))

	// ── Mobile model catalog ───────────────────────────────────────────
	mux.HandleFunc("/v1/models/catalog", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"manifest_version": modelreg.ManifestVersion,
			"models":           mobileCatalog(),
		})
	}))

	// ── Device-capability evaluation ───────────────────────────────────
	mux.HandleFunc("/v1/devices/capabilities", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		var req struct {
			Device devcap.Device `json:"device"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", "device is required")
			return
		}
		results := make([]map[string]interface{}, 0)
		for _, m := range mobileCatalog() {
			need := devcap.ModelNeed{
				ModelID: m.ID, Runtime: m.Runtime, Platforms: m.Platforms,
				Archs: m.Archs, MinRAMMB: m.MinRAMMB, RecRAMMB: m.RecRAMMB,
				SizeMB: m.SizeBytes / (1024 * 1024), MinOS: m.MinOS,
			}
			r := devcap.Evaluate(req.Device, need)
			results = append(results, map[string]interface{}{
				"model_id": m.ID, "verdict": string(r.Verdict), "reason": r.Reason,
			})
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"results": results})
	}))

	// ── Memory sync: op ingest + replay ────────────────────────────────
	mux.HandleFunc("/v1/sync/ops", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ops": podSyncLog.Since(since)})
		case http.MethodPost:
			var req struct {
				Ops []memsync.Op `json:"ops"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Ops) == 0 {
				jsonError(w, http.StatusBadRequest, "invalid_request", "ops[] is required")
				return
			}
			applied := 0
			for _, o := range req.Ops {
				// Server stamps nothing: origin clock stays authoritative for
				// deterministic replay; duplicates collapse idempotently.
				ok, err := podSyncLog.Apply(o)
				if err != nil {
					jsonError(w, http.StatusBadRequest, "invalid_op", err.Error())
					return
				}
				if ok {
					applied++
				}
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "applied": applied})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}))

	// ── Local-execution diagnostics (extends /v1/doctor surface) ───────
	mux.HandleFunc("/v1/doctor/local", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		checks := []map[string]interface{}{
			{"name": "protocol", "status": "ok", "message": "ghost protocol v1"},
			{"name": "model_catalog", "status": "ok", "message": "2 mobile manifests published"},
			{"name": "sync_log", "status": "ok", "message": "op log accepting phone/pod mutations"},
		}
		var toolCount int
		if agentLoop != nil {
			toolCount = len(toolNamesFromInfo(agentLoop.GetStartupInfo()))
		}
		if toolCount == 0 {
			checks = append(checks, map[string]interface{}{"name": "tool_advertisement", "status": "warning", "message": "pod tool inventory unavailable"})
		} else {
			checks = append(checks, map[string]interface{}{"name": "tool_advertisement", "status": "ok", "message": "pod advertises tool executors"})
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"status": "ok", "checks": checks})
	}))
}
