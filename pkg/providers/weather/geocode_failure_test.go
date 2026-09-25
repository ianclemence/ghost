package weather

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
)

// Every geocode failure cause must map to its honest class. Regression:
// any lookup failure was blanket-classified FailInvalid, so a dropped
// connection surfaced to the owner as "I got an unexpected response and
// didn't want to guess" (observed live twice in one minute).
func TestGeocodeFailureClassification(t *testing.T) {
	var malformed map[string]interface{}
	jsonErr := json.Unmarshal([]byte("{"), &malformed)

	cases := []struct {
		name string
		err  error
		want provider.FailureClass
	}{
		{"rate limited", &httpError{Status: 429}, provider.FailRateLimited},
		{"server error", &httpError{Status: 500}, provider.FailServer},
		{"unavailable", &httpError{Status: 503}, provider.FailUnavailable},
		{"bad request", &httpError{Status: 404}, provider.FailInvalid},
		{"no results", provider.Empty("no geocode results"), provider.FailEmpty},
		{"malformed payload", jsonErr, provider.FailMalformed},
		{"timeout", &url.Error{Op: "Get", URL: "x", Err: context.DeadlineExceeded}, provider.FailTimeout},
		{"connection refused", &url.Error{Op: "Get", URL: "x", Err: errors.New("connection refused")}, provider.FailNetwork},
	}
	for _, c := range cases {
		got := geocodeFailure(c.err)
		if got != c.want {
			t.Errorf("%s: geocodeFailure = %q, want %q", c.name, got, c.want)
		}
	}
}

// End-to-end: an unreachable geocoder must surface as a network-class
// failure (product text: "The network request failed. I'll try again
// shortly."), never as FailInvalid ("unexpected response").
func TestCurrentByPlaceUnreachableGeocoderIsNetworkFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // closed listener: connection refused

	svc := New(Config{
		GeocodeBase: srv.URL,
		HTTPClient:  &http.Client{Timeout: 2 * time.Second},
	})
	_, res := svc.CurrentByPlace(context.Background(), "Bangkok", false)
	if res.Err == nil {
		t.Fatal("expected a geocode failure")
	}
	if res.Failure == provider.FailInvalid {
		t.Fatalf("unreachable geocoder misclassified as FailInvalid: %v", res.Err)
	}
	if res.Failure != provider.FailNetwork && res.Failure != provider.FailTimeout {
		t.Fatalf("Failure = %q, want network/timeout (err: %v)", res.Failure, res.Err)
	}
}
