package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/siteicon"
)

// The Vault's owner-facing API. The phone saves what the owner types here and
// lists it back; what it lists never contains a secret. Every route sits under
// /v1/vault/, which the gateway treats as sensitive for reads as well as writes
// (see localTokenPrefixes): a command the model runs in its shell reaches
// loopback, and must not be able to ask the Vault what it holds.

// maxVaultBody caps a request. The largest thing in the Vault is a key.
const maxVaultBody = 64 << 10

var (
	iconsMu   sync.Mutex
	iconsDir  string
	iconsPool *siteicon.Fetcher
)

// siteIcons is the icon fetcher for the current workspace.
func siteIcons() *siteicon.Fetcher {
	dir := filepath.Join(apiWorkspaceDir, ".cache", "site-icons")
	iconsMu.Lock()
	defer iconsMu.Unlock()
	if iconsPool == nil || iconsDir != dir {
		iconsPool, iconsDir = siteicon.New(dir), dir
	}
	return iconsPool
}

// readVaultJSON decodes one request body into v, strictly: a body that is too
// large, has a field the Vault does not have, or has trailing data is refused.
func readVaultJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxVaultBody))
	if err != nil {
		jsonError(w, http.StatusRequestEntityTooLarge, "too_large", "that is too large to save")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || dec.More() {
		jsonError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return false
	}
	return true
}

func registerVaultRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/vault/items", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			kind := credentials.ItemKind(strings.TrimSpace(r.URL.Query().Get("kind")))
			if kind != "" && !kind.Valid() {
				jsonError(w, http.StatusBadRequest, "invalid_request", "unknown kind")
				return
			}
			items := credentials.ListItems(kind)
			counts := map[string]int{}
			for _, it := range credentials.ListItems("") {
				counts[string(it.Kind)]++
			}
			if items == nil {
				items = []credentials.View{}
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "items": items, "counts": counts})

		case http.MethodPost, http.MethodPut:
			var in credentials.Item
			if !readVaultJSON(w, r, &in) {
				return
			}
			if r.Method == http.MethodPost {
				// A new item is new: an id sent with it is not trusted to pick
				// which stored item it replaces.
				if in.Kind != credentials.KindLogin {
					in.ID = ""
				}
			} else if strings.TrimSpace(in.ID) == "" && in.Kind != credentials.KindLogin {
				jsonError(w, http.StatusBadRequest, "invalid_request", "an id is required to change an item")
				return
			}
			view, err := credentials.SaveItem(in)
			if err != nil {
				// Validation failures are the owner's to fix, and say nothing
				// about any value.
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "item": view})

		case http.MethodDelete:
			kind := credentials.ItemKind(strings.TrimSpace(r.URL.Query().Get("kind")))
			id := strings.TrimSpace(r.URL.Query().Get("id"))
			if !kind.Valid() || id == "" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "kind and id are required")
				return
			}
			if err := credentials.DeleteItem(kind, id); err != nil {
				jsonError(w, http.StatusBadRequest, "delete_failed", "couldn't remove that")
				return
			}
			if kind == credentials.KindLogin {
				siteIcons().Forget(id)
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})

		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET, POST, PUT or DELETE")
		}
	}))

	// A site's icon, fetched by the Pod and kept. 200 with ok:false means the
	// site has none, which the phone remembers and draws a letter for.
	mux.HandleFunc("/v1/vault/icon", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
			return
		}
		host := credentials.NormalizeWebHost(r.URL.Query().Get("host"))
		if !siteicon.ValidHost(host) {
			jsonError(w, http.StatusBadRequest, "invalid_request", "that is not a site")
			return
		}
		// Only for a site the owner has in the Vault: this is not an open
		// fetch-any-site service for whoever can reach the route.
		if !vaultHasHost(host) {
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": false})
			return
		}
		png, ok := siteIcons().Icon(r.Context(), host)
		if !ok {
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": false})
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok": true, "mime": "image/png", "data": base64.StdEncoding.EncodeToString(png),
		})
	}))

	// The owner's answer to a credentials card. The site comes from the card
	// the Pod made, never from the request, so a form for one site cannot be
	// used to store a sign-in for another.
	mux.HandleFunc("/v1/vault/credentials", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
			return
		}
		var in struct {
			CardID   string `json:"card_id"`
			Username string `json:"username"`
			Password string `json:"password"`
			Save     bool   `json:"save"`
		}
		if !readVaultJSON(w, r, &in) {
			return
		}
		channel, card, ok := cards.DefaultStore.FindAnywhere(strings.TrimSpace(in.CardID))
		if !ok || card.Kind != cards.KindCredentials {
			jsonError(w, http.StatusNotFound, "not_found", "that request is no longer open")
			return
		}
		if card.Resolved != nil {
			jsonError(w, http.StatusConflict, "already_answered", "that request was already answered")
			return
		}
		host := cards.CredentialHostOf(card)
		username := strings.TrimSpace(in.Username)
		if username == "" || in.Password == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "enter both a username and a password")
			return
		}
		if len(username) > 254 || len(in.Password) > 1024 {
			jsonError(w, http.StatusBadRequest, "invalid_request", "that is too long to be a sign-in")
			return
		}
		login := credentials.WebLogin{Host: host, URL: "https://" + host, Username: username, Password: in.Password}
		label := "Signed in to " + host
		if in.Save {
			if _, err := credentials.SaveItem(credentials.Item{
				Kind: credentials.KindLogin, Host: host, URL: login.URL, Username: username, Password: in.Password,
				Title: cards.CredentialServiceOf(card),
			}); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			label += " · saved to your Vault"
		} else {
			if err := credentials.PutEphemeralWebLogin(login); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			label += " · for this task only"
		}
		resolved, _ := cards.DefaultStore.ResolveAs(channel, card.ID, "submitted", label)
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "saved": in.Save, "card": resolved})
	}))
}

// vaultHasHost reports whether any login in the Vault is on the site.
func vaultHasHost(host string) bool {
	for _, v := range credentials.ListItems(credentials.KindLogin) {
		if credentials.SameSite(v.Host, host) {
			return true
		}
	}
	return false
}
