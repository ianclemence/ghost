package skills

import (
	"fmt"
	"os"
	"path/filepath"
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
				Message:     "Calendar access isn't connected yet. Connect your calendar in Ghost settings under Integrations to view your schedule.",
				UserAction:  "connect_calendar",
			}
		}
	}
	if skillName == "homeassistant" {
		if !HassConfigured() {
			return SkillReadiness{
				Status:      StatusNeedsConfiguration,
				Requirement: "homeassistant_connection",
				Message:     "Home Assistant isn't connected yet. Add your Home Assistant URL and token in Ghost settings under Integrations to control devices.",
				UserAction:  "connect_homeassistant",
			}
		}
	}
	// User-side state skills: binaries may be present but the user's
	// device/service still needs action. These stay amber until the user
	// acts — never green on binaries alone.
	if skillName == "mobile" {
		return SkillReadiness{
			Status:      StatusNeedsConfiguration,
			Requirement: "android_device",
			Message:     "To control your phone, pair an Android device with USB debugging enabled. See Ghost settings under Integrations.",
			UserAction:  "pair_android_device",
		}
	}
	if skillName == "spotify" {
		return SkillReadiness{
			Status:      StatusNeedsConfiguration,
			Requirement: "spotify_app",
			Message:     "Spotify control needs the Spotify desktop app running and logged in on a device Ghost can reach.",
			UserAction:  "start_spotify",
		}
	}
	if skillName == "camera" {
		// ffmpeg present + a video device = ready. No user account needed,
		// so green when the hardware is actually there.
		if !isCommandAvailable("ffmpeg") {
			return SkillReadiness{
				Status:      StatusNeedsConfiguration,
				Requirement: "missing_binary:ffmpeg",
				Message:     "Camera needs ffmpeg installed on this device.",
				UserAction:  "install_ffmpeg",
			}
		}
		if !hasCameraDevice() {
			return SkillReadiness{
				Status:      StatusNeedsConfiguration,
				Requirement: "camera_device",
				Message:     "No camera was detected on this Ghost device. Connect a camera to use this skill.",
				UserAction:  "connect_camera",
			}
		}
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
		// Either credential (primary AviationStack or fallback
		// AeroDataBox) makes the capability ready; both absent is
		// NEEDS_CONFIGURATION handled by the fast-path; never fake data.
		if !FlightConfigured() {
			return SkillReadiness{
				Status:      StatusNeedsConfiguration,
				Requirement: "flight_provider",
				Message:     "Flight tracking isn't connected yet. Add your flight data key in Ghost settings under Integrations, then try again.",
				UserAction:  "connect_flight_provider",
			}
		}
		if flight, ok := providedInputs["flight_number"]; !ok || strings.TrimSpace(flight) == "" {
			return SkillReadiness{
				Status:      StatusNeedsUserInput,
				Requirement: "flight_number",
				Question:    "Which flight number should I check? (e.g., TG123 or AA456)",
				UserAction:  "provide_flight_number",
			}
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

// ListDisabled returns names of installed skills currently disabled via
// SKILL.md.disabled (no SKILL.md present). Used by the chat fast-path so a
// request for a disabled skill gets an honest "it's off" answer instead of
// the model silently improvising around the toggle.
func ListDisabled(workspace string) []string {
	dir := workspace + "/skills"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := dir + "/" + e.Name()
		if _, err := os.Stat(skillDir + "/SKILL.md"); err == nil {
			continue
		}
		if _, err := os.Stat(skillDir + "/SKILL.md.disabled"); err == nil {
			out = append(out, e.Name())
		}
	}
	return out
}

// MatchDisabledSkill reports whether the message is asking for a disabled
// skill, by normalized name match (hyphens ≈ spaces, case-insensitive).
// Returns the skill name or "".
func MatchDisabledSkill(workspace, msg string) string {
	lower := strings.ToLower(msg)
	norm := strings.NewReplacer("-", " ", "_", " ").Replace(lower)
	for _, name := range ListDisabled(workspace) {
		n := strings.ToLower(name)
		if strings.Contains(lower, n) || strings.Contains(norm, strings.ReplaceAll(n, "-", " ")) {
			return name
		}
	}
	return ""
}

// hasCameraDevice reports whether a video capture device exists.
// Pure glob, no exec — safe to call per request.
func hasCameraDevice() bool {
	matches, _ := filepath.Glob("/dev/video*")
	return len(matches) > 0
}

// CameraCheck is the shared camera readiness used by both the Skills list
// (via CheckReadiness) and the Integrations section, so the two screens
// can never disagree.
type CameraState struct {
	Available bool
	Detail    string
}

// CameraCheck reports ffmpeg + device presence.
func CameraCheck() CameraState {
	if !isCommandAvailable("ffmpeg") {
		return CameraState{Detail: "ffmpeg not installed"}
	}
	if !hasCameraDevice() {
		return CameraState{Detail: "no camera detected"}
	}
	return CameraState{Available: true, Detail: "camera detected"}
}

func isCalendarConfigured() bool {
	// Single source of truth lives in calendar_oauth.go (service-owned
	// config dir first, legacy home paths for migration).
	return CalendarCheck().Connected
}
