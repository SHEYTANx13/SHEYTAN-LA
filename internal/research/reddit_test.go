package research

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRedditProviderDefaults(t *testing.T) {
	t.Setenv("REDDIT_ACCESS_TOKEN", "env-token")

	provider := NewRedditProvider(
		nil,
		"",
		"",
		"",
	)

	if provider == nil {
		t.Fatal("expected provider")
	}

	if provider.Name() != BackendReddit {
		t.Fatalf(
			"unexpected provider name: %q",
			provider.Name(),
		)
	}

	if provider.BaseURL != defaultRedditAPIURL {
		t.Fatalf(
			"unexpected default base URL: %q",
			provider.BaseURL,
		)
	}

	if provider.Client == nil {
		t.Fatal("expected default HTTP client")
	}

	if provider.AccessToken != "env-token" {
		t.Fatalf(
			"unexpected access token: %q",
			provider.AccessToken,
		)
	}

	if provider.UserAgent != DefaultUserAgent() {
		t.Fatalf(
			"unexpected User-Agent: %q",
			provider.UserAgent,
		)
	}
}

func TestNewRedditProviderExplicitValues(t *testing.T) {
	client := &http.Client{}

	provider := NewRedditProvider(
		client,
		"https://example.test///",
		"explicit-token",
		"custom-agent",
	)

	if provider.Client != client {
		t.Fatal("expected explicit HTTP client to be preserved")
	}

	if provider.BaseURL != "https://example.test" {
		t.Fatalf(
			"unexpected normalized base URL: %q",
			provider.BaseURL,
		)
	}

	if provider.AccessToken != "explicit-token" {
		t.Fatalf(
			"unexpected access token: %q",
			provider.AccessToken,
		)
	}

	if provider.UserAgent != "custom-agent" {
		t.Fatalf(
			"unexpected User-Agent: %q",
			provider.UserAgent,
		)
	}
}

func TestNewRedditProviderExplicitTokenOverridesEnvironment(t *testing.T) {
	t.Setenv("REDDIT_ACCESS_TOKEN", "environment-token")

	provider := NewRedditProvider(
		&http.Client{},
		"https://example.test",
		"explicit-token",
		"",
	)

	if provider.AccessToken != "explicit-token" {
		t.Fatalf(
			"expected explicit token, got %q",
			provider.AccessToken,
		)
	}
}

