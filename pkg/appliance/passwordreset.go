package appliance

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Forgetting the console password must not need a terminal. A normal owner has
// no shell on their Pod, so there are two ways back in, both of which prove the
// person has the Pod (not just its address):
//
//  1. A paired phone asks the Pod for a one-time code and shows it. The phone
//     is already trusted with the Pod, so it can vouch for the owner. The code
//     lasts ten minutes, works once, and is stored only as a hash.
//  2. A file on the SD card's boot partition. Whoever can put a file there is
//     holding the card, which is the same trust the setup code relies on.
//
// Neither reveals the old password (it is only ever stored hashed) and neither
// touches memory, files or paired phones.

const (
	resetCodeFileName = ".password-reset"
	// ResetCodeTTL is how long an issued code works.
	ResetCodeTTL = 10 * time.Minute
	// maxResetAttempts wrong guesses burn the code.
	maxResetAttempts = 5

	// BootResetFileName is the file an owner drops on the SD card.
	BootResetFileName = "ghost-reset-password"
)

// bootResetPaths are where the boot-partition file is looked for. A variable
// so tests can point it elsewhere.
var bootResetPaths = []string{
	"/boot/firmware/" + BootResetFileName, "/boot/" + BootResetFileName,
	"/boot/firmware/" + BootResetFileName + ".txt", "/boot/" + BootResetFileName + ".txt",
}

type resetRecord struct {
	Hash     string    `json:"hash"`
	Expires  time.Time `json:"expires"`
	Attempts int       `json:"attempts"`
	IssuedAt time.Time `json:"issued_at"`
}

// resetAlphabet leaves out characters that read alike (0/O, 1/I/L).
const resetAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func hashResetCode(code string) string {
	h := sha256.Sum256([]byte(normalizeResetCode(code)))
	return hex.EncodeToString(h[:])
}

// normalizeResetCode accepts the code however it was typed: any case, with or
// without the dash or spaces.
func normalizeResetCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.NewReplacer("-", "", " ", "").Replace(code)
	return code
}

func resetCodePath(ghostDir string) string { return filepath.Join(ghostDir, resetCodeFileName) }

// IssueResetCode creates a fresh code (replacing any earlier one) and returns
// it. It is shown once, to the person who asked; only its hash is kept.
func IssueResetCode(ghostDir string, now time.Time) (code string, expires time.Time, err error) {
	if !AdminConfigured(ghostDir) {
		return "", time.Time{}, errors.New("there is no console password to reset yet; finish setup first")
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	var b strings.Builder
	for i, x := range buf {
		if i == 4 {
			b.WriteByte('-')
		}
		b.WriteByte(resetAlphabet[int(x)%len(resetAlphabet)])
	}
	code = b.String()
	expires = now.Add(ResetCodeTTL)
	rec := resetRecord{Hash: hashResetCode(code), Expires: expires, IssuedAt: now}
	data, _ := json.Marshal(rec)
	if err := os.MkdirAll(ghostDir, 0o755); err != nil {
		return "", time.Time{}, err
	}
	if err := os.WriteFile(resetCodePath(ghostDir), data, 0o600); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// RedeemResetCode reports whether code is the live one, and uses it up when it
// is. A wrong guess counts against the code; a few burn it.
func RedeemResetCode(ghostDir, code string, now time.Time) bool {
	path := resetCodePath(ghostDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var rec resetRecord
	if json.Unmarshal(data, &rec) != nil || now.After(rec.Expires) {
		_ = os.Remove(path)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(rec.Hash), []byte(hashResetCode(code))) == 1 && normalizeResetCode(code) != "" {
		_ = os.Remove(path)
		return true
	}
	rec.Attempts++
	if rec.Attempts >= maxResetAttempts {
		_ = os.Remove(path)
		return false
	}
	updated, _ := json.Marshal(rec)
	_ = os.WriteFile(path, updated, 0o600)
	return false
}

// ResetCodePending reports whether an unexpired code exists.
func ResetCodePending(ghostDir string, now time.Time) bool {
	data, err := os.ReadFile(resetCodePath(ghostDir))
	if err != nil {
		return false
	}
	var rec resetRecord
	return json.Unmarshal(data, &rec) == nil && now.Before(rec.Expires)
}

// ApplyBootReset looks for the reset file on the SD card. When it holds a new
// password that passes the same rules as any other, it sets it and removes the
// file. A file that cannot be used is renamed with the reason inside, so the
// owner sees what to fix instead of nothing happening. It returns whether a
// password was changed.
func ApplyBootReset(ghostDir string) (changed bool, err error) {
	for _, p := range bootResetPaths {
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			continue
		}
		if !AdminConfigured(ghostDir) {
			return false, nil // nothing to reset; leave the file for later
		}
		pw := strings.TrimSpace(firstLine(string(raw)))
		if verr := ValidatePassword(pw); verr != nil {
			note := fmt.Sprintf("The password in this file was not used: %v.\nWrite a new one on the first line and save it as %s.\n", verr, BootResetFileName)
			_ = os.WriteFile(p+".rejected.txt", []byte(note), 0o644)
			_ = os.Remove(p)
			return false, verr
		}
		if serr := SetAdminPassword(ghostDir, pw); serr != nil {
			return false, serr
		}
		_ = os.Remove(p)
		return true, nil
	}
	return false, nil
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}
