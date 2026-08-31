package skills

// Curated skill classification.
//
// Ghost ships a broad set of bundled skills. Many simply work out of the box
// (they only need the network or a tool Ghost already has), while others depend
// on a local binary, hardware, or an external service the user must set up —
// git, tmux, adb, ffmpeg, i2c-tools, a Home Assistant instance, and so on.
//
// For a personal, mainstream install we surface the two tiers differently:
//   - core skills are the zero-setup, day-to-day ones. If one is missing a
//     prerequisite, diagnostics should say so.
//   - optional skills are "needs setup". They are never held against the user
//     in diagnostics (a normal install doesn't ship git/adb/ffmpeg), and the
//     console labels them so the user knows why they aren't immediately useful.
//
// The list is intentionally a curated map of bundled, tool-dependent skill
// names. Extra skills the user installs are treated as core (unknown == core)
// so Ghost surfaces their real needs.

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
