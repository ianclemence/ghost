package connector

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// InstalledDir is the workspace-relative directory that holds installed
// connectors (one subdirectory per connector id).
const InstalledDir = "connectors"

// LoadInstalled loads every valid connector under <workspace>/connectors.
// A missing directory is not an error. Invalid manifests are skipped here —
// `ghost connector review` is the surface that reports them — so one bad
// install can never break the runtime.
func LoadInstalled(workspace string) ([]*Manifest, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, nil
	}
	root := filepath.Join(workspace, InstalledDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, verrs, err := Load(filepath.Join(root, e.Name()))
		if err != nil || len(verrs) > 0 {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
