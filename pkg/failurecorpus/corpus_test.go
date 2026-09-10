package failurecorpus

import (
	"strings"
	"testing"
)

func TestAppendAndListNewestFirst(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Category: CatModel, Task: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(Record{Category: CatVerification, Task: "second"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 records, got %d", len(got))
	}
	if got[0].Task != "second" {
		t.Fatalf("expected newest first, got %q", got[0].Task)
	}
	if got[0].ID == "" || got[0].At.IsZero() {
		t.Fatal("records must be stamped with id and time")
	}
}

// Free-text fields are redacted before they are written.
func TestAppendRedactsSecrets(t *testing.T) {
	s, _ := New(t.TempDir())
	secret := "sk-1234567890abcdef1234567890abcdef"
	if err := s.Append(Record{Category: CatOther, Observed: "failed with api_key=" + secret}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.List(1)
	if len(got) != 1 {
		t.Fatal("expected one record")
	}
	if strings.Contains(got[0].Observed, secret) {
		t.Fatalf("secret leaked into corpus: %q", got[0].Observed)
	}
}

func TestListMissingFileIsEmpty(t *testing.T) {
	s, _ := New(t.TempDir())
	got, err := s.List(10)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing corpus must be empty, got %v %v", got, err)
	}
}
