package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
)

func TestHonchoStore_CreateProfile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{
		Skills: config.SkillsConfig{
			Honcho: config.HonchoConfig{
				Enabled: true,
			},
		},
	}

	store := NewHonchoStore(&cfg, tmpDir)
	ctx := context.Background()

	profile, err := store.GetProfile(ctx, "user-123")
	if err != nil {
		t.Fatalf("failed to get profile: %v", err)
	}

	if profile.UserID != "user-123" {
		t.Errorf("expected user_id user-123, got %s", profile.UserID)
	}
	if profile.Interactions != 1 {
		t.Errorf("expected 1 interaction, got %d", profile.Interactions)
	}
}

func TestHonchoStore_AddFact(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{
		Skills: config.SkillsConfig{
			Honcho: config.HonchoConfig{
				Enabled: true,
			},
		},
	}

	store := NewHonchoStore(&cfg, tmpDir)
	ctx := context.Background()

	err := store.AddFact(ctx, "user-123", "User prefers Python", "conversation", 0.9)
	if err != nil {
		t.Fatalf("failed to add fact: %v", err)
	}

	profile, err := store.GetProfile(ctx, "user-123")
	if err != nil {
		t.Fatalf("failed to get profile: %v", err)
	}

	if len(profile.Facts) != 1 {
		t.Errorf("expected 1 fact, got %d", len(profile.Facts))
	}
	if profile.Facts[0].Fact != "User prefers Python" {
		t.Errorf("unexpected fact: %s", profile.Facts[0].Fact)
	}
}

func TestHonchoStore_AddTopic(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{
		Skills: config.SkillsConfig{
			Honcho: config.HonchoConfig{
				Enabled: true,
			},
		},
	}

	store := NewHonchoStore(&cfg, tmpDir)
	ctx := context.Background()

	err := store.AddTopic(ctx, "user-123", "machine-learning")
	if err != nil {
		t.Fatalf("failed to add topic: %v", err)
	}

	err = store.AddTopic(ctx, "user-123", "web-development")
	if err != nil {
		t.Fatalf("failed to add topic: %v", err)
	}

	// Add duplicate
	err = store.AddTopic(ctx, "user-123", "machine-learning")
	if err != nil {
		t.Fatalf("failed to add duplicate topic: %v", err)
	}

	profile, err := store.GetProfile(ctx, "user-123")
	if err != nil {
		t.Fatalf("failed to get profile: %v", err)
	}

	if len(profile.Topics) != 2 {
		t.Errorf("expected 2 topics, got %d", len(profile.Topics))
	}
}

func TestHonchoStore_GetContext(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{
		Skills: config.SkillsConfig{
			Honcho: config.HonchoConfig{
				Enabled: true,
			},
		},
	}

	store := NewHonchoStore(&cfg, tmpDir)
	ctx := context.Background()

	store.UpdateProfile(ctx, "user-123", map[string]interface{}{
		"display_name":   "John",
		"communication":  "technical",
	})
	store.AddFact(ctx, "user-123", "Prefers dark mode", "ui", 0.95)

	context := store.GetContext(ctx, "user-123")

	if !containsStr(context, "John") {
		t.Errorf("expected name in context: %s", context)
	}
	if !containsStr(context, "technical") {
		t.Errorf("expected communication style in context: %s", context)
	}
}

func TestHonchoStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{
		Skills: config.SkillsConfig{
			Honcho: config.HonchoConfig{
				Enabled: true,
			},
		},
	}

	// Create profile with first store
	store1 := NewHonchoStore(&cfg, tmpDir)
	ctx := context.Background()
	store1.AddFact(ctx, "user-123", "Test fact", "test", 0.5)

	// Load with second store
	store2 := NewHonchoStore(&cfg, tmpDir)
	profile, err := store2.GetProfile(ctx, "user-123")
	if err != nil {
		t.Fatalf("failed to get profile: %v", err)
	}

	if len(profile.Facts) != 1 {
		t.Errorf("expected 1 fact after persistence, got %d", len(profile.Facts))
	}
}

func TestHonchoStore_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{
		Skills: config.SkillsConfig{
			Honcho: config.HonchoConfig{
				Enabled: false,
			},
		},
	}

	store := NewHonchoStore(&cfg, tmpDir)
	ctx := context.Background()

	_, err := store.GetProfile(ctx, "user-123")
	if err == nil {
		t.Error("expected error when honcho is disabled")
	}
}

func TestHonchoStore_ListProfiles(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{
		Skills: config.SkillsConfig{
			Honcho: config.HonchoConfig{
				Enabled: true,
			},
		},
	}

	store := NewHonchoStore(&cfg, tmpDir)
	ctx := context.Background()

	store.GetProfile(ctx, "user-1")
	store.GetProfile(ctx, "user-2")

	profiles := store.ListProfiles()
	if len(profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(profiles))
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestHonchoStore_UpdateProfile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Config{
		Skills: config.SkillsConfig{
			Honcho: config.HonchoConfig{
				Enabled: true,
			},
		},
	}

	store := NewHonchoStore(&cfg, tmpDir)
	ctx := context.Background()

	// Create profile
	store.GetProfile(ctx, "user-123")

	// Update
	err := store.UpdateProfile(ctx, "user-123", map[string]interface{}{
		"display_name": "Jane",
		"language":     "es",
	})
	if err != nil {
		t.Fatalf("failed to update profile: %v", err)
	}

	profile, _ := store.GetProfile(ctx, "user-123")
	if profile.DisplayName != "Jane" {
		t.Errorf("expected display name Jane, got %s", profile.DisplayName)
	}
	if profile.Language != "es" {
		t.Errorf("expected language es, got %s", profile.Language)
	}
}

// Ensure time import is used
var _ = time.Now
var _ = filepath.Join
