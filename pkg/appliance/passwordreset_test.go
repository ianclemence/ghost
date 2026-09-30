package appliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func configuredDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := SetAdminPassword(dir, "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResetCodeWorksOnceAndOnlyBeforeItExpires(t *testing.T) {
	dir := configuredDir(t)
	now := time.Now()
	code, exp, err := IssueResetCode(dir, now)
	if err != nil || len(code) != 9 || code[4] != '-' || !exp.After(now) {
		t.Fatalf("code %q exp %v err %v", code, exp, err)
	}
	if raw, _ := os.ReadFile(resetCodePath(dir)); strings.Contains(string(raw), strings.ReplaceAll(code, "-", "")) {
		t.Fatal("the code itself must never be stored, only its hash")
	}
	// Typed however the owner likes.
	if !RedeemResetCode(dir, strings.ToLower(strings.ReplaceAll(code, "-", " ")), now.Add(time.Minute)) {
		t.Fatal("the right code, in any casing and spacing, redeems")
	}
	if RedeemResetCode(dir, code, now.Add(time.Minute)) {
		t.Fatal("a code works once")
	}
	code2, _, _ := IssueResetCode(dir, now)
	if RedeemResetCode(dir, code2, now.Add(ResetCodeTTL+time.Second)) {
		t.Fatal("an expired code must not work")
	}
}

func TestWrongGuessesBurnTheCode(t *testing.T) {
	dir := configuredDir(t)
	now := time.Now()
	code, _, _ := IssueResetCode(dir, now)
	for i := 0; i < maxResetAttempts; i++ {
		if RedeemResetCode(dir, "AAAA-AAAA", now) {
			t.Fatal("a wrong guess must fail")
		}
	}
	if RedeemResetCode(dir, code, now) {
		t.Fatal("after too many wrong guesses even the right code is refused")
	}
}

func TestANewCodeReplacesTheOldOne(t *testing.T) {
	dir := configuredDir(t)
	now := time.Now()
	first, _, _ := IssueResetCode(dir, now)
	second, _, _ := IssueResetCode(dir, now)
	if RedeemResetCode(dir, first, now) {
		t.Fatal("asking again cancels the earlier code")
	}
	if !RedeemResetCode(dir, second, now) {
		t.Fatal("the latest code works")
	}
}

func TestNoCodeWithoutAPasswordToReset(t *testing.T) {
	if _, _, err := IssueResetCode(t.TempDir(), time.Now()); err == nil {
		t.Fatal("before setup there is nothing to reset")
	}
	if RedeemResetCode(t.TempDir(), "ABCD-EFGH", time.Now()) {
		t.Fatal("no code was issued")
	}
}

func TestSDCardFileResetsThePasswordOnce(t *testing.T) {
	dir := configuredDir(t)
	boot := t.TempDir()
	old := bootResetPaths
	bootResetPaths = []string{filepath.Join(boot, BootResetFileName), filepath.Join(boot, BootResetFileName+".txt")}
	defer func() { bootResetPaths = old }()

	os.WriteFile(bootResetPaths[1], []byte("a brand new passphrase\nignored second line\n"), 0o644)
	changed, err := ApplyBootReset(dir)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if ok, _ := VerifyAdminPassword(dir, "a brand new passphrase"); !ok {
		t.Fatal("the new password must work")
	}
	if ok, _ := VerifyAdminPassword(dir, "correct horse battery"); ok {
		t.Fatal("the old password must stop working")
	}
	if _, err := os.Stat(bootResetPaths[1]); !os.IsNotExist(err) {
		t.Fatal("the file must be removed so the password is not left lying on the card")
	}
	if changed, _ := ApplyBootReset(dir); changed {
		t.Fatal("nothing to do the second time")
	}
}

func TestAnUnusableSDCardFileSaysWhy(t *testing.T) {
	dir := configuredDir(t)
	boot := t.TempDir()
	old := bootResetPaths
	bootResetPaths = []string{filepath.Join(boot, BootResetFileName)}
	defer func() { bootResetPaths = old }()
	os.WriteFile(bootResetPaths[0], []byte("short\n"), 0o644)
	if changed, err := ApplyBootReset(dir); changed || err == nil {
		t.Fatalf("a weak password is refused: changed=%v err=%v", changed, err)
	}
	note, err := os.ReadFile(bootResetPaths[0] + ".rejected.txt")
	if err != nil || !strings.Contains(string(note), "at least 8") {
		t.Fatalf("the owner must be told why: %q %v", note, err)
	}
	if ok, _ := VerifyAdminPassword(dir, "correct horse battery"); !ok {
		t.Fatal("a refused file must not change anything")
	}
}
