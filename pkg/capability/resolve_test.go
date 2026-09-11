package capability

import "testing"

func yes() bool { return true }

func testResolver(a Availability) *Resolver {
	r := NewResolver()
	RegisterDefaults(r, a)
	return r
}

// Provider selection is runtime-owned and credential-aware.
func TestResolverSelectsByCredential(t *testing.T) {
	// No OpenWeather key: keyless Open-Meteo is chosen.
	r := testResolver(Availability{})
	impl, ok := r.Resolve("weather.get", false)
	if !ok || impl.Provider != "open-meteo" {
		t.Fatalf("without key: got %+v ok=%v, want open-meteo", impl, ok)
	}
	// With OpenWeather key: higher priority wins.
	r = testResolver(Availability{OpenWeather: func() bool { return true }})
	impl, ok = r.Resolve("weather.get", false)
	if !ok || impl.Provider != "openweather" {
		t.Fatalf("with key: got %+v ok=%v, want openweather", impl, ok)
	}
}

// Provider replacement must not change capability identity or the tool.
func TestProviderReplacementKeepsIdentity(t *testing.T) {
	base := testResolver(Availability{})
	keyed := testResolver(Availability{OpenWeather: func() bool { return true }})
	b, _ := base.Resolve("weather.get", false)
	k, _ := keyed.Resolve("weather.get", false)
	if b.Capability != k.Capability || b.Tool != k.Tool {
		t.Fatalf("provider change altered capability/tool: %+v vs %+v", b, k)
	}
	if b.Provider == k.Provider {
		t.Fatal("expected a different provider after credential change")
	}
}

// No available implementation yields a capability-specific miss, not a panic.
func TestResolverNoImplementation(t *testing.T) {
	r := testResolver(Availability{}) // no Home Assistant
	if _, ok := r.Resolve("device.control", false); ok {
		t.Fatal("device.control must not resolve without Home Assistant")
	}
	if !r.Has("device.control") {
		t.Fatal("the implementation should still be registered (unavailable)")
	}
	r2 := testResolver(Availability{HomeAssistant: func() bool { return true }})
	if impl, ok := r2.Resolve("device.control", false); !ok || impl.Provider != "home-assistant" {
		t.Fatalf("with HA: got %+v ok=%v", impl, ok)
	}
}

// Local-first preference is honored when a local implementation exists.
func TestResolverPrefersLocal(t *testing.T) {
	r := NewResolver()
	r.Register(Implementation{Capability: "x.do", Provider: "cloud", Tool: "cloud_tool", Priority: 100})
	r.Register(Implementation{Capability: "x.do", Provider: "local", Tool: "local_tool", Local: true, Priority: 1})
	impl, _ := r.Resolve("x.do", true)
	if impl.Provider != "local" {
		t.Fatalf("preferLocal must pick local, got %+v", impl)
	}
	impl, _ = r.Resolve("x.do", false)
	if impl.Provider != "cloud" {
		t.Fatalf("non-local must pick highest priority, got %+v", impl)
	}
}

// Deterministic tie-break: equal priority orders by provider name.
func TestResolverDeterministicTieBreak(t *testing.T) {
	r := NewResolver()
	r.Register(Implementation{Capability: "y.do", Provider: "zeta", Tool: "z"})
	r.Register(Implementation{Capability: "y.do", Provider: "alpha", Tool: "a"})
	impl, _ := r.Resolve("y.do", false)
	if impl.Provider != "alpha" {
		t.Fatalf("tie-break must be deterministic by name, got %+v", impl)
	}
}
