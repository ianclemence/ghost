package profiles

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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
		Title:       name,
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

func (m *Manager) Duplicate(sourceName, newName string) (*Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	srcPath := filepath.Join(m.profilesDir, sourceName)
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("source profile %q not found", sourceName)
	}

	dstPath := filepath.Join(m.profilesDir, newName)
	if _, err := os.Stat(dstPath); err == nil {
		return nil, fmt.Errorf("profile %q already exists", newName)
	}

	if err := copyDir(srcPath, dstPath); err != nil {
		return nil, fmt.Errorf("failed to duplicate profile: %w", err)
	}

	p := &Profile{
		Name:      newName,
		GhostHome: dstPath,
	}
	if err := p.Load(); err != nil {
		os.RemoveAll(dstPath)
		return nil, fmt.Errorf("failed to load duplicated profile: %w", err)
	}

	p.Name = newName
	p.Title = newName
	if err := p.Save(); err != nil {
		os.RemoveAll(dstPath)
		return nil, fmt.Errorf("failed to save duplicated profile: %w", err)
	}

	return p, nil
}

func (m *Manager) UniqueName(base string) string {
	name := base
	for i := 2; ; i++ {
		path := filepath.Join(m.profilesDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return name
		}
		name = base + "-" + strconv.Itoa(i)
	}
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
		"GHOST_HOME":    p.GhostHome,
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

func (m *Manager) ListGroups() ([]string, error) {
	profiles, err := m.List()
	if err != nil {
		return nil, err
	}

	groupSet := make(map[string]bool)
	for _, p := range profiles {
		if p.Group != "" {
			groupSet[p.Group] = true
		}
	}

	groups := make([]string, 0, len(groupSet))
	for g := range groupSet {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups, nil
}

func (m *Manager) GroupRoster() (ungrouped []*Profile, groups map[string][]*Profile, err error) {
	profiles, err := m.List()
	if err != nil {
		return nil, nil, err
	}

	groups = make(map[string][]*Profile)
	for _, p := range profiles {
		if p.Group == "" {
			ungrouped = append(ungrouped, p)
		} else {
			groups[p.Group] = append(groups[p.Group], p)
		}
	}

	return ungrouped, groups, nil
}

func (m *Manager) SetGroup(name, group string) error {
	p, err := m.Get(name)
	if err != nil {
		return err
	}

	p.Group = group
	return p.Save()
}

func (m *Manager) CreateChannel(name, topic, createdBy string, members []string) (*Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(members) == 0 {
		return nil, fmt.Errorf("channel must have at least one member")
	}

	channelID := strings.ReplaceAll(strings.ToLower(name), " ", "-")
	for _, member := range members {
		channelsDir := filepath.Join(m.profilesDir, member, "channels")
		if err := os.MkdirAll(channelsDir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create channels dir for %s: %w", member, err)
		}

		channel := &Channel{
			ID:        channelID,
			Name:      name,
			Topic:     topic,
			CreatedBy: createdBy,
			Members:   members,
			CreatedAt: time.Now(),
		}

		channelPath := filepath.Join(channelsDir, channelID+".json")
		data, err := json.MarshalIndent(channel, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(channelPath, data, 0600); err != nil {
			return nil, err
		}

		messagesPath := filepath.Join(channelsDir, channelID+".log")
		if _, err := os.Stat(messagesPath); os.IsNotExist(err) {
			os.WriteFile(messagesPath, nil, 0600)
		}
	}

	return &Channel{
		ID:        channelID,
		Name:      name,
		Topic:     topic,
		CreatedBy: createdBy,
		Members:   members,
		CreatedAt: time.Now(),
	}, nil
}

func (m *Manager) GetChannel(channelID string) (*Channel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	profiles, err := m.listUnlocked()
	if err != nil {
		return nil, err
	}

	for _, p := range profiles {
		channelPath := filepath.Join(p.ChannelsPath(), channelID+".json")
		data, err := os.ReadFile(channelPath)
		if err != nil {
			continue
		}
		var channel Channel
		if err := json.Unmarshal(data, &channel); err != nil {
			continue
		}
		return &channel, nil
	}

	return nil, fmt.Errorf("channel %q not found", channelID)
}

func (m *Manager) ListChannels(profileName string) ([]*Channel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channelsDir := filepath.Join(m.profilesDir, profileName, "channels")
	entries, err := os.ReadDir(channelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var channels []*Channel
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(channelsDir, entry.Name()))
		if err != nil {
			continue
		}
		var channel Channel
		if err := json.Unmarshal(data, &channel); err != nil {
			continue
		}
		channels = append(channels, &channel)
	}

	return channels, nil
}

func (m *Manager) SendChannelMessage(channelID, sender, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	channel, err := m.getChannelUnlocked(channelID)
	if err != nil {
		return err
	}

	isMember := false
	for _, member := range channel.Members {
		if member == sender {
			isMember = true
			break
		}
	}
	if !isMember {
		return fmt.Errorf("%q is not a member of channel %q", sender, channelID)
	}

	msg := ChannelMessage{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		ChannelID: channelID,
		Sender:    sender,
		Content:   content,
		Timestamp: time.Now(),
	}

	msgLine := fmt.Sprintf("%s %s | %s\n",
		msg.Timestamp.Format("15:04:05"),
		msg.Sender,
		msg.Content)

	for _, member := range channel.Members {
		messagesPath := filepath.Join(m.profilesDir, member, "channels", channelID+".log")
		f, err := os.OpenFile(messagesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			continue
		}
		f.WriteString(msgLine)
		f.Close()
	}

	return nil
}

func (m *Manager) ReadChannelHistory(channelID string, maxLines int) ([]ChannelMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, err := m.getChannelUnlocked(channelID)
	if err != nil {
		return nil, err
	}

	if len(channel.Members) == 0 {
		return nil, nil
	}

	messagesPath := filepath.Join(m.profilesDir, channel.Members[0], "channels", channelID+".log")
	data, err := os.ReadFile(messagesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	lines := splitLines(string(data))
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	var messages []ChannelMessage
	for _, line := range lines {
		if msg := parseChannelMessage(line, channelID); msg != nil {
			messages = append(messages, *msg)
		}
	}

	return messages, nil
}

func (m *Manager) DeleteChannel(channelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	channel, err := m.getChannelUnlocked(channelID)
	if err != nil {
		return err
	}

	for _, member := range channel.Members {
		channelPath := filepath.Join(m.profilesDir, member, "channels", channelID+".json")
		messagesPath := filepath.Join(m.profilesDir, member, "channels", channelID+".log")
		os.Remove(channelPath)
		os.Remove(messagesPath)
	}

	return nil
}

func (m *Manager) UpdateAvatar(name string, avatar *Avatar) error {
	p, err := m.Get(name)
	if err != nil {
		return err
	}

	p.Avatar = avatar
	return p.Save()
}

func (m *Manager) getChannelUnlocked(channelID string) (*Channel, error) {
	profiles, err := m.listUnlocked()
	if err != nil {
		return nil, err
	}

	for _, p := range profiles {
		channelPath := filepath.Join(p.ChannelsPath(), channelID+".json")
		data, err := os.ReadFile(channelPath)
		if err != nil {
			continue
		}
		var channel Channel
		if err := json.Unmarshal(data, &channel); err != nil {
			continue
		}
		return &channel, nil
	}

	return nil, fmt.Errorf("channel %q not found", channelID)
}

func (m *Manager) listUnlocked() ([]*Profile, error) {
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

	return profiles, nil
}

func parseChannelMessage(line, channelID string) *ChannelMessage {
	if len(line) < 10 {
		return nil
	}

	parts := splitN(line, " | ", 2)
	if len(parts) < 2 {
		return nil
	}

	tsSenderPart := parts[0]
	content := parts[1]

	spaceIdx := -1
	for i := 0; i < len(tsSenderPart); i++ {
		if tsSenderPart[i] == ' ' {
			spaceIdx = i
			break
		}
	}

	sender := "unknown"
	tsStr := tsSenderPart
	if spaceIdx > 0 {
		tsStr = tsSenderPart[:spaceIdx]
		sender = tsSenderPart[spaceIdx+1:]
	}

	ts, err := time.Parse("15:04:05", tsStr)
	if err != nil {
		return nil
	}

	return &ChannelMessage{
		ID:        fmt.Sprintf("msg-%d", ts.UnixNano()),
		ChannelID: channelID,
		Sender:    sender,
		Content:   content,
		Timestamp: ts,
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
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

func splitN(s, sep string, n int) []string {
	idx := 0
	var parts []string
	for i := 0; i < n-1; i++ {
		pos := indexOf(s[idx:], sep)
		if pos < 0 {
			break
		}
		parts = append(parts, s[idx:idx+pos])
		idx += pos + len(sep)
	}
	parts = append(parts, s[idx:])
	return parts
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
