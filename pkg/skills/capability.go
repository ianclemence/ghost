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
	// Risk declares what the capability can do: read_only, low_risk,
	// consequential, high_impact. The permission broker enforces it;
	// the model never classifies risk itself.
	Risk string `json:"risk,omitempty"`
	// Deterministic marks local ops that should bypass the LLM when possible
	// (e.g. shopping.add, reminder.create).
	Deterministic bool `json:"deterministic,omitempty"`
}

// capabilityRegistry is the code-defined default. Frontmatter `capability:`
// blocks in SKILL.md override these when present (see LoadCapability).
var capabilityRegistry = map[string]Capability{
	// Provider-backed tools are the deterministic primary path; exec
	// remains allowed so skill scripts stay usable as fallback. The
	// SKILL.md of each migrated skill instructs the model to call the
	// tool first.
	"weather":     {ID: "weather.current", Skill: "weather", RequiredInput: []string{"location"}, AllowedTools: []string{"weather_now", "read_file", "exec"}, Primary: "weather_now tool (open-meteo, openweather fallback)", Fallback: "wttr.in via exec curl", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	"aqi":         {ID: "aqi.current", Skill: "aqi", RequiredInput: []string{"location"}, AllowedTools: []string{"aqi_now", "read_file", "exec"}, Primary: "aqi_now tool (open-meteo air-quality)", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	"currency":    {ID: "currency.convert", Skill: "currency", AllowedTools: []string{"currency_convert", "read_file", "exec"}, Primary: "currency_convert tool (er-api, frankfurter fallback)", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	"crypto":      {ID: "crypto.price", Skill: "crypto", AllowedTools: []string{"crypto_price", "read_file", "exec"}, Primary: "crypto_price tool (coingecko, coinbase fallback)", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	"recipe":      {ID: "recipe.search", Skill: "recipe", AllowedTools: []string{"read_file", "exec"}, Primary: "themealdb via exec curl", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	"flight":      {ID: "flight.status", Skill: "flight", RequiredInput: []string{"flight_number"}, AllowedTools: []string{"flight_status", "read_file", "exec"}, Primary: "flight_status tool (aviationstack, aerodatabox fallback)", Fallback: "aviationstack via exec curl", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	"find-nearby": {ID: "nearby.search", Skill: "find-nearby", RequiredInput: []string{"location"}, AllowedTools: []string{"places_nearby", "read_file", "exec"}, Primary: "places_nearby tool (overpass + mirror)", Fallback: "nominatim/overpass via exec", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	"travel":      {ID: "travel.route", Skill: "travel", RequiredInput: []string{"origin", "destination"}, AllowedTools: []string{"read_file", "exec"}, Primary: "osrm via exec curl", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	"scraper":     {ID: "scraper.fetch", Skill: "scraper", RequiredInput: []string{"url"}, AllowedTools: []string{"read_file", "exec", "web_fetch"}, Primary: "jina reader", Fallback: "direct fetch", MaxAttempts: 2, Timeout: 15 * time.Second, NetworkRequired: true, Risk: "read_only"},

	// Declared write capabilities: narrow grant targets. Runtime gating
	// for the calendar skill uses the read contract (same consequential
	// risk); grants may name these IDs to scope standing permission.
	"calendar-create": {ID: "calendar.create", Skill: "calendar", AllowedTools: []string{"read_file", "exec"}, Primary: "calendar add via exec", Fallback: "none", MaxAttempts: 1, Timeout: 15 * time.Second, Risk: "consequential"},
	"calendar-update": {ID: "calendar.update_event", Skill: "calendar", AllowedTools: []string{"read_file", "exec"}, Primary: "calendar edit via exec", Fallback: "none", MaxAttempts: 1, Timeout: 15 * time.Second, Risk: "consequential"},
	"calendar":        {ID: "calendar.read", Skill: "calendar", AllowedTools: []string{"read_file", "exec"}, Primary: "gcalcli agenda via exec", Fallback: "none", MaxAttempts: 1, Timeout: 15 * time.Second, Risk: "consequential"},
	"shopping":        {ID: "shopping.add", Skill: "shopping", AllowedTools: []string{"read_file", "write_file", "append_file", "edit_file"}, MaxAttempts: 1, Deterministic: true, Risk: "low_risk"},
	"reminders":       {ID: "reminder.create", Skill: "reminders", AllowedTools: []string{"read_file", "append_file", "write_file", "schedule"}, MaxAttempts: 1, Deterministic: true, Risk: "low_risk"},
	"daily-briefing":  {ID: "briefing.daily", Skill: "daily-briefing", AllowedTools: []string{"read_file", "exec", "write_file", "append_file"}, MaxAttempts: 3, Timeout: 15 * time.Second, Risk: "low_risk"},
	"hardware":        {ID: "hardware.read", Skill: "hardware", AllowedTools: []string{"read_file", "exec"}, Primary: "i2c/spi via exec", MaxAttempts: 1, Risk: "consequential"},
	"homeassistant":   {ID: "hass.control", Skill: "homeassistant", AllowedTools: []string{"hass", "read_file", "exec"}, Primary: "hass tool (device REST API)", Fallback: "HASS API via exec curl", MaxAttempts: 2, Timeout: 10 * time.Second, Risk: "consequential"},
	"unit-converter":  {ID: "units.convert", Skill: "unit-converter", AllowedTools: []string{"read_file", "exec"}, Primary: "local python convert.py", Fallback: "none", MaxAttempts: 1, Timeout: 10 * time.Second, Risk: "read_only"},
	"world-clock":     {ID: "time.convert", Skill: "world-clock", AllowedTools: []string{"read_file", "exec"}, Primary: "local python clock.py zoneinfo", Fallback: "none", MaxAttempts: 1, Timeout: 10 * time.Second, Risk: "read_only"},
	"calculator":      {ID: "math.evaluate", Skill: "calculator", AllowedTools: []string{"read_file", "exec"}, Primary: "local python calc.py", Fallback: "none", MaxAttempts: 1, Timeout: 10 * time.Second, Deterministic: true, Risk: "read_only"},
	"dictionary":      {ID: "dict.define", Skill: "dictionary", RequiredInput: []string{"word"}, AllowedTools: []string{"read_file", "exec"}, Primary: "dictionaryapi.dev via exec curl", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	"translate":       {ID: "translate.phrase", Skill: "translate", RequiredInput: []string{"text"}, AllowedTools: []string{"read_file", "exec"}, Primary: "mymemory via exec curl", Fallback: "none", MaxAttempts: 2, Timeout: 10 * time.Second, NetworkRequired: true, Risk: "read_only"},
	// Standing-grant targets for core (non-skill) capabilities. Declared
	// here so the runtime — never the model — defines what can be granted.
	"memory":   {ID: "memory.write", Skill: "memory", AllowedTools: []string{"remember"}, Primary: "remember tool", Fallback: "none", MaxAttempts: 1, Risk: "low_risk"},
	"telegram": {ID: "telegram.send", Skill: "telegram", AllowedTools: []string{"message"}, Primary: "message tool via channel", Fallback: "none", MaxAttempts: 1, Risk: "consequential"},
	"timer":    {ID: "timer.countdown", Skill: "timer", AllowedTools: []string{"read_file", "schedule"}, Primary: "schedule one-shot", Fallback: "none", MaxAttempts: 1, Deterministic: true, Risk: "low_risk"},
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

// HasCapability reports whether id is a runtime-declared capability.
// Standing grants may only reference declared capabilities — the model
// can never authorize a capability the runtime doesn't know.
func HasCapability(id string) bool {
	for _, cap := range capabilityRegistry {
		if cap.ID == id {
			return true
		}
	}
	return false
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
	case "unit-converter":
		return "I couldn't convert those units. Please check the unit names and try again."
	case "world-clock":
		return "I couldn't convert that time. Please check the city names and try again."
	case "calculator":
		return "I couldn't evaluate that. Please rephrase as plain math."
	case "dictionary":
		return "I couldn't find a definition for that word."
	case "translate":
		return "I couldn't translate that right now. Please try again with a shorter phrase."
	case "timer":
		return "I couldn't set that timer. Please give a duration like 10 minutes."
	default:
		return "I couldn't complete that right now. Please try again in a bit."
	}
}
