package research

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearXNGProviderSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		if r.URL.Path != "/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		if got := r.URL.Query().Get("format"); got != "json" {
			t.Fatalf("expected JSON format, got %q", got)
		}

		if got := r.URL.Query().Get("q"); got != "golang repair" {
			t.Fatalf("unexpected query: %q", got)
		}

		if got := r.URL.Query().Get("categories"); got != "general" {
			t.Fatalf("unexpected categories: %q", got)
		}

		_, _ = w.Write([]byte(`{
                        "query":"golang repair",
                        "results":[
                                {
                                        "title":"  Go Repair Guide  ",
                                        "url":"https://example.com/go",
                                        "content":"  Useful   repair   guidance. ",
                                        "engine":"google",
                                        "publishedDate":"2026-09-01T12:00:00Z",
                                        "score":3.5
                                },
                                {
                                        "title":"Second result",
                                        "url":"https://example.com/second",
                                        "content":"Second snippet",
                                        "engine":"bing",
                                        "score":0.2
                                }
                        ]
                }`))
	}))
	defer server.Close()

	provider := NewSearXNGProvider(
		server.Client(),
		server.URL,
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query:      "golang repair",
			MaxResults: 2,
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Provider != BackendSearXNG {
		t.Fatalf("unexpected provider: %q", response.Provider)
	}

	if len(response.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(response.Results))
	}

	first := response.Results[0]

	if first.Title != "Go Repair Guide" {
		t.Fatalf("unexpected title: %q", first.Title)
	}

	if first.Snippet != "Useful repair guidance." {
		t.Fatalf("unexpected snippet: %q", first.Snippet)
	}

	if first.URL != "https://example.com/go" {
		t.Fatalf("unexpected URL: %q", first.URL)
	}

	if first.Provider != BackendSearXNG {
		t.Fatalf("unexpected result provider: %q", first.Provider)
	}

	if first.Source == "" {
		t.Fatal("expected source")
	}

	if first.ContentHash == "" {
		t.Fatal("expected content hash")
	}

	if first.MatchScore < 0 || first.MatchScore > 1 {
		t.Fatalf("expected normalized score, got %v", first.MatchScore)
	}
}

func TestSearXNGProviderNormalizesBaseURL(t *testing.T) {
	provider := NewSearXNGProvider(
		nil,
		"https://example.test///",
	)

	if provider.BaseURL != "https://example.test" {
		t.Fatalf(
			"unexpected normalized base URL: %q",
			provider.BaseURL,
		)
	}
}

func TestSearXNGProviderRejectsInvalidQuery(t *testing.T) {
	provider := NewSearXNGProvider(
		&http.Client{},
		"https://example.test",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "   ",
		},
	)

	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("expected ErrInvalidQuery, got %v", err)
	}
}

func TestSearXNGProviderRejectsMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer server.Close()

	provider := NewSearXNGProvider(
		server.Client(),
		server.URL,
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "test",
		},
	)

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf(
			"expected ErrProviderUnavailable, got %v",
			err,
		)
	}
}

func TestDuckDuckGoProviderSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		if r.URL.Path != "/html" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		if got := r.URL.Query().Get("q"); got != "golang repair" {
			t.Fatalf("unexpected query: %q", got)
		}

		if got := r.URL.Query().Get("kl"); got != "wt-wt" {
			t.Fatalf("unexpected region parameter: %q", got)
		}

		if got := r.URL.Query().Get("kp"); got != "-2" {
			t.Fatalf("unexpected safe-search parameter: %q", got)
		}

		_, _ = w.Write([]byte(`
                        <html>
                          <body>
                            <div class="result results_links results_links_deep web-result">
                              <a class="result__a" href="https://example.com/go">  Go Repair Guide  </a>
                              <a class="result__snippet"> Useful   repair guidance. </a>
                              <a class="result__url">example.com/go</a>
                            </div>

                            <div class="result results_links results_links_deep web-result">
                              <a class="result__a" href="https://example.com/second">Second result</a>
                              <a class="result__snippet">Second snippet</a>
                              <a class="result__url">example.com/second</a>
                            </div>
                          </body>
                        </html>
                `))
	}))
	defer server.Close()

	provider := NewDuckDuckGoProvider(
		server.Client(),
		server.URL,
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query:      "golang repair",
			MaxResults: 2,
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Provider != BackendDuckDuckGo {
		t.Fatalf("unexpected provider: %q", response.Provider)
	}

	if len(response.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(response.Results))
	}

	first := response.Results[0]

	if first.Title != "Go Repair Guide" {
		t.Fatalf("unexpected title: %q", first.Title)
	}

	if first.Snippet != "Useful repair guidance." {
		t.Fatalf("unexpected snippet: %q", first.Snippet)
	}

	if first.URL != "https://example.com/go" {
		t.Fatalf("unexpected URL: %q", first.URL)
	}

	if first.Provider != BackendDuckDuckGo {
		t.Fatalf(
			"unexpected result provider: %q",
			first.Provider,
		)
	}

	if first.Source == "" {
		t.Fatal("expected source")
	}

	if first.Authority == AuthorityUnknown {
		t.Fatal("expected provider authority classification")
	}

	if first.ContentHash == "" {
		t.Fatal("expected content hash")
	}
}

func TestDuckDuckGoProviderSearchEscapesQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != `golang "repair" test` {
			t.Fatalf("query was not decoded correctly: %q", got)
		}

		_, _ = w.Write([]byte(`
                        <html>
                          <body>
                            <div class="result results_links results_links_deep web-result">
                              <a class="result__a" href="https://example.com/test">Test</a>
                              <a class="result__snippet">Snippet</a>
                            </div>
                          </body>
                        </html>
                `))
	}))
	defer server.Close()

	provider := NewDuckDuckGoProvider(
		server.Client(),
		server.URL,
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query:      `golang "repair" test`,
			MaxResults: 1,
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
}

func TestDuckDuckGoProviderEmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
                        <html>
                          <body>
                            <div class="no-results">
                              No results found
                            </div>
                          </body>
                        </html>
                `))
	}))
	defer server.Close()

	provider := NewDuckDuckGoProvider(
		server.Client(),
		server.URL,
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "nothing",
		},
	)

	if !errors.Is(err, ErrNoResults) {
		t.Fatalf("expected ErrNoResults, got %v", err)
	}

	if len(response.Results) != 0 {
		t.Fatalf(
			"expected zero results, got %d",
			len(response.Results),
		)
	}
}

func TestDuckDuckGoProviderMalformedHTMLDoesNotPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("<div>", 100)))
	}))
	defer server.Close()

	provider := NewDuckDuckGoProvider(
		server.Client(),
		server.URL,
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "test",
		},
	)

	if err != nil && !errors.Is(err, ErrNoResults) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearXNGProviderResultLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
                        "results":[
                                {"title":"One","url":"https://example.com/1","content":"One"},
                                {"title":"Two","url":"https://example.com/2","content":"Two"},
                                {"title":"Three","url":"https://example.com/3","content":"Three"}
                        ]
                }`))
	}))
	defer server.Close()

	provider := NewSearXNGProvider(
		server.Client(),
		server.URL,
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query:      "test",
			MaxResults: 2,
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 2 {
		t.Fatalf(
			"expected result limit of 2, got %d",
			len(response.Results),
		)
	}
}
