package research

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewGitHubProviderDefaults(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")

	provider := NewGitHubProvider(nil, "", "")

	if provider == nil {
		t.Fatal("expected provider")
	}

	if provider.Name() != "github" {
		t.Fatalf("unexpected provider name: %q", provider.Name())
	}

	if provider.BaseURL != defaultGitHubAPIURL {
		t.Fatalf("unexpected default base URL: %q", provider.BaseURL)
	}

	if provider.Client == nil {
		t.Fatal("expected default HTTP client")
	}

	if provider.Token != "env-token" {
		t.Fatalf("unexpected token: %q", provider.Token)
	}
}

func TestNewGitHubProviderExplicitValues(t *testing.T) {
	client := &http.Client{}
	provider := NewGitHubProvider(client, "https://example.test///", "explicit-token")

	if provider.Client != client {
		t.Fatal("expected explicit client to be preserved")
	}

	if provider.BaseURL != "https://example.test" {
		t.Fatalf("unexpected normalized base URL: %q", provider.BaseURL)
	}

	if provider.Token != "explicit-token" {
		t.Fatalf("unexpected explicit token: %q", provider.Token)
	}
}

func TestGitHubProviderSearch(t *testing.T) {
	const (
		token   = "test-token"
		query   = "go test race"
		bodyOne = "first   result\nwith multiple\tspaces"
	)

	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		if r.URL.Path != "/search/issues" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		values := r.URL.Query()

		if values.Get("q") != query {
			t.Fatalf("unexpected query: %q", values.Get("q"))
		}

		if values.Get("per_page") != "3" {
			t.Fatalf("unexpected per_page: %q", values.Get("per_page"))
		}

		if values.Get("page") != "1" {
			t.Fatalf("unexpected page: %q", values.Get("page"))
		}

		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("unexpected Accept header: %q", got)
		}

		if got := r.Header.Get("User-Agent"); got != DefaultUserAgent() {
			t.Fatalf("unexpected User-Agent: %q", got)
		}

		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Fatalf("unexpected GitHub API version: %q", got)
		}

		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("unexpected Authorization header: %q", got)
		}

		response := githubSearchResponse{
			TotalCount: 2,
			Items: []githubSearchItem{
				{
					Title:     "  First issue  ",
					HTMLURL:   "https://github.com/example/repo/issues/1",
					Body:      bodyOne,
					State:     "open",
					CreatedAt: "2026-09-01T12:34:56Z",
					UpdatedAt: "2026-09-02T00:00:00Z",
					Score:     2.75,
					Comments:  4,
					User: struct {
						Login string `json:"login"`
					}{
						Login: "maintainer",
					},
					RepositoryURL: "https://api.github.com/repos/example/repo",
				},
				{
					Title:     "pull request",
					HTMLURL:   "https://github.com/example/repo/pull/7",
					Body:      "A PR body",
					State:     "closed",
					CreatedAt: "invalid-time",
					UpdatedAt: "2026-09-02T01:00:00Z",
					Score:     -1,
					Comments:  2,
					User: struct {
						Login string `json:"login"`
					}{
						Login: "contributor",
					},
					RepositoryURL: "https://api.github.com/repos/example/repo",
					PullRequest: &struct {
						URL string `json:"url"`
					}{
						URL: "https://api.github.com/repos/example/repo/pulls/7",
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL+"/", token)

	result, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query:      query,
			MaxResults: 3,
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if requestCount.Load() != 1 {
		t.Fatalf("expected one request, got %d", requestCount.Load())
	}

	if result.Provider != "github" {
		t.Fatalf("unexpected provider: %q", result.Provider)
	}

	if result.Query != query {
		t.Fatalf("unexpected response query: %q", result.Query)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	first := result.Results[0]

	if first.Title != "First issue" {
		t.Fatalf("unexpected title: %q", first.Title)
	}

	if first.URL != "https://github.com/example/repo/issues/1" {
		t.Fatalf("unexpected URL: %q", first.URL)
	}

	if first.Snippet != "first result with multiple spaces" {
		t.Fatalf("unexpected snippet: %q", first.Snippet)
	}

	if first.Source != "GitHub" {
		t.Fatalf("unexpected source: %q", first.Source)
	}

	if first.Provider != "github" {
		t.Fatalf("unexpected result provider: %q", first.Provider)
	}

	if first.Authority != AuthorityTechnicalDiscussion {
		t.Fatalf("unexpected authority: %v", first.Authority)
	}

	// GitHub scores are opaque ranking values; the provider normalizes them
	// monotonically into [0,1) via score/(score+1) rather than clamping.
	if first.MatchScore <= 0 || first.MatchScore >= 1 {
		t.Fatalf("expected normalized score in (0,1), got %v", first.MatchScore)
	}

	if want := 2.75 / 3.75; math.Abs(first.MatchScore-want) > 1e-9 {
		t.Fatalf("expected normalized score %v, got %v", want, first.MatchScore)
	}

	expectedTime, err := time.Parse(time.RFC3339, "2026-09-01T12:34:56Z")
	if err != nil {
		t.Fatal(err)
	}

	if !first.PublishedAt.Equal(expectedTime) {
		t.Fatalf("unexpected published time: %v", first.PublishedAt)
	}

	if first.ContentHash == "" {
		t.Fatal("expected content hash")
	}

	if first.Metadata["state"] != "open" {
		t.Fatalf("unexpected state metadata: %#v", first.Metadata["state"])
	}

	if first.Metadata["author"] != "maintainer" {
		t.Fatalf("unexpected author metadata: %#v", first.Metadata["author"])
	}

	if first.Metadata["comments"] != 4 {
		t.Fatalf("unexpected comments metadata: %#v", first.Metadata["comments"])
	}

	if first.Metadata["is_pull_request"] != false {
		t.Fatalf("unexpected pull request metadata: %#v", first.Metadata["is_pull_request"])
	}

	second := result.Results[1]

	if !second.PublishedAt.IsZero() {
		t.Fatalf("expected invalid date to normalize to zero, got %v", second.PublishedAt)
	}

	if second.MatchScore != 0 {
		t.Fatalf("expected negative score to clamp to 0, got %v", second.MatchScore)
	}

	if second.Metadata["is_pull_request"] != true {
		t.Fatalf("expected pull request metadata to be true")
	}

	if result.Duration < 0 {
		t.Fatalf("unexpected negative duration: %v", result.Duration)
	}
}

func TestGitHubProviderSearchDefaultAndMaximumResults(t *testing.T) {
	tests := []struct {
		name     string
		request  int
		expected int
	}{
		{
			name:     "default",
			request:  0,
			expected: 8,
		},
		{
			name:     "maximum",
			request:  1000,
			expected: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("per_page"); got != strconv.Itoa(tt.expected) {
					t.Fatalf("expected per_page=%d, got %q", tt.expected, got)
				}

				writeGitHubResults(t, w, 1)
			}))
			defer server.Close()

			provider := NewGitHubProvider(server.Client(), server.URL, "")

			_, err := provider.Search(
				context.Background(),
				SearchRequest{
					Query:      "test",
					MaxResults: tt.request,
				},
			)
			if err != nil {
				t.Fatalf("Search returned error: %v", err)
			}
		})
	}
}

