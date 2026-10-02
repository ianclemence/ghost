package personalcontext

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

// SeedHome records where the owner lives and their time zone from first-run
// setup, as beliefs the owner declared. It never replaces a belief that
// already exists, so re-running setup cannot overwrite what Ghost has since
// learned. A blank or unusable value is skipped, not an error: both fields are
// optional in setup.
func SeedHome(s *Store, city, timezone string) error {
	now := time.Now().UTC()
	put := func(predicate, value string) error {
		for _, e := range s.Current() {
			if e.Subject == "user" && e.Predicate == predicate {
				return nil
			}
		}
		raw, _ := json.Marshal(value)
		_, err := s.Create(Entry{
			ID: NewEntryID(), Kind: KindFact, Subject: "user", Predicate: predicate,
			Value: raw, Status: StatusCurrent, Confidence: 0.95,
			Quote: "Entered during first-run setup",
			Sources: []Source{{
				Type: SourceCommand, Kind: SourceUserDeclared, Ref: "setup", Timestamp: now,
			}},
		})
		return err
	}
	city = strings.TrimSpace(city)
	if city != "" && utf8.RuneCountInString(city) <= 80 && !strings.ContainsAny(city, "\n\r\x00") {
		if err := put("fact/location", city); err != nil {
			return err
		}
	}
	timezone = strings.TrimSpace(timezone)
	if timezone != "" {
		if _, err := time.LoadLocation(timezone); err == nil {
			if err := put("fact/timezone", timezone); err != nil {
				return err
			}
		}
	}
	return nil
}
