// Package ghost is the canonical Ghost entity: the persistent AI that
// owns conversations, contexts, memory, capabilities, permissions,
// routines, and activity.
//
// Borrowed pattern (OpenMausBot): agents-as-contacts — each agent is an
// entity with identity, model, instructions, memory scope, tools,
// permissions, and state, and the UI talks to the entity, not to a raw
// chat endpoint. Ghost keeps ONE primary entity in V1 (no multi-agent
// orchestration); the Agent domain model is extensible so future
// specialized agents (Work, Research, Home, Travel) attach without a
// rewrite.
//
// A conversation is one interface into Ghost — never the system of
// record. Ghost state exists independently of any chat session.
package ghost

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle state of the Ghost entity.
type Status string

const (
	StatusReady      Status = "ready"
	StatusSetup      Status = "setup"
	StatusDegraded   Status = "degraded"
	StatusRecovering Status = "recovering"
)

// Ghost is the persistent entity. Identity fields are canonical here;
// ghoststate.Identity remains the on-disk root — this package reads it
// and adds the product layer (status, timezone, locale, updated_at)
// without duplicating the ghost_id across random config files.
type Ghost struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"owner_id"`
	Status    Status    `json:"status"`
	Timezone  string    `json:"timezone"`
	Locale    string    `json:"locale"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Owner is the canonical owner identity. V1 has exactly one owner.
