package dashboards

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Result is what a query returned.
type Result struct {
	Columns   []string        `json:"columns"`
	Rows      [][]interface{} `json:"rows"`
	Truncated bool            `json:"truncated,omitempty"`
}

const (
	maxRows      = 1000
	queryTimeout = 10 * time.Second
)

var (
	lineComment  = regexp.MustCompile(`--[^\n]*`)
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// Words that change data or the server, refused even inside a read-only
	// transaction, so a mistaken query never gets as far as being refused there.
	writeWord = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|copy|attach|detach|pragma|vacuum|reindex|replace|merge|call|do|lock|set|reset|listen|notify|refresh|cluster|comment|security|load)\b`)
)

// CheckSQL accepts one read-only query (SELECT or WITH … SELECT) and returns
// it cleaned, or says why not.
func CheckSQL(q string) (string, error) {
	s := blockComment.ReplaceAllString(lineComment.ReplaceAllString(q, " "), " ")
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	if s == "" {
		return "", errors.New("the query is empty")
	}
	if len(s) > 8000 {
		return "", errors.New("the query is too long")
	}
	if strings.Contains(s, ";") {
		return "", errors.New("one query at a time")
	}
	low := strings.ToLower(s)
	if !strings.HasPrefix(low, "select") && !strings.HasPrefix(low, "with") {
		return "", errors.New("only SELECT queries: dashboards read, they never change data")
	}
	// Strings may say anything; only the words outside them count.
	if writeWord.MatchString(stripStrings(s)) {
		return "", errors.New("only SELECT queries: dashboards read, they never change data")
	}
	return s, nil
}

var quoted = regexp.MustCompile(`'(?:[^']|'')*'|"(?:[^"]|"")*"`)

func stripStrings(s string) string { return quoted.ReplaceAllString(s, "''") }

// Run runs one query against a source: "pod" (the owner's own data and files)
// or the name of a connected database.
func Run(ctx context.Context, workspace, source, query string) (Result, error) {
	q, err := CheckSQL(query)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	if source == "" || source == PodSource {
		db, _, err := podDB(ctx, workspace)
		if err != nil {
			return Result{}, err
		}
		defer db.Close()
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return Result{}, errors.New(friendlyErr(err))
		}
		defer rows.Close()
		return collect(rows)
	}
	db, err := openPG(source)
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Result{}, errors.New(friendlyErr(err))
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", queryTimeout.Milliseconds())); err != nil {
		return Result{}, errors.New(friendlyErr(err))
	}
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return Result{}, errors.New(friendlyErr(err))
	}
	defer rows.Close()
	return collect(rows)
}

func collect(rows *sql.Rows) (Result, error) {
	cols, err := rows.Columns()
	if err != nil {
		return Result{}, err
	}
	out := Result{Columns: cols, Rows: [][]interface{}{}}
	for rows.Next() {
		if len(out.Rows) >= maxRows {
			out.Truncated = true
			break
		}
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Result{}, err
		}
		for i, v := range vals {
			vals[i] = plain(v)
		}
		out.Rows = append(out.Rows, vals)
	}
	return out, rows.Err()
}

// plain turns what a driver returns into JSON-friendly values.
func plain(v interface{}) interface{} {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
			return t.Format("2006-01-02")
		}
		return t.Format(time.RFC3339)
	case int64:
		return float64(t)
	case int32:
		return float64(t)
	case float32:
		return float64(t)
	case bool, float64, string:
		return t
	}
	// Numerics and the like: as text, which the phone reads as a number when it is one.
	return fmt.Sprint(v)
}

func openPG(name string) (*sql.DB, error) {
	d, ok := credentials.DatabaseFor(name)
	if !ok {
		return nil, fmt.Errorf("%w %q: connect it under Data sources", errNoSource, name)
	}
	db, err := sql.Open("pgx", d.URL)
	if err != nil {
		return nil, errors.New(friendlyErr(err))
	}
	db.SetMaxOpenConns(2)
	db.SetConnMaxLifetime(time.Minute)
	return db, nil
}

// pgTables lists a database's tables and columns (outside the system schemas).
func pgTables(ctx context.Context, name string) ([]Table, error) {
	db, err := openPG(name)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT c.table_schema, c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c
		WHERE c.table_schema NOT IN ('pg_catalog','information_schema') AND c.table_schema NOT LIKE 'pg_%'
		ORDER BY c.table_schema, c.table_name, c.ordinal_position LIMIT 2000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Table
	idx := map[string]int{}
	for rows.Next() {
		var schema, table, col, typ string
		if err := rows.Scan(&schema, &table, &col, &typ); err != nil {
			return nil, err
		}
		full := table
		if schema != "public" {
			full = schema + "." + table
		}
		i, ok := idx[full]
		if !ok {
			if len(out) >= 80 {
				continue
			}
			out = append(out, Table{Name: full})
			i = len(out) - 1
			idx[full] = i
		}
		kind := "text"
		switch {
		case strings.Contains(typ, "int"), strings.Contains(typ, "numeric"), strings.Contains(typ, "double"), strings.Contains(typ, "real"), strings.Contains(typ, "decimal"):
			kind = "number"
		}
		out[i].Columns = append(out[i].Columns, Column{Name: col, Type: kind})
	}
	return out, rows.Err()
}

// TestConnection checks that a database answers (for the phone's Connect).
func TestConnection(ctx context.Context, name string) error {
	_, err := pgTables(ctx, name)
	if err != nil {
		return errors.New(friendlyErr(err))
	}
	return nil
}