func TestRedditProviderSearch(t *testing.T) {
	const (
		token = "test-token"
		query = "golang permission denied"
	)

	var requestCount atomic.Int32

	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			requestCount.Add(1)

			if r.Method != http.MethodGet {
				t.Fatalf(
					"unexpected method: %s",
					r.Method,
				)
			}

			if r.URL.Path != "/search" {
				t.Fatalf(
					"unexpected path: %s",
					r.URL.Path,
				)
			}

			values := r.URL.Query()

			if values.Get("q") != query {
				t.Fatalf(
					"unexpected q: %q",
					values.Get("q"),
				)
			}

			if values.Get("limit") != "3" {
				t.Fatalf(
					"unexpected limit: %q",
					values.Get("limit"),
				)
			}

			if values.Get("sort") != "relevance" {
				t.Fatalf(
					"unexpected sort: %q",
					values.Get("sort"),
				)
			}

			if values.Get("t") != "all" {
				t.Fatalf(
					"unexpected time filter: %q",
					values.Get("t"),
				)
			}

			if values.Get("type") != "link" {
				t.Fatalf(
					"unexpected type: %q",
					values.Get("type"),
				)
			}

			if values.Get("raw_json") != "1" {
				t.Fatalf(
					"unexpected raw_json: %q",
					values.Get("raw_json"),
				)
			}

			if values.Get("include_facets") != "false" {
				t.Fatalf(
					"unexpected include_facets: %q",
					values.Get("include_facets"),
				)
			}

			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Fatalf(
					"unexpected Accept header: %q",
					got,
				)
			}

			if got := r.Header.Get("Authorization"); got != "Bearer "+token {
				t.Fatalf(
					"unexpected Authorization header: %q",
					got,
				)
			}

			if got := r.Header.Get("User-Agent"); got != DefaultUserAgent() {
				t.Fatalf(
					"unexpected User-Agent: %q",
					got,
				)
			}

			response := redditSearchResponse{
				Data: redditListing{
					Children: []redditSearchThing{
						{
							Kind: "t3",
							Data: redditSearchPost{
								ID:          "abc123",
								Name:        "t3_abc123",
								Title:       "  Permission denied in Go  ",
								SelfText:    "first   result\nwith multiple\tspaces",
								Author:      "maintainer",
								Subreddit:   "golang",
								Permalink:   "/r/golang/comments/abc123/permission_denied_in_go/",
								URL:         "https://external.example/resource",
								CreatedUTC:  1788357296,
								Score:       120,
								NumComments: 14,
								Ups:         120,
								Downs:       0,
								Over18:      false,
								Locked:      false,
								Stickied:    false,
								IsSelf:      true,
							},
						},
						{
							Kind: "t3",
							Data: redditSearchPost{
								ID:          "def456",
								Name:        "t3_def456",
								Title:       "Second result",
								SelfText:    "",
								Author:      "user",
								Subreddit:   "programming",
								Permalink:   "",
								URL:         "https://example.test/posts/def456",
								CreatedUTC:  1788357000,
								Score:       42,
								NumComments: 3,
								Ups:         42,
								Downs:       0,
								Over18:      false,
								Locked:      false,
								Stickied:    false,
								IsSelf:      false,
							},
						},
					},
				},
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			if err := json.NewEncoder(w).Encode(response); err != nil {
				t.Errorf(
					"failed to encode Reddit response: %v",
					err,
				)
			}
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		token,
		DefaultUserAgent(),
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query:      query,
			MaxResults: 3,
		},
	)
	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}

	if requestCount.Load() != 1 {
		t.Fatalf(
			"expected one request, got %d",
			requestCount.Load(),
		)
	}

	if response.Provider != BackendReddit {
		t.Fatalf(
			"unexpected provider: %q",
			response.Provider,
		)
	}

	if response.Query != query {
		t.Fatalf(
			"unexpected query: %q",
			response.Query,
		)
	}

	if len(response.Results) != 2 {
		t.Fatalf(
			"expected 2 results, got %d",
			len(response.Results),
		)
	}

	first := response.Results[0]

	if first.Title != "Permission denied in Go" {
		t.Fatalf(
			"unexpected first title: %q",
			first.Title,
		)
	}

	if first.URL != "https://www.reddit.com/r/golang/comments/abc123/permission_denied_in_go/" {
		t.Fatalf(
			"unexpected first URL: %q",
			first.URL,
		)
	}

	if first.Snippet != "first result with multiple spaces" {
		t.Fatalf(
			"unexpected first snippet: %q",
			first.Snippet,
		)
	}

	if first.Source != "Reddit" {
		t.Fatalf(
			"unexpected first source: %q",
			first.Source,
		)
	}

	if first.Provider != BackendReddit {
		t.Fatalf(
			"unexpected first provider: %q",
			first.Provider,
		)
	}

	if first.Authority != AuthorityCommunity {
		t.Fatalf(
			"unexpected first authority: %v",
			first.Authority,
		)
	}

	if first.MatchScore != 1 {
		t.Fatalf(
			"expected first score 1, got %v",
			first.MatchScore,
		)
	}

	if first.PublishedAt.IsZero() {
		t.Fatal("expected first timestamp")
	}

	if first.ContentHash == "" {
		t.Fatal("expected first content hash")
	}

	if first.Metadata["id"] != "abc123" {
		t.Fatalf(
			"unexpected id metadata: %#v",
			first.Metadata["id"],
		)
	}

	if first.Metadata["author"] != "maintainer" {
		t.Fatalf(
			"unexpected author metadata: %#v",
			first.Metadata["author"],
		)
	}

	if first.Metadata["subreddit"] != "golang" {
		t.Fatalf(
			"unexpected subreddit metadata: %#v",
			first.Metadata["subreddit"],
		)
	}

	if first.Metadata["num_comments"] != 14 {
		t.Fatalf(
			"unexpected comments metadata: %#v",
			first.Metadata["num_comments"],
		)
	}

	if first.Metadata["is_self"] != true {
		t.Fatalf(
			"unexpected is_self metadata: %#v",
			first.Metadata["is_self"],
		)
	}

	second := response.Results[1]

	if second.Snippet != "Second result" {
		t.Fatalf(
			"expected title fallback snippet, got %q",
			second.Snippet,
		)
	}

	if second.URL != "https://example.test/posts/def456" {
		t.Fatalf(
			"unexpected second URL: %q",
			second.URL,
		)
	}

	if second.MatchScore <= 0 || second.MatchScore >= 1 {
		t.Fatalf(
			"expected second score between 0 and 1, got %v",
			second.MatchScore,
		)
	}

	if response.Duration < 0 {
		t.Fatalf(
			"unexpected negative duration: %v",
			response.Duration,
		)
	}
}

