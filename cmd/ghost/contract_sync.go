package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type contractFile struct {
	name         string
	productOwned bool
}

// The operating contract the model reads. GHOST.md and AGENTS.md are Ghost's
// own and always follow the release; the persona and cadence files belong to
// the owner as soon as they change them.
var contractFiles = []contractFile{
	{"GHOST.md", true},
	{"AGENTS.md", true},
	{"SOUL.md", false},
	{"IDENTITY.md", false},
	{"HEARTBEAT.md", false},
}

type contractReport struct {
	Updated []string
	Kept    []string
}

func contractHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// syncContract brings the live contract files up to the release's. A file
// the owner has changed is never overwritten unless it is Ghost's own, in
// which case the owner's version is kept beside it first.
func syncContract(seed fs.FS, root string, workspace string) (contractReport, error) {
	var rep contractReport
	statePath := filepath.Join(workspace, "state", "contract-seed.json")
	recorded := map[string]string{}
	if raw, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(raw, &recorded)
	}
	next := map[string]string{}
	for k, v := range recorded {
		next[k] = v
	}
	for _, cf := range contractFiles {
		want, err := fs.ReadFile(seed, root+"/"+cf.name)
		if err != nil {
			continue
		}
		wantHash := contractHash(want)
		livePath := filepath.Join(workspace, cf.name)
		live, err := os.ReadFile(livePath)
		switch {
		case err != nil:
		case contractHash(live) == wantHash:
			next[cf.name] = wantHash
			continue
		case recorded[cf.name] != "" && contractHash(live) == recorded[cf.name]:
		case cf.productOwned:
			if err := backupContract(workspace, cf.name, live); err != nil {
				return rep, err
			}
		default:
			if recorded[cf.name] == "" {
				next[cf.name] = contractHash(live)
			}
			rep.Kept = append(rep.Kept, cf.name)
			continue
		}
		if err := os.WriteFile(livePath, want, 0o644); err != nil {
			return rep, err
		}
		next[cf.name] = wantHash
		rep.Updated = append(rep.Updated, cf.name)
	}
	if len(rep.Updated) > 0 || len(next) != len(recorded) {
		if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
			return rep, err
		}
		raw, _ := json.MarshalIndent(next, "", "  ")
		if err := os.WriteFile(statePath, raw, 0o600); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func backupContract(workspace, name string, content []byte) error {
	dir := filepath.Join(workspace, "state", "contract-backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.%d", name, time.Now().Unix())), content, 0o600)
}

func syncEmbeddedContract(workspace string) {
	rep, err := syncContract(embeddedFiles, "workspace", workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contract sync: %v\n", err)
		return
	}
	if len(rep.Updated) > 0 {
		fmt.Printf("  • Updated operating contract: %v\n", rep.Updated)
	}
}
