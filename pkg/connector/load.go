package connector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Load reads a connector manifest from a file, or from a directory containing
// connector.json. A parse failure is an error; schema problems are returned as
// validation errors so a caller can report both.
func Load(path string) (*Manifest, []ValidationError, error) {
	p := path
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		p = filepath.Join(path, FileName)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", p, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &m, m.Validate(), nil
}

// Save writes a manifest as indented JSON (0600), creating parent dirs.
func Save(m *Manifest, path string) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0600)
}

// Slugify reduces a human name to a manifest-safe slug (lowercase, digits,
// single hyphens). CamelCase boundaries become separators so "listContacts"
// reads as "list-contacts". Used by init and the OpenAPI generator.
func Slugify(s string) string {
	var b strings.Builder
	prevDash := false
	prevLower := false
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash, prevLower = false, true
		case r >= 'A' && r <= 'Z':
			if prevLower && !prevDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
			prevDash, prevLower = false, false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
			prevLower = false
		}
	}
	return strings.Trim(b.String(), "-")
}
