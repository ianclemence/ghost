package tools

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

type ToolState string

const (
	ToolStateActive   ToolState = "active"
	ToolStateStale    ToolState = "stale"
	ToolStateArchived ToolState = "archived"
)

type ToolRecord struct {
	Name       string    `json:"name"`
	State      ToolState `json:"state"`
	UseCount   int       `json:"use_count"`
	LastUsedAt time.Time `json:"last_used_at"`
	CreatedAt  time.Time `json:"created_at"`
	Pinned     bool      `json:"pinned"`
}

type CuratorConfig struct {
	Enabled           bool `json:"enabled"`
	StaleAfterDays    int  `json:"stale_after_days"`
	ArchiveAfterDays  int  `json:"archive_after_days"`
	CheckIntervalMins int  `json:"check_interval_mins"`
}

type Curator struct {
	db       *sql.DB
	config   CuratorConfig
	mu       sync.RWMutex
	records  map[string]*ToolRecord
	stopCh   chan struct{}
}

func NewCurator(database *sql.DB, cfg CuratorConfig) *Curator {
	return &Curator{
		db:      database,
		config:  cfg,
		records: make(map[string]*ToolRecord),
		stopCh:  make(chan struct{}),
	}
}

func (c *Curator) EnsureSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS tool_usage (
			name TEXT PRIMARY KEY,
			state TEXT DEFAULT 'active',
			use_count INTEGER DEFAULT 0,
			last_used_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			pinned BOOLEAN DEFAULT FALSE
		)`,
	}
	for _, q := range queries {
		if _, err := c.db.Exec(q); err != nil {
			return fmt.Errorf("curator schema: %w", err)
		}
	}
	return c.loadRecords()
}

func (c *Curator) loadRecords() error {
	rows, err := c.db.Query(`SELECT name, state, use_count, last_used_at, created_at, pinned FROM tool_usage`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r ToolRecord
		var state string
		var lastUsed sql.NullTime
		var createdAt sql.NullTime
		if err := rows.Scan(&r.Name, &state, &r.UseCount, &lastUsed, &createdAt, &r.Pinned); err != nil {
			return err
		}
		r.State = ToolState(state)
		if lastUsed.Valid {
			r.LastUsedAt = lastUsed.Time
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time
		}
		c.records[r.Name] = &r
	}
	return rows.Err()
}

func (c *Curator) Start(ctx context.Context) {
	if !c.config.Enabled {
		return
	}
	interval := time.Duration(c.config.CheckIntervalMins) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.RunCheck()
		}
	}
}

func (c *Curator) Stop() {
	close(c.stopCh)
}

func (c *Curator) RecordUsage(toolName string) {
	if !c.config.Enabled {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.records[toolName]; ok {
		r.UseCount++
		r.LastUsedAt = now
		if r.State == ToolStateStale {
			r.State = ToolStateActive
		}
		c.db.Exec(`UPDATE tool_usage SET use_count = ?, last_used_at = ?, state = ? WHERE name = ?`,
			r.UseCount, now, string(r.State), toolName)
	} else {
		r = &ToolRecord{
			Name:       toolName,
			State:      ToolStateActive,
			UseCount:   1,
			LastUsedAt: now,
			CreatedAt:  now,
		}
		c.records[toolName] = r
		c.db.Exec(`INSERT INTO tool_usage (name, state, use_count, last_used_at, created_at) VALUES (?, ?, ?, ?, ?)`,
			toolName, string(r.State), r.UseCount, now, now)
	}
}

func (c *Curator) RunCheck() map[string]ToolState {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	staleThreshold := now.AddDate(0, 0, -c.config.StaleAfterDays)
	archiveThreshold := now.AddDate(0, 0, -c.config.ArchiveAfterDays)
	transitions := make(map[string]ToolState)
	for _, r := range c.records {
		if r.Pinned {
			continue
		}
		if r.UseCount == 0 && r.CreatedAt.After(staleThreshold) {
			continue
		}
		var newState ToolState
		switch {
		case r.LastUsedAt.Before(archiveThreshold) && r.State != ToolStateArchived:
			newState = ToolStateArchived
		case r.LastUsedAt.Before(staleThreshold) && r.State == ToolStateActive:
			newState = ToolStateStale
		case r.LastUsedAt.After(staleThreshold) && r.State == ToolStateStale:
			newState = ToolStateActive
		default:
			continue
		}
		r.State = newState
		transitions[r.Name] = newState
		c.db.Exec(`UPDATE tool_usage SET state = ? WHERE name = ?`, string(newState), r.Name)
	}
	return transitions
}

func (c *Curator) GetRecords() []ToolRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()
	records := make([]ToolRecord, 0, len(c.records))
	for _, r := range c.records {
		records = append(records, *r)
	}
	return records
}

func (c *Curator) PinTool(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.records[name]; ok {
		r.Pinned = true
		_, err := c.db.Exec(`UPDATE tool_usage SET pinned = TRUE WHERE name = ?`, name)
		return err
	}
	return fmt.Errorf("tool %q not found", name)
}

func (c *Curator) UnpinTool(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.records[name]; ok {
		r.Pinned = false
		_, err := c.db.Exec(`UPDATE tool_usage SET pinned = FALSE WHERE name = ?`, name)
		return err
	}
	return fmt.Errorf("tool %q not found", name)
}

func (c *Curator) GetState(name string) (ToolState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if r, ok := c.records[name]; ok {
		return r.State, true
	}
	return "", false
}
