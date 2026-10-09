package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A page watch is the owner asking Ghost to keep an eye on a web page: a
// price to drop (or fall under a number they named), something to come back
// in stock, an appointment slot or a ticket to appear, or the page simply to
// change. It follows the package's rules: it exists only because the owner
// asked by name with the link in their message, the rule is read from their
// words (never chosen by a model), the probe is one bounded HTTPS GET, and a
// change is a diff of observed values filtered by that rule.

// KindPage is a web page the owner asked Ghost to watch.
const KindPage Kind = "page"

// PageRule is what about a page the owner cares about.
type PageRule struct {
	// Want is one of: below (price at or under Threshold), drop (any price
	// fall), stock (back in stock), appears (Phrase shows up), disappears
	// (Phrase goes away), change (anything on the page changes).
	Want      string  `json:"want"`
	Threshold float64 `json:"threshold,omitempty"`
	Phrase    string  `json:"phrase,omitempty"`
}

var (
	urlRE       = regexp.MustCompile(`https?://[^\s<>"']+`)
	belowRE     = regexp.MustCompile(`(?i)\b(?:below|under|less\s+than|cheaper\s+than|drops?\s+(?:below|under|to)|falls?\s+(?:below|under|to)|at\s+most|for\s+(?:less|under))\s+(?:[A-Z]{3}\s*|[$€£¥₦₹]\s*)?([0-9][0-9,]*(?:\.[0-9]+)?)`)
	dropRE      = regexp.MustCompile(`(?i)\b(?:price\s+drop|price\s+(?:goes|comes)\s+down|gets?\s+cheaper|on\s+sale|drops?\s+in\s+price|price\s+falls?|discount)`)
	stockRE     = regexp.MustCompile(`(?i)\b(?:back\s+in\s+stock|in\s+stock|restock(?:ed|s)?|available\s+again|becomes?\s+available|comes?\s+back)\b`)
	appearsRE   = regexp.MustCompile(`(?i)\b(?:when|if)\s+(?:there(?:'s|\s+is|\s+are)\s+)?(?:a\s+|an\s+)?(?:new\s+)?(slot|slots|appointment|appointments|opening|openings|tickets?|seat|seats|date|dates|space|spaces|place|places)\b(?:\s+(?:is|are))?\s*(?:open(?:s)?|available|free|released|up)?`)
	quotedRE    = regexp.MustCompile(`["“]([^"”]{2,80})["”]`)
	disappearRE = regexp.MustCompile(`(?i)\b(?:no\s+longer\s+says|stops?\s+saying|disappears?|goes\s+away|is\s+(?:gone|removed))\b`)
)

// detectPage reads a page watch from an explicit request that carries a link.
func detectPage(text string) (Candidate, bool) {
	loc := urlRE.FindStringIndex(text)
	if loc == nil {
		return Candidate{}, false
	}
	raw := strings.TrimRight(text[loc[0]:loc[1]], ".,;:!?)]")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return Candidate{}, false
	}
	rule := PageRule{Want: "change"}
	rest := text[:loc[0]] + " " + text[loc[1]:]
	switch {
	case belowRE.MatchString(rest):
		m := belowRE.FindStringSubmatch(rest)
		if v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64); err == nil && v > 0 {
			rule = PageRule{Want: "below", Threshold: v}
		}
	case stockRE.MatchString(rest):
		rule = PageRule{Want: "stock"}
	case dropRE.MatchString(rest):
		rule = PageRule{Want: "drop"}
	case quotedRE.MatchString(rest):
		phrase := quotedRE.FindStringSubmatch(rest)[1]
		rule = PageRule{Want: "appears", Phrase: phrase}
		if disappearRE.MatchString(rest) {
			rule.Want = "disappears"
		}
	case appearsRE.MatchString(rest):
		// "tell me when a slot opens": the page changing is the signal; the
		// notice names what the owner was waiting for.
		rule = PageRule{Want: "change", Phrase: strings.ToLower(appearsRE.FindStringSubmatch(rest)[1])}
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	label := host
	if p := strings.Trim(u.Path, "/"); p != "" {
		seg := strings.Split(p, "/")
		last := strings.ReplaceAll(seg[len(seg)-1], "-", " ")
		if len(last) > 2 && len(last) < 60 && !strings.ContainsAny(last, "=?&") {
			label = host + " · " + last
		}
	}
	c := Candidate{
		Kind: KindPage, Entity: u.String(), Label: label,
		Quote: sentenceAround(text, loc[0]), Confidence: 0.95, Origin: "explicit", Explicit: true,
	}
	c.Rule = &rule
	c.URL = u.String()
	return c, true
}

