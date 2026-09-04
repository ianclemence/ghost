package skills

import (
	"fmt"
	"os"
	"strings"
)

// SkillStatus represents the readiness of a skill.
type SkillStatus string

const (
	StatusReady                 SkillStatus = "ready"
	StatusNeedsUserInput        SkillStatus = "needs_user_input"
	StatusNeedsConfiguration    SkillStatus = "needs_configuration"
	StatusNeedsPermission       SkillStatus = "needs_permission"
	StatusUnavailable           SkillStatus = "unavailable"
	StatusTemporarilyUnavailable SkillStatus = "temporarily_unavailable"
	StatusOffline               SkillStatus = "offline"
)

// SkillReadiness describes what a skill needs before it can execute.
type SkillReadiness struct {
	Status      SkillStatus `json:"status"`
	Requirement string      `json:"requirement,omitempty"`
	Question    string      `json:"question,omitempty"`
	UserAction  string      `json:"user_action,omitempty"`
	Message     string      `json:"message,omitempty"`
}

// CheckReadiness determines if a skill can execute in the current environment.
// It checks prerequisites (binaries), credentials, and user inputs.
func CheckReadiness(skillName, workspace string, providedInputs map[string]string) SkillReadiness {
	// Check if skill is enabled
	loader := NewSkillsLoader(workspace, "", "")
	skills := loader.ListSkills()
	found := false
	var skillPath string
	for _, s := range skills {
		if s.Name == skillName {
			found = true
			skillPath = s.Path
			break
		}
	}
	if !found {
		// Check if it's disabled
		disabledPath := fmt.Sprintf("%s/skills/%s/SKILL.md.disabled", workspace, skillName)
		if _, err := os.Stat(disabledPath); err == nil {
			return SkillReadiness{
				Status:     StatusUnavailable,
				Requirement: "skill_disabled",
				Message:    fmt.Sprintf("The %s skill is currently disabled. Enable it in Ghost's Skills settings to use it.", skillName),
				UserAction: "enable_skill",
			}
		}
		return SkillReadiness{
			Status:     StatusUnavailable,
			Requirement: "skill_not_found",
			Message:    fmt.Sprintf("The %s skill is not installed.", skillName),
		}
	}

	// Product-level readiness first: enabled != configured != ready.
	// Calendar is enabled by default; unconfigured returns a product
	// message (never raw gcalcli errors). This runs before binary checks
	// so users get "connect your calendar" instead of "missing gcalcli".
	if skillName == "calendar" {
		if !isCalendarConfigured() {
			return SkillReadiness{
				Status:      StatusNeedsConfiguration,
				Requirement: "calendar_connection",
				Question:    "Calendar access isn't connected yet. Connect your calendar to let me check your schedule?",
				Message:     "Calendar access isn't connected yet. Connect your calendar in Ghost settings to view your schedule.",
				UserAction:  "connect_calendar",
			}
		}
	}
	if skillName == "homeassistant" {
		if os.Getenv("HASS_URL") == "" || os.Getenv("HASS_TOKEN") == "" {
			return SkillReadiness{
				Status:      StatusNeedsConfiguration,
				Requirement: "homeassistant_connection",
				Message:     "Home Assistant isn't connected yet. Add your Home Assistant URL and token in Ghost settings under Integrations to control devices.",
				UserAction:  "connect_homeassistant",
			}
		}
	}
	// Optional hardware skills get explicit product states, never
	// "command not found".
	if skillName == "camera" || skillName == "hardware" || skillName == "network" || skillName == "mobile" {
		prereqs := parsePrerequisites(skillPath)
		for _, cmd := range prereqs.Commands {
			if !isCommandAvailable(cmd) {
				return SkillReadiness{
					Status:      StatusNeedsConfiguration,
					Requirement: "missing_binary:" + cmd,
					Message:     fmt.Sprintf("The %s feature needs setup (%s not available on this device). Check Ghost settings for setup steps.", skillName, cmd),
					UserAction:  "install_" + cmd,
				}
			}
		}
		// Binaries present but hardware may still be absent; runtime
		// execution will confirm. Fall through to ready.
		return SkillReadiness{Status: StatusReady}
	}

	// Check prerequisites (binaries) - generic path.
	prereqs := parsePrerequisites(skillPath)
	for _, cmd := range prereqs.Commands {
		if !isCommandAvailable(cmd) {
			if IsOptionalSkill(skillName) {
				return SkillReadiness{
					Status:     StatusNeedsConfiguration,
					Requirement: "missing_binary:" + cmd,
					Message:    fmt.Sprintf("The %s skill needs `%s` installed. Run `ghost doctor` for setup instructions.", skillName, cmd),
					UserAction: "install_" + cmd,
				}
			}
			return SkillReadiness{
				Status:     StatusTemporarilyUnavailable,
				Requirement: "missing_binary:" + cmd,
				Message:    fmt.Sprintf("The %s skill is temporarily unavailable — missing `%s`.", skillName, cmd),
			}
		}
	}

	// Skill-specific credential / config checks
	switch skillName {
	case "calendar":
		// Already handled above; double-check oauth here as well.
		if !isCalendarConfigured() {
			return SkillReadiness{
				Status:      StatusNeedsConfiguration,
				Requirement: "calendar_connection",
				Question:    "Calendar access isn't connected yet. Connect your calendar to let me check your schedule?",
				Message:     "Calendar access isn't connected yet. Connect your calendar in Ghost's settings to view your schedule.",
				UserAction:  "connect_calendar",
			}
		}
	case "flight":
		if flight, ok := providedInputs["flight_number"]; !ok || strings.TrimSpace(flight) == "" {
			return SkillReadiness{
				Status:      StatusNeedsUserInput,
				Requirement: "flight_number",
				Question:    "Which flight number should I check? (e.g., TG123 or AA456)",
				UserAction:  "provide_flight_number",
			}
		}
		// Check API key
		if os.Getenv("AVIATION_API_KEY") == "" {
			// Also check workspace .env or config, but for now env only
			// Don't fail hard — let skill try and return API error, but warn
		}
	case "find-nearby", "travel":
		if loc, ok := providedInputs["location"]; !ok || strings.TrimSpace(loc) == "" {
			// Location will be requested via natural clarification, not hard fail
			// Return needs_user_input so caller can ask
			return SkillReadiness{
				Status:      StatusNeedsUserInput,
				Requirement: "location",
				Question:    "Which location should I check?",
				UserAction:  "provide_location",
			}
		}
	case "weather", "aqi":
		// Weather can use device location context if available, otherwise ask
		if loc, ok := providedInputs["location"]; !ok || strings.TrimSpace(loc) == "" {
			// Check if device location available in context — if not, will ask naturally
			// Don't block, let skill try with default
		}
	case "crypto", "currency":
		// These work without config, no input check needed here
	case "spotify":
		// Spotify needs running app, check at runtime
	}

	return SkillReadiness{Status: StatusReady}
}

func isCalendarConfigured() bool {
	// Check for gcalcli oauth token
	// gcalcli stores oauth in ~/.gcalcli_oauth or similar
	// For now, check if gcalcli is available and if oauth file exists
	// If gcalcli not installed, CheckReadiness already returned missing_binary
	// If installed but not oauth'd, gcalcli agenda will fail with auth error — detect via file check
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	// Common locations
	for _, p := range []string{
		home + "/.gcalcli_oauth",
		home + "/.config/gcalcli/oauth",
		"/var/lib/ghost/.gcalcli_oauth",
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	// If no oauth file, assume not configured — let skill attempt and fail gracefully,
	// but we return needs_configuration to give user actionable message
	return false
}