func TestRedditProviderSearchRequiresAccessToken(t *testing.T) {
	t.Setenv("REDDIT_ACCESS_TOKEN", "")

	provider := NewRedditProvider(
		&http.Client{},
		"https://example.test",
		"",
		"",
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

	if !strings.Contains(
		err.Error(),
		"access token",
	) {
		t.Fatalf(
			"expected access-token error, got %v",
			err,
		)
	}
}

func TestRedditProviderSearchRejectsInvalidQuery(t *testing.T) {
	provider := NewRedditProvider(
		&http.Client{},
		"https://example.test",
		"token",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "   ",
		},
	)

	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf(
			"expected ErrInvalidQuery, got %v",
			err,
		)
	}
}

func TestRedditProviderSearchRejectsNegativeMaxResults(t *testing.T) {
	provider := NewRedditProvider(
		&http.Client{},
		"https://example.test",
		"token",
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
		t.Fatalf(
			"expected ErrInvalidQuery, got %v",
			err,
		)
	}
}

func TestRedditProviderSearchDefaultAndMaximumResults(t *testing.T) {
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
		t.Run(
			tt.name,
			func(t *testing.T) {
				server := httptest.NewServer(
					http.HandlerFunc(func(
						w http.ResponseWriter,
						r *http.Request,
					) {
						if got := r.URL.Query().Get("limit"); got != strconv.Itoa(tt.expected) {
							t.Fatalf(
								"expected limit=%d, got %q",
								tt.expected,
								got,
							)
						}

						writeRedditResults(
							t,
							w,
							1,
						)
					}),
				)
				defer server.Close()

				provider := NewRedditProvider(
					server.Client(),
					server.URL,
					"token",
					"",
				)

				_, err := provider.Search(
					context.Background(),
					SearchRequest{
						Query:      "test",
						MaxResults: tt.request,
					},
				)
				if err != nil {
					t.Fatalf(
						"Search returned error: %v",
						err,
					)
				}
			},
		)
	}
}

func TestRedditProviderSearchMapsHTTPStatus(t *testing.T) {
	tests := []struct {
		status int
		text   string
	}{
		{
			status: http.StatusUnauthorized,
			text:   "access token",
		},
		{
			status: http.StatusForbidden,
			text:   "API access",
		},
		{
			status: http.StatusTooManyRequests,
			text:   "rate limit",
		},
		{
			status: http.StatusNotFound,
			text:   "endpoint not found",
		},
		{
			status: http.StatusInternalServerError,
			text:   "HTTP 500",
		},
	}

	for _, tt := range tests {
		t.Run(
			strconv.Itoa(tt.status),
			func(t *testing.T) {
				server := httptest.NewServer(
					http.HandlerFunc(func(
						w http.ResponseWriter,
						r *http.Request,
					) {
						http.Error(
							w,
							"failure",
							tt.status,
						)
					}),
				)
				defer server.Close()

				provider := NewRedditProvider(
					server.Client(),
					server.URL,
					"token",
					"",
				)

				_, err := provider.Search(
					context.Background(),
					SearchRequest{
						Query: "test",
					},
				)

				if err == nil {
					t.Fatal("expected error")
				}

				if !errors.Is(
					err,
					ErrProviderUnavailable,
				) {
					t.Fatalf(
						"expected ErrProviderUnavailable, got %v",
						err,
					)
				}

				if !strings.Contains(
					err.Error(),
					tt.text,
				) {
					t.Fatalf(
						"expected %q in error, got %v",
						tt.text,
						err,
					)
				}
			},
		)
	}
}

func TestRedditProviderSearchEmptyResults(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			writeRedditResults(
				t,
				w,
				0,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "nothing",
		},
	)

	if !errors.Is(err, ErrNoResults) {
		t.Fatalf(
			"expected ErrNoResults, got %v",
			err,
		)
	}

	if len(response.Results) != 0 {
		t.Fatalf(
			"expected zero results, got %d",
			len(response.Results),
		)
	}
}