// PageTimeout bounds one page fetch.
const PageTimeout = 15 * time.Second

// maxPageBytes bounds what is read of a page.
const maxPageBytes = 3 << 20

// PageProbe fetches a page and reads what its rule needs. check is the
// network safety gate (no private addresses, no credentials in the URL); a
// URL it refuses is never fetched.
type PageProbe struct {
	client *http.Client
	check  func(rawURL string) error
}

// NewPageProbe builds the page probe with a safety check for every URL
// (including each redirect).
func NewPageProbe(check func(rawURL string) error) *PageProbe {
	p := &PageProbe{check: check}
	p.client = &http.Client{
		Timeout: PageTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if p.check != nil {
				return p.check(req.URL.String())
			}
			return nil
		},
	}
	return p
}

func (p *PageProbe) Name() string    { return "page" }
func (p *PageProbe) Available() bool { return true }

// Fetch reads the page once and returns the observed values for its rule.
func (p *PageProbe) Fetch(ctx context.Context, w Watch) (map[string]string, string, error) {
	target := w.URL
	if target == "" {
		target = w.Entity
	}
	if p.check != nil {
		if err := p.check(target); err != nil {
			return nil, "", failf(FailInvalid, "that address can't be watched: %v", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, "", failf(FailInvalid, "%v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux aarch64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126 Safari/537.36 Ghost")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", failf(FailTransient, "%v", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		return nil, "", failf(FailNotFound, "the page is gone (%d)", resp.StatusCode)
	case resp.StatusCode == 429 || resp.StatusCode >= 500:
		return nil, "", failf(FailTransient, "the site answered %d", resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, "", failf(FailInvalid, "the site refused (%d)", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return nil, "", failf(FailTransient, "%v", err)
	}
	return ReadPage(string(body), w.Rule)
}

// ReadPage extracts what a rule needs from a page's HTML. It is the whole of
// what a page watch observes, so it is pure and testable.
func ReadPage(doc string, rule *PageRule) (map[string]string, string, error) {
	r := PageRule{Want: "change"}
	if rule != nil {
		r = *rule
	}
	text := visibleText(doc)
	state := map[string]string{}
	excerpt := ""
	switch r.Want {
	case "below", "drop":
		price, cur, ok := findPrice(doc, text)
		if !ok {
			return nil, "", failf(FailInvalid, "no price could be read on the page")
		}
		state["price"] = strconv.FormatFloat(price, 'f', -1, 64)
		state["_currency"] = cur
		excerpt = "price " + strings.TrimSpace(cur+" "+state["price"])
	case "stock":
		s, ok := findStock(doc, text)
		if !ok {
			return nil, "", failf(FailInvalid, "the page doesn't say whether it is in stock")
		}
		state["stock"] = s
		excerpt = s
	case "appears", "disappears":
		if strings.Contains(strings.ToLower(text), strings.ToLower(r.Phrase)) {
			state["phrase"] = "present"
		} else {
			state["phrase"] = "absent"
		}
		excerpt = fmt.Sprintf("%q is %s", r.Phrase, state["phrase"])
	default:
		sum := sha256.Sum256([]byte(normalizeText(text)))
		state["content"] = hex.EncodeToString(sum[:8])
		excerpt = truncate(normalizeText(text), 160)
	}
	if t := pageTitle(doc); t != "" {
		state["_title"] = t
	}
	return state, excerpt, nil
}

// Relevant keeps the changes that mean what the owner asked for: a price at
// or under their number, a lower price, back in stock, the phrase arriving or
// going. Other kinds of watch keep every change.
func Relevant(w Watch, changes []Change) []Change {
	if w.Kind != KindPage || w.Rule == nil {
		return changes
	}
	var out []Change
	for _, c := range changes {
		keep := false
		switch w.Rule.Want {
		case "below":
			v, err := strconv.ParseFloat(c.To, 64)
			keep = c.Field == "price" && err == nil && v <= w.Rule.Threshold
		case "drop":
			from, e1 := strconv.ParseFloat(c.From, 64)
			to, e2 := strconv.ParseFloat(c.To, 64)
			keep = c.Field == "price" && e1 == nil && e2 == nil && to < from
		case "stock":
			keep = c.Field == "stock" && c.To == "in stock"
		case "appears":
			keep = c.Field == "phrase" && c.To == "present"
		case "disappears":
			keep = c.Field == "phrase" && c.To == "absent"
		default:
			keep = c.Field == "content"
		}
		if keep {
			out = append(out, c)
		}
	}
	return out
}

// AlreadyMet is the change to announce when a page already meets its rule
// the first time it is read (there is no earlier value to compare with).
func AlreadyMet(w Watch) []Change {
	if w.Kind != KindPage || w.Rule == nil {
		return nil
	}
	field := map[string]string{"below": "price", "stock": "stock", "appears": "phrase"}[w.Rule.Want]
	if field == "" {
		return nil
	}
	return []Change{{Field: field, From: "", To: w.Current[field]}}
}

// RenderPageNotice is the sentence for a page watch whose rule was met.
func RenderPageNotice(w Watch, changes []Change) string {
	subject := SubjectPhrase(w)
	if t := strings.TrimSpace(w.Current["_title"]); t != "" {
		subject = truncate(t, 70)
	}
	cur := strings.TrimSpace(w.Current["_currency"])
	money := func(v string) string { return strings.TrimSpace(cur + " " + v) }
	c := changes[0]
	switch w.Rule.Want {
	case "below":
		want := money(strconv.FormatFloat(w.Rule.Threshold, 'f', -1, 64))
		if c.From == "" {
			return fmt.Sprintf("%s is already %s, at or under the %s you wanted. %s", subject, money(c.To), want, w.URL)
		}
		return fmt.Sprintf("%s is now %s (it was %s), at or under the %s you wanted. %s", subject, money(c.To), money(c.From), want, w.URL)
	case "drop":
		return fmt.Sprintf("The price of %s dropped to %s (from %s). %s", subject, money(c.To), money(c.From), w.URL)
	case "stock":
		if c.From == "" {
			return fmt.Sprintf("%s is in stock right now. %s", subject, w.URL)
		}
		return fmt.Sprintf("%s is back in stock. %s", subject, w.URL)
	case "appears":
		return fmt.Sprintf("%q now appears on %s. %s", w.Rule.Phrase, subject, w.URL)
	case "disappears":
		return fmt.Sprintf("%q is gone from %s. %s", w.Rule.Phrase, subject, w.URL)
	}
	what := "changed"
	if w.Rule.Phrase != "" {
		what = fmt.Sprintf("changed: there may be a %s", strings.TrimSuffix(w.Rule.Phrase, "s"))
	}
	return fmt.Sprintf("%s %s. %s", subject, what, w.URL)
}

var (
	scriptRE  = regexp.MustCompile(`(?is)<(script|style|noscript|svg|template)[^>]*>.*?</(?:script|style|noscript|svg|template)>`)
	tagRE     = regexp.MustCompile(`(?s)<[^>]+>`)
	titleRE   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	ogTitleRE = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)["']`)
	ldRE      = regexp.MustCompile(`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	metaPrice = regexp.MustCompile(`(?i)<meta[^>]+(?:property|itemprop|name)=["'](?:og:price:amount|product:price:amount|price)["'][^>]+content=["']([0-9][0-9.,]*)["']`)
	metaCur   = regexp.MustCompile(`(?i)<meta[^>]+(?:property|itemprop|name)=["'](?:og:price:currency|product:price:currency|pricecurrency)["'][^>]+content=["']([A-Za-z]{3})["']`)
	textPrice = regexp.MustCompile(`(?:(KSh|KES|USD|EUR|GBP|US\$|\$|€|£|₦|₹)\s?([0-9]{1,3}(?:[,\s][0-9]{3})*(?:\.[0-9]{1,2})?|[0-9]+(?:\.[0-9]{1,2})?))`)
	spaceRE   = regexp.MustCompile(`\s+`)
)

func visibleText(doc string) string {
	t := scriptRE.ReplaceAllString(doc, " ")
	t = tagRE.ReplaceAllString(t, " ")
	return html.UnescapeString(spaceRE.ReplaceAllString(t, " "))
}

// normalizeText drops what changes on every load (times, counters) so a page
// "changes" only when its words do.
var volatileRE = regexp.MustCompile(`\b\d{1,2}:\d{2}(?::\d{2})?\b|\b\d+\s*(?:seconds?|minutes?|mins?|hours?|hrs?)\s+ago\b|\b(?:viewed|watching|people)\s+\d+\b`)

func normalizeText(t string) string {
	return strings.TrimSpace(spaceRE.ReplaceAllString(volatileRE.ReplaceAllString(strings.ToLower(t), " "), " "))
}

func pageTitle(doc string) string {
	if m := ogTitleRE.FindStringSubmatch(doc); m != nil {
		return strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if m := titleRE.FindStringSubmatch(doc); m != nil {
		return strings.TrimSpace(html.UnescapeString(spaceRE.ReplaceAllString(m[1], " ")))
	}
	return ""
}

// findPrice reads the product's price: structured data first (what the shop
// declares), then the shop's meta tags, then the first price written on the
// page.
func findPrice(doc, text string) (float64, string, bool) {
	for _, m := range ldRE.FindAllStringSubmatch(doc, -1) {
		if p, c, ok := ldPrice(m[1]); ok {
			return p, c, true
		}
	}
	if m := metaPrice.FindStringSubmatch(doc); m != nil {
		if v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64); err == nil && v > 0 {
			cur := ""
			if c := metaCur.FindStringSubmatch(doc); c != nil {
				cur = strings.ToUpper(c[1])
			}
			return v, cur, true
		}
	}
	if m := textPrice.FindStringSubmatch(text); m != nil {
		num := strings.NewReplacer(",", "", " ", "").Replace(m[2])
		if v, err := strconv.ParseFloat(num, 64); err == nil && v > 0 {
			return v, currencyCode(m[1]), true
		}
	}
	return 0, "", false
}

func currencyCode(sym string) string {
	switch sym {
	case "KSh", "KES":
		return "KES"
	case "$", "US$", "USD":
		return "USD"
	case "€", "EUR":
		return "EUR"
	case "£", "GBP":
		return "GBP"
	case "₦":
		return "NGN"
	case "₹":
		return "INR"
	}
	return sym
}

// ldPrice finds an offer's price in JSON-LD (a Product, a list of them, or a
// graph).
func ldPrice(raw string) (float64, string, bool) {
	var v interface{}
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &v) != nil {
		return 0, "", false
	}
	var walk func(x interface{}, depth int) (float64, string, bool)
	walk = func(x interface{}, depth int) (float64, string, bool) {
		if depth > 6 {
			return 0, "", false
		}
		switch t := x.(type) {
		case []interface{}:
			for _, e := range t {
				if p, c, ok := walk(e, depth+1); ok {
					return p, c, true
				}
			}
		case map[string]interface{}:
			for _, k := range []string{"price", "lowPrice"} {
				if pv, ok := t[k]; ok {
					var f float64
					switch n := pv.(type) {
					case float64:
						f = n
					case string:
						f, _ = strconv.ParseFloat(strings.ReplaceAll(n, ",", ""), 64)
					}
					if f > 0 {
						cur, _ := t["priceCurrency"].(string)
						return f, strings.ToUpper(cur), true
					}
				}
			}
			for _, k := range []string{"offers", "@graph", "mainEntity", "itemListElement"} {
				if sub, ok := t[k]; ok {
					if p, c, ok := walk(sub, depth+1); ok {
						return p, c, true
					}
				}
			}
		}
		return 0, "", false
	}
	return walk(v, 0)
}

var (
	ldStockRE = regexp.MustCompile(`(?i)"availability"\s*:\s*"(?:https?://schema\.org/)?(InStock|OutOfStock|SoldOut|PreOrder|BackOrder|LimitedAvailability|Discontinued)"`)
	outTextRE = regexp.MustCompile(`(?i)\b(?:out\s+of\s+stock|sold\s+out|currently\s+unavailable|not\s+available|notify\s+me\s+when\s+available)\b`)
	inTextRE  = regexp.MustCompile(`(?i)\b(?:in\s+stock|add\s+to\s+(?:cart|basket|bag)|buy\s+now)\b`)
)

// findStock reads whether the page says the thing can be bought now.
func findStock(doc, text string) (string, bool) {
	if m := ldStockRE.FindStringSubmatch(doc); m != nil {
		switch strings.ToLower(m[1]) {
		case "instock", "limitedavailability", "preorder", "backorder":
			return "in stock", true
		default:
			return "out of stock", true
		}
	}
	if outTextRE.MatchString(text) {
		return "out of stock", true
	}
	if inTextRE.MatchString(text) {
		return "in stock", true
	}
	return "", false
}
