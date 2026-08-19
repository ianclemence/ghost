package ghoststate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
)

// ExportOptions controls an export. Passphrase must be non-empty; the CLI
// prompts for it, tests pass it directly.
type ExportOptions struct {
	Workspace      string
	ConfigPath     string
	Destination    string
	Passphrase     string
	IncludeSecrets bool
}

// Export produces an encrypted .ghost archive containing the workspace's
// portable Ghost State plus a versioned manifest. The manifest is the schema
// contract for the archive; the archive layout is independent of the source
// filesystem layout. Any artifact that is not recognized as Ghost State fails
// the export so state can never silently disappear.
func Export(opts ExportOptions) (*Manifest, error) {
	if opts.Workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if opts.Destination == "" {
		return nil, fmt.Errorf("destination archive path is required")
	}
	if opts.Passphrase == "" {
		return nil, fmt.Errorf("passphrase is required")
	}

	id, err := EnsureIdentity(opts.Workspace)
	if err != nil {
		return nil, fmt.Errorf("ensure identity: %w", err)
	}

	hostname, _ := os.Hostname()
	manifest := &Manifest{
		Format:            Format,
		SchemaVersion:     CurrentSchemaVersion,
		GhostID:           id.GhostID,
		IdentityCreatedAt: id.CreatedAt,
		ExportedAt:        time.Now().UTC().Format(time.RFC3339),
		SecretsIncluded:   opts.IncludeSecrets,
		Origin:            Origin{Hostname: hostname},
	}

	stagingDir, err := os.MkdirTemp("", "ghost-state-export-*")
	if err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	staging := make(map[string]string)

	// Configuration is portable, but device-specific fields are zeroed and
	// recorded as rebound so the target re-applies its own networking.
	if fileExists(opts.ConfigPath) {
		cfg, err := config.LoadConfig(opts.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
		zeroDeviceFields(cfg)
		data, err := config.MarshalSanitized(cfg)
		if err != nil {
			return nil, fmt.Errorf("sanitize config: %w", err)
		}
		if err := stageEntry(staging, stagingDir, manifest, configJSONLogical, CategoryPortable, data, 0600); err != nil {
			return nil, err
		}
		manifest.Rebound = append(manifest.Rebound,
			"config/config.json: agents.defaults.workspace, gateway.host, gateway.port, providers.*.api_base, providers.*.proxy")
	} else {
		manifest.Rebound = append(manifest.Rebound, "config/config.json (absent on source; not exported)")
	}

	// The strict secrets boundary lives next to the config file. Both are
	// excluded by default and only travel when explicitly opted in.
	for _, sc := range []struct {
		logical string
		path    string
	}{
		{configSecretsLogical, config.SecretsPath(opts.ConfigPath)},
		{configEnvLogical, filepath.Join(filepath.Dir(opts.ConfigPath), ".env")},
	} {
		if !fileExists(sc.path) {
			continue
		}
		data, err := os.ReadFile(sc.path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", sc.path, err)
		}
		if opts.IncludeSecrets {
			if err := stageEntry(staging, stagingDir, manifest, sc.logical, CategorySecret, data, 0600); err != nil {
				return nil, err
			}
		} else {
			manifest.SecretsExcluded = append(manifest.SecretsExcluded, sc.logical)
		}
	}

	// Workspace walk. Every artifact is classified; unknown files abort.
	err = filepath.WalkDir(opts.Workspace, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(opts.Workspace, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		cat, err := classifyWorkspaceFile(rel)
		if err != nil {
			return err
		}
		switch cat {
		case CategoryDisposable:
			return nil
		case CategoryRebound:
			manifest.Rebound = append(manifest.Rebound, rel)
			return nil
		case CategorySecret:
			if opts.IncludeSecrets {
				return stageFromDisk(staging, stagingDir, manifest, rel, cat, d, p)
			}
			manifest.SecretsExcluded = append(manifest.SecretsExcluded, rel)
			return nil
		case CategoryPortable, CategoryDerived:
			if rel == ghostDBLogical {
				// Snapshot the WAL-mode database into a consistent single
				// file so the archive never carries -wal/-shm fragments.
				return stageDB(staging, stagingDir, manifest, p, d)
			}
			return stageFromDisk(staging, stagingDir, manifest, rel, cat, d, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("export workspace: %w", err)
	}

	// Deterministic manifest ordering.
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })

	archive, err := buildArchive(staging, manifest)
	if err != nil {
		return nil, err
	}
	encrypted, err := encryptBytes(archive, opts.Passphrase)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(opts.Destination, encrypted, 0600); err != nil {
		return nil, fmt.Errorf("write archive %s: %w", opts.Destination, err)
	}
	return manifest, nil
}

// zeroDeviceFields strips fields that belong to the source machine so the
// exported config is portable: the workspace path and local networking.
func zeroDeviceFields(cfg *config.Config) {
	cfg.Agents.Defaults.Workspace = ""
	for _, p := range allProviders(cfg) {
		p.APIBase = ""
		p.Proxy = ""
	}
	cfg.Gateway.Host = ""
	cfg.Gateway.Port = 0
}

func allProviders(cfg *config.Config) []*config.ProviderConfig {
	return []*config.ProviderConfig{
		&cfg.Providers.Anthropic, &cfg.Providers.OpenAI,
		&cfg.Providers.OpenRouter, &cfg.Providers.Groq,
		&cfg.Providers.Zhipu, &cfg.Providers.VLLM,
		&cfg.Providers.Gemini, &cfg.Providers.Nvidia,
		&cfg.Providers.Moonshot, &cfg.Providers.ShengSuanYun,
		&cfg.Providers.DeepSeek, &cfg.Providers.GitHubCopilot,
		&cfg.Providers.Ollama,
	}
}

func stageEntry(staging map[string]string, stagingDir string, m *Manifest, logical string, cat Category, data []byte, perm os.FileMode) error {
	src, err := writeStaged(stagingDir, data, perm)
	if err != nil {
		return err
	}
	staging[logical] = src
	m.Files = append(m.Files, FileEntry{
		Path:     logical,
		Category: cat,
		Digest:   digestBytes(data),
		Size:     int64(len(data)),
		Mode:     uint32(perm),
	})
	return nil
}

func stageFromDisk(staging map[string]string, stagingDir string, m *Manifest, logical string, cat Category, d os.DirEntry, abs string) error {
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("read %s: %w", abs, err)
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	return stageEntry(staging, stagingDir, m, logical, cat, data, info.Mode().Perm())
}

func stageDB(staging map[string]string, stagingDir string, m *Manifest, abs string, d os.DirEntry) error {
	snap := filepath.Join(stagingDir, "ghost.db")
	if err := snapshotDB(abs, snap); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(snap)
	if err != nil {
		return fmt.Errorf("read database snapshot: %w", err)
	}
	staging[ghostDBLogical] = snap
	m.Files = append(m.Files, FileEntry{
		Path:     ghostDBLogical,
		Category: CategoryPortable,
		Digest:   digestBytes(data),
		Size:     int64(len(data)),
		Mode:     uint32(info.Mode().Perm()),
	})
	return nil
}

func writeStaged(stagingDir string, data []byte, perm os.FileMode) (string, error) {
	f, err := os.CreateTemp(stagingDir, "f-*")
	if err != nil {
		return "", fmt.Errorf("create staging file: %w", err)
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return name, nil
}