func TestGitHubProviderSearchRejectsInvalidQuery(t *testing.T) {
	provider := NewGitHubProvider(&http.Client{}, "https://example.test", "")

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

func TestGitHubProviderSearchMapsHTTPStatus(t *testing.T) {
	tests := []struct {
		status int
		check  func(error) bool
	}{
		{
			status: http.StatusUnauthorized,
			check: func(err error) bool {
				return errors.Is(err, ErrProviderUnavailable) &&
					strings.Contains(err.Error(), "authentication")
			},
		},
		{
			status: http.StatusForbidden,
			check: func(err error) bool {
				return errors.Is(err, ErrProviderUnavailable) &&
					strings.Contains(err.Error(), "rate limit")
			},
		},
		{
			status: http.StatusNotFound,
			check: func(err error) bool {
				return errors.Is(err, ErrProviderUnavailable) &&
					strings.Contains(err.Error(), "endpoint not found")
			},
		},
		{
			status: http.StatusInternalServerError,
			check: func(err error) bool {
				return errors.Is(err, ErrProviderUnavailable) &&
					strings.Contains(err.Error(), "HTTP 500")
			},
		},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "failure", tt.status)
			}))
			defer server.Close()

			provider := NewGitHubProvider(server.Client(), server.URL, "")

			_, err := provider.Search(
				context.Background(),
				SearchRequest{Query: "test"},
			)
			if err == nil {
				t.Fatal("expected error")
			}

			if !tt.check(err) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGitHubProviderSearchEmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGitHubResults(t, w, 0)
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	result, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "nothing"},
	)

	if !errors.Is(err, ErrNoResults) {
		t.Fatalf("expected ErrNoResults, got %v", err)
	}

	if len(result.Results) != 0 {
		t.Fatalf("expected no results, got %d", len(result.Results))
	}
}

func TestGitHubProviderSearchMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{not-json")
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	_, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestGitHubProviderSearchContextCancellation(t *testing.T) {
	started := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan error, 1)

	go func() {
		_, err := provider.Search(
			ctx,
			SearchRequest{Query: "cancel"},
		)
		resultCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive request")
	}

	cancel()

	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Search did not return after context cancellation")
	}
}

