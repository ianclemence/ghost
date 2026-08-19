package ghoststate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// ImportOptions controls an import into a target installation.
type ImportOptions struct {
	Workspace  string
	ConfigPath string
	Source     string
	Passphrase string
	Force      bool
}

// Import restores a Ghost State archive into a fresh Ghost installation. It
// refuses to run over an existing installation unless Force is set; there is
// no merge logic in v1. Portable and derived state are written back, rebound
// (device-specific) state is intentionally not, and secrets are restored only
// when the archive was exported with --include-secrets.
func Import(opts ImportOptions) (*Manifest, error) {
	if opts.Workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if opts.ConfigPath == "" {
		return nil, fmt.Errorf("config path is required")
	}
	if opts.Source == "" {
		return nil, fmt.Errorf("source archive path is required")
	}
	if opts.Passphrase == "" {
		return nil, fmt.Errorf("passphrase is required")
	}

	blob, err := os.ReadFile(opts.Source)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	plain, err := decryptBytes(blob, opts.Passphrase)
	if err != nil {
		return nil, err
	}
	manifest, files, err := readArchive(plain)
	if err != nil {
		return nil, err
	}

	if !opts.Force {
		if fileExists(IdentityPath(opts.Workspace)) || fileExists(filepath.Join(opts.Workspace, "ghost.db")) {
			return nil, fmt.Errorf("target workspace %s is not a fresh Ghost installation (identity or ghost.db already present); re-run with --force to overwrite", opts.Workspace)
		}
	}

	for _, f := range manifest.Files {
		data, ok := files[f.Path]
		if !ok {
			return nil, fmt.Errorf("archive is missing %s declared in the manifest", f.Path)
		}
		if digestBytes(data) != f.Digest {
			return nil, fmt.Errorf("integrity check failed for %s", f.Path)
		}
		if strings.HasPrefix(f.Path, conversationsDirLogical+"/") {
			// Portable conversations are rehydrated into a fresh ghost.db below,
			// never written as inert files the runtime would ignore.
			continue
		}
		if err := writeImportedArtifact(opts, f, data); err != nil {
			return nil, err
		}
	}

	// Conversations are the portable record; the runtime database is rebuilt
	// from them so the target's ghost.db is a fresh index on this machine.
	if manifest.File(conversationsFormatLogical) != nil {
		if err := rehydrateConversations(opts.Workspace, files); err != nil {
			return nil, fmt.Errorf("rehydrate conversations: %w", err)
		}
	}

	// Identity is portable state and always restored so the migrated Ghost is
	// the same Ghost, even on new hardware.
	createdAt := manifest.IdentityCreatedAt
	if createdAt == "" {
		createdAt = manifest.ExportedAt
	}
	id := &Identity{SchemaVersion: CurrentSchemaVersion, GhostID: manifest.GhostID, CreatedAt: createdAt}
	idData, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal identity: %w", err)
	}
	if err := writeFileAtomic(IdentityPath(opts.Workspace), idData, 0600); err != nil {
		return nil, fmt.Errorf("write identity: %w", err)
	}

	return manifest, nil
}

func writeImportedArtifact(opts ImportOptions, f FileEntry, data []byte) error {
	perm := os.FileMode(f.Mode)
	switch f.Path {
	case configJSONLogical:
		return writeImportedConfig(opts, data)
	case configSecretsLogical:
		return writeFileAtomic(config.SecretsPath(opts.ConfigPath), data, 0600)
	case configEnvLogical:
		return writeFileAtomic(filepath.Join(filepath.Dir(opts.ConfigPath), ".env"), data, 0600)
	case identityJSONLogical:
		return writeFileAtomic(IdentityPath(opts.Workspace), data, 0600)
	default:
		dest := filepath.Join(opts.Workspace, filepath.FromSlash(f.Path))
		if !withinWorkspace(opts.Workspace, dest) {
			return fmt.Errorf("refusing to write outside workspace: %s", f.Path)
		}
		if perm == 0 {
			perm = 0644
		}
		return writeFileAtomic(dest, data, perm)
	}
}

// writeImportedConfig merges the portable config back while forcing the target
// workspace and preserving device-specific networking from the target machine.
func writeImportedConfig(opts ImportOptions, exported []byte) error {
	final := config.DefaultConfig()
	if err := json.Unmarshal(exported, final); err != nil {
		return fmt.Errorf("parse exported config: %w", err)
	}
	final.Agents.Defaults.Workspace = opts.Workspace

	if fileExists(opts.ConfigPath) {
		target, err := config.LoadConfig(opts.ConfigPath)
		if err != nil {
			return fmt.Errorf("load target config: %w", err)
		}
		src := allProviders(target)
		dst := allProviders(final)
		for i := range src {
			if src[i].APIBase != "" {
				dst[i].APIBase = src[i].APIBase
			}
			if src[i].Proxy != "" {
				dst[i].Proxy = src[i].Proxy
			}
		}
		if target.Gateway.Host != "" {
			final.Gateway.Host = target.Gateway.Host
		}
		if target.Gateway.Port != 0 {
			final.Gateway.Port = target.Gateway.Port
		}
	}

	data, err := config.MarshalSanitized(final)
	if err != nil {
		return fmt.Errorf("sanitize config: %w", err)
	}
	return writeFileAtomic(opts.ConfigPath, data, 0600)
}

func withinWorkspace(workspace, dest string) bool {
	rel, err := filepath.Rel(workspace, dest)
	if err != nil {
		return false
	}
	return rel != ".." && !hasPrefixSegment(filepath.ToSlash(rel), "..")
}
