package golden

import "fmt"

// The watch suite: 25 conversations covering the whole selective
// proactive-watch contract — automatic creation from the owner's own
// words, every refusal guard, owner control (list/stop/dedupe), and the
// deterministic background lifecycle (poll → diff → notice → dedupe →
// failure → expiry → budget/quiet gates).
//
// Each case has two halves:
//
//	conversation  — the real turn path (fast path or model + extraction)
//	               creates or honestly refuses the durable watch.
//	WatchScript   — deterministic background steps after the turns: due
//	               checks, changed world state, injected failures, time
//	               passing. Every step runs the PRODUCTION poll path
//	               (PollWatches → probe → diff → noticer); no step ever
//	               calls a model, and the golden runner never starts the
//	               proactive eval loop, so polling happens exactly when
//	               the script asks — race-free by construction.
//
// Assertions read runtime state only: the watch ledger (kind, entity,
// status, fingerprints, evidence count), canonical watch.* event counts,
// and the held-notice outbox. Never the model's narration.

// watchDefaultPrefs pins quiet hours OFF for watch cases that don't set
// their own policy, so delivery (and outbox) behaviour is identical at any
// hour the suite runs. Product default is 23:00–08:00.
const watchDefaultPrefs = "# Golden watch case: quiet hours off so delivery is hour-independent.\n`quiet_hours: 00:00 - 00:00`\n"

// watchPrefs builds a PROACTIVE_PREFERENCES.md body from bare "key: value"
// lines — the exact backticked shape proactive.Load parses.
func watchPrefs(lines ...string) string {
	body := "# Golden watch case preferences\n"
	for _, l := range lines {
		body += fmt.Sprintf("`%s`\n", l)
	}
	return body
}

