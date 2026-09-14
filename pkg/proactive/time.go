package proactive

import (
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/tools"
)

// UserLocation resolves the user's timezone for quiet-hours evaluation:
// structured memory (a current user fact mentioning timezone) first, then
// the device timezone, then UTC. Per product decision the user profile
// wins over the box.
func UserLocation(store *personalcontext.Store) *time.Location {
	if store != nil {
		for _, e := range store.Current() {
			if e.Subject != "user" || e.Status != personalcontext.StatusCurrent {
				continue
			}
			if !strings.Contains(e.Predicate, "timezone") {
				continue
			}
			val := strings.Trim(strings.TrimSpace(personalcontext.Value(e)), `"`)
			if tz := tools.ValidateTimezone(val); tz != "" {
				if l, err := time.LoadLocation(tz); err == nil {
					return l
				}
			}
		}
	}
	return tools.DeviceLocation()
}
