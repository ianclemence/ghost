package credentials

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Databases the owner connected for dashboards: a name and a Postgres address
// with its user and password. Each is sealed under "database:<name>" in the
// same vault as every other credential, like a website login: the model never
// sees the address, it never appears in chat, and removing it revokes it. Only
// the dashboards package opens a connection with it, read-only.

const databaseIDPrefix = "database:"

// Database is one connection. URL holds the secret; use DatabaseFor only
// where the connection is opened.
type Database struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // postgres
	URL  string `json:"url"`
}

// DatabaseMeta is what the owner sees: never the password.
type DatabaseMeta struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Host string `json:"host"`
	DB   string `json:"database"`
	User string `json:"user"`
}

var dbNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,30}$`)

// NormalizeDatabaseName is the form a database's name is kept in.
func NormalizeDatabaseName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if r == ' ' {
			return '-'
		}
		return r
	}, s)
	return s
}

// SaveDatabase stores (or replaces) a database connection.
func SaveDatabase(d Database) error {
	d.Name = NormalizeDatabaseName(d.Name)
	if !dbNameRE.MatchString(d.Name) {
		return errors.New("a database needs a short name: letters, numbers and dashes")
	}
	if d.Kind == "" {
		d.Kind = "postgres"
	}
	if d.Kind != "postgres" {
		return errors.New("only Postgres databases can be connected for now")
	}
	u, err := url.Parse(strings.TrimSpace(d.URL))
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return errors.New("the address is a Postgres URL: postgres://user:password@host:5432/database")
	}
	d.URL = u.String()
	blob, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return New(configDirForWrites()).Store(databaseIDPrefix+d.Name, string(blob))
}

// DatabaseFor returns a saved connection (false = none).
func DatabaseFor(name string) (Database, bool) {
	raw := New(configDirForWrites()).secretValue(databaseIDPrefix + NormalizeDatabaseName(name))
	if raw == "" {
		return Database{}, false
	}
	var d Database
	if json.Unmarshal([]byte(raw), &d) != nil {
		return Database{}, false
	}
	return d, true
}

// ListDatabases is every saved connection, without secrets, by name.
func ListDatabases() []DatabaseMeta {
	v := New(configDirForWrites())
	var out []DatabaseMeta
	for _, c := range v.List() {
		if !strings.HasPrefix(c.ID, databaseIDPrefix) {
			continue
		}
		var d Database
		if json.Unmarshal([]byte(v.secretValue(c.ID)), &d) != nil {
			continue
		}
		m := DatabaseMeta{Name: d.Name, Kind: d.Kind}
		if u, err := url.Parse(d.URL); err == nil {
			m.Host = u.Hostname()
			m.DB = strings.TrimPrefix(u.Path, "/")
			if u.User != nil {
				m.User = u.User.Username()
			}
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DeleteDatabase revokes a saved connection.
func DeleteDatabase(name string) error {
	return New(configDirForWrites()).Disconnect(databaseIDPrefix + NormalizeDatabaseName(name))
}
