package voice

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
)

func localTestServer(t *testing.T, text string, check func(r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":` + strconv.Quote(text) + `}`))
	}))
}

func TestLocalTranscribe(t *testing.T) {
	srv := localTestServer(t, "hello ghost", func(r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("not multipart: %v", err)
			return
		}
		if r.FormValue("model") == "" || r.FormValue("response_format") != "json" {
			t.Errorf("bad fields: model=%q response_format=%q", r.FormValue("model"), r.FormValue("response_format"))
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("no file part: %v", err)
			return
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		if string(b) != "fake-audio" {
			t.Errorf("wrong file content %q", b)
		}
	})
	defer srv.Close()

	f := t.TempDir() + "/note.ogg"
	if err := os.WriteFile(f, []byte("fake-audio"), 0600); err != nil {
		t.Fatal(err)
	}
	tr := NewLocalTranscriber(srv.URL)
	if !tr.IsAvailable() {
		t.Fatal("sidecar must be available")
	}
	res, err := tr.Transcribe(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello ghost" {
		t.Fatalf("wrong text %q", res.Text)
	}
}

func TestLocalUnavailable(t *testing.T) {
	tr := NewLocalTranscriber("http://127.0.0.1:1")
	if tr.IsAvailable() {
		t.Fatal("nothing listens on port 1")
	}
	if _, err := tr.Transcribe(context.Background(), "/nonexistent"); err == nil {
		t.Fatal("missing file must error")
	}
}

type stubTranscriber struct {
	available bool
	text      string
}

func (s stubTranscriber) IsAvailable() bool { return s.available }
func (s stubTranscriber) Transcribe(_ context.Context, _ string) (*TranscriptionResponse, error) {
	return &TranscriptionResponse{Text: s.text}, nil
}

func TestAutoFallback(t *testing.T) {
	down := NewLocalTranscriber("http://127.0.0.1:1")
	up := stubTranscriber{available: true, text: "from cloud"}
	auto := NewAutoTranscriber(down, up)
	if !auto.IsAvailable() {
		t.Fatal("fallback keeps it available")
	}
	res, err := auto.Transcribe(context.Background(), "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "from cloud" {
		t.Fatalf("must use fallback, got %q", res.Text)
	}
	if (NewAutoTranscriber(nil, nil)).IsAvailable() {
		t.Fatal("empty auto must be unavailable")
	}
	if _, err := NewAutoTranscriber(nil, nil).Transcribe(context.Background(), "x"); err == nil {
		t.Fatal("empty auto must error")
	}
}

func TestSelectTranscriber(t *testing.T) {
	if SelectTranscriber(SelectConfig{Engine: "off", GroqKey: "k"}) != nil {
		t.Fatal("off must yield nil")
	}
	if _, ok := SelectTranscriber(SelectConfig{Engine: "local", LocalURL: "http://127.0.0.1:11435"}).(*LocalTranscriber); !ok {
		t.Fatal("local must yield LocalTranscriber")
	}
	if _, ok := SelectTranscriber(SelectConfig{GroqKey: "k"}).(*GroqTranscriber); !ok {
		t.Fatal("auto with only groq key must yield GroqTranscriber")
	}
	if _, ok := SelectTranscriber(SelectConfig{LocalURL: "http://127.0.0.1:11435", GroqKey: "k"}).(*AutoTranscriber); !ok {
		t.Fatal("auto with local+cloud must yield AutoTranscriber")
	}
	if SelectTranscriber(SelectConfig{}) != nil {
		t.Fatal("nothing configured must yield nil")
	}
	if SelectTranscriber(SelectConfig{Engine: "groq"}) != nil {
		t.Fatal("pinned groq without key must yield nil")
	}
}
