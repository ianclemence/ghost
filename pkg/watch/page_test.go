package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPageWatchesAreReadFromTheOwnersWords(t *testing.T) {
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		msg   string
		want  string
		thr   float64
		label string
	}{
		{"Watch this and tell me when it drops below KES 25,000 https://www.jumia.co.ke/airpods-pro-2.html", "below", 25000, "jumia.co.ke · airpods pro 2.html"},
		{"keep an eye on https://shop.example.com/p/kettle for a price drop", "drop", 0, "shop.example.com · kettle"},
		{"Let me know when https://store.example.com/ps5 is back in stock!", "stock", 0, ""},
		{"track https://clinic.example.com/book and tell me when a slot opens", "change", 0, ""},
		{"monitor https://example.com/status and tell me when it no longer says \"Sold out\"", "disappears", 0, ""},
		{"watch https://example.com/news", "change", 0, "example.com · news"},
	}
	for _, c := range cases {
		got := Detect(c.msg, now, "UTC")
		if len(got) != 1 || got[0].Kind != KindPage || got[0].Rule == nil {
			t.Fatalf("%q: %+v", c.msg, got)
		}
		r := got[0].Rule
		if r.Want != c.want || r.Threshold != c.thr {
			t.Fatalf("%q: rule %+v", c.msg, r)
		}
		if c.label != "" && got[0].Label != c.label {
			t.Fatalf("%q: label %q", c.msg, got[0].Label)
		}
		w := got[0].ToWatch("main", "m1", now)
		if w.URL == "" || w.ExpiresAt == nil || !w.ExpiresAt.After(now) {
			t.Fatalf("%q: watch %+v", c.msg, w)
		}
	}
	// No explicit ask, or someone else's page: nothing.
	for _, msg := range []string{"I saw this https://example.com/x", "watch my sister's page https://example.com/her"} {
		if got := Detect(msg, now, "UTC"); len(got) != 0 {
			t.Fatalf("%q made a watch: %+v", msg, got)
		}
	}
}

func TestReadPagePricesStockAndPhrases(t *testing.T) {
	ld := `<html><head><title>Kettle</title><script type="application/ld+json">{"@type":"Product","name":"Kettle","offers":{"@type":"Offer","price":"2499.00","priceCurrency":"KES","availability":"https://schema.org/InStock"}}</script></head><body>Was KSh 3,000</body></html>`
	st, _, err := ReadPage(ld, &PageRule{Want: "below", Threshold: 2500})
	if err != nil || st["price"] != "2499" || st["_currency"] != "KES" || st["_title"] != "Kettle" {
		t.Fatalf("json-ld: %+v %v", st, err)
	}
	meta := `<meta property="og:price:amount" content="1,099.50"><meta property="og:price:currency" content="usd"><body>$5</body>`
	if st, _, _ := ReadPage(meta, &PageRule{Want: "drop"}); st["price"] != "1099.5" || st["_currency"] != "USD" {
		t.Fatalf("meta: %+v", st)
	}
	text := `<body><script>var x = "$1";</script><h1>Lamp</h1><p>Now only KSh 1,250</p></body>`
	if st, _, _ := ReadPage(text, &PageRule{Want: "drop"}); st["price"] != "1250" || st["_currency"] != "KES" {
		t.Fatalf("text price (scripts ignored): %+v", st)
	}
	if _, _, err := ReadPage(`<p>no prices here</p>`, &PageRule{Want: "below", Threshold: 5}); err == nil {
		t.Fatal("a page with no price was read as having one")
	}
	if st, _, _ := ReadPage(`<button>Add to cart</button>`, &PageRule{Want: "stock"}); st["stock"] != "in stock" {
		t.Fatalf("stock: %+v", st)
	}
	if st, _, _ := ReadPage(`<p>Sorry, this item is SOLD OUT</p><button>Add to cart</button>`, &PageRule{Want: "stock"}); st["stock"] != "out of stock" {
		t.Fatalf("sold out wins over the button: %+v", st)
	}
	if st, _, _ := ReadPage(`<p>Status: sold out</p>`, &PageRule{Want: "disappears", Phrase: "Sold out"}); st["phrase"] != "present" {
		t.Fatalf("phrase: %+v", st)
	}
	a, _, _ := ReadPage(`<p>Updated 10:42, 3 minutes ago. Slots: none</p>`, nil)
	b, _, _ := ReadPage(`<p>Updated 11:05, 7 minutes ago. Slots: none</p>`, nil)
	c, _, _ := ReadPage(`<p>Updated 11:05, 7 minutes ago. Slots: Tue 14:00</p>`, nil)
	if a["content"] != b["content"] || a["content"] == c["content"] {
		t.Fatalf("a page changes when its words do, not its clock: %v %v %v", a, b, c)
	}
}

