package hass

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer(fn http.HandlerFunc) *httptest.Server { return httptest.NewServer(fn) }

const statesOK = `[{"entity_id":"light.bedroom","state":"off","attributes":{"friendly_name":"Bedroom"}},{"entity_id":"sensor.temp","state":"21.5","attributes":{}}]`

func TestReachable(t *testing.T) {
	s := testServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/" {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer")
		}
		w.WriteHeader(200)
	})
	defer s.Close()
	svc := New(Config{Base: s.URL, Token: "tok", BreakerCooldown: time.Second})
	if fc, err := svc.Reachable(context.Background()); err != nil {
		t.Fatalf("reachable failed: %v %s", err, fc)
	}
}

func TestReachableAuthFailure(t *testing.T) {
	s := testServer(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	defer s.Close()
	svc := New(Config{Base: s.URL, Token: "bad", BreakerCooldown: time.Second})
	if fc, err := svc.Reachable(context.Background()); err == nil {
		t.Fatal("401 must fail")
	} else if fc != "authentication_failure" {
		t.Fatalf("wrong class: %s", fc)
	}
}

func TestStates(t *testing.T) {
	s := testServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, statesOK)
	})
	defer s.Close()
	svc := New(Config{Base: s.URL, Token: "tok", BreakerCooldown: time.Second})
	ents, r := svc.States(context.Background())
	if r.Err != nil || len(ents) != 2 || ents[0].Name != "Bedroom" {
		t.Fatalf("states wrong: %+v %+v", ents, r)
	}
}

func TestStatesMalformed(t *testing.T) {
	s := testServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `[{"entity_id":123}]`)
	})
	defer s.Close()
	svc := New(Config{Base: s.URL, Token: "tok", BreakerCooldown: time.Second})
	if _, r := svc.States(context.Background()); r.Err == nil {
		t.Fatal("malformed must fail")
	}
}

func TestActuate(t *testing.T) {
	var gotPath, gotBody string
	s := testServer(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(200)
		fmt.Fprint(w, `[]`)
	})
	defer s.Close()
	svc := New(Config{Base: s.URL, Token: "tok", BreakerCooldown: time.Second})
	r := svc.Actuate(context.Background(), "light", "turn_off", "light.bedroom")
	if r.Err != nil || !r.Value {
		t.Fatalf("actuate failed: %+v", r)
	}
	if gotPath != "/api/services/light/turn_off" {
		t.Fatalf("wrong path: %s", gotPath)
	}
	if !strings.Contains(gotBody, "light.bedroom") {
		t.Fatalf("wrong body: %s", gotBody)
	}
}

func TestActuateRejectsInjection(t *testing.T) {
	svc := New(Config{Base: "http://x", Token: "t"})
	for _, c := range [][3]string{
		{"light;rm", "turn_on", "light.a"},
		{"light", "turn_on", "../../etc"},
		{"", "turn_on", "light.a"},
		{"light", "turn_on", "no-dot-here"},
	} {
		if r := svc.Actuate(context.Background(), c[0], c[1], c[2]); r.Err == nil {
			t.Fatalf("injection must fail: %v", c)
		}
	}
}

func TestUnconfiguredHonest(t *testing.T) {
	svc := New(Config{})
	if _, err := svc.Reachable(context.Background()); err == nil {
		t.Fatal("unconfigured must report")
	}
}
