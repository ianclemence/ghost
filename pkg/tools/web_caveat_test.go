package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A search result is listing-level evidence. That is a fact about the
// retrieval, recorded structurally so nothing has to narrate it — and it must
// not put a caveat into the text the model reads.
func TestSearchRecordsSourceFactsWithoutCaveatProse(t *testing.T) {
	listing := "Results for: latest news in bangkok\n" +
		"1. Flooding swamps Bangkok\n   https://www.nationthailand.com/news/1\n   Three days of rain\n" +
		"2. Bangkok floods disrupt traffic\n   https://apnews.com/article/2\n   Evacuations under way\n" +
		"3. District disaster zones declared\n   https://www.nationthailand.com/news/3\n   All 50 districts\n"
	tool := &WebSearchTool{provider: stubSearchProvider{out: listing}, maxResults: 5}

	res := tool.Execute(context.Background(), map[string]interface{}{"query": "latest news in bangkok"})
	if res.IsError {
		t.Fatalf("search failed: %s", res.ForLLM)
	}
	if got := EvidenceSourceAccess(res); got != SourceAccessListings {
		t.Errorf("source access = %q, want %q", got, SourceAccessListings)
	}
	srcs := EvidenceSources(res)
	if len(srcs) != 2 { // two distinct domains, deduplicated
		t.Fatalf("sources = %v, want the two distinct domains", srcs)
	}
	if got := EvidenceSummary(res); !strings.Contains(got, "latest news in bangkok") {
		t.Errorf("summary = %q, want the query recorded", got)
	}
	// The text the model reads is the listing. It must not editorialise about
	// how the listing was obtained.
	for _, banned := range []string{"caveat", "headline only", "snippet only", "not full article"} {
		if strings.Contains(strings.ToLower(res.ForLLM), banned) {
			t.Errorf("search text must not carry caveat prose (%q): %s", banned, res.ForLLM)
		}
	}
}

func TestSourceNamesFromListingsDedupesAndBounds(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 12; i++ {
		b.WriteString("https://example.com/page\n")
	}
	b.WriteString("https://www.reuters.com/x\n")
	got := SourceNamesFromListings(b.String())
	if len(got) != 2 {
		t.Fatalf("got %v, want example.com and reuters.com", got)
	}
	if got[1] != "reuters.com" {
		t.Errorf("www. prefix must be stripped, got %q", got[1])
	}
}

// A page the runtime could not read produces a direct, product-language
// statement — not a transport error the model can relay as plumbing.
func TestFetchFailureIsStatedPlainly(t *testing.T) {
	// A port that nothing listens on.
	tool := NewWebFetchToolWithConfig(50000, "", true)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"url": "http://127.0.0.1:1/article",
	})
	if !res.IsError {
		t.Fatal("an unreachable page must be an error")
	}
	if !strings.Contains(res.ForLLM, "I couldn't read that page") {
		t.Errorf("failure text = %q, want a plain statement", res.ForLLM)
	}
	for _, plumbing := range []string{"dial tcp", "connect:", "http.Client", "transport"} {
		if strings.Contains(strings.ToLower(res.ForLLM), plumbing) {
			t.Errorf("failure text leaks transport plumbing (%q): %s", plumbing, res.ForLLM)
		}
	}
}

// A successful fetch records what was read and how, so activity can attribute
// it and the limitation policy can decide separately.
func TestFetchRecordsSourceFacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><p>Flooding continues.</p></body></html>"))
	}))
	defer server.Close()

	tool := NewWebFetchToolWithConfig(50000, "", true)
	res := tool.Execute(context.Background(), map[string]interface{}{"url": server.URL})
	if res.IsError {
		t.Fatalf("fetch failed: %s", res.ForLLM)
	}
	srcs := EvidenceSources(res)
	if len(srcs) != 1 || !strings.HasPrefix(srcs[0], "127.0.0.1") {
		t.Fatalf("sources = %v, want the fetched host", srcs)
	}
	if !strings.Contains(EvidenceSummary(res), "Read 127.0.0.1") {
		t.Errorf("summary = %q", EvidenceSummary(res))
	}
}

type stubSearchProvider struct{ out string }

func (s stubSearchProvider) Search(ctx context.Context, query string, count int) (string, error) {
	return s.out, nil
}