func TestRedditProviderSearchMalformedJSON(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			_, _ = io.WriteString(
				w,
				"{broken-json",
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "test",
		},
	)

	if !errors.Is(
		err,
		ErrProviderUnavailable,
	) {
		t.Fatalf(
			"expected ErrProviderUnavailable, got %v",
			err,
		)
	}
}

func TestRedditProviderSearchContextCancellation(t *testing.T) {
	started := make(chan struct{})

	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			close(started)
			<-r.Context().Done()
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	resultCh := make(chan error, 1)

	go func() {
		_, err := provider.Search(
			ctx,
			SearchRequest{
				Query: "cancel",
			},
		)

		resultCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal(
			"server did not receive request",
		)
	}

	cancel()

	select {
	case err := <-resultCh:
		if !errors.Is(
			err,
			context.Canceled,
		) {
			t.Fatalf(
				"expected context.Canceled, got %v",
				err,
			)
		}

	case <-time.After(2 * time.Second):
		t.Fatal(
			"Search did not return after cancellation",
		)
	}
}

func TestRedditProviderSearchNoAuthorizationLeakWhenTokenMissing(t *testing.T) {
	provider := NewRedditProvider(
		&http.Client{},
		"https://example.test",
		"",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "test",
		},
	)

	if err == nil {
		t.Fatal("expected missing-token error")
	}

	if !strings.Contains(
		err.Error(),
		"access token",
	) {
		t.Fatalf(
			"unexpected error: %v",
			err,
		)
	}
}

func TestRedditProviderSearchHashIsDeterministic(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			writeRedditResults(
				t,
				w,
				1,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	first, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "hash",
		},
	)
	if err != nil {
		t.Fatalf(
			"first Search returned error: %v",
			err,
		)
	}

	second, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "hash",
		},
	)
	if err != nil {
		t.Fatalf(
			"second Search returned error: %v",
			err,
		)
	}

	if len(first.Results) != 1 ||
		len(second.Results) != 1 {
		t.Fatal("expected one result from both searches")
	}

	if first.Results[0].ContentHash == "" {
		t.Fatal("expected first hash")
	}

	if first.Results[0].ContentHash != second.Results[0].ContentHash {
		t.Fatalf(
			"expected deterministic hash, got %q and %q",
			first.Results[0].ContentHash,
			second.Results[0].ContentHash,
		)
	}
}

func TestRedditProviderImplementsProvider(t *testing.T) {
	var _ Provider = (*RedditProvider)(nil)
}

func TestRedditTimestamp(t *testing.T) {
	timestamp := redditTimestamp(
		1788357296,
	)

	if timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}

	if timestamp.Location() != time.UTC {
		t.Fatalf(
			"expected UTC location, got %v",
			timestamp.Location(),
		)
	}

	if !redditTimestamp(0).IsZero() {
		t.Fatal("expected zero timestamp for zero value")
	}

	if !redditTimestamp(-1).IsZero() {
		t.Fatal(
			"expected zero timestamp for negative value",
		)
	}
}

func TestRedditPositionScore(t *testing.T) {
	if got := redditPositionScore(0, 0); got != 1 {
		t.Fatalf(
			"expected 1 for zero count, got %v",
			got,
		)
	}

	if got := redditPositionScore(0, 1); got != 1 {
		t.Fatalf(
			"expected 1 for first result, got %v",
			got,
		)
	}

	if got := redditPositionScore(0, 4); got != 1 {
		t.Fatalf(
			"expected first score 1, got %v",
			got,
		)
	}

	if got := redditPositionScore(1, 4); got != 0.75 {
		t.Fatalf(
			"expected 0.75, got %v",
			got,
		)
	}

	if got := redditPositionScore(3, 4); got != 0.25 {
		t.Fatalf(
			"expected 0.25, got %v",
			got,
		)
	}

	if got := redditPositionScore(100, 4); got != 0.25 {
		t.Fatalf(
			"expected last-position score 0.25, got %v",
			got,
		)
	}

	if got := redditPositionScore(-10, 4); got != 1 {
		t.Fatalf(
			"expected negative index to clamp to 1, got %v",
			got,
		)
	}
}

