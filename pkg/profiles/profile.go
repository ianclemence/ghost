package profiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Avatar struct {
	Shape  string `json:"shape,omitempty"`
	Color  string `json:"color,omitempty"`
	Image  string `json:"image,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

type Group struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Channel struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Topic       string    `json:"topic,omitempty"`
	CreatedBy   string    `json:"created_by"`
	Members     []string  `json:"members"`
	CreatedAt   time.Time `json:"created_at"`
}

type ChannelMessage struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	Sender    string    `json:"sender"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type Profile struct {
	Name        string            `json:"name"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description"`
	Group       string            `json:"group,omitempty"`
	Avatar      *Avatar           `json:"avatar,omitempty"`
	GhostHome   string            `json:"ghost_home"`
	Workspace   string            `json:"workspace"`
	Skills      []string          `json:"skills,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Config      map[string]any    `json:"config,omitempty"`
	Channels    []string          `json:"channels,omitempty"`
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

func (p *Profile) ChannelsPath() string {
	return filepath.Join(p.GhostHome, "channels")
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
		p.ChannelsPath(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

const (
	AvatarShapeCircle    = "circle"
	AvatarShapeSquircle  = "squircle"
	AvatarShapePill      = "pill"
	AvatarShapeTriangle  = "triangle"
	AvatarShapeHexagon   = "hexagon"
	AvatarShapeCloud     = "cloud"
	AvatarShapeDrop      = "drop"
)

var AvatarShapes = []string{
	AvatarShapeCircle,
	AvatarShapeSquircle,
	AvatarShapePill,
	AvatarShapeTriangle,
	AvatarShapeHexagon,
	AvatarShapeCloud,
	AvatarShapeDrop,
}

var AvatarColors = []string{
	"#f5f5f4", "#8d6748", "#ef4444", "#f97316",
	"#eab308", "#22c55e", "#3b82f6", "#8b5cf6",
	"#ec4899", "#9ca3af",
}
