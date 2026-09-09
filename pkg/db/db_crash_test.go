package db

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestCrashMidTransactionRecovers proves the exact durability mechanism
// Ghost relies on: a writer SIGKILLed mid-transaction (simulated power
// loss) must leave the database integral, without the uncommitted row,
// and fully writable afterwards. WAL mode + atomic commit is what makes
// abrupt appliance power loss safe.
func TestCrashMidTransactionRecovers(t *testing.T) {
	if os.Getenv("GO_WANT_DB_CRASH_HELPER") == "1" {
		dbPath := os.Getenv("GO_WANT_DB_CRASH_PATH")
		db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer db.Close()
		tx, err := db.Begin()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if _, err := tx.Exec(`INSERT INTO crash_probe (id, body) VALUES ('uncommitted', 'must not survive')`); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		// Signal readiness, then hold the uncommitted write until killed.
		// The parent SIGKILLs us here: no COMMIT, no ROLLBACK, no cleanup
		// — like a power cut.
		fmt.Println("ready")
		select {}
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ghost.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crash_probe (id TEXT PRIMARY KEY, body TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO crash_probe VALUES ('committed', 'must survive')`); err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0], "-test.run=TestCrashMidTransactionRecovers")
	child.Env = append(os.Environ(),
		"GO_WANT_DB_CRASH_HELPER=1",
		"GO_WANT_DB_CRASH_PATH="+dbPath,
	)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if err := child.Start(); err != nil {
		t.Fatalf("start writer: %v", err)
	}
	// Wait until the child has staged its uncommitted write ("ready\n"),
	// then kill it mid-transaction.
	ready := make([]byte, 6)
	done := make(chan error, 1)
	go func() {
		_, err := stdout.Read(ready)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			child.Process.Kill()
			t.Fatalf("child pipe: %v", err)
		}
	case <-time.After(15 * time.Second):
		child.Process.Kill()
		t.Fatal("child never staged its write")
	}
	if err := child.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("kill: %v", err)
	}
	child.Wait()
	db.Close()

	// Reopen like a reboot would: integrity must hold, the uncommitted
	// row must be gone, the committed row present, writes working.
	db2, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var integrity string
	if err := db2.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity_check = %q, %v", integrity, err)
	}
	var n int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM crash_probe WHERE id='uncommitted'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("uncommitted row survived crash: n=%d err=%v", n, err)
	}
	if err := db2.QueryRow(`SELECT COUNT(*) FROM crash_probe WHERE id='committed'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("committed row lost: n=%d err=%v", n, err)
	}
	if _, err := db2.Exec(`INSERT INTO crash_probe VALUES ('after', 'writes work')`); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
}
