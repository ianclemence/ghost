package session

import (
	"testing"
)

func TestIdentityManager_LinkAndResolve(t *testing.T) {
	im := NewIdentityManager()

	im.Link("user-alice", "telegram", "12345")
	im.Link("user-alice", "discord", "alice#1234")

	// Resolve should work for both aliases
	canonical, ok := im.Resolve("telegram", "12345")
	if !ok || canonical != "user-alice" {
		t.Errorf("Expected user-alice, got %s (found=%v)", canonical, ok)
	}

	canonical, ok = im.Resolve("discord", "alice#1234")
	if !ok || canonical != "user-alice" {
		t.Errorf("Expected user-alice, got %s (found=%v)", canonical, ok)
	}

	// Unknown alias should not resolve
	_, ok = im.Resolve("slack", "unknown")
	if ok {
		t.Error("Expected unknown alias to not resolve")
	}
}

func TestIdentityManager_Unlink(t *testing.T) {
	im := NewIdentityManager()

	im.Link("user-alice", "telegram", "12345")
	im.Link("user-alice", "discord", "alice#1234")

	im.Unlink("discord", "alice#1234")

	// Discord should no longer resolve
	_, ok := im.Resolve("discord", "alice#1234")
	if ok {
		t.Error("Expected discord alias to be unlinked")
	}

	// Telegram should still work
	canonical, ok := im.Resolve("telegram", "12345")
	if !ok || canonical != "user-alice" {
		t.Errorf("Expected telegram to still resolve, got %s", canonical)
	}
}

func TestIdentityManager_GetSessionKey(t *testing.T) {
	im := NewIdentityManager()

	im.Link("user-alice", "telegram", "12345")
	im.Link("user-alice", "discord", "alice#1234")

	key1 := im.GetSessionKey("telegram", "12345")
	key2 := im.GetSessionKey("discord", "alice#1234")

	// Both should get the same session key
	if key1 != key2 {
		t.Errorf("Expected same session key, got '%s' and '%s'", key1, key2)
	}

	// Unknown channel should get a fallback key
	key3 := im.GetSessionKey("email", "alice@example.com")
	if key3 == "" {
		t.Error("Expected non-empty fallback key")
	}
	if key3 == key1 {
		t.Error("Expected fallback key to differ from linked key")
	}
}

func TestIdentityManager_GetLink(t *testing.T) {
	im := NewIdentityManager()

	im.Link("user-alice", "telegram", "12345")
	im.Link("user-alice", "discord", "alice#1234")

	link, ok := im.GetLink("user-alice")
	if !ok {
		t.Fatal("Expected link to exist")
	}

	if link.CanonicalID != "user-alice" {
		t.Errorf("Expected canonical ID 'user-alice', got '%s'", link.CanonicalID)
	}

	if len(link.Aliases) != 2 {
		t.Errorf("Expected 2 aliases, got %d", len(link.Aliases))
	}

	// Should be sorted
	if link.Aliases[0] != "discord:alice#1234" || link.Aliases[1] != "telegram:12345" {
		t.Errorf("Expected sorted aliases, got %v", link.Aliases)
	}
}

func TestIdentityManager_GetLinkByAlias(t *testing.T) {
	im := NewIdentityManager()

	im.Link("user-alice", "telegram", "12345")

	link, ok := im.GetLinkByAlias("telegram", "12345")
	if !ok {
		t.Fatal("Expected link to exist")
	}

	if link.CanonicalID != "user-alice" {
		t.Errorf("Expected canonical ID 'user-alice', got '%s'", link.CanonicalID)
	}
}

func TestIdentityManager_ListLinks(t *testing.T) {
	im := NewIdentityManager()

	im.Link("user-alice", "telegram", "12345")
	im.Link("user-bob", "discord", "bob#5678")

	links := im.ListLinks()
	if len(links) != 2 {
		t.Fatalf("Expected 2 links, got %d", len(links))
	}

	// Should be sorted by canonical ID
	if links[0].CanonicalID != "user-alice" || links[1].CanonicalID != "user-bob" {
		t.Errorf("Expected sorted links, got %v", links)
	}
}

