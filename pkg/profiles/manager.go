package profiles

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const DefaultProfilesDir = ".ghost/profiles"

type Manager struct {
	profilesDir string
	mu          sync.RWMutex
}

func NewManager(homeDir string) *Manager {
	return &Manager{
		profilesDir: filepath.Join(homeDir, "profiles"),
	}
}

func (m *Manager) ProfilesDir() string {
	return m.profilesDir
}

func (m *Manager) List() ([]*Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := os.ReadDir(m.profilesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var profiles []*Profile
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		configPath := filepath.Join(m.profilesDir, entry.Name(), "config.json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			continue
		}
		p := &Profile{
			Name:      entry.Name(),
			GhostHome: filepath.Join(m.profilesDir, entry.Name()),
		}
		if err := p.Load(); err != nil {
			continue
		}
		profiles = append(profiles, p)
	}

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})

	return profiles, nil
}

func (m *Manager) Get(name string) (*Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := filepath.Join(m.profilesDir, name)
	configPath := filepath.Join(path, "config.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("profile %q not found", name)
	}

	p := &Profile{
		Name:      name,
		GhostHome: path,
	}
	if err := p.Load(); err != nil {
		return nil, fmt.Errorf("failed to load profile %q: %w", name, err)
	}
	return p, nil
}

func (m *Manager) Create(name, description string, env map[string]string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.profilesDir, name)
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("profile %q already exists", name)
	}

	p := &Profile{
		Name:        name,
		Description: description,
		GhostHome:   path,
		Workspace:   filepath.Join(path, "workspace"),
		Skills:      []string{},
		Env:         env,
		Config:      make(map[string]any),
	}

	if err := p.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("failed to create profile dirs: %w", err)
	}

	if err := p.Save(); err != nil {
		return nil, fmt.Errorf("failed to save profile: %w", err)
	}

	return p, nil
}

func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.profilesDir, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("profile %q not found", name)
	}

	return os.RemoveAll(path)
}

func (m *Manager) ActiveProfile() string {
	val := os.Getenv("GHOST_PROFILE")
	if val != "" {
		return val
	}
	return "default"
}

func (m *Manager) SetActive(name string) error {
	return os.Setenv("GHOST_PROFILE", name)
}

func (m *Manager) ExportEnv(name string) (map[string]string, error) {
	p, err := m.Get(name)
	if err != nil {
		return nil, err
	}
	env := map[string]string{
		"GHOST_HOME":  p.GhostHome,
		"GHOST_PROFILE": p.Name,
	}
	for k, v := range p.Env {
		env[k] = v
	}
	return env, nil
}

func (m *Manager) LoadEnvFile(name string) (map[string]string, error) {
	p, err := m.Get(name)
	if err != nil {
		return nil, err
	}

	envPath := p.EnvPath()
	data, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return p.Env, nil
		}
		return nil, err
	}

	result := make(map[string]string)
	for k, v := range p.Env {
		result[k] = v
	}

	lines := splitLines(string(data))
	for _, line := range lines {
		line = trimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		eqPos := indexOf(line, "=")
		if eqPos < 0 {
			continue
		}
		key := trimSpace(line[:eqPos])
		val := trimSpace(line[eqPos+1:])
		result[key] = val
	}

	return result, nil
}

func splitLines(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
