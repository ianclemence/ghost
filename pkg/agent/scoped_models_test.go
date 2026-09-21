package agent

import "testing"

// The scoped cycling set round-trips through the workspace store; a nil set
// clears back to all-enabled.
func TestScopedModelsPersistence(t *testing.T) {
	al := pinTestLoop(t, nil)
	if al.DB() == nil {
		t.Skip("workspace store is not SQLite-backed in this configuration")
	}
	if !al.GetScopedModels().AllEnabled() {
		t.Fatal("default scope must be all-enabled")
	}
	ids := []string{"deepseek:deepseek-flash", "ollama:qwen3:0.6b"}
	if err := al.SetScopedModels(ids); err != nil {
		t.Fatal(err)
	}
	got := al.GetScopedModels()
	if got.AllEnabled() {
		t.Fatal("explicit scope must not read back as all-enabled")
	}
	back := got.IDs()
	if len(back) != 2 || back[0] != ids[0] || back[1] != ids[1] {
		t.Fatalf("scope did not round-trip: %v", back)
	}
	// Clearing restores the default.
	if err := al.SetScopedModels(nil); err != nil {
		t.Fatal(err)
	}
	if !al.GetScopedModels().AllEnabled() {
		t.Fatal("nil save must clear to all-enabled")
	}
}