func TestGitHubProviderSearchDoesNotLeakAuthorizationWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}

		writeGitHubResults(t, w, 1)
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	_, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
}

func TestGitHubProviderSearchUsesEnvironmentToken(t *testing.T) {
	const token = "environment-token"

	t.Setenv("GITHUB_TOKEN", token)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("unexpected Authorization header: %q", got)
		}

		writeGitHubResults(t, w, 1)
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	_, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
}

func TestGitHubProviderSearchExplicitTokenOverridesEnvironment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "environment-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer explicit-token" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}

		writeGitHubResults(t, w, 1)
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "explicit-token")

	_, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
}

func TestGitHubProviderSearchHashIsDeterministic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGitHubResults(t, w, 1)
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	first, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("first Search returned error: %v", err)
	}

	second, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("second Search returned error: %v", err)
	}

	if first.Results[0].ContentHash == "" || second.Results[0].ContentHash == "" {
		t.Fatal("expected content hashes")
	}

	if first.Results[0].ContentHash != second.Results[0].ContentHash {
		t.Fatalf(
			"expected deterministic hash, got %q and %q",
			first.Results[0].ContentHash,
			second.Results[0].ContentHash,
		)
	}
}

func TestCompactGitHubBody(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		maxLen  int
		expects string
	}{
		{
			name:    "empty",
			body:    "   ",
			maxLen:  10,
			expects: "",
		},
		{
			name:    "whitespace",
			body:    "  hello \n world\tagain  ",
			maxLen:  100,
			expects: "hello world again",
		},
		{
			name:    "bounded",
			body:    "abcdefghijklmnopqrstuvwxyz",
			maxLen:  10,
			expects: "abcdefghij…",
		},
		{
			name:    "unbounded",
			body:    "abc",
			maxLen:  0,
			expects: "abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compactGitHubBody(tt.body, tt.maxLen)
			if got != tt.expects {
				t.Fatalf("expected %q, got %q", tt.expects, got)
			}
		})
	}
}

func TestParseGitHubTime(t *testing.T) {
	valid := parseGitHubTime("2026-09-02T12:30:45Z")
	if valid.IsZero() {
		t.Fatal("expected valid timestamp")
	}

	invalid := parseGitHubTime("not-a-time")
	if !invalid.IsZero() {
		t.Fatalf("expected zero timestamp, got %v", invalid)
	}

	empty := parseGitHubTime("")
	if !empty.IsZero() {
		t.Fatalf("expected zero timestamp, got %v", empty)
	}
}