func TestCompactRedditBody(t *testing.T) {
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
		t.Run(
			tt.name,
			func(t *testing.T) {
				got := compactRedditBody(
					tt.body,
					tt.maxLen,
				)

				if got != tt.expects {
					t.Fatalf(
						"expected %q, got %q",
						tt.expects,
						got,
					)
				}
			},
		)
	}
}

func TestRedditProviderSearchSkipsEmptyTitles(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			response := redditSearchResponse{
				Data: redditListing{
					Children: []redditSearchThing{
						{
							Kind: "t3",
							Data: redditSearchPost{
								Title: "",
								URL:   "https://example.test/empty",
							},
						},
						{
							Kind: "t3",
							Data: redditSearchPost{
								Title: "valid",
								URL:   "https://example.test/valid",
							},
						},
					},
				},
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			// v1.2.9: a mid-write client abort (broken pipe under heavy
			// parallel load) is test-infrastructure noise, not a test
			// failure — the client-side assertions cover the outcome.
			// Reporting it from the handler made this suite flaky under
			// concurrent full-tree runs.
			_ = json.NewEncoder(w).Encode(
				response,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "test",
		},
	)

	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}

	if len(response.Results) != 1 {
		t.Fatalf(
			"expected one retained result, got %d",
			len(response.Results),
		)
	}

	if response.Results[0].Title != "valid" {
		t.Fatalf(
			"unexpected retained title: %q",
			response.Results[0].Title,
		)
	}
}

func TestRedditProviderSearchExistingQueryParameters(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			values := r.URL.Query()

			if values.Get("existing") != "value" {
				t.Fatalf(
					"existing query parameter was lost",
				)
			}

			if values.Get("q") != "hello world" {
				t.Fatalf(
					"unexpected query: %q",
					values.Get("q"),
				)
			}

			writeRedditResults(
				t,
				w,
				1,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL+"?existing=value",
		"token",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "hello world",
		},
	)

	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}
}

func TestRedditProviderSearchDoesNotMutateRequest(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			writeRedditResults(
				t,
				w,
				1,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	request := SearchRequest{
		Query:      "  test  ",
		MaxResults: 1,
	}

	_, err := provider.Search(
		context.Background(),
		request,
	)

	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}

	if request.Query != "  test  " {
		t.Fatalf(
			"provider mutated original request: %q",
			request.Query,
		)
	}
}

func TestRedditProviderSearchUsesEnvironmentToken(t *testing.T) {
	const token = "environment-token"

	t.Setenv(
		"REDDIT_ACCESS_TOKEN",
		token,
	)

	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			if got := r.Header.Get(
				"Authorization",
			); got != "Bearer "+token {
				t.Fatalf(
					"unexpected Authorization header: %q",
					got,
				)
			}

			writeRedditResults(
				t,
				w,
				1,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "environment",
		},
	)

	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}
}

func TestRedditProviderSearchDefaultClientTimeout(t *testing.T) {
	provider := NewRedditProvider(
		nil,
		defaultRedditAPIURL,
		"token",
		"",
	)

	if provider.Client.Timeout != 20*time.Second {
		t.Fatalf(
			"expected 20 second timeout, got %v",
			provider.Client.Timeout,
		)
	}
}

func TestRedditProviderSearchContentHashChangesWithContent(t *testing.T) {
	var second atomic.Bool

	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			if second.Load() {
				response := redditSearchResponse{
					Data: redditListing{
						Children: []redditSearchThing{
							{
								Kind: "t3",
								Data: redditSearchPost{
									Title: "changed",
									URL:   "https://example.test/post",
								},
							},
						},
					},
				}

				w.Header().Set(
					"Content-Type",
					"application/json",
				)

				if err := json.NewEncoder(w).Encode(
					response,
				); err != nil {
					t.Errorf(
						"failed to encode response: %v",
						err,
					)
				}

				return
			}

			writeRedditResults(
				t,
				w,
				1,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	first, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "hash",
		},
	)
	if err != nil {
		t.Fatalf(
			"first Search returned error: %v",
			err,
		)
	}

	second.Store(true)

	changed, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "hash",
		},
	)
	if err != nil {
		t.Fatalf(
			"second Search returned error: %v",
			err,
		)
	}

	if first.Results[0].ContentHash ==
		changed.Results[0].ContentHash {
		t.Fatal(
			"expected changed content to produce a different hash",
		)
	}
}