// watchConversations returns the CatWatches cases (appended to Suite()).
func watchConversations() []Conversation {
	return []Conversation{
		// ---------- A. Automatic creation (model + extraction) ----------
		{
			ID: "watch-01", Category: CatWatches,
			Title:    "Automatic watch from the owner's own words",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("I'm flying BA123 tomorrow.")),
			Expect: Expect{
				Watches:       []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "active"}},
				WatchCount:    1,
				WatchCountSet: true,
				EventCounts:   map[string]int{"watch.created": 1},
			},
		},
		{
			ID: "watch-02", Category: CatWatches,
			Title:    "Explicit track request creates and confirms from the store",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow")),
			Expect: Expect{
				LastResponseContains: []string{"watching flight ba123"},
				Watches:              []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "active"}},
				WatchCount:           1,
				WatchCountSet:        true,
				EventCounts:          map[string]int{"watch.created": 1},
			},
		},
		// ---------- B. Refusal guards (never create what was not asked) ----------
		{
			ID: "watch-03", Category: CatWatches,
			Title:    "A reminder request must not fork into a watch",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow and remind me to check in")),
			Expect: Expect{
				LastResponseContains: []string{"tell what to watch"},
				NoWatches:            true,
				EventCounts:          map[string]int{"watch.created": 0},
			},
		},
		{
			ID: "watch-04", Category: CatWatches,
			Title:    "A recurring routine must not become a watch",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my dentist appointment on Thursdays")),
			Expect: Expect{
				LastResponseContains: []string{"tell what to watch"},
				NoWatches:            true,
				EventCounts:          map[string]int{"watch.created": 0},
			},
		},
		{
			ID: "watch-05", Category: CatWatches,
			Title:    "Somebody else's flight is not the owner's watch",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track her flight BA123 tomorrow")),
			Expect: Expect{
				LastResponseContains: []string{"tell what to watch"},
				NoWatches:            true,
				EventCounts:          map[string]int{"watch.created": 0},
			},
		},
		{
			ID: "watch-06", Category: CatWatches,
			Title:    "Stopping nothing says so honestly",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("stop watching my flight")),
			Expect: Expect{
				LastResponseContains: []string{"not watching anything"},
				NoWatches:            true,
				EventCounts:          map[string]int{"watch.created": 0, "watch.cancelled": 0},
			},
		},
		{
			ID: "watch-07", Category: CatWatches,
			Title:    "A past event is history, not a watch",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("My flight BA123 yesterday was a mess.")),
			Expect: Expect{
				NoWatches:   true,
				EventCounts: map[string]int{"watch.created": 0},
			},
		},
		{
			ID: "watch-08", Category: CatWatches,
			Title:    "An undated event is a guess, not a watch",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("I fly BA123.")),
			Expect: Expect{
				NoWatches:   true,
				EventCounts: map[string]int{"watch.created": 0},
			},
		},
		// ---------- C. Dedupe: one thing in the world, watched once ----------
		{
			ID: "watch-09", Category: CatWatches,
			Title:    "Restating the same flight keeps one watch",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("My flight BA123 tomorrow."),
				turn("By the way, my flight BA123 tomorrow.")),
			Expect: Expect{
				Watches:       []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "active"}},
				WatchCount:    1,
				WatchCountSet: true,
				EventCounts:   map[string]int{"watch.created": 1},
			},
		},
		{
			ID: "watch-10", Category: CatWatches,
			Title:    "A second explicit request answers already-watching",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow"),
				turn("track my flight BA123 again")),
			Expect: Expect{
				LastResponseContains: []string{"already watching"},
				WatchCount:           1,
				WatchCountSet:        true,
				EventCounts:          map[string]int{"watch.created": 1},
			},
		},
		{
			ID: "watch-11", Category: CatWatches,
			Title:    "No connected source refuses honestly (env-proof: appointment)",
			Severity: "high", // deliberately NO fixture: no sandbox source
			People: onePerson("maya", "maya",
				turn("track my dentist appointment tomorrow")),
			Expect: Expect{
				LastResponseContains: []string{"no source connected"},
				NoWatches:            true,
				EventCounts:          map[string]int{"watch.created": 0},
			},
		},
		// ---------- D. Owner control: list and stop ----------
		{
			ID: "watch-12", Category: CatWatches,
			Title:    "Listing watches reads back the store",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow"),
				turn("what am I watching")),
			Expect: Expect{
				LastResponseContains: []string{"you have 1 watch", "ba123"},
				Watches:              []WatchExpect{{Kind: "flight", Entity: "BA123"}},
				WatchCount:           1,
				WatchCountSet:        true,
			},
		},
		{
			ID: "watch-13", Category: CatWatches,
			Title:    "Stopping a watch closes it durably",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow"),
				turn("stop watching my flight")),
			Expect: Expect{
				LastResponseContains: []string{"stopped watching flight ba123"},
				Watches:              []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "disabled"}},
				WatchCount:           1,
				WatchCountSet:        true,
				EventCounts:          map[string]int{"watch.created": 1, "watch.cancelled": 1},
			},
		},
		// ---------- E. Background lifecycle (scripted, production poll path) ----------
		{
			ID: "watch-14", Category: CatWatches,
			Title:    "Baseline polls observe without reporting change",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow")),
			WatchScript: []string{
				"set status=on time",
				"set gate=A1",
				"poll",
				"poll",
			},
			Expect: Expect{
				Watches:       []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "active", MinProbes: 2, MaxProbes: 2}},
				WatchCount:    1,
				WatchCountSet: true,
				EventCounts:   map[string]int{"watch.created": 1, "watch.changed": 0, "watch.notified": 0},
				HeldNotices:   0, HeldNoticesSet: true,
			},
		},
		{
			ID: "watch-15", Category: CatWatches,
			Title:    "A gate change notifies once with evidence",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow")),
			WatchScript: []string{
				"set status=on time",
				"set gate=A1",
				"poll",
				"poll",
				"set gate=B12",
				"poll",
			},
			Expect: Expect{
				Watches:               []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "active", NotifiedFields: []string{"gate"}, MinProbes: 3, MaxProbes: 3}},
				WatchCount:            1,
				WatchCountSet:         true,
				EventCounts:           map[string]int{"watch.created": 1, "watch.changed": 1, "watch.notified": 1},
				WatchNotifiedEntities: []string{"BA123"},
				HeldNotices:           0, HeldNoticesSet: true,
			},
		},
		{
			ID: "watch-16", Category: CatWatches,
			Title:    "A watched-and-notified watch turns triggered",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow")),
			WatchScript: []string{
				"set status=on time",
				"set gate=A1",
				"poll",
				"poll",
				"set gate=B12",
				"poll",
				"poll",
			},
			Expect: Expect{
				Watches:               []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "triggered", NotifiedFields: []string{"gate"}, MinProbes: 4, MaxProbes: 4}},
				WatchCount:            1,
				WatchCountSet:         true,
				EventCounts:           map[string]int{"watch.created": 1, "watch.changed": 2, "watch.notified": 1},
				WatchNotifiedEntities: []string{"BA123"},
				HeldNotices:           0, HeldNoticesSet: true,
			},
		},
		{
			ID: "watch-17", Category: CatWatches,
			Title:    "Distinct changes notify; an old fingerprint never repeats",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow")),
			WatchScript: []string{
				"set status=on time",
				"set gate=A1",
				"poll",
				"set gate=B12",
				"poll",
				"set gate=A1",
				"poll",
				"set gate=C7",
				"poll",
				"set gate=B12",
				"poll",
			},
			Expect: Expect{
				// Gate returns to A1 equal the fixed baseline (no change);
				// B12 → C7 is a new fingerprint (notifies); the repeat of
				// B12 carries an already-delivered fingerprint (changed
				// event, no second notice).
				Watches:               []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "triggered", NotifiedFields: []string{"gate"}, MinProbes: 5, MaxProbes: 5}},
				WatchCount:            1,
				WatchCountSet:         true,
				EventCounts:           map[string]int{"watch.created": 1, "watch.changed": 3, "watch.notified": 2},
				WatchNotifiedEntities: []string{"BA123"},
				HeldNotices:           0, HeldNoticesSet: true,
			},
		},
		{
			ID: "watch-18", Category: CatWatches,
			Title:    "The daily push budget caps how many changes reach the owner",
			Severity: "high", Fixture: FixtureWatchSource,
			Prefs: watchPrefs("quiet_hours: 00:00 - 00:00", "max_pushes_per_day: 1"),
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow"),
				turn("track my flight KL456 tomorrow")),
			WatchScript: []string{
				"set status=on time",
				"poll",
				"set status=delayed",
				"poll",
			},
			Expect: Expect{
				// Both watches change; the noticer approves one push and
				// refuses the second (budget) — the refusal is recorded as
				// watch.suppressed, never silently dropped. Specs match
				// ledger rows order-insensitively.
				Watches: []WatchExpect{
					{Kind: "flight", Entity: "KL456", Status: "active", MinProbes: 2, MaxProbes: 2},
					{Kind: "flight", Entity: "BA123", Status: "active", MinProbes: 2, MaxProbes: 2},
				},
				WatchCount:    2,
				WatchCountSet: true,
				EventCounts: map[string]int{
					"watch.created": 2, "watch.changed": 2, "watch.notified": 1, "watch.suppressed": 1,
				},
				HeldNotices: 0, HeldNoticesSet: true,
			},
		},
		{
			ID: "watch-19", Category: CatWatches,
			Title:    "Five consecutive failures end the watch honestly",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow")),
			WatchScript: []string{
				"error unavailable: source offline",
				"poll",
				"poll",
				"poll",
				"poll",
				"poll",
			},
			Expect: Expect{
				Watches:       []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "failed"}},
				WatchCount:    1,
				WatchCountSet: true,
				EventCounts:   map[string]int{"watch.created": 1, "watch.failed": 5, "watch.notified": 1},
				HeldNotices:   0, HeldNoticesSet: true,
			},
		},
		{
			ID: "watch-20", Category: CatWatches,
			Title:    "A transient failure backs off and recovers",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow")),
			WatchScript: []string{
				"set status=on time",
				"error source offline",
				"poll",
				"clear-error",
				"poll",
			},
			Expect: Expect{
				Watches:       []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "active", MinProbes: 1, MaxProbes: 1}},
				WatchCount:    1,
				WatchCountSet: true,
				EventCounts:   map[string]int{"watch.created": 1, "watch.failed": 1, "watch.changed": 0},
			},
		},
		{
			ID: "watch-21", Category: CatWatches,
			Title:    "An explicit watch expires with a farewell notice",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("track my flight BA123")),
			WatchScript: []string{
				"past",
				"poll",
			},
			Expect: Expect{
				Watches:       []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "expired"}},
				WatchCount:    1,
				WatchCountSet: true,
				EventCounts:   map[string]int{"watch.created": 1, "watch.expired": 1, "watch.notified": 1},
				HeldNotices:   0, HeldNoticesSet: true,
			},
		},
		{
			ID: "watch-22", Category: CatWatches,
			Title:    "An automatic watch expires silently",
			Severity: "high", Fixture: FixtureWatchSource,
			People: onePerson("maya", "maya",
				turn("I have a dentist appointment tomorrow.")),
			WatchScript: []string{
				"past",
				"poll",
			},
			Expect: Expect{
				// Nobody asked for this watch, so its ending never
				// becomes a message: expired event, no notice, empty outbox.
				Watches:       []WatchExpect{{Kind: "appointment", Entity: "dentist", Status: "expired"}},
				WatchCount:    1,
				WatchCountSet: true,
				EventCounts:   map[string]int{"watch.created": 1, "watch.expired": 1, "watch.notified": 0},
				HeldNotices:   0, HeldNoticesSet: true,
			},
		},
		// ---------- F. Policy gates (preferences decide, model never does) ----------
		{
			ID: "watch-23", Category: CatWatches,
			Title:    "Master switch gates automatic watches, not explicit ones",
			Severity: "high", Fixture: FixtureWatchSource,
			Prefs: watchPrefs("enabled: false"),
			People: onePerson("maya", "maya",
				turn("I have a dentist appointment tomorrow."),
				turn("track my flight BA123 tomorrow")),
			Expect: Expect{
				// The volunteered appointment is refused by the disabled
				// switch; the named request still gets its watch.
				Watches:       []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "active"}},
				WatchCount:    1,
				WatchCountSet: true,
				EventCounts:   map[string]int{"watch.created": 1},
			},
		},
		{
			ID: "watch-24", Category: CatWatches,
			Title:    "Quiet hours hold the non-urgent notice and release the urgent one",
			Severity: "high", Fixture: FixtureWatchSource,
			// Quiet at every instant except minute 00 (a ~0.07% daily
			// window), so the gate change is held while the cancellation
			// (urgent) breaks through.
			Prefs: watchPrefs("quiet_hours: 00:01 - 00:00"),
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow")),
			WatchScript: []string{
				"set status=on time",
				"set gate=A1",
				"poll",
				"set gate=B12",
				"poll",
				"set status=cancelled",
				"poll",
			},
			Expect: Expect{
				Watches:               []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "triggered", NotifiedFields: []string{"gate", "status"}, MinProbes: 3, MaxProbes: 3}},
				WatchCount:            1,
				WatchCountSet:         true,
				EventCounts:           map[string]int{"watch.created": 1, "watch.changed": 2, "watch.notified": 2},
				WatchNotifiedEntities: []string{"BA123"},
				HeldNotices:           1, HeldNoticesSet: true,
			},
		},
		{
			ID: "watch-25", Category: CatWatches,
			Title:    "The daily probe budget caps background work",
			Severity: "high", Fixture: FixtureWatchSource,
			Prefs: watchPrefs("quiet_hours: 00:00 - 00:00", "max_watch_checks_per_day: 1"),
			People: onePerson("maya", "maya",
				turn("track my flight BA123 tomorrow")),
			WatchScript: []string{
				"set status=on time",
				"set gate=A1",
				"poll",
				"set status=delayed",
				"poll",
			},
			Expect: Expect{
				// One probe is allowed (baseline), the second is refused
				// by the daily budget before it ever touches the source:
				// the changed state goes unnoticed rather than unnoticed-
				// but-probed. Honesty: no evidence, no notice, no change.
				Watches:       []WatchExpect{{Kind: "flight", Entity: "BA123", Status: "active", MinProbes: 1, MaxProbes: 1}},
				WatchCount:    1,
				WatchCountSet: true,
				EventCounts:   map[string]int{"watch.created": 1, "watch.changed": 0, "watch.notified": 0},
			},
		},
	}
}