func TestGitHubProviderSearchBaseURLWithExistingQuery(t *testing.T) {
	base, err := url.Parse("https://example.test/api?existing=value")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search/issues" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		if r.URL.Query().Get("existing") != "value" {
			t.Fatalf("existing query parameter was lost")
		}

		if r.URL.Query().Get("q") != "hello world" {
			t.Fatalf("unexpected search query: %q", r.URL.Query().Get("q"))
		}

		writeGitHubResults(t, w, 1)
	}))
	defer server.Close()

	baseURL := server.URL + base.Path + "?existing=value"

	provider := NewGitHubProvider(server.Client(), baseURL, "")

	_, err = provider.Search(
		context.Background(),
		SearchRequest{Query: "hello world"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
}

func TestGitHubProviderImplementsProvider(t *testing.T) {
	var _ Provider = (*GitHubProvider)(nil)
}

func writeGitHubResults(t *testing.T, w http.ResponseWriter, count int) {
	t.Helper()

	response := githubSearchResponse{
		TotalCount: count,
		Items:      make([]githubSearchItem, 0, count),
	}

	for i := 0; i < count; i++ {
		response.Items = append(response.Items, githubSearchItem{
			Title:     "result " + strconv.Itoa(i),
			HTMLURL:   "https://github.com/example/repo/issues/" + strconv.Itoa(i+1),
			Body:      "body",
			State:     "open",
			CreatedAt: "2026-09-02T00:00:00Z",
			UpdatedAt: "2026-09-02T00:00:00Z",
			User: struct {
				Login string `json:"login"`
			}{
				Login: "user",
			},
			RepositoryURL: "https://api.github.com/repos/example/repo",
			Comments:      i,
			Score:         0.5,
		})
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Errorf("failed to encode GitHub test response: %v", err)
	}
}

func TestGitHubProviderSearchSkipsMissingURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := githubSearchResponse{
			TotalCount: 2,
			Items: []githubSearchItem{
				{
					Title: "missing URL",
					Body:  "ignored",
				},
				{
					Title:   "valid",
					HTMLURL: "https://github.com/example/repo/issues/1",
					Body:    "kept",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	result, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected one retained result, got %d", len(result.Results))
	}

	if result.Results[0].Title != "valid" {
		t.Fatalf("unexpected retained result: %q", result.Results[0].Title)
	}
}

func TestGitHubProviderSearchEscapesQueryCorrectly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")

		if query != `error "permission denied" path:C:\work` {
			t.Fatalf("unexpected decoded query: %q", query)
		}

		writeGitHubResults(t, w, 1)
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: `error "permission denied" path:C:\work`,
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
}

func TestGitHubProviderSearchDefaultClientTimeout(t *testing.T) {
	provider := NewGitHubProvider(nil, defaultGitHubAPIURL, "")

	if provider.Client.Timeout != 20*time.Second {
		t.Fatalf("expected 20 second timeout, got %v", provider.Client.Timeout)
	}
}

func TestGitHubProviderSearchIgnoresTotalCountForReturnedResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := githubSearchResponse{
			TotalCount: 1000,
			Items: []githubSearchItem{
				{
					Title:   "only returned result",
					HTMLURL: "https://github.com/example/repo/issues/1",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	result, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected one returned result, got %d", len(result.Results))
	}
}

func TestGitHubProviderSearchMetadataTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := githubSearchResponse{
			TotalCount: 1,
			Items: []githubSearchItem{
				{
					Title:         "metadata",
					HTMLURL:       "https://github.com/example/repo/issues/1",
					State:         "closed",
					Comments:      9,
					RepositoryURL: "https://api.github.com/repos/example/repo",
					CreatedAt:     "2026-09-02T00:00:00Z",
					UpdatedAt:     "2026-09-02T01:00:00Z",
					User: struct {
						Login string `json:"login"`
					}{
						Login: "author",
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	result, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	metadata := result.Results[0].Metadata

	if _, ok := metadata["state"].(string); !ok {
		t.Fatalf("state metadata has unexpected type: %T", metadata["state"])
	}

	if _, ok := metadata["author"].(string); !ok {
		t.Fatalf("author metadata has unexpected type: %T", metadata["author"])
	}

	if _, ok := metadata["comments"].(int); !ok {
		t.Fatalf("comments metadata has unexpected type: %T", metadata["comments"])
	}

	if _, ok := metadata["repository"].(string); !ok {
		t.Fatalf("repository metadata has unexpected type: %T", metadata["repository"])
	}

	if _, ok := metadata["is_pull_request"].(bool); !ok {
		t.Fatalf(
			"is_pull_request metadata has unexpected type: %T",
			metadata["is_pull_request"],
		)
	}
}

func TestGitHubProviderSearchDoesNotMutateOriginalRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGitHubResults(t, w, 1)
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	request := SearchRequest{
		Query:      "  test  ",
		MaxResults: 1,
	}

	_, err := provider.Search(context.Background(), request)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if request.Query != "  test  " {
		t.Fatalf("provider mutated original request query: %q", request.Query)
	}
}

func TestGitHubProviderSearchRejectsNegativeMaxResults(t *testing.T) {
	provider := NewGitHubProvider(
		&http.Client{},
		"https://example.test",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query:      "test",
			MaxResults: -1,
		},
	)
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("expected ErrInvalidQuery, got %v", err)
	}
}

func TestGitHubProviderSearchHandlesEmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := githubSearchResponse{
			TotalCount: 1,
			Items: []githubSearchItem{
				{
					Title:   "empty body",
					HTMLURL: "https://github.com/example/repo/issues/1",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	result, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if result.Results[0].Snippet != "" {
		t.Fatalf("expected empty snippet, got %q", result.Results[0].Snippet)
	}
}

func TestGitHubProviderSearchResponseValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"total_count": 0,
			"items":       []any{},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	response, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "empty"},
	)
	if !errors.Is(err, ErrNoResults) {
		t.Fatalf("expected ErrNoResults, got %v", err)
	}

	if response.Provider != "github" {
		t.Fatalf("unexpected provider: %q", response.Provider)
	}
}

func TestGitHubProviderSearchWithSpecialBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/search/issues" {
			t.Fatalf("unexpected path: %q", r.URL.Path)
		}

		writeGitHubResults(t, w, 1)
	}))
	defer server.Close()

	provider := NewGitHubProvider(
		server.Client(),
		strings.TrimRight(server.URL, "/")+"/custom////",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
}

func TestGitHubProviderSearchDoesNotUseOSProxyForTestServer(t *testing.T) {
	previous := os.Getenv("HTTPS_PROXY")
	t.Setenv("HTTPS_PROXY", "")

	defer func() {
		_ = os.Setenv("HTTPS_PROXY", previous)
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeGitHubResults(t, w, 1)
	}))
	defer server.Close()

	provider := NewGitHubProvider(server.Client(), server.URL, "")

	_, err := provider.Search(
		context.Background(),
		SearchRequest{Query: "proxy test"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
}