func TestIdentityManager_Count(t *testing.T) {
	im := NewIdentityManager()

	if im.Count() != 0 {
		t.Errorf("Expected 0, got %d", im.Count())
	}

	im.Link("user-alice", "telegram", "12345")
	im.Link("user-alice", "discord", "alice#1234") // Same canonical, no new link

	if im.Count() != 1 {
		t.Errorf("Expected 1 (same canonical), got %d", im.Count())
	}

	im.Link("user-bob", "telegram", "99999")
	if im.Count() != 2 {
		t.Errorf("Expected 2, got %d", im.Count())
	}
}

func TestIdentityManager_DuplicateLink(t *testing.T) {
	im := NewIdentityManager()

	im.Link("user-alice", "telegram", "12345")
	im.Link("user-alice", "telegram", "12345") // Duplicate

	link, ok := im.GetLink("user-alice")
	if !ok {
		t.Fatal("Expected link to exist")
	}

	if len(link.Aliases) != 1 {
		t.Errorf("Expected 1 alias (no duplicate), got %d", len(link.Aliases))
	}
}

func TestIdentityManager_RelinkToDifferentCanonical(t *testing.T) {
	im := NewIdentityManager()

	im.Link("user-alice", "telegram", "12345")
	im.Link("user-bob", "telegram", "12345") // Re-link to different canonical

	// Should now resolve to bob
	canonical, ok := im.Resolve("telegram", "12345")
	if !ok || canonical != "user-bob" {
		t.Errorf("Expected user-bob, got %s", canonical)
	}

	// Alice should no longer have this alias
	link, ok := im.GetLink("user-alice")
	if ok && containsStr(link.Aliases, "telegram:12345") {
		t.Error("Expected alice to not have telegram:12345 alias")
	}
}

func TestBuildAlias(t *testing.T) {
	alias := BuildAlias("Telegram", "12345")
	if alias != "telegram:12345" {
		t.Errorf("Expected 'telegram:12345', got '%s'", alias)
	}
}

func TestBuildSessionKey(t *testing.T) {
	key1 := BuildSessionKey("user-alice", []string{"channel", "sender"})
	key2 := BuildSessionKey("user-alice", []string{"channel", "sender"})
	key3 := BuildSessionKey("user-bob", []string{"channel", "sender"})

	if key1 != key2 {
		t.Errorf("Expected same key for same input, got '%s' and '%s'", key1, key2)
	}
	if key1 == key3 {
		t.Error("Expected different keys for different canonical IDs")
	}
}

func TestBuildSessionKey_DimensionOrdering(t *testing.T) {
	// Dimensions should be sorted for deterministic keys
	key1 := BuildSessionKey("user-alice", []string{"sender", "channel"})
	key2 := BuildSessionKey("user-alice", []string{"channel", "sender"})

	if key1 != key2 {
		t.Errorf("Expected same key regardless of dimension order, got '%s' and '%s'", key1, key2)
	}
}

func TestBuildLegacySessionKey(t *testing.T) {
	key := BuildLegacySessionKey("telegram", "12345")
	if key != "telegram:12345" {
		t.Errorf("Expected 'telegram:12345', got '%s'", key)
	}
}

func TestIdentityManager_SetDimensions(t *testing.T) {
	im := NewIdentityManager()
	im.SetDimensions([]string{"channel", "sender", "chat"})

	im.Link("user-alice", "telegram", "12345")
	key := im.GetSessionKey("telegram", "12345")

	if key == "" {
		t.Error("Expected non-empty session key")
	}

	// Key should be different from default dimensions
	im2 := NewIdentityManager()
	im2.Link("user-alice", "telegram", "12345")
	key2 := im2.GetSessionKey("telegram", "12345")

	if key == key2 {
		t.Error("Expected different keys with different dimensions")
	}
}

func TestIdentityManager_GetLink_NotFound(t *testing.T) {
	im := NewIdentityManager()

	_, ok := im.GetLink("nonexistent")
	if ok {
		t.Error("Expected not found")
	}
}

func TestIdentityManager_GetLinkByAlias_NotFound(t *testing.T) {
	im := NewIdentityManager()

	_, ok := im.GetLinkByAlias("telegram", "99999")
	if ok {
		t.Error("Expected not found")
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
