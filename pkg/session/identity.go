package session

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// IdentityLink maps multiple channel identities to a single canonical identity.
type IdentityLink struct {
	CanonicalID string   `json:"canonical_id"`
	Aliases     []string `json:"aliases"`     // Format: "channel:senderID"
	SessionKey  string   `json:"session_key"` // The unified session key
	Created     string   `json:"created,omitempty"`
}

// IdentityManager manages cross-channel identity resolution.
type IdentityManager struct {
	links       map[string]*IdentityLink // canonicalID -> link
	aliasLookup map[string]string        // "channel:senderID" -> canonicalID
	dimensions  []string                 // configurable session dimensions
	mu          sync.RWMutex
}

// NewIdentityManager creates a new IdentityManager with default dimensions.
func NewIdentityManager() *IdentityManager {
	return &IdentityManager{
		links:       make(map[string]*IdentityLink),
		aliasLookup: make(map[string]string),
		dimensions:  []string{"channel", "sender"},
	}
}

// SetDimensions configures which dimensions are used for session key construction.
func (im *IdentityManager) SetDimensions(dims []string) {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.dimensions = dims
}

// Link links a channel:senderID alias to a canonical identity.
// If the canonicalID doesn't exist, a new identity is created.
// If the alias is already linked to a different canonical ID, it is re-linked.
func (im *IdentityManager) Link(canonicalID, channel, senderID string) {
	im.mu.Lock()
	defer im.mu.Unlock()

	alias := BuildAlias(channel, senderID)

	// Remove old linkage if alias was already linked
	if oldCanonical, ok := im.aliasLookup[alias]; ok {
		if oldCanonical == canonicalID {
			return // Already linked
		}
		if oldLink, ok := im.links[oldCanonical]; ok {
			oldLink.Aliases = removeString(oldLink.Aliases, alias)
			if len(oldLink.Aliases) == 0 {
				delete(im.links, oldCanonical)
			}
		}
	}

	// Get or create the canonical link
	link, ok := im.links[canonicalID]
	if !ok {
		link = &IdentityLink{
			CanonicalID: canonicalID,
			Aliases:     []string{alias},
			SessionKey:  BuildSessionKey(canonicalID, im.dimensions),
		}
		im.links[canonicalID] = link
	} else {
		// Add alias if not present
		if !containsString(link.Aliases, alias) {
			link.Aliases = append(link.Aliases, alias)
			sort.Strings(link.Aliases) // Deterministic ordering
		}
	}

	im.aliasLookup[alias] = canonicalID
}

// Unlink removes a channel:senderID alias from its canonical identity.
func (im *IdentityManager) Unlink(channel, senderID string) {
	im.mu.Lock()
	defer im.mu.Unlock()

	alias := BuildAlias(channel, senderID)
	canonicalID, ok := im.aliasLookup[alias]
	if !ok {
		return
	}

	delete(im.aliasLookup, alias)

	if link, ok := im.links[canonicalID]; ok {
		link.Aliases = removeString(link.Aliases, alias)
		if len(link.Aliases) == 0 {
			delete(im.links, canonicalID)
		}
	}
}

// Resolve returns the canonical identity for a channel:senderID alias.
func (im *IdentityManager) Resolve(channel, senderID string) (string, bool) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	alias := BuildAlias(channel, senderID)
	canonicalID, ok := im.aliasLookup[alias]
	return canonicalID, ok
}

// GetLink returns the identity link for a canonical ID.
func (im *IdentityManager) GetLink(canonicalID string) (*IdentityLink, bool) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	link, ok := im.links[canonicalID]
	if !ok {
		return nil, false
	}
	// Return a copy
	cp := *link
	cp.Aliases = make([]string, len(link.Aliases))
	copy(cp.Aliases, link.Aliases)
	return &cp, true
}

// GetLinkByAlias returns the identity link for a channel:senderID alias.
func (im *IdentityManager) GetLinkByAlias(channel, senderID string) (*IdentityLink, bool) {
	im.mu.RLock()
	defer im.mu.RUnlock()

	alias := BuildAlias(channel, senderID)
	canonicalID, ok := im.aliasLookup[alias]
	if !ok {
		return nil, false
	}
	link := im.links[canonicalID]
	cp := *link
	cp.Aliases = make([]string, len(link.Aliases))
	copy(cp.Aliases, link.Aliases)
	return &cp, true
}

// GetSessionKey returns the unified session key for a channel:senderID.
// If the identity is linked, returns the canonical session key.
// Otherwise, falls back to a per-channel session key.
func (im *IdentityManager) GetSessionKey(channel, senderID string) string {
	im.mu.RLock()
	defer im.mu.RUnlock()

	alias := BuildAlias(channel, senderID)
	if canonicalID, ok := im.aliasLookup[alias]; ok {
		if link, ok := im.links[canonicalID]; ok {
			return link.SessionKey
		}
	}

	// Fallback: per-channel session key
	return BuildSessionKey(fmt.Sprintf("%s:%s", channel, senderID), im.dimensions)
}

// ListLinks returns all identity links.
func (im *IdentityManager) ListLinks() []*IdentityLink {
	im.mu.RLock()
	defer im.mu.RUnlock()

	result := make([]*IdentityLink, 0, len(im.links))
	for _, link := range im.links {
		cp := *link
		cp.Aliases = make([]string, len(link.Aliases))
		copy(cp.Aliases, link.Aliases)
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CanonicalID < result[j].CanonicalID
	})
	return result
}

// Count returns the number of canonical identities.
func (im *IdentityManager) Count() int {
	im.mu.RLock()
	defer im.mu.RUnlock()
	return len(im.links)
}

// BuildAlias creates a channel:senderID alias string.
func BuildAlias(channel, senderID string) string {
	return fmt.Sprintf("%s:%s", strings.ToLower(strings.TrimSpace(channel)), strings.TrimSpace(senderID))
}

// BuildSessionKey creates a deterministic session key from a canonical ID and dimensions.
func BuildSessionKey(canonicalID string, dimensions []string) string {
	// Sort dimensions for deterministic ordering
	sortedDims := make([]string, len(dimensions))
	copy(sortedDims, dimensions)
	sort.Strings(sortedDims)

	h := sha256.New()
	h.Write([]byte(canonicalID))
	h.Write([]byte("|"))
	h.Write([]byte(strings.Join(sortedDims, ",")))

	return fmt.Sprintf("sk_%x", h.Sum(nil)[:16])
}

// BuildLegacySessionKey creates a session key in the legacy format for backward compatibility.
func BuildLegacySessionKey(channel, senderID string) string {
	return fmt.Sprintf("%s:%s", strings.ToLower(channel), senderID)
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, v := range slice {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}