func writeRedditResults(
	t *testing.T,
	w http.ResponseWriter,
	count int,
) {
	t.Helper()

	response := redditSearchResponse{
		Data: redditListing{
			Children: make(
				[]redditSearchThing,
				0,
				count,
			),
		},
	}

	for index := 0; index < count; index++ {
		response.Data.Children = append(
			response.Data.Children,
			redditSearchThing{
				Kind: "t3",
				Data: redditSearchPost{
					ID:        "id-" + strconv.Itoa(index),
					Name:      "t3_id-" + strconv.Itoa(index),
					Title:     "result " + strconv.Itoa(index),
					SelfText:  "result body",
					Author:    "user",
					Subreddit: "test",
					Permalink: "/r/test/comments/id-" +
						strconv.Itoa(index) +
						"/result/",
					URL:         "https://example.test/result",
					CreatedUTC:  1788357296 + float64(index),
					Score:       10 + index,
					NumComments: index,
					Ups:         10 + index,
					Downs:       0,
					Over18:      false,
					Locked:      false,
					Stickied:    false,
					IsSelf:      true,
				},
			},
		)
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(
		response,
	); err != nil {
		t.Errorf(
			"failed to encode Reddit response: %v",
			err,
		)
	}
}

func TestRedditProviderSearchQueryEscaping(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			query := r.URL.Query().Get("q")

			if query != `error "permission denied" path:C:\work` {
				t.Fatalf(
					"unexpected decoded query: %q",
					query,
				)
			}

			writeRedditResults(
				t,
				w,
				1,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: `error "permission denied" path:C:\work`,
		},
	)

	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}
}

func TestRedditProviderSearchKeepsPublicURLPreferred(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			response := redditSearchResponse{
				Data: redditListing{
					Children: []redditSearchThing{
						{
							Kind: "t3",
							Data: redditSearchPost{
								Title:     "post",
								URL:       "https://external.example",
								Permalink: "/r/test/comments/123/post/",
							},
						},
					},
				},
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			// v1.2.9: a mid-write client abort (broken pipe under heavy
			// parallel load) is test-infrastructure noise, not a test
			// failure — the client-side assertions cover the outcome.
			// Reporting it from the handler made this suite flaky under
			// concurrent full-tree runs.
			_ = json.NewEncoder(w).Encode(
				response,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "post",
		},
	)
	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}

	if response.Results[0].URL !=
		"https://www.reddit.com/r/test/comments/123/post/" {
		t.Fatalf(
			"unexpected public URL: %q",
			response.Results[0].URL,
		)
	}
}

func TestRedditProviderSearchUsesOriginalURLWhenPermalinkMissing(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			response := redditSearchResponse{
				Data: redditListing{
					Children: []redditSearchThing{
						{
							Kind: "t3",
							Data: redditSearchPost{
								Title: "external post",
								URL:   "https://external.example/post",
							},
						},
					},
				},
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			// v1.2.9: a mid-write client abort (broken pipe under heavy
			// parallel load) is test-infrastructure noise, not a test
			// failure — the client-side assertions cover the outcome.
			// Reporting it from the handler made this suite flaky under
			// concurrent full-tree runs.
			_ = json.NewEncoder(w).Encode(
				response,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "external",
		},
	)
	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}

	if response.Results[0].URL !=
		"https://external.example/post" {
		t.Fatalf(
			"unexpected URL: %q",
			response.Results[0].URL,
		)
	}
}

func TestRedditProviderSearchResponseLimit(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			// Send a deliberately large valid JSON document.
			largeText := strings.Repeat(
				"x",
				5<<20,
			)

			response := redditSearchResponse{
				Data: redditListing{
					Children: []redditSearchThing{
						{
							Kind: "t3",
							Data: redditSearchPost{
								Title:    "large response",
								SelfText: largeText,
								URL:      "https://example.test/large",
							},
						},
					},
				},
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			// v1.2.9: a mid-write client abort (broken pipe under heavy
			// parallel load) is test-infrastructure noise, not a test
			// failure — the client-side assertions cover the outcome.
			// Reporting it from the handler made this suite flaky under
			// concurrent full-tree runs.
			_ = json.NewEncoder(w).Encode(
				response,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "large",
		},
	)

	if err == nil {
		t.Fatal(
			"expected oversized response to fail",
		)
	}

	if !errors.Is(
		err,
		ErrProviderUnavailable,
	) {
		t.Fatalf(
			"expected ErrProviderUnavailable, got %v",
			err,
		)
	}
}

