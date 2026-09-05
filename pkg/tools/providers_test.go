package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/providers/aqi"
	"github.com/ianclemence/ghost/pkg/providers/crypto"
	"github.com/ianclemence/ghost/pkg/providers/currency"
	"github.com/ianclemence/ghost/pkg/providers/flight"
	"github.com/ianclemence/ghost/pkg/providers/hass"
	"github.com/ianclemence/ghost/pkg/providers/nearby"
	"github.com/ianclemence/ghost/pkg/providers/weather"
)

func fakeServer(body string, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

func TestWeatherToolCoords(t *testing.T) {
	m := fakeServer(`{"current":{"temperature_2m":21.5,"weather_code":1,"time":"2026-09-05T10:00"}}`, 200)
	defer m.Close()
	tool := &WeatherTool{cfg: &weather.Config{OpenMeteoBase: m.URL, GeocodeBase: m.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second}}
	res := tool.Execute(context.Background(), map[string]interface{}{"latitude": 13.7, "longitude": 100.5})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "21.5") {
		t.Fatalf("missing temp: %s", res.ForLLM)
	}
}

func TestWeatherToolNeedsLocation(t *testing.T) {
	tool := NewWeatherTool("")
	res := tool.Execute(context.Background(), map[string]interface{}{})
	if !res.IsError || !strings.Contains(res.ForLLM, "Which location") {
		t.Fatalf("must ask for location: %+v", res)
	}
}

func TestWeatherToolProviderFailureHonest(t *testing.T) {
	m := fakeServer(`x`, 500)
	defer m.Close()
	tool := &WeatherTool{cfg: &weather.Config{OpenMeteoBase: m.URL, GeocodeBase: m.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second}}
	res := tool.Execute(context.Background(), map[string]interface{}{"location": "Nowhere"})
	// Geocode hits the same fake (500) -> honest failure, product language.
	if !res.IsError {
		t.Fatal("must be honest failure")
	}
	for _, banned := range []string{"API_KEY", "curl", "stack trace", "/var/lib"} {
		if strings.Contains(res.ForLLM, banned) {
			t.Fatalf("leaked %q in %q", banned, res.ForLLM)
		}
	}
}

func TestFlightToolNeedsNumber(t *testing.T) {
	tool := NewFlightTool()
	res := tool.Execute(context.Background(), map[string]interface{}{})
	if !res.IsError || !strings.Contains(res.ForLLM, "flight number") {
		t.Fatalf("must ask for number: %+v", res)
	}
}

func TestFlightToolSuccess(t *testing.T) {
	av := fakeServer(`{"data":[{"flight":{"iata":"TG123"},"airline":{"name":"Thai"},"departure":{"iata":"BKK"},"arrival":{"iata":"HKT"},"flight_status":"active"}]}`, 200)
	defer av.Close()
	tool := &FlightTool{cfg: &flight.Config{AviationBase: av.URL, AviationKey: "k", CacheTTL: time.Minute, BreakerCooldown: time.Second}}
	res := tool.Execute(context.Background(), map[string]interface{}{"flight_number": "tg123"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "TG123") || !strings.Contains(res.ForLLM, "active") {
		t.Fatalf("bad output: %s", res.ForLLM)
	}
}

func TestFlightToolUnconfigured(t *testing.T) {
	tool := &FlightTool{cfg: &flight.Config{}}
	res := tool.Execute(context.Background(), map[string]interface{}{"flight_number": "TG123"})
	if !res.IsError || !strings.Contains(res.ForLLM, "Flight tracking isn't connected") {
		t.Fatalf("must report product state: %+v", res)
	}
}

func TestAQIToolCoords(t *testing.T) {
	m := fakeServer(`{"current":{"us_aqi":42,"time":"2026-09-05T10:00"}}`, 200)
	defer m.Close()
	tool := &AQITool{cfg: &aqi.Config{Base: m.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second}}
	res := tool.Execute(context.Background(), map[string]interface{}{"latitude": 13.7, "longitude": 100.5})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "42") || !strings.Contains(res.ForLLM, "Good") {
		t.Fatalf("bad output: %s", res.ForLLM)
	}
}

