package skills

// Curated skill classification.
//
// Ghost ships a broad set of bundled skills across three product tiers.
// Only Tiers 1-2 are ever live in the model prompt; Tier 3 is contributor
// documentation and never enters the runtime.
//
//   Tier 1 — core (default): zero-setup, day-to-day capabilities a normal
//     person asks for naturally (weather, shopping, reminders, recipes).
//     If one is missing a prerequisite, diagnostics surface it.
//   Tier 2 — optional packs (opt-in via Skills settings): need a binary,
//     hardware, or external service (git, tmux, adb, ffmpeg, i2c-tools,
//     Home Assistant, flight key, Spotify). They report needs_setup with a
//     product message and never fail as "command not found". The console
//     labels them so the user knows why they aren't immediately useful.
//   Tier 3 — dev/docs only (never live): nested containers (github/*,
//     productivity/*, research/*, software-development/*, email/himalaya)
//     and flat workflows/*.md templates. The loader only scans 1-level
//     skills/<name>/SKILL.md, so these never reach ListSkills or the model.
//     They live in the repo as contributor reference and automation
//     templates (workflows belong in Automations UI, not the prompt).
//
// The list below marks Tier 2. Extra skills the user installs are treated
// as core (unknown == core) so Ghost surfaces their real needs.

// curatedOptional names bundled skills that require setup (a local tool,
// hardware, or an external service) and therefore should not produce dependency
// warnings on a clean install.
var curatedOptional = map[string]bool{
	"git":                  true,
	"github":               true,
	"tmux":                 true,
	"hardware":             true,
	"mobile":               true,
	"camera":               true,
	"process-manager":      true,
	"ascii-art":            true,
	"speedtest":            true,
	"network":              true,
	"system":               true,
	"skill-creator":        true,
	"homeassistant":        true,
	"flight":               true,
	"spotify":              true,
	"email":                true,
	"productivity":         true,
	"research":             true,
	"software-development": true,
	"workflows":            true,
	"internet-reading":     true,
	"document-convert":     true,
}

// IsOptionalSkill reports whether a skill requires setup and should be treated
// as optional. Unknown skills are treated as core so user-installed skills stay
// visible and honest.
func IsOptionalSkill(name string) bool {
	return curatedOptional[name]
}

// IsCoreSkill reports whether a skill is a zero-setup, day-to-day capability
// whose missing prerequisites diagnostics should surface.
func IsCoreSkill(name string) bool {
	return !curatedOptional[name]
}
