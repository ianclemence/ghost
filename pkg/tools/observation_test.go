package tools

import "testing"

func TestClassifyErrorAndRetryability(t *testing.T) {
	cases := []struct {
		msg       string
		timedOut  bool
		wantClass ErrorClass
	}{
		{"tool timed out after 5s", true, ErrTimeout},
		{"permission denied for that action", false, ErrPermission},
		{"invalid arguments: missing path", false, ErrValidation},
		{"no such file or directory", false, ErrNotFound},
		{"connection refused", false, ErrNetwork},
		{"service unavailable, try again", false, ErrNetwork},
		{"rate limit exceeded", false, ErrTransient},
		{"unexpected panic in tool", false, ErrInternal},
	}
	for _, c := range cases {
		res := &ToolResult{IsError: true, TimedOut: c.timedOut, ForLLM: c.msg}
		got := ClassifyError(res)
		if got != c.wantClass {
			t.Fatalf("%q: got %s want %s", c.msg, got, c.wantClass)
		}
	}
	// Retryability is deterministic per class.
	if !ErrTimeout.Retryable() || !ErrNetwork.Retryable() || !ErrTransient.Retryable() {
		t.Fatal("transient classes must be retryable")
	}
	if ErrValidation.Retryable() || ErrPermission.Retryable() || ErrNotFound.Retryable() || ErrInternal.Retryable() {
		t.Fatal("deterministic classes must not be retryable")
	}
}

func TestObservationSuccess(t *testing.T) {
	o := NewObservation("read_file", NewToolResult("hello"))
	if o.Status != "success" || o.ErrorClass != "" || o.Retryable {
		t.Fatalf("unexpected observation: %+v", o)
	}
	if o.ObservedAt.IsZero() {
		t.Fatal("observation must be timestamped")
	}
}

func TestObservationFailureCarriesClass(t *testing.T) {
	res := ErrorResult("connection refused")
	o := NewObservation("web_fetch", res)
	if o.Status != "error" || o.ErrorClass != string(ErrNetwork) || !o.Retryable {
		t.Fatalf("unexpected observation: %+v", o)
	}
}

func TestObservationReconstructable(t *testing.T) {
	if !NewObservation("list_dir", NewToolResult("DIR:  a\nFILE: b")).Reconstructable {
		t.Fatal("directory listing must be reconstructable")
	}
	big := make([]byte, 3000)
	for i := range big {
		big[i] = 'x'
	}
	if !NewObservation("exec", NewToolResult(string(big))).Reconstructable {
		t.Fatal("large output must be reconstructable")
	}
	if NewObservation("read_file", NewToolResult("small note")).Reconstructable {
		t.Fatal("small output is not reconstructable")
	}
}

func TestVerificationContract(t *testing.T) {
	if ContractFor("write_file") != "read_file" {
		t.Fatal("write_file must declare a read-back contract")
	}
	if ContractFor("read_file") != "" {
		t.Fatal("read-only tool has no verification contract")
	}
}

// The artifact a publish created must survive normalization so the
// canonical event can name it and a later deletion can retract exactly it.
func TestNewObservationExtractsArtifactID(t *testing.T) {
	res := &ToolResult{
		ForLLM:   "Published artifact art-123 (file: Q3 report). It is now available to the user.",
		Evidence: ArtifactEvidence("art-123", "file", "Q3 report"),
	}
	o := NewObservation("publish_artifact", res)
	if o.ArtifactID != "art-123" {
		t.Fatalf("ArtifactID = %q, want art-123", o.ArtifactID)
	}
}

func TestNewObservationWithoutArtifactEvidence(t *testing.T) {
	o := NewObservation("web_search", &ToolResult{ForLLM: "results"})
	if o.ArtifactID != "" {
		t.Fatalf("ArtifactID = %q, want empty when no artifact evidence", o.ArtifactID)
	}
}
