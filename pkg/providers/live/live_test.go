// Live end-to-end tests: real HTTP through the real provider strategy,
// validation, and cache layers. No mocks.
//
// Gate: GHOST_LIVE_TESTS=1 (default: skip). Keyed vendors run only when
// their env keys are present, else skip (recorded as NEEDS_CONFIGURATION,
// never PASS, never FAIL).
//
// Failure semantics (the L6 distinction):
//   - transport/infra failure (DNS, timeout, 5xx, 429) → Skip:
//     EXTERNAL_SERVICE_UNAVAILABLE, not a Ghost defect.
//   - HTTP 200 with unparseable/invalid payload → Fail: our parser or
//     contract is broken (GHOST_DEFECT).
package live

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/providers/aqi"
	"github.com/ianclemence/ghost/pkg/providers/crypto"
	"github.com/ianclemence/ghost/pkg/providers/currency"
	"github.com/ianclemence/ghost/pkg/providers/flight"
	"github.com/ianclemence/ghost/pkg/providers/nearby"
	"github.com/ianclemence/ghost/pkg/providers/weather"
)

func liveEnabled(t *testing.T) bool {
	t.Helper()
	if os.Getenv("GHOST_LIVE_TESTS") != "1" {
		t.Skip("live tests off (GHOST_LIVE_TESTS=1 to run)")
	}
	return true
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 25*time.Second)
}

// skipInfra skips on transport/infra failures, fails on 200-but-invalid
// (our defect). Callers pass the strategy result's error + class.
func skipInfra(t *testing.T, err error, failure provider.FailureClass) {
	t.Helper()
	switch failure {
	case provider.FailDNS, provider.FailNetwork, provider.FailTimeout,
		provider.FailServer, provider.FailUnavailable, provider.FailRateLimited:
		t.Skipf("EXTERNAL_SERVICE_UNAVAILABLE (%s): %v", failure, err)
	}
}

// --- Weather (keyless) ---

func TestLiveWeatherBangkok(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	svc := weather.New(weather.Config{})
	c, cancel := ctx()
	defer cancel()
	cur, r := svc.CurrentByCoords(c, 13.75, 100.5, false)
	if r.Err != nil {
		skipInfra(t, r.Err, r.Failure)
		t.Fatalf("GHOST_DEFECT: live weather failed: %v (class %s)", r.Err, r.Failure)
	}
	if cur.TemperatureC < -90 || cur.TemperatureC > 60 {
		t.Fatalf("implausible live temp: %f", cur.TemperatureC)
	}
	t.Logf("Bangkok: %.1f°C via %s", cur.TemperatureC, r.Provider)
}

func TestLiveWeatherPlace(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	svc := weather.New(weather.Config{})
	c, cancel := ctx()
	defer cancel()
	cur, r := svc.CurrentByPlace(c, "Bangkok", false)
	if r.Err != nil {
		skipInfra(t, r.Err, r.Failure)
		t.Fatalf("GHOST_DEFECT: live place weather failed: %v", r.Err)
	}
	t.Logf("Bangkok via place: %.1f°C (%s)", cur.TemperatureC, r.Provider)
}

// --- AQI (keyless) ---

func TestLiveAQIBangkok(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	svc := aqi.New(aqi.Config{})
	c, cancel := ctx()
	defer cancel()
	rep, r := svc.CurrentByCoords(c, 13.75, 100.5, false)
	if r.Err != nil {
		skipInfra(t, r.Err, r.Failure)
		t.Fatalf("GHOST_DEFECT: live AQI failed: %v", r.Err)
	}
	if rep.Category == "" {
		t.Fatal("missing AQI category")
	}
	t.Logf("Bangkok AQI %d (%s)", rep.AQI, rep.Category)
}

// --- Currency (keyless) ---

func TestLiveCurrencyUSDEUR(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	svc := currency.New(currency.Config{})
	c, cancel := ctx()
	defer cancel()
	conv, r := svc.Convert(c, "USD", "EUR", 100)
	if r.Err != nil {
		skipInfra(t, r.Err, r.Failure)
		t.Fatalf("GHOST_DEFECT: live currency failed: %v", r.Err)
	}
	if conv.Rate <= 0 || conv.Converted <= 0 {
		t.Fatalf("bad conversion: %+v", conv)
	}
	t.Logf("100 USD = %.2f EUR via %s", conv.Converted, r.Provider)
}

