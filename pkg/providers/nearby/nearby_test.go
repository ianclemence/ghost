package nearby

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

const overpassOK = `{"elements":[{"lat":13.7563,"lon":100.5018,"tags":{"name":"Cafe A","amenity":"cafe"}},{"lat":13.76,"lon":100.51,"tags":{"name":"Cafe B","amenity":"cafe"}}]}`

func TestPrimarySuccess(t *testing.T) {
	p := testServer(overpassOK, 200)
	defer p.Close()
	m := testServer(overpassOK, 200)
	defer m.Close()
	svc := New(Config{OverpassPrimary: p.URL, OverpassFallback: m.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	places, r := svc.SearchByCoords(context.Background(), "cafe", 13.75, 100.5, 1500, 10)
	if r.Err != nil {
		t.Fatalf("failed: %v", r.Err)
	}
	if len(places) != 2 || places[0].Name != "Cafe A" || r.Provider != "overpass-primary" {
		t.Fatalf("wrong: %+v %+v", places, r)
	}
}

func TestMirrorFallback(t *testing.T) {
	p := testServer(`x`, 500)
	defer p.Close()
	m := testServer(overpassOK, 200)
	defer m.Close()
	svc := New(Config{OverpassPrimary: p.URL, OverpassFallback: m.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	places, r := svc.SearchByCoords(context.Background(), "cafe", 13.75, 100.5, 1500, 10)
	if r.Err != nil || r.Provider != "overpass-mirror" || len(places) != 2 {
		t.Fatalf("mirror fallback failed: %+v %+v", places, r)
	}
}

func TestMalformed(t *testing.T) {
	p := testServer(`not json`, 200)
	defer p.Close()
	m := testServer(`not json`, 200)
	defer m.Close()
	svc := New(Config{OverpassPrimary: p.URL, OverpassFallback: m.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.SearchByCoords(context.Background(), "cafe", 0, 0, 1500, 10); r.Err == nil {
		t.Fatal("malformed must fail")
	}
}

func TestEmptyHonest(t *testing.T) {
	p := testServer(`{"elements":[]}`, 200)
	defer p.Close()
	m := testServer(`{"elements":[]}`, 200)
	defer m.Close()
	svc := New(Config{OverpassPrimary: p.URL, OverpassFallback: m.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.SearchByCoords(context.Background(), "cafe", 0, 0, 1500, 10); r.Err == nil {
		t.Fatal("zero results must fail honestly, not fabricate")
	}
}

func TestGeocode(t *testing.T) {
	g := testServer(`[{"lat":"13.7563","lon":"100.5018"}]`, 200)
	defer g.Close()
	svc := New(Config{NominatimBase: g.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	lat, lon, err := svc.Geocode(context.Background(), "Bangkok")
	if err != nil {
		t.Fatal(err)
	}
	if lat < 13.7 || lat > 13.8 || lon < 100.4 || lon > 100.6 {
		t.Fatalf("wrong coords: %f %f", lat, lon)
	}
}

func TestGeocodeNotFound(t *testing.T) {
	g := testServer(`[]`, 200)
	defer g.Close()
	svc := New(Config{NominatimBase: g.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, _, err := svc.Geocode(context.Background(), "Nowhere XYZ"); err == nil {
		t.Fatal("unknown place must fail honestly")
	}
}
