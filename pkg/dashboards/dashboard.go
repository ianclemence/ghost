package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Dashboard is a set of tiles, each a question answered by a query.
type Dashboard struct {
	Title string `json:"title"`
	Tiles []Tile `json:"tiles"`
}

// Tile is one chart: what it shows, as what, and the query behind it.
//
//	metric  the first value of the first row, large (a second row, if any, is "before")
//	bar     rows of (label, value)
//	line    rows of (label, value), in order
//	donut   rows of (label, value): shares of a whole
//	table   the rows as they come
type Tile struct {
	Title  string `json:"title"`
	Chart  string `json:"chart"`
	Source string `json:"source"`
	Query  string `json:"query"`
	Unit   string `json:"unit,omitempty"`
	Note   string `json:"note,omitempty"`
}

const maxTiles = 12

var charts = map[string]bool{"metric": true, "bar": true, "line": true, "donut": true, "table": true}

// Check validates a dashboard's shape (not yet its queries).
func (d *Dashboard) Check() error {
	d.Title = strings.TrimSpace(d.Title)
	if d.Title == "" || len([]rune(d.Title)) > 80 {
		return errors.New("a dashboard needs a title of at most 80 characters")
	}
	if len(d.Tiles) == 0 || len(d.Tiles) > maxTiles {
		return fmt.Errorf("a dashboard has 1 to %d tiles", maxTiles)
	}
	for i := range d.Tiles {
		t := &d.Tiles[i]
		t.Title = strings.TrimSpace(t.Title)
		if t.Title == "" || len([]rune(t.Title)) > 80 {
			return fmt.Errorf("tile %d needs a title of at most 80 characters", i+1)
		}
		if !charts[t.Chart] {
			return fmt.Errorf("tile %d: chart is metric, bar, line, donut or table, not %q", i+1, t.Chart)
		}
		if t.Source == "" {
			t.Source = PodSource
		}
		q, err := CheckSQL(t.Query)
		if err != nil {
			return fmt.Errorf("tile %d: %w", i+1, err)
		}
		t.Query = q
		if len(t.Unit) > 16 || len([]rune(t.Note)) > 200 {
			return fmt.Errorf("tile %d: the unit or note is too long", i+1)
		}
	}
	return nil
}

// TileData is a tile with what its query returned now, shaped for its chart.
type TileData struct {
	Tile
	Value    *float64        `json:"value,omitempty"`
	Previous *float64        `json:"previous,omitempty"`
	Label    string          `json:"label,omitempty"`
	Points   []Point         `json:"points,omitempty"`
	Columns  []string        `json:"columns,omitempty"`
	Rows     [][]interface{} `json:"rows,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// Point is one labelled value.
type Point struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// Data runs every tile's query now. A tile whose query fails says why; the
// rest still show.
func Data(ctx context.Context, workspace string, d Dashboard) []TileData {
	out := make([]TileData, len(d.Tiles))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for i, t := range d.Tiles {
		wg.Add(1)
		go func(i int, t Tile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			td := TileData{Tile: t}
			res, err := Run(ctx, workspace, t.Source, t.Query)
			if err != nil {
				td.Error = err.Error()
			} else {
				shape(&td, res)
			}
			out[i] = td
		}(i, t)
	}
	wg.Wait()
	return out
}

func shape(td *TileData, r Result) {
	switch td.Chart {
	case "metric":
		if len(r.Rows) == 0 {
			td.Error = "no rows"
			return
		}
		if v, ok := firstNumber(r.Rows[0]); ok {
			td.Value = &v
		} else if allNil(r.Rows[0]) {
			// A total over nothing (SUM of no rows) is nothing yet: 0.
			zero := 0.0
			td.Value = &zero
		} else {
			td.Error = "the first row has no number"
			return
		}
		if len(r.Rows) > 1 {
			if v, ok := firstNumber(r.Rows[1]); ok {
				td.Previous = &v
			}
		}
		if len(r.Columns) > 0 {
			td.Label = r.Columns[0]
		}
	case "bar", "line", "donut":
		limit := 40
		if td.Chart == "line" {
			limit = 120
		}
		for _, row := range r.Rows {
			if len(td.Points) >= limit || len(row) < 2 {
				break
			}
			v, ok := asNumber(row[1])
			if !ok {
				continue
			}
			td.Points = append(td.Points, Point{Label: fmt.Sprint(nilEmpty(row[0])), Value: v})
		}
		if len(td.Points) == 0 {
			td.Error = "no rows of a label and a number"
		}
	default:
		td.Columns = r.Columns
		if len(r.Rows) > 50 {
			td.Rows = r.Rows[:50]
		} else {
			td.Rows = r.Rows
		}
	}
}

func firstNumber(row []interface{}) (float64, bool) {
	for _, v := range row {
		if f, ok := asNumber(v); ok {
			return f, true
		}
	}
	return 0, false
}

func asNumber(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	}
	return 0, false
}

func nilEmpty(v interface{}) interface{} {
	if v == nil {
		return "—"
	}
	return v
}

// ── keeping dashboards ───────────────────────────────────────────────────

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Path is where a dashboard with this title is kept.
func Path(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(title)), "-"), "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		s = "dashboard"
	}
	return "dashboards/" + s + ".json"
}

var dashPath = regexp.MustCompile(`^dashboards/[a-z0-9-]+\.json$`)

// IsDashboard reports whether a workspace path is a saved dashboard.
func IsDashboard(rel string) bool { return dashPath.MatchString(filepath.ToSlash(rel)) }

// Save keeps a dashboard (replacing one with the same title) and returns its path.
func Save(workspace string, d Dashboard) (string, error) {
	if err := d.Check(); err != nil {
		return "", err
	}
	rel := Path(d.Title)
	if err := os.MkdirAll(filepath.Join(workspace, "dashboards"), 0o755); err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(d, "", " ")
	tmp := filepath.Join(workspace, rel+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return "", err
	}
	return rel, os.Rename(tmp, filepath.Join(workspace, rel))
}

// Load reads a saved dashboard.
func Load(workspace, rel string) (Dashboard, error) {
	if !IsDashboard(rel) {
		return Dashboard{}, errors.New("not a dashboard")
	}
	b, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(rel)))
	if err != nil {
		return Dashboard{}, err
	}
	var d Dashboard
	if err := json.Unmarshal(b, &d); err != nil {
		return Dashboard{}, err
	}
	return d, d.Check()
}

func allNil(row []interface{}) bool {
	for _, v := range row {
		if v != nil {
			return false
		}
	}
	return len(row) > 0
}
