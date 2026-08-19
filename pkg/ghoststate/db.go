package ghoststate

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// snapshotDB produces a consistent single-file snapshot of a WAL-mode SQLite
// database via VACUUM INTO, which captures every committed transaction
// including anything still sitting in the -wal file. The destination must not
// exist (fresh temp path).
func snapshotDB(srcPath, dstPath string) error {
	if !fileExists(srcPath) {
		return fmt.Errorf("database %s does not exist", srcPath)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)", srcPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer conn.Close()
	// VACUUM INTO does not accept bound parameters; the destination is a
	// freshly minted temp path, so quote it to survive unusual characters.
	quoted := "'" + strings.ReplaceAll(dstPath, "'", "''") + "'"
	if _, err := conn.Exec("VACUUM INTO " + quoted); err != nil {
		return fmt.Errorf("vacuum into snapshot: %w", err)
	}
	return nil
}
