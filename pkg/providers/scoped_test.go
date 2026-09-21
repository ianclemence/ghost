package providers

import "testing"

func opts() []ModelOption {
	return []ModelOption{
		{Name: "local", Target: "local", Available: true},
		{Name: "deepseek", Target: "deepseek:deepseek-flash", Available: true},
		{Name: "kimi", Target: "moonshot:kimi-k3", Available: true},
	}
}

func TestScopedAllEnabledByDefault(t *testing.T) {
	var sc ScopedModels
	if !sc.AllEnabled() {
		t.Fatal("zero value must be all-enabled")
	}
	if sc.IDs() != nil {
		t.Fatalf("all-enabled must expose nil ids, got %v", sc.IDs())
	}
	if !sc.IsEnabled("anything") {
		t.Fatal("all-enabled must report every target enabled")
	}
	if got := FilterScoped(opts(), sc); len(got) != 3 {
		t.Fatalf("all-enabled filter must pass options through, got %d", len(got))
	}
}

func TestScopedFilterAndNormalize(t *testing.T) {
	var sc ScopedModels
	sc.Set([]string{"deepseek:deepseek-flash", "local"})
	got := FilterScoped(opts(), sc)
	if len(got) != 2 || got[0].Target != "deepseek:deepseek-flash" || got[1].Target != "local" {
		t.Fatalf("filter must honor scoped order, got %+v", got)
	}
	if sc.IsEnabled("kimi") {
		t.Fatal("kimi should be disabled")
	}
	// A list covering every option collapses to nil.
	if n := NormalizeScoped(TargetsFromOptions(opts()), opts()); n != nil {
		t.Fatalf("full coverage must collapse to all-enabled, got %v", n)
	}
	// Unknown targets are dropped; a partial set survives.
	if n := NormalizeScoped([]string{"local", "ghost:x"}, opts()); len(n) != 1 || n[0] != "local" {
		t.Fatalf("normalize should drop unknown targets, got %v", n)
	}
}

func TestCycleScopedOrderAndWrap(t *testing.T) {
	var sc ScopedModels
	sc.Set([]string{"local", "deepseek:deepseek-flash"})
	list := opts()

	next, ok := CycleScoped(list, sc, "local", 1)
	if !ok || next.Target != "deepseek:deepseek-flash" {
		t.Fatalf("cycle from local should reach deepseek, got %+v ok=%v", next, ok)
	}
	next, ok = CycleScoped(list, sc, "deepseek:deepseek-flash", 1)
	if !ok || next.Target != "local" {
		t.Fatalf("cycle should wrap within scope, got %+v ok=%v", next, ok)
	}
	// A single enabled model cannot cycle.
	one := ScopedModels{}
	one.Set([]string{"local"})
	if _, ok := CycleScoped(list, one, "local", 1); ok {
		t.Fatal("a one-model scope must not cycle")
	}
}

func TestFilterScopedFallsBackWhenEmpty(t *testing.T) {
	var sc ScopedModels
	sc.Set([]string{"nonexistent"})
	if got := FilterScoped(opts(), sc); len(got) != 3 {
		t.Fatalf("an unmatched scope must fall back to the input, got %d", len(got))
	}
}
