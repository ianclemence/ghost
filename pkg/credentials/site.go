package credentials

import "strings"

// A saved sign-in belongs to a site, and Ghost types it only there. "Only
// filled on ads.google.com" has to hold when a prompt-injected model asks the
// browser to sign in on some other address, so the browser checks every
// address it is given against the site the login was saved for.

// multiLabelSuffixes are the public suffixes with more than one label that
// owners and sites are likely to sit under. It is deliberately a short list,
// not the full public-suffix database: a missing suffix makes two tenants of a
// shared platform (good.github.io, evil.github.io) look like one site, so the
// common hosting suffixes are listed too. A wrong answer here refuses a
// sign-in; the list errs on the side of fewer matches.
var multiLabelSuffixes = map[string]bool{
	"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "me.uk": true,
	"com.au": true, "net.au": true, "org.au": true, "edu.au": true,
	"co.nz": true, "org.nz": true, "co.za": true, "org.za": true,
	"co.ke": true, "or.ke": true, "go.ke": true, "ac.ke": true, "co.tz": true, "co.ug": true,
	"com.ng": true, "com.gh": true, "co.in": true, "net.in": true, "org.in": true,
	"com.br": true, "com.mx": true, "com.ar": true, "com.co": true, "com.pe": true, "com.tr": true,
	"co.jp": true, "ne.jp": true, "or.jp": true, "com.cn": true, "com.hk": true, "com.sg": true,
	"com.my": true, "co.th": true, "in.th": true, "or.th": true, "com.ph": true, "com.vn": true,
	"co.id": true, "or.id": true, "com.pk": true, "com.bd": true, "com.eg": true, "com.sa": true,
	// Shared hosting and publishing platforms. Without these, one tenant's
	// login could be typed on another tenant's site (good.github.io and
	// evil.github.io both reduce to github.io).
	"github.io": true, "gitlab.io": true, "pages.dev": true, "workers.dev": true,
	"netlify.app": true, "vercel.app": true, "web.app": true, "firebaseapp.com": true,
	"appspot.com": true, "azurewebsites.net": true, "herokuapp.com": true, "onrender.com": true,
	"fly.dev": true, "glitch.me": true, "repl.co": true, "surge.sh": true, "now.sh": true,
	"blogspot.com": true, "wordpress.com": true, "tumblr.com": true, "substack.com": true,
	"myshopify.com": true, "wixsite.com": true, "weebly.com": true, "notion.site": true,
	"trycloudflare.com": true, "ngrok.io": true, "ngrok-free.app": true,
}

// RegistrableDomain reduces a host to the part an owner registers: the label
// before its public suffix, and the suffix. "accounts.google.com" and
// "ads.google.com" both give "google.com"; "shop.example.co.uk" gives
// "example.co.uk".
func RegistrableDomain(host string) string {
	h := NormalizeWebHost(host)
	if h == "" {
		return ""
	}
	labels := strings.Split(h, ".")
	if len(labels) <= 2 {
		return h
	}
	last2 := strings.Join(labels[len(labels)-2:], ".")
	if multiLabelSuffixes[last2] && len(labels) >= 3 {
		return strings.Join(labels[len(labels)-3:], ".")
	}
	return last2
}

// SameSite reports whether two addresses are on the same registered site, so
// that a login saved for ads.google.com may be used on accounts.google.com
// (where Google's own sign-in form lives) and never on google.com.evil.example.
func SameSite(a, b string) bool {
	ra, rb := RegistrableDomain(a), RegistrableDomain(b)
	return ra != "" && ra == rb
}
