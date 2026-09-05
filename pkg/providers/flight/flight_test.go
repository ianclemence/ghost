package flight

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func testServer(body string, status int, check func(r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

const aviationOK = `{"data":[{"flight":{"iata":"TG123"},"airline":{"name":"Thai Airways"},"departure":{"iata":"BKK","scheduled":"2026-09-05T10:00:00+00:00","gate":"A1","terminal":"1"},"arrival":{"iata":"HKT"},"flight_status":"active","flight_date":"2026-09-05"}]}`
const aeroOK = `[{"number":"TG123","airline":{"name":"Thai Airways"},"status":"Departed","departure":{"airport":{"iata":"BKK"},"scheduledTimeLocal":"2026-09-05 10:00"},"arrival":{"airport":{"iata":"HKT"}}}]`

func svc(av, aero *httptest.Server, aKey, dKey string) *Service {
	cfg := Config{CacheTTL: time.Minute, BreakerCooldown: time.Second}
	if av != nil {
		cfg.AviationBase = av.URL
		cfg.AviationKey = aKey
	}
	if aero != nil {
		cfg.AeroDataBoxBase = aero.URL
		cfg.AeroDataBoxKey = dKey
	}
	return New(cfg)
}

func TestPrimarySuccess(t *testing.T) {
	av := testServer(aviationOK, 200, func(r *http.Request) {
		if r.URL.Query().Get("flight_iata") != "TG123" {
			t.Errorf("must pass flight number, got %s", r.URL.RawQuery)
		}
	})
	defer av.Close()
	s := svc(av, nil, "k", "")
	f, r := s.Lookup(context.Background(), "tg123")
	if r.Err != nil {
		t.Fatalf("primary failed: %v", r.Err)
	}
	if r.Provider != "aviationstack" || f.Status != StatusActive || f.From != "BKK" || f.Number != "TG123" {
		t.Fatalf("wrong result: %+v %+v", f, r)
	}
}

func TestPrimary500Fallback(t *testing.T) {
	av := testServer(`err`, 500, nil)
	defer av.Close()
	aero := testServer(aeroOK, 200, func(r *http.Request) {
		if r.Header.Get("X-RapidAPI-Key") != "dk" {
			t.Errorf("fallback must send key header")
		}
	})
	defer aero.Close()
	s := svc(av, aero, "k", "dk")
	f, r := s.Lookup(context.Background(), "TG123")
	if r.Err != nil || r.Provider != "aerodatabox" || f.To != "HKT" {
		t.Fatalf("fallback failed: %+v %+v", f, r)
	}
}

func TestPrimary429Fallback(t *testing.T) {
	av := testServer(`{}`, 429, nil)
	defer av.Close()
	aero := testServer(aeroOK, 200, nil)
	defer aero.Close()
	s := svc(av, aero, "k", "dk")
	if _, r := s.Lookup(context.Background(), "TG123"); r.Err != nil || r.Provider != "aerodatabox" {
		t.Fatalf("429 must fall back: %+v", r)
	}
}

func TestMalformedStatus(t *testing.T) {
	av := testServer(`{"data":[{"flight":{"iata":"TG123"},"flight_status":{"weird":1}}]}`, 200, nil)
	defer av.Close()
	s := svc(av, nil, "k", "")
	if _, r := s.Lookup(context.Background(), "TG123"); r.Err == nil {
		t.Fatal("non-string status must fail validation")
	}
}

func TestUnknownFlightHonest(t *testing.T) {
	av := testServer(`{"data":[]}`, 200, nil)
	defer av.Close()
	aero := testServer(`[]`, 200, nil)
	defer aero.Close()
	s := svc(av, aero, "k", "dk")
	_, r := s.Lookup(context.Background(), "XX0000")
	if r.Err == nil {
		t.Fatal("unknown flight must fail honestly, never fabricate")
	}
}

func TestAllFailHonest(t *testing.T) {
	av := testServer(`x`, 500, nil)
	defer av.Close()
	aero := testServer(`y`, 500, nil)
	defer aero.Close()
	s := svc(av, aero, "k", "dk")
	if _, r := s.Lookup(context.Background(), "TG123"); r.Err == nil {
		t.Fatal("all-fail must be honest")
	}
}

func TestUnconfiguredHonest(t *testing.T) {
	s := New(Config{})
	_, r := s.Lookup(context.Background(), "TG123")
	if r.Err == nil {
		t.Fatal("unconfigured must fail honestly")
	}
}

func TestSingleKeyStillReady(t *testing.T) {
	aero := testServer(aeroOK, 200, nil)
	defer aero.Close()
	s := svc(nil, aero, "", "dk")
	if !s.cfg.Configured() {
		t.Fatal("fallback key alone must make capability ready")
	}
	if _, r := s.Lookup(context.Background(), "TG123"); r.Err != nil {
		t.Fatalf("single-key lookup failed: %v", r.Err)
	}
}

func TestNormalizeStatus(t *testing.T) {
	cases := map[string]Status{
		"scheduled": StatusScheduled, "active": StatusActive, "en-route": StatusActive,
		"landed": StatusLanded, "cancelled": StatusCancelled, "diverted": StatusDiverted,
		"Departed": StatusActive, "Arrived": StatusLanded, "bogus": StatusUnknown, "": StatusUnknown,
	}
	for in, want := range cases {
		if got := NormalizeStatus(in); got != want {
			t.Fatalf("%q: got %s want %s", in, got, want)
		}
	}
}

func TestCacheRepeat(t *testing.T) {
	hits := 0
	av := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(200)
		fmt.Fprint(w, aviationOK)
	}))
	defer av.Close()
	s := svc(av, nil, "k", "")
	ctx := context.Background()
	if _, r := s.Lookup(ctx, "TG123"); r.Err != nil {
		t.Fatal(r.Err)
	}
	if _, r := s.Lookup(ctx, "TG123"); r.Err != nil || !r.FromCache {
		t.Fatalf("repeat must hit cache: %+v", r)
	}
	if hits != 1 {
		t.Fatalf("expected 1 upstream hit, got %d", hits)
	}
}

