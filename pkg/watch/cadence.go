package watch

import "time"

// Polling cadence: purely a function of how far away the watched event is.
// Nothing here consults a model — a flight three days out is checked every
// four hours, a flight in ninety minutes every ten minutes, and the clock
// alone decides when the next check happens.
//
// The bounds keep a busy watchlist cheap and a quiet one quiet:
//
//	> 48h out   → 12h      (nothing moves fast this far out)
//	12h–48h     → 4h
//	3h–12h      → 1h
//	1h–3h       → 20m
//	< 1h        → 10m      (event is imminent; a gate change matters now)
const (
	CadenceFar      = 12 * time.Hour
	CadenceDay      = 4 * time.Hour
	CadenceHours    = time.Hour
	CadenceNear     = 20 * time.Minute
	CadenceImminent = 10 * time.Minute
	// CadenceNoEvent is the cadence for a watch whose owner gave no time
	// (a delivery with no date): frequent enough to be useful, sparse
	// enough to stay cheap.
	CadenceNoEvent = 4 * time.Hour
	// PageCadenceFast is for pages where what the owner waits for goes
	// quickly (an appointment slot, tickets, a restock).
	PageCadenceFast = time.Hour
	// PageHorizon is how long a page is watched.
	PageHorizon = 30 * 24 * time.Hour
)

// Cadence returns how long to wait before the next probe of a watch whose
// event lands at eventAt (nil = no stated time). now is only used to measure
// distance, so callers can test historical instants.
func Cadence(eventAt *time.Time, now time.Time) time.Duration {
	if eventAt == nil {
		return CadenceNoEvent
	}
	until := eventAt.Sub(now)
	switch {
	case until > 48*time.Hour:
		return CadenceFar
	case until > 12*time.Hour:
		return CadenceDay
	case until > 3*time.Hour:
		return CadenceHours
	case until > time.Hour:
		return CadenceNear
	default:
		return CadenceImminent
	}
}

// NextCheckAt returns the instant of the watch's next probe. A watch that
// already passed its event stays on the finest cadence until it expires or
// completes — the window right after the event is when a change (landed,
// cancelled, gate called) actually reaches the owner.
func NextCheckAt(w Watch, now time.Time) time.Time {
	every := Cadence(w.EventAt, now)
	if w.Kind == KindPage && w.Rule != nil && (w.Rule.Phrase != "" || w.Rule.Want == "stock") {
		// A slot, a ticket or a restock goes quickly: hourly.
		every = PageCadenceFast
	}
	next := now.Add(every)
	if w.ExpiresAt != nil && next.After(*w.ExpiresAt) {
		// Never schedule a probe past the horizon; Sweep will expire it.
		return *w.ExpiresAt
	}
	return next
}
