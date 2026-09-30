package connectedapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Connecting an app must not claim "connected" until something has checked.
// A pasted key is tried against the service it belongs to. A refusal is
// reported in plain words and nothing is saved; a service that cannot be
// reached is saved with an honest "couldn't check it"; anything that passes is
// "connected". Before this, a made-up key and a Home Assistant "address" of
// "not a url" were both reported as connected.

type Verdict int

const (
	Verified    Verdict = iota // the service accepted it
	Rejected                   // the service refused it
	Unreachable                // could not be checked right now
)

type CheckResult struct {
	Verdict Verdict
	Message string // owner-facing when not verified
}

// CheckBase lets tests point checks at a local server.
var CheckBase = map[string]string{}

var checkClient = &http.Client{Timeout: 8 * time.Second}

func checkBase(id, def string) string {
	if b := CheckBase[id]; b != "" {
		return b
	}
	return def
}

func rejected(service string) CheckResult {
	return CheckResult{Rejected, service + " didn't accept that. Check it was copied completely and try again."}
}

func unreachable(service string) CheckResult {
	return CheckResult{Unreachable, "Saved, but I couldn't reach " + service + " to check it just now. Ghost will tell you if it doesn't work."}
}

func doCheck(ctx context.Context, req *http.Request) (int, []byte, error) {
	resp, err := checkClient.Do(req.WithContext(ctx))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, body, nil
}

// Check tries a pasted secret against its service. extra carries the
// second half of a pair (Home Assistant's address).
func Check(ctx context.Context, id, value, extra string) CheckResult {
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	switch id {
	case "github":
		req, _ := http.NewRequest(http.MethodGet, checkBase(id, "https://api.github.com")+"/user", nil)
		req.Header.Set("Authorization", "Bearer "+value)
		req.Header.Set("User-Agent", "ghost")
		return verdictFromStatus("GitHub", ctx, req)
	case "notion":
		req, _ := http.NewRequest(http.MethodGet, checkBase(id, "https://api.notion.com")+"/v1/users/me", nil)
		req.Header.Set("Authorization", "Bearer "+value)
		req.Header.Set("Notion-Version", "2022-06-28")
		return verdictFromStatus("Notion", ctx, req)
	case "openweather":
		q := url.Values{"q": {"London"}, "appid": {value}}
		req, _ := http.NewRequest(http.MethodGet, checkBase(id, "https://api.openweathermap.org")+"/data/2.5/weather?"+q.Encode(), nil)
		return verdictFromStatus("OpenWeather", ctx, req)
	case "aviationstack":
		q := url.Values{"access_key": {value}, "limit": {"1"}}
		req, _ := http.NewRequest(http.MethodGet, checkBase(id, "http://api.aviationstack.com")+"/v1/flights?"+q.Encode(), nil)
		code, body, err := doCheck(ctx, req)
		if err != nil {
			return unreachable("AviationStack")
		}
		// AviationStack answers 200 with an error object for a bad key.
		var probe struct {
			Error *struct{ Code string } `json:"error"`
		}
		if json.Unmarshal(body, &probe) == nil && probe.Error != nil {
			if strings.Contains(strings.ToLower(probe.Error.Code), "key") || strings.Contains(strings.ToLower(probe.Error.Code), "access") {
				return rejected("AviationStack")
			}
		}
		if code == 401 || code == 403 {
			return rejected("AviationStack")
		}
		if code >= 500 {
			return unreachable("AviationStack")
		}
		return CheckResult{Verdict: Verified}
	case "home-assistant", "homeassistant":
		return checkHomeAssistant(ctx, value, extra)
	}
	// No way to check this one (a key for a service Ghost has no test call
	// for): saved as given, and said so.
	return CheckResult{Unreachable, "Saved. Ghost can't test this one until it's used."}
}

func verdictFromStatus(service string, ctx context.Context, req *http.Request) CheckResult {
	code, _, err := doCheck(ctx, req)
	switch {
	case err != nil:
		return unreachable(service)
	case code == 401 || code == 403:
		return rejected(service)
	case code >= 200 && code < 300:
		return CheckResult{Verdict: Verified}
	case code >= 500 || code == 429:
		return unreachable(service)
	default:
		return rejected(service)
	}
}

// SplitHomeAssistant accepts the address and the token in either order.
func SplitHomeAssistant(a, b string) (addr, token string, ok bool) {
	isURL := func(s string) bool {
		u, err := url.Parse(strings.TrimSpace(s))
		return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
	}
	switch {
	case isURL(b) && !isURL(a):
		return strings.TrimRight(strings.TrimSpace(b), "/"), strings.TrimSpace(a), true
	case isURL(a) && !isURL(b):
		return strings.TrimRight(strings.TrimSpace(a), "/"), strings.TrimSpace(b), true
	}
	return "", "", false
}

func checkHomeAssistant(ctx context.Context, value, extra string) CheckResult {
	addr, token, ok := SplitHomeAssistant(value, extra)
	if !ok {
		return CheckResult{Rejected, "Home Assistant needs its address (like http://homeassistant.local:8123) and a long-lived token. One of those doesn't look right."}
	}
	req, _ := http.NewRequest(http.MethodGet, addr+"/api/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	code, _, err := doCheck(ctx, req)
	switch {
	case err != nil:
		return CheckResult{Unreachable, fmt.Sprintf("Saved, but I couldn't reach Home Assistant at %s from the Pod just now.", addr)}
	case code == 401 || code == 403:
		return rejected("Home Assistant")
	case code >= 200 && code < 300:
		return CheckResult{Verdict: Verified}
	default:
		return CheckResult{Unreachable, fmt.Sprintf("Saved, but Home Assistant answered with an unexpected status (%d).", code)}
	}
}
