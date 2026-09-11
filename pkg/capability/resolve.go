package capability

import (
	"sort"
)

// Implementation is a replaceable way to fulfil a capability. Provider
// selection is runtime-owned: the model requests a capability, never an
// implementation. An implementation names the internal invocation mechanism
// (a tool) and the provider/integration behind it.
type Implementation struct {
	// Capability is the semantic identity this fulfils, e.g. "weather.get".
	Capability string
	// Provider identifies the replaceable implementation, e.g. "open-meteo",
	// "openweather", "google-calendar", "home-assistant", "local".
	Provider string
	// Tool is the internal invocation mechanism that executes it.
	Tool string
	// Priority orders implementations when several are available (higher
	// wins). Ties break deterministically on provider name.
	Priority int
	// Local marks a local/offline implementation, preferred when the runtime
	// is in local-first mode.
	Local bool
	// Available reports whether this implementation can serve right now
	// (credential present, connection healthy). Nil means always available.
	Available func() bool
}

// Resolver selects an implementation for a capability deterministically.
// It is a small runtime-owned mechanism, not a framework.
type Resolver struct {
	impls map[string][]Implementation
}

// NewResolver returns an empty resolver.
func NewResolver() *Resolver {
	return &Resolver{impls: map[string][]Implementation{}}
}

// Register adds an implementation for its capability.
func (r *Resolver) Register(impl Implementation) {
	if impl.Capability == "" || impl.Tool == "" {
		return
	}
	r.impls[impl.Capability] = append(r.impls[impl.Capability], impl)
}

// Implementations returns the registered implementations for a capability in
// deterministic preference order (available first, then priority desc, then
// provider name). Unavailable implementations are still returned so callers
// can report why nothing served.
func (r *Resolver) Implementations(capID string) []Implementation {
	out := append([]Implementation(nil), r.impls[capID]...)
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := availability(out[i]), availability(out[j])
		if ai != aj {
			return ai
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

// Resolve returns the preferred available implementation for a capability.
// preferLocal biases toward local implementations when they are available.
// The boolean is false when no implementation is available (the caller must
// then report a capability-specific outcome, not a provider error).
func (r *Resolver) Resolve(capID string, preferLocal bool) (Implementation, bool) {
	impls := r.Implementations(capID)
	if preferLocal {
		for _, impl := range impls {
			if impl.Local && availability(impl) {
				return impl, true
			}
		}
	}
	for _, impl := range impls {
		if availability(impl) {
			return impl, true
		}
	}
	return Implementation{}, false
}

// Has reports whether any implementation is registered for the capability.
func (r *Resolver) Has(capID string) bool {
	return len(r.impls[capID]) > 0
}

func availability(impl Implementation) bool {
	if impl.Available == nil {
		return true
	}
	return impl.Available()
}

// Availability reports which credential/connection-dependent implementations
// are usable right now. The runtime populates it from its credential and
// connected-app state; the capability package stays free of those imports.
// A nil predicate means the implementation is unconditionally available.
type Availability struct {
	OpenWeather   func() bool
	AviationStack func() bool
	AeroDataBox   func() bool
	HomeAssistant func() bool
	Calendar      func() bool
}

// avail adapts a runtime predicate. A keyed/connected implementation with no
// predicate is treated as unavailable: the runtime must explicitly report the
// credential/connection as present for it to serve.
func avail(fn func() bool) func() bool {
	if fn == nil {
		return func() bool { return false }
	}
	return fn
}

// RegisterDefaults registers Ghost's built-in capability→implementation map
// on r. This is a registration change, never a model-facing change.
func RegisterDefaults(r *Resolver, a Availability) {
	// weather.get — Open-Meteo (keyless) primary; OpenWeather preferred when
	// a key is present. Both execute the same semantic tool; the weather
	// service picks the provider at call time.
	r.Register(Implementation{Capability: "weather.get", Provider: "open-meteo", Tool: "weather_now", Priority: 10})
	r.Register(Implementation{Capability: "weather.get", Provider: "openweather", Tool: "weather_now", Priority: 20,
		Available: avail(a.OpenWeather)})

	// aqi.get — Open-Meteo air quality (keyless).
	r.Register(Implementation{Capability: "aqi.get", Provider: "open-meteo", Tool: "aqi_now", Priority: 10})

	// currency.convert — er-api primary, Frankfurter fallback (in-service).
	r.Register(Implementation{Capability: "currency.convert", Provider: "er-api", Tool: "currency_convert", Priority: 10})

	// crypto.price — CoinGecko primary, Coinbase fallback (in-service).
	r.Register(Implementation{Capability: "crypto.price", Provider: "coingecko", Tool: "crypto_price", Priority: 10})

	// places.nearby — Overpass/Nominatim.
	r.Register(Implementation{Capability: "places.nearby", Provider: "overpass", Tool: "places_nearby", Priority: 10})

	// flight.status — AviationStack primary, AeroDataBox fallback.
	r.Register(Implementation{Capability: "flight.status", Provider: "aviationstack", Tool: "flight_status", Priority: 20,
		Available: avail(a.AviationStack)})
	r.Register(Implementation{Capability: "flight.status", Provider: "aerodatabox", Tool: "flight_status", Priority: 10,
		Available: avail(a.AeroDataBox)})

	// web.search / web.fetch — local tools.
	r.Register(Implementation{Capability: "web.search", Provider: "local", Tool: "web_search", Local: true, Priority: 10})
	r.Register(Implementation{Capability: "web.fetch", Provider: "local", Tool: "web_fetch", Local: true, Priority: 10})

	// message.send — local delivery mechanism.
	r.Register(Implementation{Capability: "message.send", Provider: "local", Tool: "message", Local: true, Priority: 10})

	// device.read / device.control — Home Assistant integration.
	r.Register(Implementation{Capability: "device.read", Provider: "home-assistant", Tool: "device", Priority: 10,
		Available: avail(a.HomeAssistant)})
	r.Register(Implementation{Capability: "device.control", Provider: "home-assistant", Tool: "device", Priority: 10,
		Available: avail(a.HomeAssistant)})

	// artifact.create — local artifact store.
	r.Register(Implementation{Capability: "artifact.create", Provider: "local", Tool: "publish_artifact", Local: true, Priority: 10})

	// calendar.read / calendar.modify — Google Calendar integration.
	r.Register(Implementation{Capability: "calendar.read", Provider: "google-calendar", Tool: "calendar", Priority: 10,
		Available: avail(a.Calendar)})
	r.Register(Implementation{Capability: "calendar.modify", Provider: "google-calendar", Tool: "calendar", Priority: 10,
		Available: avail(a.Calendar)})

	// browser/computer — their own gates; registered so resolution is total.
	r.Register(Implementation{Capability: "browser.inspect", Provider: "local", Tool: "browser_navigate", Local: true, Priority: 10})
	r.Register(Implementation{Capability: "browser.control", Provider: "local", Tool: "browser_click", Local: true, Priority: 10})
	r.Register(Implementation{Capability: "computer.inspect", Provider: "local", Tool: "computer_inspect_ui", Local: true, Priority: 10})
	r.Register(Implementation{Capability: "computer.control", Provider: "local", Tool: "computer_click", Local: true, Priority: 10})
}
