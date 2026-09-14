package browser

// Checkout detection: before any transactional submit, Ghost must know
// whether the page is a checkout and what it would charge. Detection is
// heuristic and conservative: checkout-like signals require a declared
// merchant + amount match from the caller (the approval), never model
// assertion. Amounts are extracted, never trusted as correct — the human
// approval carries the authoritative figure.

import (
	"net/url"
	"regexp"
	"strings"
)

var checkoutKeywords = []string{
	"checkout", "place order", "place your order", "submit order",
	"complete purchase", "complete order", "pay now", "pay $", "buy now",
	"confirm purchase", "confirm order", "proceed to payment",
	"payment method", "billing address", "shipping address",
	"order summary", "order total", "cart total", "grand total",
	"add to cart", "shopping cart", "your cart",
}

var amountREs = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:total|amount|balance|due)[^\d$€£]{0,20}([$€£])\s*([\d,]+\.\d{2})`),
	regexp.MustCompile(`([$€£])\s*([\d,]+\.\d{2})`),
	regexp.MustCompile(`(?i)\bUSD\s*([\d,]+\.\d{2})`),
}

// Quote is what the page appears to offer to charge.
type Quote struct {
	// IsCheckout reports checkout-like signals on the page.
	IsCheckout bool
	// Total is the largest extracted amount (heuristic, not authoritative).
	Total string
	// Currency is $, €, £, or USD when detected.
	Currency string
	// Merchant is the page host.
	Merchant string
}

// DetectCheckout scans page text for checkout signals and amounts.
func DetectCheckout(pageURL, text string) Quote {
	q := Quote{}
	if u, err := url.Parse(pageURL); err == nil && u.Host != "" {
		q.Merchant = u.Host
	}
	lower := strings.ToLower(text)
	hits := 0
	for _, k := range checkoutKeywords {
		if strings.Contains(lower, k) {
			hits++
		}
	}
	if hits == 0 {
		return q
	}
	q.IsCheckout = true
	best := ""
	for _, re := range amountREs {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			var cur, sym string
			if len(m) == 3 {
				sym, cur = m[1], m[2]
			} else if len(m) == 2 {
				cur = m[1]
			}
			if compareAmount(cur, best) > 0 {
				best, q.Currency = cur, sym
			}
		}
	}
	q.Total = best
	return q
}

// compareAmount compares "1,234.56"-shaped amounts numerically.
func compareAmount(a, b string) int {
	fa := parseAmount(a)
	fb := parseAmount(b)
	switch {
	case fa > fb:
		return 1
	case fa < fb:
		return -1
	default:
		return 0
	}
}

func parseAmount(s string) float64 {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	var v float64
	for _, r := range s {
		if r < '0' || r > '9' {
			if r == '.' {
				break
			}
			continue
		}
		v = v*10 + float64(r-'0')
	}
	frac := 0.0
	if i := strings.Index(s, "."); i >= 0 {
		mult := 0.1
		for _, r := range s[i+1:] {
			if r < '0' || r > '9' {
				break
			}
			frac += float64(r-'0') * mult
			mult /= 10
		}
	}
	v += frac
	if neg {
		v = -v
	}
	return v
}

// ConfirmationKeywords reports whether post-submit text looks like an order
// receipt. Advisory only: receipt evidence records the observation, and the
// broker approval (not the model) decides success.
func ConfirmationKeywords(text string) bool {
	lower := strings.ToLower(text)
	for _, k := range []string{
		"order confirmation", "order confirmed", "thank you for your order",
		"order number", "order #", "receipt", "payment successful",
		"purchase complete",
	} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}
