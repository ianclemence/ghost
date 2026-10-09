package dashboards

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/credentials"
)

// TestAConnectedPostgres runs against a real database when one is given
// (GHOST_PG_TEST_URL); it is skipped otherwise.
func TestAConnectedPostgres(t *testing.T) {
	url := os.Getenv("GHOST_PG_TEST_URL")
	if url == "" {
		t.Skip("set GHOST_PG_TEST_URL to test against a real Postgres")
	}
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	if err := credentials.SaveDatabase(credentials.Database{Name: "shop", URL: url}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := TestConnection(ctx, "shop"); err != nil {
		t.Fatal(err)
	}
	var shop *Source
	for _, s := range Sources(ctx, t.TempDir()) {
		if s.Name == "shop" {
			s := s
			shop = &s
		}
	}
	if shop == nil || shop.Error != "" || len(shop.Tables) < 2 {
		t.Fatalf("source: %+v", shop)
	}
	r, err := Run(ctx, "", "shop", `SELECT c.city, SUM(o.total) AS revenue FROM orders o JOIN customers c ON c.id = o.customer_id
		WHERE o.status = 'paid' GROUP BY c.city ORDER BY revenue DESC`)
	if err != nil || len(r.Rows) != 2 || r.Rows[0][0] != "Nairobi" {
		t.Fatalf("revenue: %+v %v", r, err)
	}
	if v, ok := asNumber(r.Rows[0][1]); !ok || v != 4800.5 {
		t.Fatalf("revenue value: %v", r.Rows[0][1])
	}
	// Words alone can't catch every write: the read-only transaction does.
	if _, err := Run(ctx, "", "shop", "SELECT nextval('ticket')"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "read-only") {
		t.Fatalf("a write inside a SELECT ran: %v", err)
	}
	start := time.Now()
	if _, err := Run(ctx, "", "shop", "SELECT pg_sleep(30)"); err == nil {
		t.Fatal("a 30 s query was not stopped")
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("the time limit took %s", time.Since(start))
	}
	// The password never shows in what a failing query says.
	if _, err := Run(ctx, "", "shop", "SELECT * FROM nowhere"); err == nil || strings.Contains(err.Error(), "pg-test-secret-91") {
		t.Fatalf("error: %v", err)
	}
}
