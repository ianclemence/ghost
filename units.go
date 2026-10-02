// Package ghostroot carries the systemd unit templates inside the binary.
//
// An update has to refresh the units from the templates of the release being
// installed, not from a git checkout that may be older than that release (or
// absent), and not from the copy of `ghost` that happens to be running the
// update. The new binary prints its own templates (`ghost __unit <name>`).
package ghostroot

import "embed"

//go:embed ghost.service.template ghost-web.service.template
var unitFS embed.FS

// UnitTemplate returns the template for "ghost" or "ghost-web".
func UnitTemplate(name string) (string, bool) {
	switch name {
	case "ghost", "ghost-web":
		b, err := unitFS.ReadFile(name + ".service.template")
		return string(b), err == nil
	}
	return "", false
}
