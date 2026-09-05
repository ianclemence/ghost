package crypto

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

const geckoOK = `{"bitcoin":{"usd":67000.5,"usd_24h_change":1.5}}`
const coinbaseOK = `{"data":{"base":"BTC","currency":"USD","amount":"67010.25"}}`

func TestPrimarySuccess(t *testing.T) {
	g := testServer(geckoOK, 200)
	defer g.Close()
	c := testServer(coinbaseOK, 200)
	defer c.Close()
	svc := New(Config{GeckoBase: g.URL, CoinbaseBase: c.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	p, r := svc.GetPrice(context.Background(), "bitcoin", "usd")
	if r.Err != nil {
		t.Fatalf("failed: %v", r.Err)
	}
	if r.Provider != "coingecko" || p.Value != 67000.5 {
		t.Fatalf("wrong: %+v %+v", p, r)
	}
}

func TestFallbackSuccess(t *testing.T) {
	g := testServer(`x`, 500)
	defer g.Close()
	c := testServer(coinbaseOK, 200)
	defer c.Close()
	svc := New(Config{GeckoBase: g.URL, CoinbaseBase: c.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	p, r := svc.GetPrice(context.Background(), "bitcoin", "USD")
	if r.Err != nil || r.Provider != "coinbase" || p.Value != 67010.25 {
		t.Fatalf("fallback failed: %+v %+v", p, r)
	}
}

func TestUnknownCoinHonest(t *testing.T) {
	g := testServer(`{}`, 200)
	defer g.Close()
	svc := New(Config{GeckoBase: g.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.GetPrice(context.Background(), "notacoin", "USD"); r.Err == nil {
		t.Fatal("unknown coin must fail honestly")
	}
}

func TestNonNumericAmount(t *testing.T) {
	g := testServer(`x`, 500)
	defer g.Close()
	c := testServer(`{"data":{"base":"BTC","currency":"USD","amount":"banana"}}`, 200)
	defer c.Close()
	svc := New(Config{GeckoBase: g.URL, CoinbaseBase: c.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.GetPrice(context.Background(), "bitcoin", "USD"); r.Err == nil {
		t.Fatal("non-numeric price must fail")
	}
}

func TestAllFailHonest(t *testing.T) {
	g := testServer(`x`, 500)
	defer g.Close()
	c := testServer(`y`, 500)
	defer c.Close()
	svc := New(Config{GeckoBase: g.URL, CoinbaseBase: c.URL, CacheTTL: time.Minute, BreakerCooldown: time.Second})
	if _, r := svc.GetPrice(context.Background(), "bitcoin", "USD"); r.Err == nil {
		t.Fatal("all-fail must be honest")
	}
}
