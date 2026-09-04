package skills

import (
	"strings"
	"time"
)

// Capability declares the generic execution contract for a skill.
// It replaces per-skill prompt hacks with a single mechanism:
//
//	Skill -> declares Capability -> Router selects -> Runtime enforces
//	allowed path -> Tool executes -> Result validated -> Model responds.
//
// Every skill gets a Capability (explicit frontmatter or registry default).
// Once Ghost commits to a capability, the model is NOT free to wander into
// unrelated tools just because the first API response was poor.
type Capability struct {
	// ID is the stable capability id, e.g. "weather.current".
	ID string `json:"id"`
	// Skill is the owning skill name, e.g. "weather".
	Skill string `json:"skill"`
	// RequiredInput lists input keys that must be present (e.g. "location").
	RequiredInput []string `json:"required_input,omitempty"`
	// OptionalInput lists inputs that improve results but are not blocking.
	OptionalInput []string `json:"optional_input,omitempty"`
	// AllowedTools is the closed execution path once committed.
	// Empty means "no restriction" (local/meta skills).
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// Primary describes the authoritative first attempt (human readable).
	Primary string `json:"primary,omitempty"`
	// Fallback describes the single bounded fallback.
	Fallback string `json:"fallback,omitempty"`
	// MaxAttempts bounds exec/API attempts (primary + fallback).
	MaxAttempts int `json:"max_attempts,omitempty"`
	// Timeout bounds a single attempt.
	Timeout time.Duration `json:"timeout,omitempty"`
	// NetworkRequired marks external API skills.
	NetworkRequired bool `json:"network_required,omitempty"`
	// Deterministic marks local ops that should bypass the LLM when possible
	// (e.g. shopping.add, reminder.create).
	Deterministic bool `json:"deterministic,omitempty"`
}

// capabilityRegistry is the code-defined default. Frontmatter `capability:`
// blocks in SKILL.md override these when present (see LoadCapability).
var capabilityRegistry = map[string]Capability{
	"weather":      {ID: "weather.current", Skill: "weather", RequiredInput: []string{"location"}, AllowedTools: []string{"read_file", "exec"}, Primary: "wttr.in via exec curl", Fallback: "open-meteo via exec curl", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true},
	"aqi":          {ID: "aqi.current", Skill: "aqi", RequiredInput: []string{"location"}, AllowedTools: []string{"read_file", "exec"}, Primary: "open-meteo air-quality via exec", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true},
	"currency":     {ID: "currency.convert", Skill: "currency", AllowedTools: []string{"read_file", "exec"}, Primary: "open.er-api via exec/python", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true},
	"crypto":       {ID: "crypto.price", Skill: "crypto", AllowedTools: []string{"read_file", "exec"}, Primary: "coingecko via exec curl", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true},
	"recipe":       {ID: "recipe.search", Skill: "recipe", AllowedTools: []string{"read_file", "exec"}, Primary: "themealdb via exec curl", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true},
	"flight":       {ID: "flight.status", Skill: "flight", RequiredInput: []string{"flight_number"}, AllowedTools: []string{"read_file", "exec"}, Primary: "aviationstack via exec curl", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true},
	"find-nearby":  {ID: "nearby.search", Skill: "find-nearby", RequiredInput: []string{"location"}, AllowedTools: []string{"read_file", "exec"}, Primary: "nominatim/overpass via exec", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true},
	"travel":       {ID: "travel.route", Skill: "travel", RequiredInput: []string{"origin", "destination"}, AllowedTools: []string{"read_file", "exec"}, Primary: "osrm via exec curl", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true},
	"scraper":      {ID: "scraper.fetch", Skill: "scraper", RequiredInput: []string{"url"}, AllowedTools: []string{"read_file", "exec", "web_fetch"}, Primary: "jina reader", Fallback: "direct fetch", MaxAttempts: 2, Timeout: 15 * time.Second, NetworkRequired: true},
	"calendar":     {ID: "calendar.read", Skill: "calendar", AllowedTools: []string{"read_file", "exec"}, Primary: "gcalcli agenda via exec", Fallback: "none", MaxAttempts: 1, Timeout: 15 * time.Second},
	"shopping":     {ID: "shopping.add", Skill: "shopping", AllowedTools: []string{"read_file", "write_file", "append_file", "edit_file"}, MaxAttempts: 1, Deterministic: true},
	"reminders":    {ID: "reminder.create", Skill: "reminders", AllowedTools: []string{"read_file", "append_file", "write_file", "schedule"}, MaxAttempts: 1, Deterministic: true},
	"daily-briefing": {ID: "briefing.daily", Skill: "daily-briefing", AllowedTools: []string{"read_file", "exec", "write_file", "append_file"}, MaxAttempts: 3, Timeout: 15 * time.Second},
	"hardware":     {ID: "hardware.read", Skill: "hardware", AllowedTools: []string{"read_file", "exec"}, Primary: "i2c/spi via exec", MaxAttempts: 1},
	"homeassistant": {ID: "hass.control", Skill: "homeassistant", AllowedTools: []string{"read_file", "exec"}, Primary: "HASS API via exec curl", MaxAttempts: 2, Timeout: 10 * time.Second},
}