func TestAeroDataBoxRequestFormation(t *testing.T) {
	// Wire-compatibility with the documented AeroDataBox API
	// (GET {base}/flights/number/{num} with X-RapidAPI-Key header).
	var gotPath, gotKey string
	aero := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-RapidAPI-Key")
		w.WriteHeader(200)
		fmt.Fprint(w, aeroOK)
	}))
	defer aero.Close()
	av := testServer(`x`, 500, nil)
	defer av.Close()
	s := svc(av, aero, "k", "dk-secret")
	if _, r := s.Lookup(context.Background(), "tg123"); r.Err != nil || r.Provider != "aerodatabox" {
		t.Fatalf("fallback must engage: %+v", r)
	}
	if gotPath != "/flights/number/TG123" {
		t.Fatalf("wrong path (number must be uppercased in path): %s", gotPath)
	}
	if gotKey != "dk-secret" {
		t.Fatal("fallback must authenticate via X-RapidAPI-Key header")
	}
}

func TestAviationStackRequestFormation(t *testing.T) {
	// Wire-compatibility with the documented AviationStack API
	// (GET {base}/v1/flights?access_key=&flight_iata=).
	var gotQuery, gotPath string
	av := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(200)
		fmt.Fprint(w, aviationOK)
	}))
	defer av.Close()
	s := svc(av, nil, "av-key", "")
	if _, r := s.Lookup(context.Background(), "TG123"); r.Err != nil {
		t.Fatalf("lookup failed: %v", r.Err)
	}
	q, _ := url.ParseQuery(gotQuery)
	if gotPath != "/v1/flights" {
		t.Fatalf("wrong path: %s", gotPath)
	}
	if q.Get("access_key") != "av-key" || q.Get("flight_iata") != "TG123" {
		t.Fatalf("malformed aviationstack query: %s", gotQuery)
	}
}
