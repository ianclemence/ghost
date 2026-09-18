package sting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testContext() context.Context { return context.Background() }

func TestClientCompleteRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		var req CompleteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.Query == "" || len(req.Tools) != 1 {
			t.Errorf("unexpected request %+v", req)
		}
		conf := 0.9
		_ = json.NewEncoder(w).Encode(CompleteResponse{
			Type:       "call",
			Confidence: &conf,
			FunctionCalls: []FunctionCall{
				{Name: "web_search", Arguments: map[string]interface{}{"query": "x"}},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, 5, "")
	if !c.Available(testContext()) {
		t.Fatal("expected available")
	}
	resp, err := c.Complete(testContext(), "", "search for x", []ToolSchema{{Name: "web_search"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.FunctionCalls) != 1 {
		t.Fatalf("unexpected %+v", resp)
	}
}

func TestClientUnavailable(t *testing.T) {
	c := New("http://127.0.0.1:1", 1, "")
	if c.Available(testContext()) {
		t.Fatal("expected unavailable")
	}
	if _, err := c.Complete(testContext(), "", "hi", []ToolSchema{{Name: "a"}}); err == nil {
		t.Fatal("expected error")
	}
}
