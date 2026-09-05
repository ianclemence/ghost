package aqi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testServer(body string, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

const aqiOK = `{"current":{"us_aqi":42,"pm2_5":9.5,"time":"2026-09-05T10:00"}}`

func TestSuccess(t *testing.T) {
	s := testServer(aqiOK, 200)
	defer s.Close()
	svc := New(Config{Base: s.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	rep, r := svc.CurrentByCoords(context.Background(), 13.7, 100.5, false)
	if r.Err != nil {
		t.Fatalf("failed: %v", r.Err)
	}
	if rep.AQI != 42 || rep.Category != "Good" || r.Provider != "open-meteo-aqi" {
		t.Fatalf("wrong: %+v %+v", rep, r)
	}
}

func TestMalformed(t *testing.T) {
	s := testServer(`{"current":{"us_aqi":"bad"}}`, 200)
	defer s.Close()
	svc := New(Config{Base: s.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.CurrentByCoords(context.Background(), 0, 0, false); r.Err == nil {
		t.Fatal("non-numeric AQI must fail")
	}
}

func TestImpossibleAQI(t *testing.T) {
	s := testServer(`{"current":{"us_aqi":5000}}`, 200)
	defer s.Close()
	svc := New(Config{Base: s.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.CurrentByCoords(context.Background(), 0, 0, false); r.Err == nil {
		t.Fatal("impossible AQI must fail")
	}
}

func TestEmpty(t *testing.T) {
	s := testServer(` `, 200)
	defer s.Close()
	svc := New(Config{Base: s.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.CurrentByCoords(context.Background(), 0, 0, false); r.Err == nil {
		t.Fatal("empty must fail")
	}
}

func TestServerErrorHonest(t *testing.T) {
	s := testServer(`x`, 500)
	defer s.Close()
	svc := New(Config{Base: s.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.CurrentByCoords(context.Background(), 0, 0, false); r.Err == nil {
		t.Fatal("500 must fail honestly")
	}
}

func TestCategories(t *testing.T) {
	if CategoryFor(350) != "Hazardous" || CategoryFor(120) != "Unhealthy for Sensitive Groups" || CategoryFor(300) != "Very Unhealthy" {
		t.Fatal("categories wrong")
	}
}

func TestCacheRepeat(t *testing.T) {
	hits := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, aqiOK)
	}))
	defer s.Close()
	svc := New(Config{Base: s.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	ctx := context.Background()
	if _, r := svc.CurrentByCoords(ctx, 1, 1, false); r.Err != nil {
		t.Fatal(r.Err)
	}
	if _, r := svc.CurrentByCoords(ctx, 1, 1, false); !r.FromCache {
		t.Fatal("repeat must hit cache")
	}
	if hits != 1 {
		t.Fatalf("expected 1 hit, got %d", hits)
	}
}