func TestCurrencyTool(t *testing.T) {
	er := fakeServer(`{"result":"success","rates":{"EUR":0.92}}`, 200)
	defer er.Close()
	fb := fakeServer(`{"rates":{"EUR":0.921}}`, 200)
	defer fb.Close()
	tool := &CurrencyTool{cfg: &currency.Config{ErAPIBase: er.URL, FrankfurterBase: fb.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second}}
	res := tool.Execute(context.Background(), map[string]interface{}{"from": "USD", "to": "EUR", "amount": 100.0})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "92.00") {
		t.Fatalf("bad output: %s", res.ForLLM)
	}
}

func TestCryptoTool(t *testing.T) {
	g := fakeServer(`{"bitcoin":{"usd":67000.5}}`, 200)
	defer g.Close()
	tool := &CryptoTool{cfg: &crypto.Config{GeckoBase: g.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second}}
	res := tool.Execute(context.Background(), map[string]interface{}{"id": "bitcoin"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "67000") {
		t.Fatalf("bad output: %s", res.ForLLM)
	}
}

func TestNearbyToolCoords(t *testing.T) {
	p := fakeServer(`{"elements":[{"lat":13.75,"lon":100.5,"tags":{"name":"Cafe A"}}]}`, 200)
	defer p.Close()
	tool := &NearbyTool{cfg: &nearby.Config{OverpassPrimary: p.URL, OverpassFallback: p.URL, NominatimBase: p.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second}}
	res := tool.Execute(context.Background(), map[string]interface{}{"latitude": 13.75, "longitude": 100.5, "type": "cafe"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Cafe A") {
		t.Fatalf("bad output: %s", res.ForLLM)
	}
}

func TestNearbyToolNeedsPlace(t *testing.T) {
	tool := NewNearbyTool()
	res := tool.Execute(context.Background(), map[string]interface{}{})
	if !res.IsError || !strings.Contains(res.ForLLM, "Which location") {
		t.Fatalf("must ask for place: %+v", res)
	}
}

func TestProviderToolsTimeouts(t *testing.T) {
	tools := []Tool{NewWeatherTool(""), NewFlightTool(), NewAQITool(), NewCurrencyTool(), NewCryptoTool(), NewNearbyTool()}
	for _, tl := range tools {
		tt, ok := tl.(interface{ Timeout() time.Duration })
		if !ok {
			t.Fatalf("%s must declare Timeout", tl.Name())
		}
		if tt.Timeout() <= 0 || tt.Timeout() > time.Minute {
			t.Fatalf("%s timeout out of bounds: %v", tl.Name(), tt.Timeout())
		}
		if tl.Description() == "" || tl.Parameters() == nil {
			t.Fatalf("%s needs description + schema", tl.Name())
		}
	}
}

func TestProviderToolsRegisteredNames(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(NewWeatherTool(""))
	reg.Register(NewFlightTool())
	reg.Register(NewAQITool())
	reg.Register(NewCurrencyTool())
	reg.Register(NewCryptoTool())
	reg.Register(NewNearbyTool())
	for _, n := range []string{"weather_now", "flight_status", "aqi_now", "currency_convert", "crypto_price", "places_nearby"} {
		if _, ok := reg.Get(n); !ok {
			t.Fatalf("tool %s not registered", n)
		}
	}
}

func TestHassToolList(t *testing.T) {
	s := fakeServer(`[{"entity_id":"light.bedroom","state":"off","attributes":{"friendly_name":"Bedroom"}}]`, 200)
	defer s.Close()
	tool := &HassTool{cfg: &hass.Config{Base: s.URL, Token: "t"}}
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Bedroom") {
		t.Fatalf("bad output: %s", res.ForLLM)
	}
}

func TestHassToolUnconfigured(t *testing.T) {
	t.Setenv("HASS_URL", "")
	t.Setenv("HASS_TOKEN", "")
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	tool := NewHassTool()
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if !res.IsError {
		t.Fatal("unconfigured must fail honestly")
	}
}
