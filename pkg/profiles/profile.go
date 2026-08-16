package profiles

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Profile struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	GhostHome   string            `json:"ghost_home"`
	Workspace   string            `json:"workspace"`
	Skills      []string          `json:"skills,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Config      map[string]any    `json:"config,omitempty"`
}

func (p *Profile) ConfigPath() string {
	return filepath.Join(p.GhostHome, "config.json")
}

func (p *Profile) EnvPath() string {
	return filepath.Join(p.GhostHome, ".env")
}

func (p *Profile) WorkspacePath() string {
	return filepath.Join(p.GhostHome, "workspace")
}

func (p *Profile) SkillsPath() string {
	return filepath.Join(p.GhostHome, "skills")
}

func (p *Profile) LogsPath() string {
	return filepath.Join(p.GhostHome, "logs")
}

func (p *Profile) Load() error {
	data, err := os.ReadFile(p.ConfigPath())
	if err != nil {
		return err
	}
	return json.Unmarshal(data, p)
}

func (p *Profile) Save() error {
	if err := os.MkdirAll(p.GhostHome, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.ConfigPath(), data, 0600)
}

func (p *Profile) EnsureDirs() error {
	dirs := []string{
		p.GhostHome,
		p.WorkspacePath(),
		p.SkillsPath(),
		p.LogsPath(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}
