// Package dashboards answers questions about the owner's data with SQL and
// keeps the answers as live dashboards: each chart is a query Ghost wrote,
// shown with that query, re-run whenever the dashboard is looked at. The data
// is read-only by construction. The owner's own records on the Pod and the
// spreadsheets they sent are loaded into a private in-memory database built
// for each run; a Postgres database they connected is queried inside a
// read-only transaction with a time limit.
package dashboards

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/life"
	"github.com/ianclemence/ghost/pkg/uploads"

	_ "modernc.org/sqlite"
)

// PodSource is the source name of the owner's own data and files.
const PodSource = "pod"

// Column is one column of a table.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"` // text | number
}

// Table is what a source holds.
type Table struct {
	Name    string   `json:"name"`
	About   string   `json:"about,omitempty"`
	Columns []Column `json:"columns"`
	Rows    int      `json:"rows"`
}

// Source is a place queries run against.
type Source struct {
	Name   string  `json:"name"`
	Kind   string  `json:"kind"` // pod | postgres
	About  string  `json:"about"`
	Tables []Table `json:"tables,omitempty"`
	Error  string  `json:"error,omitempty"`
}

const (
	maxFiles    = 20
	maxFileRows = 50000
	maxFileSize = 20 << 20
)

// podDB builds the private database of the owner's own data and files. The
// caller closes it. It lives only in memory and only for one run.
func podDB(ctx context.Context, workspace string) (*sql.DB, []Table, error) {
	db, err := sql.Open("sqlite", "file::memory:?cache=private")
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(1) // one connection: the memory database is per connection
	var tables []Table
	add := func(name, about string, cols []Column, rows [][]interface{}) error {
		defs := make([]string, len(cols))
		for i, c := range cols {
			t := "TEXT"
			if c.Type == "number" {
				t = "REAL"
			}
			defs[i] = quoteIdent(c.Name) + " " + t
		}
		if _, err := db.ExecContext(ctx, "CREATE TABLE "+quoteIdent(name)+" ("+strings.Join(defs, ", ")+")"); err != nil {
			return err
		}
		if len(rows) > 0 {
			ph := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			st, err := tx.PrepareContext(ctx, "INSERT INTO "+quoteIdent(name)+" VALUES ("+ph+")")
			if err != nil {
				tx.Rollback()
				return err
			}
			for _, r := range rows {
				if _, err := st.ExecContext(ctx, r...); err != nil {
					st.Close()
					tx.Rollback()
					return err
				}
			}
			st.Close()
			if err := tx.Commit(); err != nil {
				return err
			}
		}
		tables = append(tables, Table{Name: name, About: about, Columns: cols, Rows: len(rows)})
		return nil
	}
	t := func(n string) Column { return Column{Name: n, Type: "text"} }
	n := func(c string) Column { return Column{Name: c, Type: "number"} }

	fin := life.FinancesFor(workspace)
	var rows [][]interface{}
	if entries, err := fin.Entries("0000-01-01", "9999-12-31"); err == nil {
		for _, e := range entries {
			amt := life.Major(e.Amount, e.Currency)
			rows = append(rows, []interface{}{e.Date, monthOf(e.Date), e.Kind, amt, e.Currency, e.Merchant, e.Category, e.Note})
		}
	}
	if err := add("finance_entries", "what the owner spent (kind expense) and earned (income), amount in major units of currency",
		[]Column{t("date"), t("month"), t("kind"), n("amount"), t("currency"), t("merchant"), t("category"), t("note")}, rows); err != nil {
		db.Close()
		return nil, nil, err
	}
	rows = nil
	if recs, err := fin.Recurrings(); err == nil {
		for _, r := range recs {
			rows = append(rows, []interface{}{r.Name, r.Kind, life.Major(r.Amount, r.Currency), r.Currency, r.Every, r.Next, r.Category, boolNum(r.Active)})
		}
	}
	if err := add("subscriptions", "subscriptions and bills that come round again (every week, month, quarter or year); active 1 or 0",
		[]Column{t("name"), t("kind"), n("amount"), t("currency"), t("every"), t("next"), t("category"), n("active")}, rows); err != nil {
		db.Close()
		return nil, nil, err
	}
	rows = nil
	if days, err := life.DeviceFor(workspace).Health("0000-01-01", "9999-12-31"); err == nil {
		for _, d := range days {
			rows = append(rows, []interface{}{d.Date, monthOf(d.Date), nz(d.Steps), nz(d.SleepMinutes), nz(d.RestingHR)})
		}
	}
	if err := add("health_days", "daily totals the phone shared: steps, minutes of sleep, resting heart rate",
		[]Column{t("date"), t("month"), n("steps"), n("sleep_minutes"), n("resting_hr")}, rows); err != nil {
		db.Close()
		return nil, nil, err
	}
	rows = nil
	if all, err := life.KnowledgeFor(workspace).All(); err == nil {
		for _, l := range all {
			rows = append(rows, []interface{}{l.Title, l.Kind, l.Author, l.Status, float64(l.Current), float64(l.Total), l.Unit, l.Started, l.Finished, l.Due})
		}
	}
	if err := add("knowledge", "what the owner reads and studies: status want, active, paused or done",
		[]Column{t("title"), t("kind"), t("author"), t("status"), n("current"), n("total"), t("unit"), t("started"), t("finished"), t("due")}, rows); err != nil {
		db.Close()
		return nil, nil, err
	}
	rows = nil
	if trips, err := life.TripsFor(workspace).All(time.Now()); err == nil {
		for _, tr := range trips {
			rows = append(rows, []interface{}{tr.Title, tr.Destination, tr.Start, tr.End, float64(len(tr.Legs))})
		}
	}
	if err := add("trips", "the owner's trips", []Column{t("title"), t("destination"), t("start"), t("end"), n("legs")}, rows); err != nil {
		db.Close()
		return nil, nil, err
	}

	// Spreadsheets the owner sent (CSV or TSV), one table each.
	used := map[string]bool{}
	count := 0
	for _, it := range uploads.List(workspace) {
		low := strings.ToLower(it.Name)
		if !strings.HasSuffix(low, ".csv") && !strings.HasSuffix(low, ".tsv") {
			continue
		}
		if count >= maxFiles || it.Size > maxFileSize {
			continue
		}
		name := fileTable(it.Name, used)
		cols, data, err := readDelimited(filepath.Join(workspace, filepath.FromSlash(it.Path)), strings.HasSuffix(low, ".tsv"))
		if err != nil || len(cols) == 0 {
			continue
		}
		if err := add(name, "from the file "+it.Name, cols, data); err != nil {
			continue
		}
		used[name] = true
		count++
	}
	return db, tables, nil
}

