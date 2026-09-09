package permissions

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// BenchmarkBrokerPolicyFiltering measures capability policy filtering: the
// broker's allow/ask/deny decision per classified operation, including the
// standing-grant lookup that gates consequential execution.
func BenchmarkBrokerPolicyFiltering(b *testing.B) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(b.TempDir(), "perm.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	broker, err := Open(db, ModeFull, 2*time.Minute)
	if err != nil {
		b.Fatal(err)
	}
	if err := broker.GrantStanding("computer.click", "computer_click", "session:s", false); err != nil {
		b.Fatal(err)
	}
	caps := []struct {
		cap, act string
		risk     Risk
	}{
		{"computer.inspect_ui", "computer_inspect_ui", RiskReadOnly},
		{"computer.click", "computer_click", RiskLow},
		{"computer.type", "computer_type", RiskConsequential},
		{"calendar.send", "message", RiskHighImpact},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := caps[i%len(caps)]
		v := broker.Evaluate(c.cap, c.act, "session:s", c.risk)
		if c.risk == RiskHighImpact && v != VerdictAsk {
			b.Fatal("high-impact must never auto-authorize")
		}
		if c.risk != RiskHighImpact && v == VerdictAsk {
			b.Fatal("ModeFull must not ask for non-high-impact")
		}
	}
}
