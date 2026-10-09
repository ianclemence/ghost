package dashboards

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/life"
	"github.com/ianclemence/ghost/pkg/uploads"
)

func TestOnlyReadingQueriesRun(t *testing.T) {
	for _, q := range []string{"SELECT 1", "with x as (select 1) select * from x", "select 'drop table x' as words;", "-- a note\nSELECT 2"} {
		if _, err := CheckSQL(q); err != nil {
			t.Fatalf("%q refused: %v", q, err)
		}
	}
	for _, q := range []string{"DELETE FROM finance_entries", "select 1; drop table x", "update t set a=1", "insert into t values (1)", "PRAGMA writable_schema=1", "", "with x as (delete from t returning *) select * from x"} {
		if _, err := CheckSQL(q); err == nil {
			t.Fatalf("%q accepted", q)
		}
	}
}

func TestTheOwnersDataAndFilesAreTables(t *testing.T) {
	ws := t.TempDir()
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	fin := life.FinancesFor(ws)
	for _, e := range []life.EntryInput{
		{Kind: "expense", Amount: "2,350", Currency: "KES", Merchant: "Naivas", Category: "groceries", Date: "2026-10-08", Source: life.Source{Kind: "conversation"}},
		{Kind: "expense", Amount: "850", Currency: "KES", Merchant: "Java", Category: "eating out", Date: "2026-10-07", Source: life.Source{Kind: "conversation"}},
		{Kind: "expense", Amount: "1,200", Currency: "KES", Merchant: "Naivas", Category: "groceries", Date: "2026-09-20", Source: life.Source{Kind: "conversation"}},
	} {
		if _, _, err := fin.Record(e, now); err != nil {
			t.Fatal(err)
		}
	}
	csv := "Region,Month,Revenue\nNairobi,2026-08,\"1,200.50\"\nMombasa,2026-08,800\nNairobi,2026-09,1500\n"
	if _, err := uploads.Save(ws, []byte(csv), "Sales Q3.csv", "text/csv", "phone"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	src := Sources(ctx, ws)
	var names []string
	for _, tb := range src[0].Tables {
		names = append(names, tb.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "file_sales_q3") || !strings.Contains(strings.Join(names, ","), "finance_entries") {
		t.Fatalf("tables: %v", names)
	}
	r, err := Run(ctx, ws, "pod", "SELECT category, SUM(amount) AS spent FROM finance_entries WHERE kind='expense' GROUP BY category ORDER BY spent DESC")
	if err != nil || len(r.Rows) != 2 || r.Rows[0][0] != "groceries" || r.Rows[0][1] != 3550.0 {
		t.Fatalf("spent: %+v %v", r, err)
	}
	r, err = Run(ctx, ws, "pod", "SELECT region, SUM(revenue) FROM file_sales_q3 GROUP BY region ORDER BY 2 DESC")
	if err != nil || len(r.Rows) != 2 || r.Rows[0][1] != 2700.5 {
		t.Fatalf("file: %+v %v", r, err)
	}
	d := Dashboard{Title: "Shop", Tiles: []Tile{
		{Title: "Spent this month", Chart: "metric", Query: "SELECT SUM(amount) FROM finance_entries WHERE month='2026-10' UNION ALL SELECT SUM(amount) FROM finance_entries WHERE month='2026-09'", Unit: "KES"},
		{Title: "By category", Chart: "bar", Query: "SELECT category, SUM(amount) FROM finance_entries GROUP BY 1"},
		{Title: "Broken", Chart: "line", Query: "SELECT nope FROM nowhere"},
		{Title: "Nothing yet", Chart: "metric", Query: "SELECT SUM(amount) FROM finance_entries WHERE month='1999-01'"},
	}}
	rel, err := Save(ws, d)
	if err != nil || rel != "dashboards/shop.json" {
		t.Fatalf("save: %v %s", err, rel)
	}
	got, err := Load(ws, rel)
	if err != nil {
		t.Fatal(err)
	}
	data := Data(ctx, ws, got)
	if *data[0].Value != 3200 || *data[0].Previous != 1200 || len(data[1].Points) != 2 || data[2].Error == "" || data[3].Value == nil || *data[3].Value != 0 {
		t.Fatalf("data: %+v", data)
	}
	// A saved dashboard that someone edited into a write is refused on load.
	_ = os.WriteFile(filepath.Join(ws, rel), []byte(`{"title":"Shop","tiles":[{"title":"x","chart":"table","query":"delete from finance_entries"}]}`), 0o644)
	if _, err := Load(ws, rel); err == nil {
		t.Fatal("a write query loaded")
	}
}
