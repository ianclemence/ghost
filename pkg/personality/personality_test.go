package personality

import (
	"testing"
)

func TestBuiltinPersonalities(t *testing.T) {
	dir := t.TempDir()
loader := NewLoader(dir)

	list := loader.List()
	if len(list) < 5 {
		t.Fatalf("expected at least 5 builtin personalities, got %d", len(list))
	}

	names := make(map[string]bool)
	for _, p := range list {
		names[p.Name] = true
		if !p.Builtin {
			t.Errorf("expected builtin flag for %s", p.Name)
		}
	}
	for _, expected := range []string{"default", "hacker", "creative", "teacher", "minimal"} {
		if !names[expected] {
			t.Errorf("missing builtin personality: %s", expected)
		}
	}
}

func TestSetAndGetActive(t *testing.T) {
	dir := t.TempDir()
loader := NewLoader(dir)

	if err := loader.Set("hacker"); err != nil {
		t.Fatal(err)
	}
	if loader.Active() != "hacker" {
		t.Fatalf("expected hacker, got %s", loader.Active())
	}
	if err := loader.Set("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent personality")
	}
}

func TestSaveAndDelete(t *testing.T) {
	dir := t.TempDir()
loader := NewLoader(dir)

	err := loader.Save("custom", "A custom personality", "Be custom.")
	if err != nil {
		t.Fatal(err)
	}

	p, ok := loader.Get("custom")
	if !ok || p.Content != "Be custom." {
		t.Fatal("failed to retrieve saved personality")
	}

	if err := loader.Delete("custom"); err != nil {
		t.Fatal(err)
	}
	if _, ok := loader.Get("custom"); ok {
		t.Fatal("expected personality to be deleted")
	}
}

func TestCannotOverwriteBuiltin(t *testing.T) {
	dir := t.TempDir()
loader := NewLoader(dir)

	err := loader.Save("hacker", "overwrite", "bad")
	if err == nil {
		t.Fatal("expected error when overwriting builtin")
	}
}

func TestCannotDeleteBuiltin(t *testing.T) {
	dir := t.TempDir()
loader := NewLoader(dir)

	err := loader.Delete("default")
	if err == nil {
		t.Fatal("expected error when deleting builtin")
	}
}

func TestDefaultContent(t *testing.T) {
	dir := t.TempDir()
loader := NewLoader(dir)

	content := loader.GetActiveContent()
	if content == "" {
		t.Fatal("expected default personality content")
	}
}
