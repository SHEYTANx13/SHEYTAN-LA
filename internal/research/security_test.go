package research

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearXNGProviderRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(
			[]byte(
				strings.Repeat("A", maxSearXNGBodyBytes+1),
			),
		)
	}))
	defer server.Close()

	provider := NewSearXNGProvider(
		server.Client(),
		server.URL,
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "oversized",
		},
	)

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf(
			"expected ErrProviderUnavailable, got %v",
			err,
		)
	}

	if !strings.Contains(
		err.Error(),
		"exceeds",
	) {
		t.Fatalf(
			"expected oversized-response error, got %v",
			err,
		)
	}
}

func TestDuckDuckGoProviderRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(
			[]byte(
				strings.Repeat("B", maxDuckDuckGoBodyBytes+1),
			),
		)
	}))
	defer server.Close()

	provider := NewDuckDuckGoProvider(
		server.Client(),
		server.URL,
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "oversized",
		},
	)

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf(
			"expected ErrProviderUnavailable, got %v",
			err,
		)
	}

	if !strings.Contains(
		err.Error(),
		"exceeds",
	) {
		t.Fatalf(
			"expected oversized-response error, got %v",
			err,
		)
	}
}

func TestSearXNGProviderURLConstructionPreservesBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance/search" {
			t.Fatalf(
				"unexpected path: %q",
				r.URL.Path,
			)
		}

		if got := r.URL.Query().Get("q"); got != `go "repair" & test` {
			t.Fatalf(
				"unexpected query: %q",
				got,
			)
		}

		if got := r.URL.Query().Get("format"); got != "json" {
			t.Fatalf(
				"unexpected format: %q",
				got,
			)
		}

		_, _ = w.Write([]byte(`{
			"results":[
				{
					"title":"URL test",
					"url":"https://example.com/url",
					"content":"URL construction works"
				}
			]
		}`))
	}))
	defer server.Close()

	baseURL := server.URL + "/instance///"

	provider := NewSearXNGProvider(
		server.Client(),
		baseURL,
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query:      `go "repair" & test`,
			MaxResults: 1,
		},
	)
	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}
}

func TestDuckDuckGoProviderURLConstructionPreservesBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance/html" {
			t.Fatalf(
				"unexpected path: %q",
				r.URL.Path,
			)
		}

		if got := r.URL.Query().Get("q"); got != `go "repair" & test` {
			t.Fatalf(
				"unexpected query: %q",
				got,
			)
		}

		if got := r.URL.Query().Get("kl"); got != "wt-wt" {
			t.Fatalf(
				"unexpected region: %q",
				got,
			)
		}

		_, _ = w.Write([]byte(`
			<html>
			  <body>
			    <div class="result">
			      <a class="result__a" href="https://example.com/url">
			        URL test
			      </a>
			      <a class="result__snippet">
			        URL construction works
			      </a>
			    </div>
			  </body>
			</html>
		`))
	}))
	defer server.Close()

	baseURL := server.URL + "/instance///"

	provider := NewDuckDuckGoProvider(
		server.Client(),
		baseURL,
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query:      `go "repair" & test`,
			MaxResults: 1,
		},
	)
	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}
}

func TestSearXNGSearchEndpointRejectsMissingSchemeOrHost(t *testing.T) {
	tests := []string{
		"example.com",
		"/relative/path",
		"://broken",
	}

	for _, baseURL := range tests {
		t.Run(baseURL, func(t *testing.T) {
			_, err := searxngSearchEndpoint(baseURL)

			if err == nil {
				t.Fatal("expected invalid endpoint error")
			}
		})
	}
}

func TestDuckDuckGoSearchEndpointRejectsMissingSchemeOrHost(t *testing.T) {
	tests := []string{
		"example.com",
		"/relative/path",
		"://broken",
	}

	for _, baseURL := range tests {
		t.Run(baseURL, func(t *testing.T) {
			_, err := duckDuckGoSearchEndpoint(baseURL)

			if err == nil {
				t.Fatal("expected invalid endpoint error")
			}
		})
	}
}

func TestResearchSearchURLsDoNotReuseCallerRawQuery(t *testing.T) {
	// Provider request URLs are built from operator-configured base URLs.
	// Any raw query string or fragment configured on the base URL must be
	// dropped so caller-supplied parameters cannot leak into (or override)
	// the provider request contract.

	searxEndpoint, err := searxngSearchEndpoint(
		"https://searx.example.test/search?evil=1#fragment",
	)
	if err != nil {
		t.Fatal(err)
	}

	if got := searxEndpoint.Query().Get("evil"); got != "" {
		t.Fatalf("SearXNG endpoint retained caller query parameter: %q", got)
	}

	if searxEndpoint.Fragment != "" {
		t.Fatalf("SearXNG endpoint retained fragment: %q", searxEndpoint.Fragment)
	}

	if searxEndpoint.Query().Get("q") != "" {
		t.Fatal("SearXNG endpoint must not carry preset query values")
	}

	duckEndpoint, err := duckDuckGoSearchEndpoint(
		"https://duck.example.test/?evil=1#fragment",
	)
	if err != nil {
		t.Fatal(err)
	}

	if got := duckEndpoint.Query().Get("evil"); got != "" {
		t.Fatalf("DuckDuckGo endpoint retained caller query parameter: %q", got)
	}

	if duckEndpoint.Fragment != "" {
		t.Fatalf("DuckDuckGo endpoint retained fragment: %q", duckEndpoint.Fragment)
	}

	if duckEndpoint.Query().Get("q") != "" {
		t.Fatal("DuckDuckGo endpoint must not carry preset query values")
	}
}
