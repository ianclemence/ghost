package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
)

// A reply a restart cut off carries a marker, and that marker has to survive
// every way a transcript is written — appending, and rewriting the whole
// file, which is what Save does.
func TestInterruptedMarkerSurvivesEveryWrite(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store func(t *testing.T) Store
	}{
		{"sqlite", func(t *testing.T) Store {
			database, err := db.NewDB(t.TempDir())
			if err != nil {
				t.Fatalf("open db: %v", err)
			}
			return NewSQLiteStore(database)
		}},
		{"jsonl", func(t *testing.T) Store {
			return NewJSONLStore(filepath.Join(t.TempDir(), "ws"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.store(t)
			st.AddFullMessage("main", providers.Message{
				Role:        "assistant",
				Content:     "You shipped three things and ",
				Interrupted: true,
			})

			assertMarker := func(when string) {
				t.Helper()
				history := st.GetDisplayHistory("main")
				if len(history) != 1 {
					t.Fatalf("%s: expected one row, got %d", when, len(history))
				}
				if !history[0].Interrupted {
					t.Fatalf("%s: the cut-short marker was lost", when)
				}
				if history[0].Content != "You shipped three things and " {
					t.Fatalf("%s: content changed: %q", when, history[0].Content)
				}
			}

			assertMarker("on append")

			// Save rewrites the file (or the row) from what it reads back.
			if err := st.Save("main"); err != nil {
				t.Fatalf("save: %v", err)
			}
			assertMarker("after Save rewrites it")

			// An ordinary reply next to it must not inherit the marker.
			st.AddFullMessage("main", providers.Message{Role: "assistant", Content: "Finished."})
			history := st.GetDisplayHistory("main")
			if len(history) != 2 {
				t.Fatalf("expected two rows, got %d", len(history))
			}
			if history[1].Interrupted {
				t.Fatal("an ordinary reply was marked as cut short")
			}
		})
	}
}

// The marker is provenance, not conversation: it must never reach a
// provider, where it would read as Ghost telling the model about a restart
// in the middle of ordinary speech.
func TestInterruptedMarkerIsNeverSerializedToProviders(t *testing.T) {
	raw, err := json.Marshal(providers.Message{
		Role:        "assistant",
		Content:     "half a reply",
		Interrupted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "interrupted") {
		t.Fatalf("provenance leaked onto the wire: %s", raw)
	}
}

// Recovery writes the row when it recovers, not when the reply was cut: a
// reply backdated to the crash would reorder history around it.
func TestInterruptedRowIsDatedWhenItIsWritten(t *testing.T) {
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	st := NewSQLiteStore(database)
	st.AddFullMessage("main", providers.Message{
		Role:        "assistant",
		Content:     "half",
		Interrupted: true,
		CreatedAt:   time.Now().Add(-time.Hour),
	})
	history := st.GetDisplayHistory("main")
	if len(history) != 1 {
		t.Fatalf("expected one row, got %d", len(history))
	}
	if history[0].CreatedAt.IsZero() {
		t.Fatal("the recovered row must be dated")
	}
	if time.Since(history[0].CreatedAt) > time.Minute {
		t.Fatalf("recovered row was backdated to %v", history[0].CreatedAt)
	}
}
