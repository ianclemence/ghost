package personalcontext

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxCorrectionRunes bounds a corrected value; beliefs are short sentences.
const MaxCorrectionRunes = 300

// ErrBadCorrection is returned for a value that is empty, too long, or not
// a change.
var ErrBadCorrection = errors.New("correction is empty, too long, or unchanged")

// Correct replaces the current value of one belief with a value the owner
// supplied, whether through the correction tool or the console. The old entry
// is kept as superseded, the new one carries the owner's words as its receipt,
// and any unconfirmed candidate for the same belief is dropped because the
// owner has now settled it. It returns the old and new entries.
func (s *Store) Correct(id, newValue, quote, ref string) (old, next Entry, err error) {
	cur, ok := s.Get(id)
	if !ok {
		return Entry{}, Entry{}, ErrNotFound
	}
	if cur.Status != StatusCurrent {
		return Entry{}, Entry{}, ErrNoCurrentEntry
	}
	newValue = strings.Join(strings.Fields(newValue), " ")
	if newValue == "" || utf8.RuneCountInString(newValue) > MaxCorrectionRunes || strings.EqualFold(newValue, Value(cur)) {
		return Entry{}, Entry{}, ErrBadCorrection
	}
	if r := []rune(strings.TrimSpace(quote)); len(r) > 200 {
		quote = string(r[:200])
	}
	raw, _ := json.Marshal(newValue)
	created, err := s.Supersede(cur.Subject, cur.Predicate, Entry{
		ID: NewEntryID(), Kind: cur.Kind, Subject: cur.Subject, Predicate: cur.Predicate,
		Value: raw, Status: StatusCurrent, Lifetime: cur.Lifetime, Scopes: cur.Scopes,
		Confidence: 0.95, Quote: strings.TrimSpace(quote),
		Sources: []Source{{Type: SourceConversation, Kind: SourceUserCorrected, Ref: ref, Timestamp: time.Now().UTC()}},
	})
	if err != nil {
		return Entry{}, Entry{}, err
	}
	for _, e := range s.ByPredicate(cur.Predicate) {
		if e.Subject == cur.Subject && e.Status == StatusUncertain {
			_ = s.Forget(e.ID)
		}
	}
	return cur, created, nil
}