type Owner struct {
	ID          string            `json:"id"`
	DisplayName string            `json:"display_name"`
	Timezone    string            `json:"timezone"`
	Locale      string            `json:"locale"`
	Preferences map[string]string `json:"preferences,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Agent is the extensible agent domain model. V1 instantiates exactly
// one primary agent (Kind="main", IsPrimary=true). Future specialized
// agents reuse every field; the permission broker and event stream
// already key on AgentID.
type Agent struct {
	ID           string            `json:"id"`
	GhostID      string            `json:"ghost_id"`
	Kind         string            `json:"kind"` // "main" in V1; e.g. "work" later
	DisplayName  string            `json:"display_name"`
	Model        string            `json:"model,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
	MemoryScope  string            `json:"memory_scope,omitempty"`
	Tools        []string          `json:"tools,omitempty"`
	Permissions  map[string]string `json:"permissions,omitempty"`
	State        map[string]string `json:"state,omitempty"`
	IsPrimary    bool              `json:"is_primary"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

const (
	entityFile = "state/ghost-entity.json"
	ownerFile  = "state/ghost-owner.json"
	agentsFile = "state/ghost-agents.json"
)

// Store is the canonical file-backed entity store. Writes are atomic;
// the ghost_id is minted once and preserved across restarts.
type Store struct {
	mu        sync.RWMutex
	workspace string
	ghost     *Ghost
	owner     *Owner
	agents    map[string]*Agent
}

func loadJSON(path string, v interface{}) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

func storeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Open loads (or initializes) the entity store for a workspace. The
// ghost_id is adopted from ghoststate identity when present so there is
// exactly one "which Ghost is this" answer; otherwise minted once here.
func Open(workspace, ghostID, ghostName, ownerName string) (*Store, error) {
	s := &Store{workspace: workspace, agents: map[string]*Agent{}}
	var g Ghost
	if loadJSON(filepath.Join(workspace, entityFile), &g) && g.ID != "" {
		s.ghost = &g
	} else {
		id := strings.TrimSpace(ghostID)
		if id == "" {
			id = "ghost-" + uuid.NewString()[:8]
		}
		now := time.Now()
		s.ghost = &Ghost{ID: id, Name: ghostName, Status: StatusSetup, CreatedAt: now, UpdatedAt: now}
		if err := s.saveGhost(); err != nil {
			return nil, err
		}
	}
	if s.ghost.Name == "" && ghostName != "" {
		s.ghost.Name = ghostName
		s.ghost.UpdatedAt = time.Now()
		_ = s.saveGhost()
	}
	var o Owner
	if loadJSON(filepath.Join(workspace, ownerFile), &o) && o.ID != "" {
		s.owner = &o
	} else {
		now := time.Now()
		s.owner = &Owner{ID: "owner-" + uuid.NewString()[:8], DisplayName: ownerName, CreatedAt: now, UpdatedAt: now}
		if err := s.saveOwner(); err != nil {
			return nil, err
		}
	}
	if s.owner.DisplayName == "" && ownerName != "" {
		s.owner.DisplayName = ownerName
		s.owner.UpdatedAt = time.Now()
		_ = s.saveOwner()
	}
	s.ghost.OwnerID = s.owner.ID
	var agents []*Agent
	if loadJSON(filepath.Join(workspace, agentsFile), &agents) {
		for _, a := range agents {
			if a != nil && a.ID != "" {
				s.agents[a.ID] = a
			}
		}
	}
	if len(s.agents) == 0 {
		now := time.Now()
		primary := &Agent{ID: "agent-main", GhostID: s.ghost.ID, Kind: "main",
			DisplayName: "Ghost", IsPrimary: true, CreatedAt: now, UpdatedAt: now}
		s.agents[primary.ID] = primary
		if err := s.saveAgents(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) saveGhost() error { return storeJSON(filepath.Join(s.workspace, entityFile), s.ghost) }
func (s *Store) saveOwner() error { return storeJSON(filepath.Join(s.workspace, ownerFile), s.owner) }
func (s *Store) saveAgents() error {
	list := make([]*Agent, 0, len(s.agents))
	for _, a := range s.agents {
		list = append(list, a)
	}
	return storeJSON(filepath.Join(s.workspace, agentsFile), list)
}

// GhostEntity returns the canonical Ghost (a copy).
func (s *Store) GhostEntity() Ghost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s.ghost
}

// OwnerEntity returns the canonical owner (a copy).
func (s *Store) OwnerEntity() Owner {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s.owner
}

// Rename updates the Ghost name (user-visible identity change).
func (s *Store) Rename(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ghost.Name = strings.TrimSpace(name)
	s.ghost.UpdatedAt = time.Now()
	return s.saveGhost()
}

// SetStatus updates lifecycle status.
func (s *Store) SetStatus(st Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ghost.Status = st
	s.ghost.UpdatedAt = time.Now()
	return s.saveGhost()
}

// SetTimezone validates (IANA) and stores the Ghost timezone.
func (s *Store) SetTimezone(tz string) error {
	if _, err := time.LoadLocation(tz); err != nil {
		return errors.New("unknown timezone")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ghost.Timezone = tz
	s.ghost.UpdatedAt = time.Now()
	return s.saveGhost()
}

// SetOwnerDetails updates display name/timezone/locale.
func (s *Store) SetOwnerDetails(name, tz, locale string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name != "" {
		s.owner.DisplayName = name
	}
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return errors.New("unknown timezone")
		}
		s.owner.Timezone = tz
	}
	if locale != "" {
		s.owner.Locale = locale
	}
	s.owner.UpdatedAt = time.Now()
	return s.saveOwner()
}

// PrimaryAgent returns the V1 primary agent.
func (s *Store) PrimaryAgent() Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.agents {
		if a.IsPrimary {
			return *a
		}
	}
	// Invariant: Open always ensures one primary; defensive fallback.
	return Agent{ID: "agent-main", GhostID: s.ghost.ID, Kind: "main", IsPrimary: true}
}

// RegisterAgent adds a future specialized agent. V1 callers should not
// need this (one primary exists); it exists so extension needs no
// rewrite. Registering a second primary is rejected.
func (s *Store) RegisterAgent(a Agent) error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.Kind) == "" {
		return errors.New("agent id and kind required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.agents[a.ID]; exists {
		return errors.New("agent already registered")
	}
	if a.IsPrimary {
		for _, e := range s.agents {
			if e.IsPrimary {
				return errors.New("a primary agent already exists")
			}
		}
	}
	now := time.Now()
	a.GhostID = s.ghost.ID
	a.CreatedAt = now
	a.UpdatedAt = now
	cp := a
	s.agents[a.ID] = &cp
	return s.saveAgents()
}

// Agents lists all registered agents.
func (s *Store) Agents() []Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, *a)
	}
	return out
}
