package personalcontext

import (
	"crypto/rand"
	"time"
)

// entryIDPrefix prefixes every entry id minted by the extractor so extracted
// entries are distinguishable from entries created by other slices.
const entryIDPrefix = "ec_"

// crockford is the Crockford base32 alphabet used by ULID.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newEntryID mints an ec_-prefixed ULID-style id: 48-bit millisecond
// timestamp followed by 80 random bits, encoded in Crockford base32, so ids
// are time-sortable and unique without coordination. No ULID utility exists in
// the repository, so the encoding is implemented here rather than pulling in a
// dependency.
func newEntryID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		fillDeterministicFallback(&b)
	}
	return entryIDPrefix + encodeULID(b)
}

// fillDeterministicFallback fills the random tail from the clock when
// crypto/rand is unavailable, rather than emitting a zero-filled id.
func fillDeterministicFallback(b *[16]byte) {
	nanos := uint64(time.Now().UnixNano())
	for i := 6; i < 16; i++ {
		b[i] = byte(nanos >> uint((i-6)*8))
	}
}

// encodeULID renders 128 bits as 26 Crockford base32 characters (the final
// character is zero-padded per the ULID spec).
func encodeULID(b [16]byte) string {
	var out [26]byte
	for i := 0; i < 26; i++ {
		out[i] = crockford[bitsValue(b[:], i*5, 5)]
	}
	return string(out[:])
}

// bitsValue extracts n bits starting at bit index bit, reading big-endian from
// b. Reads past the end of b are zero.
func bitsValue(b []byte, bit, n int) uint8 {
	var v uint8
	for i := 0; i < n; i++ {
		idx := bit + i
		var bitVal uint8
		if idx < len(b)*8 {
			bitVal = (b[idx/8] >> (7 - uint(idx%8))) & 1
		}
		v = v<<1 | bitVal
	}
	return v
}
