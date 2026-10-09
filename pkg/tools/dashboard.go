package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/artifacts"
	"github.com/ianclemence/ghost/pkg/dashboards"
)

// DashboardTool answers questions about the owner's data with SQL and keeps
// the answer as a live dashboard: each chart a query, shown with its query,
// re-run whenever the dashboard is opened. Reading only; nothing is changed.
type DashboardTool struct {
	workspace string
	mu        sync.Mutex
	pub       CanvasPublisher
}

func NewDashboardTool(workspace string) *DashboardTool { return &DashboardTool{workspace: workspace} }

// SetPublisher wires the artifact store once it exists.
func (t *DashboardTool) SetPublisher(p CanvasPublisher) { t.mu.Lock(); t.pub = p; t.mu.Unlock() }

func (t *DashboardTool) Name() string { return "dashboard" }

func (t *DashboardTool) Description() string {
	return `Answer a question about the owner's data in plain language with SQL, and keep the answer as a live dashboard that updates as the data changes. Sources: "pod" (SQLite: their finances, subscriptions, health days, knowledge, trips, and every CSV/TSV file they sent, as tables) and any database they connected (Postgres, by its name). Reading only: one SELECT per query.

action:
- sources: every source, its tables and columns. Always look first; never guess a table or column.
- query: source, sql → up to 30 rows, to check a query or answer a quick question. Answer the owner in words (or with present_card), not with raw rows.
- make: title, tiles: [{title, chart: metric | bar | line | donut | table, source, query, unit?, note?}] (at most 12). metric: the first number of the first row, big (a second row is shown as "before", for a change); bar/line/donut: rows of (label, value) (line in time order); table: rows as they are. Every query is run before it is saved; a failing one is reported. Saving again with the same title replaces it. The dashboard appears in the chat and on the shelf; every chart shows its query.
Plain language for the owner: say what the dashboard shows, never the SQL, unless they ask.`
}

func (t *DashboardTool) Parameters() map[string]interface{} {
	s := map[string]interface{}{"type": "string"}
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"action"},
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string", "enum": []string{"sources", "query", "make"}},
			"source": s,
			"sql":    s,
			"title":  s,
			"tiles": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object",
				"properties": map[string]interface{}{
					"title": s, "source": s, "query": s, "unit": s, "note": s,
					"chart": map[string]interface{}{"type": "string", "enum": []string{"metric", "bar", "line", "donut", "table"}},
				}}},
		},
	}
}

func (t *DashboardTool) Timeout() time.Duration { return 90 * time.Second }

func (t *DashboardTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	switch sarg(args, "action") {
	case "sources":
		var sb strings.Builder
		for _, s := range dashboards.Sources(ctx, t.workspace) {
			fmt.Fprintf(&sb, "source %q (%s): %s\n", s.Name, s.Kind, s.About)
			if s.Error != "" {
				fmt.Fprintf(&sb, "  can't be reached: %s\n", s.Error)
				continue
			}
			for _, tb := range s.Tables {
				cols := make([]string, len(tb.Columns))
				for i, c := range tb.Columns {
					cols[i] = c.Name + " " + c.Type
				}
				fmt.Fprintf(&sb, "  %s (%d rows)", tb.Name, tb.Rows)
				if tb.About != "" {
					fmt.Fprintf(&sb, ": %s", tb.About)
				}
				fmt.Fprintf(&sb, "\n    %s\n", strings.Join(cols, ", "))
			}
		}
		return NewToolResult(sb.String())
	case "query":
		r, err := dashboards.Run(ctx, t.workspace, sarg(args, "source"), sarg(args, "sql"))
		if err != nil {
			return ErrorResult("The query didn't run: " + err.Error())
		}
		if len(r.Rows) > 30 {
			r.Rows, r.Truncated = r.Rows[:30], true
		}
		b, _ := json.Marshal(r)
		return NewToolResult(string(b))
	case "make":
		var d dashboards.Dashboard
		d.Title = sarg(args, "title")
		raw, _ := json.Marshal(args["tiles"])
		if err := json.Unmarshal(raw, &d.Tiles); err != nil {
			return ErrorResult("tiles is a list of {title, chart, source, query}")
		}
		if err := d.Check(); err != nil {
			return ErrorResult("Not saved: " + err.Error())
		}
		// Every query runs before the dashboard is kept.
		var bad []string
		for _, td := range dashboards.Data(ctx, t.workspace, d) {
			if td.Error != "" {
				bad = append(bad, fmt.Sprintf("%q: %s", td.Title, td.Error))
			}
		}
		if len(bad) > 0 {
			return ErrorResult("Not saved; these tiles don't work yet: " + strings.Join(bad, "; ") + ". Check the tables with sources, fix the queries and call make again.")
		}
		rel, err := dashboards.Save(t.workspace, d)
		if err != nil {
			return ErrorResult("Not saved: " + err.Error())
		}
		t.mu.Lock()
		pub := t.pub
		t.mu.Unlock()
		if pub == nil {
			return NewToolResult(fmt.Sprintf("Saved the dashboard %q, but the chat can't show it right now.", d.Title))
		}
		a, err := pub.Publish(artifacts.Input{SessionKey: SessionKeyFromContext(ctx), Kind: "file", Title: d.Title,
			Summary: fmt.Sprintf("Dashboard · %d charts", len(d.Tiles)), Path: rel})
		if err != nil {
			return ErrorResult("Saved, but it couldn't be shown in the chat: " + err.Error())
		}
		return &ToolResult{ForLLM: fmt.Sprintf("Showing the dashboard %q (%d charts, artifact %s) in the owner's chat; it updates whenever they open it, and every chart shows its query. Say in a sentence or two what it shows and the one thing worth noticing.", d.Title, len(d.Tiles), a.ID), Silent: true}
	}
	return ErrorResult("action is sources, query or make")
}
