package cevents

import (
	"time"
)

// Durable consumer support: at-least-once processing with checkpoints.
//
// The stream already persists durable events with monotonic seq and reads
// them back via Since. What was missing: consumers had no durable memory
// of where they stopped, so a restart either reprocessed everything
// (duplicate side effects) or missed events published while down. These
// helpers close that gap:
//
//   - Ack records how far a named consumer got. It only moves forward.
//   - Claim gives exactly-once-per-consumer effects: the first claimant
//     processes, replays and redeliveries find the claim taken and skip.
//   - Replay returns everything after the consumer's checkpoint.
//   - SubscribeDurable replays the backlog, then stays live.
//
// Non-durable types (progress heartbeats) are never persisted and can
// never replay — SubscribeDurable still delivers them live, but only
// durable events advance the checkpoint.

// consumerTables belong in Open's schema statements alongside
// canonical_events: consumer checkpoints and exactly-once claims.
var consumerTables = []string{
	`CREATE TABLE IF NOT EXISTS event_consumers (
		consumer TEXT PRIMARY KEY,
		last_seq INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS event_claims (
		event_id TEXT NOT NULL,
		consumer TEXT NOT NULL,
		claimed_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (event_id, consumer)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_event_claims_consumer ON event_claims(consumer)`,
}

// Checkpoint returns the last seq a consumer acked, or 0 for a consumer
// that never ran. Unknown consumers start from the beginning: missing an
// event is worse than reprocessing one (claims make reprocessing safe).
func (s *Stream) Checkpoint(consumer string) int64 {
	var seq int64
	if err := s.db.QueryRow(`SELECT last_seq FROM event_consumers WHERE consumer=?`, consumer).Scan(&seq); err != nil {
		return 0
	}
	return seq
}

// Ack records progress. It only moves forward: a stale worker acking an
// old seq after a newer one was recorded cannot drag the checkpoint back.
func (s *Stream) Ack(consumer string, seq int64) error {
	if consumer == "" || seq <= 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO event_consumers (consumer, last_seq, updated_at) VALUES (?, 0, ?)`, consumer, now); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE event_consumers SET last_seq=?, updated_at=? WHERE consumer=? AND last_seq < ?`, seq, now, consumer, seq)
	return err
}

// Claim attempts exactly-once ownership of one event for one consumer.
// True means "you are the first — process it"; false means it was already
// claimed (replay overlap, redelivery) and must be skipped.
func (s *Stream) Claim(consumer, eventID string) (bool, error) {
	if consumer == "" || eventID == "" {
		return false, nil
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO event_claims (event_id, consumer, claimed_at) VALUES (?,?,?)`,
		eventID, consumer, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// Replay returns up to limit events after the consumer's checkpoint, in
// seq order. The caller claims each event and acks the last processed
// seq; unacked events come back next time.
func (s *Stream) Replay(consumer string, limit int, f Filter) []*Event {
	return s.Since(s.Checkpoint(consumer), limit, f)
}

// SubscribeDurable replays the consumer's backlog, then stays subscribed
// live. fn returning an error stops that event without acking: durable
// events are redelivered on the next boot, so a failed effect retries
// instead of silently dropping. Returns unsubscribe.
func (s *Stream) SubscribeDurable(consumer string, f Filter, fn func(*Event) error) func() {
	for _, e := range s.Replay(consumer, 500, f) {
		s.applyDurable(consumer, e, fn)
	}
	return s.Subscribe(f, func(e *Event) {
		s.applyDurable(consumer, e, fn)
	})
}

func (s *Stream) applyDurable(consumer string, e *Event, fn func(*Event) error) {
	first, err := s.Claim(consumer, e.ID)
	if err != nil || !first {
		return
	}
	if err := fn(e); err != nil {
		// Release the claim: the event stays unacked AND unclaimed, so a
		// restart replays and reprocesses it instead of skipping it as a
		// duplicate. Effects must therefore be idempotent downstream too.
		_, _ = s.db.Exec(`DELETE FROM event_claims WHERE event_id=? AND consumer=?`, e.ID, consumer)
		return
	}
	if e.Seq > 0 {
		_ = s.Ack(consumer, e.Seq)
	}
}
