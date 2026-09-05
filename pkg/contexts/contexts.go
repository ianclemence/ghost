// Package contexts defines scoped environments within ONE Ghost.
//
// A context (Personal, Work, Project, Home, Travel) scopes instructions,
// memory, files, capabilities, permissions, conversations, and routines
// — but never forks the Ghost. Isolation is explicit ownership/scope
// metadata (no graph database): every check below is a pure function
// over the context record and the resource's scope tags.
package contexts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Kind classifies contexts; custom project contexts use KindProject.
type Kind string

const (
	KindPersonal Kind = "personal"
	KindWork     Kind = "work"
	KindHome     Kind = "home"
	KindTravel   Kind = "travel"
	KindProject  Kind = "project"
)

// Context is a scoped environment owned by a Ghost.
type Context struct {
	ID              string    `json:"id"`
	GhostID         string    `json:"ghost_id"`
	Kind            Kind      `json:"kind"`
	Name            string    `json:"name"`
	Instructions    string    `json:"instructions,omitempty"`
	MemoryScopes    []string  `json:"memory_scopes,omitempty"`
	Capabilities    []string  `json:"capabilities,omitempty"`
	FileRoots       []string  `json:"file_roots,omitempty"`
	ConversationIDs []string  `json:"conversation_ids,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Store is the file-backed context registry (one Ghost, many contexts).
type Store struct {
	mu       sync.RWMutex
	path     string
	ghostID  string
	contexts map[string]*Context
	sessions map[string]string // sessionKey → contextID
}

func (s *Store) persist() error {
	list := make([]*Context, 0, len(s.contexts))
	for _, c := range s.contexts {
		list = append(list, c)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Open loads the registry, ensuring the default personal context.
func Open(workspace, ghostID string) (*Store, error) {
	s := &Store{path: filepath.Join(workspace, "state", "contexts.json"), ghostID: ghostID, contexts: map[string]*Context{}}
	s.loadSessions()
	if data, err := os.ReadFile(s.path); err == nil {
		var list []*Context
		if json.Unmarshal(data, &list) == nil {
			for _, c := range list {
				if c != nil && c.ID != "" && c.GhostID == ghostID {
					s.contexts[c.ID] = c
				}
			}
		}
	}
	if _, ok := s.contexts["personal"]; !ok {
		now := time.Now()
		s.contexts["personal"] = &Context{ID: "personal", GhostID: ghostID, Kind: KindPersonal,
			Name: "Personal", CreatedAt: now, UpdatedAt: now}
		if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
			return nil, err
		}
		if err := s.persist(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Create adds a context (project contexts carry their own instructions
// and scopes; personal/work/home/travel are singletons by kind).
func (s *Store) Create(kind Kind, name string) (*Context, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("context name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind != KindProject {
		for _, c := range s.contexts {
			if c.Kind == kind {
				return nil, errors.New("context kind already exists")
			}
		}
	}
	id := strings.ToLower(string(kind))
	if kind == KindProject {
		id = "project-" + strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			if r >= 'A' && r <= 'Z' {
				return r + 32
			}
			return '-'
		}, name)
		id = strings.Trim(id, "-")
		if id == "project-" || id == "project" {
			return nil, errors.New("invalid project name")
		}
	}
	if _, exists := s.contexts[id]; exists {
		return nil, errors.New("context already exists")
	}
	now := time.Now()
	c := &Context{ID: id, GhostID: s.ghostID, Kind: kind, Name: name, CreatedAt: now, UpdatedAt: now}
	s.contexts[id] = c
	return c, s.persist()
}

// SetCapabilities replaces a context's capability allowlist (empty =
// all allowed). Explicit user configuration only — never model output.
func (s *Store) SetCapabilities(id string, caps []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.contexts[id]
	if !ok {
		return errors.New("unknown context")
	}
	c.Capabilities = append([]string{}, caps...)
	c.UpdatedAt = time.Now()
	return s.persist()
}

// Get returns a context by id.
func (s *Store) Get(id string) (*Context, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.contexts[id]
	if !ok {
		return nil, false
	}
	cp := *c
	return &cp, true
}

// List returns all contexts.
func (s *Store) List() []*Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Context, 0, len(s.contexts))
	for _, c := range s.contexts {
		cp := *c
		out = append(out, &cp)
	}
	return out
}

// CanAccessMemory: a context sees memories tagged with one of its scopes,
// plus untagged (global) memories. Memories scoped to OTHER contexts are
// invisible — Work never sees Home-scoped facts.
func CanAccessMemory(c *Context, memoryScopes []string) bool {
	if len(memoryScopes) == 0 {
		return true // global memory is shared
	}
	mine := map[string]bool{"context:" + c.ID: true}
	for _, s := range c.MemoryScopes {
		mine[s] = true
	}
	for _, s := range memoryScopes {
		if mine[s] {
			return true
		}
	}
	return false
}

// CanUseCapability: empty context capability list means all allowed
// (V1 default); a non-empty list is an allowlist.
func CanUseCapability(c *Context, capabilityID string) bool {
	if len(c.Capabilities) == 0 {
		return true
	}
	for _, allowed := range c.Capabilities {
		if allowed == capabilityID {
			return true
		}
	}
	return false
}

// FileInScope: files must live under one of the context's roots when
// roots are configured; unconfigured contexts allow workspace files
// (runtime still confines to the workspace separately).
func FileInScope(c *Context, workspace, path string) bool {
	if len(c.FileRoots) == 0 {
		return true
	}
	rel, err := filepath.Rel(workspace, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return false
	}
	for _, root := range c.FileRoots {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

// sessionFile maps sessions to contexts (default: personal).
const sessionFile = "state/session-context.json"

// SessionContext returns the context id for a session (personal default).
func (s *Store) SessionContext(sessionKey string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sessions == nil {
		return "personal"
	}
	if id, ok := s.sessions[sessionKey]; ok {
		if _, exists := s.contexts[id]; exists {
			return id
		}
	}
	return "personal"
}

// SetSessionContext moves a session into a context. Unknown contexts fail.
func (s *Store) SetSessionContext(sessionKey, contextID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.contexts[contextID]; !ok {
		return errors.New("unknown context")
	}
	if s.sessions == nil {
		s.sessions = map[string]string{}
	}
	s.sessions[sessionKey] = contextID
	return s.persistSessions()
}

func (s *Store) persistSessions() error {
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(s.path), "session-context.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) loadSessions() {
	s.sessions = map[string]string{}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(s.path), "session-context.json"))
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.sessions)
}

// ScopesForSession returns the memory scopes for a session: the global
// set plus the session context's tag (e.g. "context:work"). Retrieval
// uses these; writes tag new entries with the context tag when the
// session sits in a non-personal context (personal memories stay global
// for V1 simplicity and backward compatibility).
func (s *Store) ScopesForSession(sessionKey string) []string {
	ctxID := s.SessionContext(sessionKey)
	c, ok := s.contexts[ctxID]
	if !ok {
		return nil
	}
	out := append([]string{}, c.MemoryScopes...)
	out = append(out, "context:"+c.ID)
	return out
}

// WriteScopes returns scope tags for NEW memories from a session: empty
// for personal (global), [context:<id>] otherwise.
func (s *Store) WriteScopes(sessionKey string) []string {
	if s.SessionContext(sessionKey) == "personal" {
		return nil
	}
	return []string{"context:" + s.SessionContext(sessionKey)}
}