// GetCapability returns the generic contract for a skill.
// Unknown skills get an unrestricted capability (no wandering guard) so
// user-installed skills stay usable without code changes.
func GetCapability(skill string) Capability {
	skill = strings.TrimSpace(strings.ToLower(skill))
	if cap, ok := capabilityRegistry[skill]; ok {
		return cap
	}
	return Capability{ID: skill + ".default", Skill: skill, MaxAttempts: 2}
}

// Allows reports whether tool is on the capability's execution path.
// read_file is always allowed (skill reading itself). An empty AllowedTools
// means unrestricted.
func (c Capability) Allows(tool string) bool {
	if tool == "read_file" {
		return true
	}
	if len(c.AllowedTools) == 0 {
		return true
	}
	for _, t := range c.AllowedTools {
		if t == tool {
			return true
		}
	}
	return false
}

// ValidateResult reports whether an exec/API result is usable.
// It rejects empty output and common failure markers so the runtime can
// trigger the single bounded fallback instead of letting the model wander.
func (c Capability) ValidateResult(content string) bool {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < 8 {
		return false
	}
	lower := strings.ToLower(trimmed)
	// Explicit failure markers from tools / fetchers.
	for _, marker := range []string{
		"failed to read file", "command not found", "no such file",
		"metadata only", "without visible content", "isn't rendering",
		"timed out", "context canceled", "no available providers",
		"401", "unauthorized", "invalid api key", "no oauth token",
		"(no output)", "tool \"exec\" not found", "tool `exec`",
	} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

// CleanFailure returns the user-facing message when primary + fallback both
// fail. It never exposes internals.
func (c Capability) CleanFailure() string {
	switch c.Skill {
	case "weather":
		return "I couldn't retrieve the latest weather data right now. Please try again in a bit."
	case "aqi":
		return "I couldn't retrieve the air quality data right now. Please try again in a bit."
	case "currency":
		return "I couldn't fetch the latest exchange rate right now. Please try again in a bit."
	case "crypto":
		return "I couldn't fetch the latest crypto price right now. Please try again in a bit."
	case "recipe":
		return "I couldn't find a recipe right now. Please try again with a dish name."
	case "flight":
		return "I couldn't check the flight status right now. Please verify the flight number and try again."
	case "find-nearby", "travel":
		return "I couldn't search nearby places right now. Please check the location and try again."
	case "calendar":
		return "Calendar access isn't connected yet. Connect your calendar in Ghost settings to view your schedule."
	default:
		return "I couldn't complete that right now. Please try again in a bit."
	}
}
