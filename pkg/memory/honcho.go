package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/logger"
)

// UserProfile represents a user's learned profile and preferences.
type UserProfile struct {
	UserID        string            `json:"user_id"`
	DisplayName   string            `json:"display_name,omitempty"`
	Preferences   map[string]string `json:"preferences"`
	Topics        []string          `json:"topics"`           // Topics the user discusses frequently
	Communication string            `json:"communication"`    // "formal", "casual", "technical"
	Language      string            `json:"language"`         // Primary language
	FirstSeen     time.Time         `json:"first_seen"`
	LastSeen      time.Time         `json:"last_seen"`
	Interactions  int               `json:"interactions"`
	Facts         []UserFact        `json:"facts"`
}

// UserFact represents a specific fact learned about a user.
type UserFact struct {
	Fact       string    `json:"fact"`
	Source     string    `json:"source"`     // Where this fact was learned
	Confidence float64   `json:"confidence"` // 0.0 to 1.0
	CreatedAt  time.Time `json:"created_at"`
}

// HonchoConfig holds configuration for Honcho integration.
type HonchoConfig struct {
	Enabled     bool   `json:"enabled"`
	APIKey      string `json:"api_key,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	BaseURL     string `json:"base_url,omitempty"`
	AutoProfile bool   `json:"auto_profile"` // Auto-extract user facts from conversations
}

// HonchoStore manages user profiles and memory using Honcho-style local storage.
// Honcho is an open-source user memory backend; this implementation provides
// local-only storage with the same data model for offline use.
type HonchoStore struct {
	config      HonchoConfig
	profilesDir string
	profiles    map[string]*UserProfile
	mu          sync.RWMutex
}

// NewHonchoStore creates a new HonchoStore.
func NewHonchoStore(cfg config.Config, workspace string) *HonchoStore {
	profilesDir := filepath.Join(workspace, "memory", "profiles")
	os.MkdirAll(profilesDir, 0755)

	honchoCfg := HonchoConfig{
		Enabled:     cfg.Skills.Honcho.Enabled,
		APIKey:      cfg.Skills.Honcho.APIKey,
		ProjectID:   cfg.Skills.Honcho.ProjectID,
		AutoProfile: true,
	}

	store := &HonchoStore{
		config:      honchoCfg,
		profilesDir: profilesDir,
		profiles:    make(map[string]*UserProfile),
	}

	// Load existing profiles
	store.loadAllProfiles()

	return store
}

// GetProfile retrieves or creates a user profile.
func (h *HonchoStore) GetProfile(ctx context.Context, userID string) (*UserProfile, error) {
	if !h.config.Enabled {
		return nil, fmt.Errorf("honcho is not enabled")
	}

	h.mu.RLock()
	profile, ok := h.profiles[userID]
	h.mu.RUnlock()

	if ok {
		profile.LastSeen = time.Now()
		profile.Interactions++
		h.saveProfile(profile)
		return profile, nil
	}

	// Create new profile
	profile = &UserProfile{
		UserID:        userID,
		Preferences:   make(map[string]string),
		Topics:        make([]string, 0),
		Communication: "casual",
		Language:      "en",
		FirstSeen:     time.Now(),
		LastSeen:      time.Now(),
		Interactions:  1,
		Facts:         make([]UserFact, 0),
	}

	h.mu.Lock()
	h.profiles[userID] = profile
	h.mu.Unlock()

	h.saveProfile(profile)

	logger.InfoCF("honcho", "Created new user profile", map[string]interface{}{
		"user_id": userID,
	})

	return profile, nil
}

// UpdateProfile updates a user profile with new information.
func (h *HonchoStore) UpdateProfile(ctx context.Context, userID string, updates map[string]interface{}) error {
	if !h.config.Enabled {
		return fmt.Errorf("honcho is not enabled")
	}

	profile, err := h.GetProfile(ctx, userID)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if v, ok := updates["display_name"].(string); ok {
		profile.DisplayName = v
	}
	if v, ok := updates["communication"].(string); ok {
		profile.Communication = v
	}
	if v, ok := updates["language"].(string); ok {
		profile.Language = v
	}
	if v, ok := updates["preferences"].(map[string]interface{}); ok {
		for k, val := range v {
			profile.Preferences[k] = fmt.Sprintf("%v", val)
		}
	}

	h.saveProfile(profile)
	return nil
}

// AddFact adds a fact about a user.
func (h *HonchoStore) AddFact(ctx context.Context, userID, fact, source string, confidence float64) error {
	if !h.config.Enabled {
		return fmt.Errorf("honcho is not enabled")
	}

	profile, err := h.GetProfile(ctx, userID)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check for duplicate facts
	for _, f := range profile.Facts {
		if f.Fact == fact {
			return nil // Already known
		}
	}

	profile.Facts = append(profile.Facts, UserFact{
		Fact:       fact,
		Source:     source,
		Confidence: confidence,
		CreatedAt:  time.Now(),
	})

	h.saveProfile(profile)
	return nil
}

// AddTopic adds a topic of interest for a user.
func (h *HonchoStore) AddTopic(ctx context.Context, userID, topic string) error {
	if !h.config.Enabled {
		return fmt.Errorf("honcho is not enabled")
	}

	profile, err := h.GetProfile(ctx, userID)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if topic already exists
	for _, t := range profile.Topics {
		if t == topic {
			return nil
		}
	}

	profile.Topics = append(profile.Topics, topic)
	h.saveProfile(profile)
	return nil
}

// GetContext returns a formatted string of user context for the agent prompt.
func (h *HonchoStore) GetContext(ctx context.Context, userID string) string {
	if !h.config.Enabled {
		return ""
	}

	profile, err := h.GetProfile(ctx, userID)
	if err != nil {
		return ""
	}

	var parts []string

	if profile.DisplayName != "" {
		parts = append(parts, fmt.Sprintf("User: %s", profile.DisplayName))
	}

	if len(profile.Preferences) > 0 {
		prefs := ""
		for k, v := range profile.Preferences {
			prefs += fmt.Sprintf("  - %s: %s\n", k, v)
		}
		parts = append(parts, fmt.Sprintf("Preferences:\n%s", prefs))
	}

	if len(profile.Topics) > 0 {
		parts = append(parts, fmt.Sprintf("Interests: %v", profile.Topics))
	}

	if profile.Communication != "" {
		parts = append(parts, fmt.Sprintf("Communication style: %s", profile.Communication))
	}

	if len(profile.Facts) > 0 {
		facts := ""
		for _, f := range profile.Facts {
			facts += fmt.Sprintf("  - %s\n", f.Fact)
		}
		parts = append(parts, fmt.Sprintf("Known facts:\n%s", facts))
	}

	if len(parts) == 0 {
		return ""
	}

	result := "# User Profile\n\n"
	for _, p := range parts {
		result += p + "\n\n"
	}
	return result
}

// ListProfiles returns all stored user profiles.
func (h *HonchoStore) ListProfiles() []*UserProfile {
	h.mu.RLock()
	defer h.mu.RUnlock()

	profiles := make([]*UserProfile, 0, len(h.profiles))
	for _, p := range h.profiles {
		profiles = append(profiles, p)
	}
	return profiles
}

// IsEnabled returns whether Honcho is configured and enabled.
func (h *HonchoStore) IsEnabled() bool {
	return h.config.Enabled
}

func (h *HonchoStore) saveProfile(profile *UserProfile) {
	filePath := filepath.Join(h.profilesDir, profile.UserID+".json")
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		logger.ErrorCF("honcho", "Failed to marshal profile", map[string]interface{}{
			"user_id": profile.UserID,
			"error":   err.Error(),
		})
		return
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		logger.ErrorCF("honcho", "Failed to save profile", map[string]interface{}{
			"user_id": profile.UserID,
			"error":   err.Error(),
		})
	}
}

func (h *HonchoStore) loadAllProfiles() {
	entries, err := os.ReadDir(h.profilesDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(h.profilesDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var profile UserProfile
		if err := json.Unmarshal(data, &profile); err != nil {
			continue
		}

		h.mu.Lock()
		h.profiles[profile.UserID] = &profile
		h.mu.Unlock()
	}
}
