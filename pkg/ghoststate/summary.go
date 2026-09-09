package ghoststate

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// RestoreSummary is the user-facing inventory of an archive, produced
// without applying anything. The console validate step renders exactly
// this so the user confirms with full knowledge of what will land.
type RestoreSummary struct {
	GhostID          string         `json:"ghost_id"`
	ExportedAt       string         `json:"exported_at"`
	SecretsIncluded  bool           `json:"secrets_included"`
	Conversations    int            `json:"conversations"`
	Routines         int            `json:"routines"`
	ScheduledItems   int            `json:"scheduled_items"`
	StandingGrants   int            `json:"standing_grants"`
	Tables           map[string]int `json:"tables"`
	Rebound          []string       `json:"rebound"`
	SecretsExcluded  []string       `json:"secrets_excluded"`
	PortableFiles    int            `json:"portable_files"`
}

// Summarize decrypts an archive and counts its restorable contents.
// Nothing is written anywhere.
func Summarize(source, passphrase string) (*RestoreSummary, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase is required")
	}
	blob, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	plain, err := decryptBytes(blob, passphrase)
	if err != nil {
		return nil, err
	}
	manifest, files, err := readArchive(plain)
	if err != nil {
		return nil, err
	}
	sum := &RestoreSummary{
		GhostID:         manifest.GhostID,
		ExportedAt:      manifest.ExportedAt,
		SecretsIncluded: manifest.SecretsIncluded,
		Rebound:         manifest.Rebound,
		SecretsExcluded: manifest.SecretsExcluded,
		Tables:          map[string]int{},
	}
	for name, data := range files {
		switch {
		case strings.HasPrefix(name, conversationsSessionsLogical+"/"):
			sum.Conversations++
		case strings.HasPrefix(name, dbSnapshotsDirLogical+"/") && strings.HasSuffix(name, ".json"):
			n, table, err := countSnapshotRows(data)
			if err != nil {
				return nil, fmt.Errorf("summarize %s: %w", name, err)
			}
			sum.Tables[table] = n
			switch table {
			case "scheduled_items":
				sum.ScheduledItems += n
			case "permission_grants":
				sum.StandingGrants += n
			}
		default:
			sum.PortableFiles++
		}
	}
	// Routines are scheduled_items rows carrying routine metadata; count
	// them from the snapshot instead of guessing.
	if data, ok := files[snapshotLogicalPath("scheduled_items")]; ok {
		r, err := countRoutineRows(data)
		if err != nil {
			return nil, fmt.Errorf("summarize routines: %w", err)
		}
		sum.Routines = r
		sum.ScheduledItems -= r
	}
	return sum, nil
}

func countSnapshotRows(data []byte) (int, string, error) {
	var sf tableSnapshotFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return 0, "", err
	}
	return len(sf.Rows), sf.Table, nil
}

func countRoutineRows(data []byte) (int, error) {
	var sf tableSnapshotFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return 0, err
	}
	srcIdx := -1
	for i, c := range sf.Columns {
		if c == "source" {
			srcIdx = i
		}
	}
	n := 0
	for _, row := range sf.Rows {
		if srcIdx >= 0 && srcIdx < len(row) {
			if s, ok := row[srcIdx].(string); ok && s == "routine" {
				n++
			}
		}
	}
	return n, nil
}
