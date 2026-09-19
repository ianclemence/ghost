package connector

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIExecutorGet(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ex := &OpenAPIExecutor{
		BaseURL: srv.URL,
		Headers: func() (map[string]string, error) {
			return map[string]string{"Authorization": "Bearer x"}, nil
		},
	}
	cap := Capability{ID: "crm.get", Operation: &Operation{Method: "GET", Path: "/contacts/{id}"}}
	out, err := ex.Execute(context.Background(), cap, map[string]interface{}{"id": "42", "fields": "name"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/contacts/42" {
		t.Fatalf("path=%q", gotPath)
	}
	if !strings.Contains(gotQuery, "fields=name") {
		t.Fatalf("query=%q", gotQuery)
	}
	if gotAuth != "Bearer x" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("out=%q", out)
	}
}

func TestOpenAPIExecutorPostBody(t *testing.T) {
	var body, ctype string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body, ctype = string(b), r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	defer srv.Close()

	ex := &OpenAPIExecutor{BaseURL: srv.URL}
	cap := Capability{ID: "crm.create", Operation: &Operation{Method: "POST", Path: "/contacts"}}
	out, err := ex.Execute(context.Background(), cap, map[string]interface{}{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if ctype != "application/json" {
		t.Fatalf("content-type=%q", ctype)
	}
	if !strings.Contains(body, `"name":"Ada"`) {
		t.Fatalf("body=%q", body)
	}
	if out != "created" {
		t.Fatalf("out=%q", out)
	}
}

func TestOpenAPIExecutorErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	ex := &OpenAPIExecutor{BaseURL: srv.URL}
	cap := Capability{ID: "crm.get", Operation: &Operation{Method: "GET", Path: "/x"}}
	if _, err := ex.Execute(context.Background(), cap, nil); err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
}
