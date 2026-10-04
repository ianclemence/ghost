package watch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A2 restart harness: a real process kill and restart, not a simulation.
//
// The test re-execs its own binary as a child. The child records a notice
// through the durable ledger and then blocks; the parent SIGKILLs it; a
// second child re-opens the same ledger and proves the notice is not re-sent.
// If the ledger were in memory, the restart would re-send.
//
// The child does the ledger work the runner does (create → observe → mark
// notified), so this proves the durable behaviour the runner depends on,
// across a genuine kill.

const (
	helperEnv  = "WATCH_RESTART_HELPER"
	helperWS   = "WATCH_RESTART_WS"
	helperMode = "WATCH_RESTART_MODE"
)

// TestWatchRestartHelper is not a normal test: it is the child entry point,
// reached only when the parent re-execs the binary with the marker env set.
func TestWatchRestartHelper(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	ws := os.Getenv(helperWS)
	switch os.Getenv(helperMode) {
	case "record":
		recordAndBlock(t, ws)
	case "verify":
		verify(t, ws)
	default:
		os.Exit(3)
	}
}

func recordAndBlock(t *testing.T, ws string) {
	st, err := New(ws)
	if err != nil {
		os.Exit(4)
	}
	w, err := st.Create(Watch{Kind: KindFlight, Entity: "TG123",
		Provenance: Provenance{Quote: "watch my flight TG123", At: time.Now()}})
	if err != nil {
		os.Exit(5)
	}
	if _, err := st.ResetBaseline(w.ID, map[string]string{"gate": "A"}); err != nil {
		os.Exit(6)
	}
	_, changes, err := st.Observe(w.ID, map[string]string{"gate": "B"}, Evidence{Field: "gate", Source: "flight"}, time.Now().Add(time.Hour))
	if err != nil || len(changes) != 1 {
		os.Exit(7)
	}
	fp := changes[0].Fingerprint(w.ID)
	// A break switch so the harness can be proven to fail when the ledger is
	// not written — "if you can't make it fail on demand, it proves nothing."
	if os.Getenv("WATCH_RESTART_SKIP_MARK") != "1" {
		sent, _, err := st.MarkNotified(w.ID, fp)
		if err != nil || !sent {
			os.Exit(8)
		}
	}
	// Hand the fingerprint to the verifying child through the workspace.
	if err := os.WriteFile(filepath.Join(ws, "fingerprint"), []byte(fp), 0o600); err != nil {
		os.Exit(9)
	}
	// Announce readiness, then block so the parent can kill us mid-life.
	_ = os.WriteFile(filepath.Join(ws, "ready"), []byte("1"), 0o600)
	select {}
}

func verify(t *testing.T, ws string) {
	fp, err := os.ReadFile(filepath.Join(ws, "fingerprint"))
	if err != nil {
		os.Exit(10)
	}
	st, err := New(ws)
	if err != nil {
		os.Exit(11)
	}
	list, err := st.List()
	if err != nil || len(list) != 1 {
		os.Exit(12)
	}
	// The notice already fired must not be re-sendable after the kill.
	sent, _, err := st.MarkNotified(list[0].ID, string(fp))
	if err != nil {
		os.Exit(13)
	}
	if sent {
		// The durable dedupe did NOT survive the restart: the failure this
		// harness exists to catch.
		os.Stdout.WriteString("RESENT\n")
		os.Exit(0)
	}
	// A watch with no change across the restart must not be due to notify;
	// but it is still due to be probed on cadence (durability of the watch).
	due, err := st.Due(time.Now().Add(2 * time.Hour))
	if err != nil {
		os.Exit(14)
	}
	if len(due) == 0 {
		os.Stdout.WriteString("NOTDUE\n")
		os.Exit(0)
	}
	os.Stdout.WriteString("OK\n")
	os.Exit(0)
}

// The parent: start the real child, kill it for real, restart, and return the
// restarting child's verdict.
func runRestartHarness(t *testing.T, skipMark bool) string {
	t.Helper()
	if os.Getenv(helperEnv) == "1" {
		t.Skip("child process")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()

	child := exec.Command(exe, "-test.run=TestWatchRestartHelper", "-test.v")
	child.Env = append(os.Environ(), helperEnv+"=1", helperWS+"="+ws, helperMode+"=record")
	if skipMark {
		child.Env = append(child.Env, "WATCH_RESTART_SKIP_MARK=1")
	}
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(ws, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = child.Process.Kill()
			t.Fatal("child never recorded the notice")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	_, _ = child.Process.Wait()

	restarted := exec.Command(exe, "-test.run=TestWatchRestartHelper", "-test.v")
	restarted.Env = append(os.Environ(), helperEnv+"=1", helperWS+"="+ws, helperMode+"=verify")
	out, err := restarted.CombinedOutput()
	if err != nil {
		t.Fatalf("verify child failed: %v\n%s", err, out)
	}
	return string(out)
}

func TestAcceptance_RestartDoesNotResendAfterRealKill(t *testing.T) {
	out := runRestartHarness(t, false)
	switch {
	case strings.Contains(out, "RESENT"):
		t.Fatal("the same notice was re-sent after a real kill; the durable ledger did not survive")
	case strings.Contains(out, "NOTDUE"):
		t.Fatal("the watch did not survive the restart as a live, due watch")
	case strings.Contains(out, "OK"):
		// proven: no re-send, watch still live
	default:
		t.Fatalf("verify child produced no verdict:\n%s", out)
	}
}

// Falsifiability: with the ledger deliberately not written, the SAME harness
// must report a re-send. If this passed too, the harness above would prove
// nothing.
func TestAcceptance_RestartHarnessFailsWithoutTheLedger(t *testing.T) {
	out := runRestartHarness(t, true)
	if !strings.Contains(out, "RESENT") {
		t.Fatalf("the harness did not detect the missing ledger; it proves nothing:\n%s", out)
	}
}