func TestOnlyWhatTheOwnerAskedForIsAnnounced(t *testing.T) {
	below := Watch{Kind: KindPage, URL: "https://x", Rule: &PageRule{Want: "below", Threshold: 300}, Current: map[string]string{"price": "290", "_currency": "USD"}}
	if got := Relevant(below, []Change{{Field: "price", From: "320", To: "310"}}); len(got) != 0 {
		t.Fatalf("310 is not under 300: %+v", got)
	}
	got := Relevant(below, []Change{{Field: "price", From: "320", To: "290"}, {Field: "_title", From: "a", To: "b"}})
	if len(got) != 1 {
		t.Fatalf("290 is: %+v", got)
	}
	if msg := RenderNotice(below, got); !strings.Contains(msg, "is now USD 290 (it was USD 320), at or under the USD 300 you wanted") {
		t.Fatalf("notice: %s", msg)
	}
	if first := AlreadyMet(below); len(Relevant(below, first)) != 1 || !strings.Contains(RenderNotice(below, first), "is already USD 290") {
		t.Fatalf("already under on the first look: %+v %s", first, RenderNotice(below, first))
	}
	stock := Watch{Kind: KindPage, Rule: &PageRule{Want: "stock"}}
	if len(Relevant(stock, []Change{{Field: "stock", From: "in stock", To: "out of stock"}})) != 0 {
		t.Fatal("going out of stock is not what they asked for")
	}
	drop := Watch{Kind: KindPage, Rule: &PageRule{Want: "drop"}}
	if len(Relevant(drop, []Change{{Field: "price", From: "10", To: "12"}})) != 0 || len(Relevant(drop, []Change{{Field: "price", From: "12", To: "10"}})) != 1 {
		t.Fatal("drop means down")
	}
	flight := Watch{Kind: KindFlight}
	if len(Relevant(flight, []Change{{Field: "gate", From: "A", To: "B"}})) != 1 {
		t.Fatal("other watches keep every change")
	}
}

func TestPageProbeFetchesThroughItsGate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<title>Kettle</title><meta itemprop="price" content="49.99"><p>In stock</p>`))
	}))
	defer srv.Close()
	p := NewPageProbe(nil)
	st, excerpt, err := p.Fetch(context.Background(), Watch{Kind: KindPage, URL: srv.URL + "/k", Rule: &PageRule{Want: "drop"}})
	if err != nil || st["price"] != "49.99" || !strings.Contains(excerpt, "49.99") {
		t.Fatalf("fetch: %+v %q %v", st, excerpt, err)
	}
	if _, _, err := p.Fetch(context.Background(), Watch{Kind: KindPage, URL: srv.URL + "/gone"}); ClassOf(err) != FailNotFound {
		t.Fatalf("a missing page is not found, not retried forever: %v", err)
	}
	gated := NewPageProbe(func(string) error { return context.Canceled })
	if _, _, err := gated.Fetch(context.Background(), Watch{Kind: KindPage, URL: srv.URL}); err == nil {
		t.Fatal("the gate was not consulted")
	}
}
