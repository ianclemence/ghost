package ghoststate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// identityFileName is the live identity record inside the workspace. It is
// the only persistent artifact that carries the ghost_id.
const identityFileName = "state/identity.json"

// Identity is the persistent, hardware-independent identity of a Ghost. The
// ghost_id is generated once at initial setup and is never reused as a device
// identifier.
type Identity struct {
	SchemaVersion int    `json:"schema_version"`
	GhostID       string `json:"ghost_id"`
	CreatedAt     string `json:"created_at"` // RFC3339
}

// IdentityPath returns the location of the identity record for a workspace.
func IdentityPath(workspace string) string {
	return filepath.Join(workspace, identityFileName)
}

// LoadIdentity reads the identity record. A missing record returns (nil, nil).
func LoadIdentity(workspace string) (*Identity, error) {
	data, err := os.ReadFile(IdentityPath(workspace))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, fmt.Errorf("parse identity %s: %w", IdentityPath(workspace), err)
	}
	if id.GhostID == "" {
		return nil, fmt.Errorf("identity record %s has empty ghost_id", IdentityPath(workspace))
	}
	return &id, nil
}

// EnsureIdentity returns the existing identity or creates one for a brand-new
// Ghost. It is idempotent: the ghost_id is minted exactly once and then
// preserved for the life of the Ghost.
func EnsureIdentity(workspace string) (*Identity, error) {
	existing, err := LoadIdentity(workspace)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	id := &Identity{
		SchemaVersion: CurrentSchemaVersion,
		GhostID:       uuid.NewString(),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal identity: %w", err)
	}
	if err := writeFileAtomic(IdentityPath(workspace), data, 0600); err != nil {
		return nil, fmt.Errorf("write identity: %w", err)
	}
	return id, nil
}
