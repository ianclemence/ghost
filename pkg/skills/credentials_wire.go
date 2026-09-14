package skills

import "github.com/ianclemence/ghost/pkg/credentials"

// Wire the calendar + Gmail credential hooks into the credential
// boundary. The integrations live here; the Vault owns credential
// lifecycle. This keeps credential storage ownership in pkg/credentials
// without a package cycle.
func init() {
	credentials.CalendarConnected = func() bool {
		return CalendarWebStatus().Connected || CalendarCheck().Connected
	}
	credentials.CalendarWebDisconnectFn = CalendarWebDisconnect
	credentials.CalendarDisconnectFn = CalendarDisconnect
	credentials.GmailConnected = func() bool {
		return GmailWebStatus().Connected
	}
	credentials.GmailDisconnectFn = GmailWebDisconnect
	credentials.OutlookConnected = func() bool {
		return OutlookWebStatus().Connected
	}
	credentials.OutlookDisconnectFn = OutlookWebDisconnect
	credentials.SpotifyConnected = func() bool {
		return SpotifyWebStatus().Connected
	}
	credentials.SpotifyDisconnectFn = SpotifyWebDisconnect
}