var nonIdent = regexp.MustCompile(`[^a-z0-9_]+`)

func fileTable(name string, used map[string]bool) string {
	base := strings.TrimSuffix(strings.ToLower(name), filepath.Ext(name))
	base = strings.Trim(nonIdent.ReplaceAllString(base, "_"), "_")
	if base == "" {
		base = "data"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	n := "file_" + base
	for i := 2; used[n]; i++ {
		n = fmt.Sprintf("file_%s_%d", base, i)
	}
	return n
}

// readDelimited reads a CSV or TSV: the header names the columns, and a
// column is a number when every filled cell in it reads as one.
func readDelimited(path string, tab bool) ([]Column, [][]interface{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	if tab {
		r.Comma = '\t'
	}
	head, err := r.Read()
	if err != nil {
		return nil, nil, err
	}
	used := map[string]bool{}
	cols := make([]Column, len(head))
	for i, h := range head {
		c := strings.Trim(nonIdent.ReplaceAllString(strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))), "_"), "_")
		if c == "" {
			c = fmt.Sprintf("column_%d", i+1)
		}
		for used[c] {
			c += "_"
		}
		used[c] = true
		cols[i] = Column{Name: c, Type: "number"}
	}
	var raw [][]string
	for len(raw) < maxFileRows {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		row := make([]string, len(cols))
		copy(row, rec)
		raw = append(raw, row)
	}
	for i := range cols {
		for _, row := range raw {
			v := strings.TrimSpace(row[i])
			if v == "" {
				continue
			}
			if _, ok := number(v); !ok {
				cols[i].Type = "text"
				break
			}
		}
	}
	out := make([][]interface{}, len(raw))
	for j, row := range raw {
		vals := make([]interface{}, len(cols))
		for i, c := range cols {
			v := strings.TrimSpace(row[i])
			if c.Type == "number" {
				if f, ok := number(v); ok {
					vals[i] = f
				} else {
					vals[i] = nil
				}
			} else {
				vals[i] = v
			}
		}
		out[j] = vals
	}
	return cols, out, nil
}

// number reads "1,250.50", "KES 1,250", "12%" or "(300)" as a number.
func number(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")")
	s = strings.Trim(s, "()")
	s = strings.TrimRight(strings.TrimLeft(s, "$€£KESUSD "), "% ")
	s = strings.ReplaceAll(s, ",", "")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if neg {
		f = -f
	}
	return f, true
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func monthOf(date string) string {
	if len(date) >= 7 {
		return date[:7]
	}
	return ""
}

func boolNum(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func nz(v int) interface{} {
	if v == 0 {
		return nil
	}
	return float64(v)
}

// Sources lists every place a dashboard can draw from, with its tables, so
// Ghost can write a query that fits. A database that can't be reached says so.
func Sources(ctx context.Context, workspace string) []Source {
	out := []Source{}
	db, tables, err := podDB(ctx, workspace)
	pod := Source{Name: PodSource, Kind: "pod", About: "the owner's own data on their Pod (SQLite dialect) and the spreadsheets they sent"}
	if err != nil {
		pod.Error = err.Error()
	} else {
		db.Close()
		pod.Tables = tables
	}
	out = append(out, pod)
	for _, m := range credentials.ListDatabases() {
		s := Source{Name: m.Name, Kind: m.Kind, About: fmt.Sprintf("Postgres database %s on %s", m.DB, m.Host)}
		if tables, err := pgTables(ctx, m.Name); err != nil {
			s.Error = friendlyErr(err)
		} else {
			s.Tables = tables
		}
		out = append(out, s)
	}
	return out
}

// friendlyErr says what went wrong without the connection's secrets.
func friendlyErr(err error) string {
	msg := err.Error()
	for _, v := range credentials.SecretValues() {
		msg = strings.ReplaceAll(msg, v, "•••")
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

var errNoSource = errors.New("no such source")