// --- Crypto (keyless) ---

func TestLiveCryptoBitcoin(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	svc := crypto.New(crypto.Config{})
	c, cancel := ctx()
	defer cancel()
	p, r := svc.GetPrice(c, "bitcoin", "USD")
	if r.Err != nil {
		skipInfra(t, r.Err, r.Failure)
		t.Fatalf("GHOST_DEFECT: live crypto failed: %v", r.Err)
	}
	if p.Value <= 0 {
		t.Fatalf("bad price: %+v", p)
	}
	t.Logf("BTC = %.2f USD via %s", p.Value, r.Provider)
}

// --- Nearby (keyless, OSM) ---

func TestLiveNearbyCafes(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	svc := nearby.New(nearby.Config{})
	c, cancel := ctx()
	defer cancel()
	places, r := svc.SearchByCoords(c, "cafe", 13.75, 100.5, 1500, 5)
	if r.Err != nil {
		skipInfra(t, r.Err, r.Failure)
		// Valid-empty (no cafes found) is honest, not a defect.
		if r.Failure == provider.FailEmpty {
			t.Skip("no cafes indexed near test point (honest empty)")
		}
		t.Fatalf("GHOST_DEFECT: live nearby failed: %v", r.Err)
	}
	if len(places) == 0 || places[0].Name == "" {
		t.Fatal("empty place list on success")
	}
	t.Logf("found %d cafes via %s, first: %s", len(places), r.Provider, places[0].Name)
}

func TestLiveGeocodeBangkok(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	svc := nearby.New(nearby.Config{})
	c, cancel := ctx()
	defer cancel()
	lat, lon, err := svc.Geocode(c, "Bangkok")
	if err != nil {
		t.Skipf("EXTERNAL_SERVICE_UNAVAILABLE: geocode: %v", err)
	}
	if lat < 13 || lat > 14.5 || lon < 99.5 || lon > 101 {
		t.Fatalf("geocode drift: %f,%f", lat, lon)
	}
}

// --- Flight (keyed; skip without credentials) ---

func TestLiveFlightLookup(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	avKey := os.Getenv("AVIATION_API_KEY")
	adKey := os.Getenv("AERODATABOX_API_KEY")
	if avKey == "" && adKey == "" {
		t.Skip("NEEDS_CONFIGURATION: set AVIATION_API_KEY or AERODATABOX_API_KEY")
	}
	svc := flight.New(flight.Config{AviationKey: avKey, AeroDataBoxKey: adKey})
	c, cancel := ctx()
	defer cancel()
	f, r := svc.Lookup(c, "TG123")
	if r.Err != nil {
		skipInfra(t, r.Err, r.Failure)
		if r.Failure == provider.FailEmpty || r.Failure == provider.FailNotConfigured {
			t.Skipf("honest non-success: %v", r.Err)
		}
		t.Fatalf("GHOST_DEFECT: live flight failed: %v", r.Err)
	}
	t.Logf("TG123: %s %s->%s via %s", f.Status, f.From, f.To, r.Provider)
}

// --- OpenWeather fallback (keyed) ---

func TestLiveWeatherFallbackChain(t *testing.T) {
	if !liveEnabled(t) {
		return
	}
	owKey := os.Getenv("OPENWEATHER_API_KEY")
	if owKey == "" {
		t.Skip("NEEDS_CONFIGURATION: set OPENWEATHER_API_KEY to exercise fallback")
	}
	svc := weather.New(weather.Config{OpenWeatherKey: owKey})
	c, cancel := ctx()
	defer cancel()
	_, r := svc.CurrentByCoords(c, 13.75, 100.5, false)
	if r.Err != nil {
		skipInfra(t, r.Err, r.Failure)
		t.Fatalf("GHOST_DEFECT: %v", r.Err)
	}
	t.Logf("weather via %s", r.Provider)
}
