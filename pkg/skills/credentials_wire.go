package skills

import "github.com/ianclemence/ghost/pkg/credentials"

// Wire the calendar credential hooks into the credential boundary. The
// calendar integration lives here; the Vault owns credential lifecycle.
// This keeps credential storage ownership in pkg/credentials without a
// package cycle.
func init() {
	credentials.CalendarConnected = func() bool {
		return CalendarWebStatus().Connected || CalendarCheck().Connected
	}
	credentials.CalendarWebDisconnectFn = CalendarWebDisconnect
	credentials.CalendarDisconnectFn = CalendarDisconnect
}
