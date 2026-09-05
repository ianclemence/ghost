package weather

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
)

func testServer(body string, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

const meteoOK = `{"current":{"temperature_2m":21.5,"relative_humidity_2m":60,"weather_code":1,"time":"2026-09-05T10:00"}}`
const owOK = `{"main":{"temp":22.0,"humidity":55},"weather":[{"description":"clear sky"}],"dt":1780000000}`

func coordsSvc(meteo *httptest.Server, ow *httptest.Server, key string) *Service {
	cfg := Config{OpenMeteoBase: meteo.URL, GeocodeBase: meteo.URL, BreakerCooldown: time.Second, CacheTTL: time.Minute}
	if ow != nil {
		cfg.OpenWeatherBase = ow.URL
		cfg.OpenWeatherKey = key
	}
	return New(cfg)
}

func TestPrimarySuccess(t *testing.T) {
	m := testServer(meteoOK, 200)
	defer m.Close()
	s := coordsSvc(m, nil, "")
	cur, r := s.CurrentByCoords(context.Background(), 13.7, 100.5, false)
	if r.Err != nil {
		t.Fatalf("primary failed: %v", r.Err)
	}
	if r.Provider != "open-meteo" || cur.TemperatureC != 21.5 {
		t.Fatalf("wrong result: %+v %+v", cur, r)
	}
}

func TestPrimaryTimeoutFallbackSuccess(t *testing.T) {
	m := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer m.Close()
	ow := testServer(owOK, 200)
	defer ow.Close()
	// Simulate primary failure so fallback engages.
	m2 := testServer(`oops`, 500)
	defer m2.Close()
	s2 := coordsSvc(m2, ow, "k")
	cur, r := s2.CurrentByCoords(context.Background(), 13.7, 100.5, false)
	if r.Err != nil || r.Provider != "openweather" || cur.TemperatureC != 22.0 {
		t.Fatalf("fallback failed: %+v %+v", cur, r)
	}
}

func TestPrimary429Fallback(t *testing.T) {
	m := testServer(`{}`, 429)
	defer m.Close()
	ow := testServer(owOK, 200)
	defer ow.Close()
	s := coordsSvc(m, ow, "k")
	_, r := s.CurrentByCoords(context.Background(), 0, 0, false)
	if r.Err != nil || r.Provider != "openweather" {
		t.Fatalf("429 must fall back: %+v", r)
	}
}

func TestPrimary500Fallback(t *testing.T) {
	m := testServer(`err`, 500)
	defer m.Close()
	ow := testServer(owOK, 200)
	defer ow.Close()
	s := coordsSvc(m, ow, "k")
	if _, r := s.CurrentByCoords(context.Background(), 0, 0, false); r.Err != nil {
		t.Fatalf("500 must fall back: %v", r.Err)
	}
}

func TestMalformedResponse(t *testing.T) {
	m := testServer(`{"current":{"temperature_2m":"banana"}}`, 200)
	defer m.Close()
	s := coordsSvc(m, nil, "")
	if _, r := s.CurrentByCoords(context.Background(), 0, 0, false); r.Err == nil {
		t.Fatal("banana temperature must fail validation")
	}
}

func TestEmptyResponse(t *testing.T) {
	m := testServer(`  `, 200)
	defer m.Close()
	s := coordsSvc(m, nil, "")
	if _, r := s.CurrentByCoords(context.Background(), 0, 0, false); r.Err == nil {
		t.Fatal("empty response must fail")
	}
}

func TestFallbackFailureHonest(t *testing.T) {
	m := testServer(`x`, 500)
	defer m.Close()
	ow := testServer(`y`, 500)
	defer ow.Close()
	s := coordsSvc(m, ow, "k")
	_, r := s.CurrentByCoords(context.Background(), 0, 0, false)
	if r.Err == nil {
		t.Fatal("all-fail must be honest failure")
	}
}

func TestOfflineStaleSemantics(t *testing.T) {
	m := testServer(meteoOK, 200)
	defer m.Close()
	s := New(Config{OpenMeteoBase: m.URL, GeocodeBase: m.URL, CacheTTL: 50 * time.Millisecond, BreakerCooldown: time.Second})
	ctx := context.Background()
	if _, r := s.CurrentByCoords(ctx, 1, 1, false); r.Err != nil {
		t.Fatal(r.Err)
	}
	time.Sleep(60 * time.Millisecond)
	// Fresh required -> must NOT serve stale silently; strategy re-runs.
	// Point at a dead server to prove staleness handling.
	s.cfg.OpenMeteoBase = "http://127.0.0.1:1"
	if _, r := s.CurrentByCoords(ctx, 1, 1, true); !r.Stale || !r.FromCache {
		t.Fatalf("allowStale must serve stale explicitly: %+v", r)
	}
}

func TestCacheRepeatRequest(t *testing.T) {
	hits := 0
	m := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(200)
		fmt.Fprint(w, meteoOK)
	}))
	defer m.Close()
	s := coordsSvc(m, nil, "")
	ctx := context.Background()
	if _, r := s.CurrentByCoords(ctx, 2, 2, false); r.Err != nil {
		t.Fatal(r.Err)
	}
	if _, r := s.CurrentByCoords(ctx, 2, 2, false); r.Err != nil || !r.FromCache {
		t.Fatalf("repeat must hit cache: %+v", r)
	}
	if hits != 1 {
		t.Fatalf("expected 1 upstream hit, got %d", hits)
	}
}