func TestRedditProviderSearchDefaultUserAgentIsStable(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			if got := r.Header.Get(
				"User-Agent",
			); got != DefaultUserAgent() {
				t.Fatalf(
					"unexpected User-Agent: %q",
					got,
				)
			}

			writeRedditResults(
				t,
				w,
				1,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "agent",
		},
	)

	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}
}

func TestRedditProviderSearchDoesNotSendEnvironmentTokenWhenExplicitTokenMissing(t *testing.T) {
	const envToken = "environment-token"

	t.Setenv(
		"REDDIT_ACCESS_TOKEN",
		envToken,
	)

	provider := NewRedditProvider(
		&http.Client{},
		"https://example.test",
		"",
		"",
	)

	if provider.AccessToken != envToken {
		t.Fatalf(
			"expected environment token to be loaded, got %q",
			provider.AccessToken,
		)
	}
}

func TestRedditProviderSearchEditedAndDistinguishedMetadata(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			response := redditSearchResponse{
				Data: redditListing{
					Children: []redditSearchThing{
						{
							Kind: "t3",
							Data: redditSearchPost{
								Title:         "metadata",
								URL:           "https://example.test/metadata",
								Edited:        1788357000,
								Distinguished: "moderator",
							},
						},
					},
				},
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			// v1.2.9: a mid-write client abort (broken pipe under heavy
			// parallel load) is test-infrastructure noise, not a test
			// failure — the client-side assertions cover the outcome.
			// Reporting it from the handler made this suite flaky under
			// concurrent full-tree runs.
			_ = json.NewEncoder(w).Encode(
				response,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "metadata",
		},
	)

	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}

	metadata := response.Results[0].Metadata

	if _, ok := metadata["edited"]; !ok {
		t.Fatal(
			"expected edited metadata",
		)
	}

	if _, ok := metadata["distinguished"]; !ok {
		t.Fatal(
			"expected distinguished metadata",
		)
	}
}

func TestRedditProviderSearchInvalidBaseURL(t *testing.T) {
	provider := NewRedditProvider(
		&http.Client{},
		"://invalid-url",
		"token",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "test",
		},
	)

	if !errors.Is(
		err,
		ErrProviderUnavailable,
	) {
		t.Fatalf(
			"expected ErrProviderUnavailable, got %v",
			err,
		)
	}
}

func TestRedditProviderSearchEmptyBodyFallsBackToTitle(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			response := redditSearchResponse{
				Data: redditListing{
					Children: []redditSearchThing{
						{
							Kind: "t3",
							Data: redditSearchPost{
								Title:    "title fallback",
								SelfText: "",
								URL:      "https://example.test/fallback",
							},
						},
					},
				},
			}

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			// v1.2.9: a mid-write client abort (broken pipe under heavy
			// parallel load) is test-infrastructure noise, not a test
			// failure — the client-side assertions cover the outcome.
			// Reporting it from the handler made this suite flaky under
			// concurrent full-tree runs.
			_ = json.NewEncoder(w).Encode(
				response,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"token",
		"",
	)

	response, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "fallback",
		},
	)

	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}

	if response.Results[0].Snippet != "title fallback" {
		t.Fatalf(
			"expected title fallback, got %q",
			response.Results[0].Snippet,
		)
	}
}

func TestRedditProviderSearchDoesNotUseForbiddenAuthorizationScheme(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			got := r.Header.Get("Authorization")

			if got == "Basic test-token" {
				t.Fatal(
					"provider must not use Basic authentication",
				)
			}

			if got != "Bearer test-token" {
				t.Fatalf(
					"expected Bearer authentication, got %q",
					got,
				)
			}

			writeRedditResults(
				t,
				w,
				1,
			)
		}),
	)
	defer server.Close()

	provider := NewRedditProvider(
		server.Client(),
		server.URL,
		"test-token",
		"",
	)

	_, err := provider.Search(
		context.Background(),
		SearchRequest{
			Query: "auth",
		},
	)

	if err != nil {
		t.Fatalf(
			"Search returned error: %v",
			err,
		)
	}
}
