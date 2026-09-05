package currency

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

const erOK = `{"result":"success","rates":{"EUR":0.92,"THB":36.5},"time_last_update_utc":"Sat, 05 Sep 2026 00:00:00 +0000"}`
const fbOK = `{"rates":{"EUR":0.921},"date":"2026-09-05"}`

func TestPrimarySuccess(t *testing.T) {
	er := testServer(erOK, 200)
	defer er.Close()
	fb := testServer(fbOK, 200)
	defer fb.Close()
	svc := New(Config{ErAPIBase: er.URL, FrankfurterBase: fb.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	c, r := svc.Convert(context.Background(), "usd", "eur", 100)
	if r.Err != nil {
		t.Fatalf("failed: %v", r.Err)
	}
	if r.Provider != "er-api" || c.Rate != 0.92 || c.Converted != 92 {
		t.Fatalf("wrong: %+v %+v", c, r)
	}
}

func TestFallbackSuccess(t *testing.T) {
	er := testServer(`x`, 500)
	defer er.Close()
	fb := testServer(fbOK, 200)
	defer fb.Close()
	svc := New(Config{ErAPIBase: er.URL, FrankfurterBase: fb.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	c, r := svc.Convert(context.Background(), "USD", "EUR", 0)
	if r.Err != nil || r.Provider != "frankfurter" || c.Rate != 0.921 {
		t.Fatalf("fallback failed: %+v %+v", c, r)
	}
}

func TestMalformed(t *testing.T) {
	er := testServer(`{"result":"success","rates":{"EUR":"banana"}}`, 200)
	defer er.Close()
	fb := testServer(`[]`, 200)
	defer fb.Close()
	svc := New(Config{ErAPIBase: er.URL, FrankfurterBase: fb.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.Convert(context.Background(), "USD", "EUR", 0); r.Err == nil {
		t.Fatal("banana rate must fail")
	}
}

func TestMissingCurrency(t *testing.T) {
	er := testServer(`{"result":"success","rates":{}}`, 200)
	defer er.Close()
	fb := testServer(`{"rates":{}}`, 200)
	defer fb.Close()
	svc := New(Config{ErAPIBase: er.URL, FrankfurterBase: fb.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.Convert(context.Background(), "USD", "XXX", 0); r.Err == nil {
		t.Fatal("unknown currency must fail honestly")
	}
}

func TestAllFailHonest(t *testing.T) {
	er := testServer(`x`, 500)
	defer er.Close()
	fb := testServer(`y`, 500)
	defer fb.Close()
	svc := New(Config{ErAPIBase: er.URL, FrankfurterBase: fb.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.Convert(context.Background(), "USD", "EUR", 0); r.Err == nil {
		t.Fatal("all-fail must be honest")
	}
}