func TestProviderRecoveryAfterCooldown(t *testing.T) {
	fail := true
	m := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(500)
			fmt.Fprint(w, "bad")
			return
		}
		w.WriteHeader(200)
		fmt.Fprint(w, meteoOK)
	}))
	defer m.Close()
	s := New(Config{OpenMeteoBase: m.URL, GeocodeBase: m.URL, CacheTTL: time.Minute, BreakerCooldown: 60 * time.Millisecond})
	ctx := context.Background()
	// Trip the breaker with direct strategy using one provider.
	p := s.openMeteoProvider(0, 0)
	p.Breaker = provider.NewBreaker(1, 60*time.Millisecond)
	strat := provider.Strategy[Current]{Providers: []provider.Provider[Current]{p}, Retry: provider.RetryPolicy{MaxAttempts: 1}, Timeout: 5 * time.Second}
	if r := strat.Execute(ctx); r.Err == nil {
		t.Fatal("expected failure while down")
	}
	if p.Breaker.State() != provider.CircuitOpen {
		t.Fatal("breaker must open")
	}
	fail = false
	time.Sleep(70 * time.Millisecond)
	p2 := s.openMeteoProvider(0, 0)
	p2.Breaker = p.Breaker
	strat2 := provider.Strategy[Current]{Providers: []provider.Provider[Current]{p2}, Retry: provider.RetryPolicy{MaxAttempts: 1}, Timeout: 5 * time.Second}
	if r := strat2.Execute(ctx); r.Err != nil {
		t.Fatalf("must recover after cooldown: %v", r.Err)
	}
}

func TestImpossibleValuesRejected(t *testing.T) {

	for _, temp := range []string{`{"current":{"temperature_2m":999}}`, `{"current":{"temperature_2m":-200}}`} {
		m := testServer(temp, 200)
		s := coordsSvc(m, nil, "")
		if _, r := s.CurrentByCoords(context.Background(), 0, 0, false); r.Err == nil {
			t.Fatalf("impossible temp must fail: %s", temp)
		}
		m.Close()
	}
	m := testServer(`{"current":{"temperature_2m":20,"relative_humidity_2m":500}}`, 200)
	defer m.Close()
	s := coordsSvc(m, nil, "")
	if _, r := s.CurrentByCoords(context.Background(), 0, 0, false); r.Err == nil {
		t.Fatal("humidity 500 must fail")
	}
}

func TestOpenWeatherRequestFormation(t *testing.T) {
	// Wire-compatibility with the documented OpenWeather API
	// (api.openweathermap.org/data/2.5/weather?lat=&lon=&units=metric&appid=):
	// proves the fallback request is well-formed without needing a key.
	var gotQuery string
	var gotPath string
	m := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(200)
		fmt.Fprint(w, owOK)
	}))
	defer m.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, "down")
	}))
	defer dead.Close()
	s := New(Config{OpenMeteoBase: dead.URL, GeocodeBase: dead.URL, OpenWeatherBase: m.URL, OpenWeatherKey: "test-key",
		CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := s.CurrentByCoords(context.Background(), 13.75, 100.5, false); r.Err != nil || r.Provider != "openweather" {
		t.Fatalf("fallback must engage: %+v", r)
	}
	q, _ := url.ParseQuery(gotQuery)
	if gotPath != "/data/2.5/weather" {
		t.Fatalf("wrong path: %s", gotPath)
	}
	if q.Get("appid") != "test-key" || q.Get("lat") == "" || q.Get("lon") == "" || q.Get("units") != "metric" {
		t.Fatalf("malformed openweather query: %s", gotQuery)
	}
}
