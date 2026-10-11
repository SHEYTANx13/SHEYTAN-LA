package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const defaultRedditAPIURL = "https://oauth.reddit.com"

// RedditProvider researches public Reddit posts through Reddit's OAuth API.
//
// The access token is optional at construction time so the provider can be
// configured before credentials are supplied. Search itself requires a token.
type RedditProvider struct {
	Client      *http.Client
	BaseURL     string
	AccessToken string
	UserAgent   string
}

// NewRedditProvider constructs a Reddit research provider.
//
// When accessToken is empty, REDDIT_ACCESS_TOKEN is read from the environment.
func NewRedditProvider(
	client *http.Client,
	baseURL string,
	accessToken string,
	userAgent string,
) *RedditProvider {
	if client == nil {
		client = &http.Client{
			Timeout: 20 * time.Second,
		}
	}

	baseURL = strings.TrimRight(
		strings.TrimSpace(baseURL),
		"/",
	)
	if baseURL == "" {
		baseURL = defaultRedditAPIURL
	}

	if accessToken == "" {
		accessToken = strings.TrimSpace(
			os.Getenv("REDDIT_ACCESS_TOKEN"),
		)
	}

	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		userAgent = DefaultUserAgent()
	}

	return &RedditProvider{
		Client:      client,
		BaseURL:     baseURL,
		AccessToken: accessToken,
		UserAgent:   userAgent,
	}
}

func (p *RedditProvider) Name() string {
	return BackendReddit
}

type redditSearchResponse struct {
	Data redditListing `json:"data"`
}

type redditListing struct {
	After    string              `json:"after"`
	Before   string              `json:"before"`
	Children []redditSearchThing `json:"children"`
}

type redditSearchThing struct {
	Kind string           `json:"kind"`
	Data redditSearchPost `json:"data"`
}

type redditSearchPost struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Title         string  `json:"title"`
	SelfText      string  `json:"selftext"`
	Author        string  `json:"author"`
	Subreddit     string  `json:"subreddit"`
	Permalink     string  `json:"permalink"`
	URL           string  `json:"url"`
	CreatedUTC    float64 `json:"created_utc"`
	Edited        any     `json:"edited"`
	Score         int     `json:"score"`
	NumComments   int     `json:"num_comments"`
	Ups           int     `json:"ups"`
	Downs         int     `json:"downs"`
	Over18        bool    `json:"over_18"`
	Locked        bool    `json:"locked"`
	Stickied      bool    `json:"stickied"`
	Distinguished any     `json:"distinguished"`
	IsSelf        bool    `json:"is_self"`
}

// Search executes a bounded Reddit post search.
//
// Reddit search is intentionally limited to posts here. Searching comment
// bodies requires a different API surface and should be implemented as a
// separate provider capability rather than silently assuming post search
// covers comments.
func (p *RedditProvider) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResponse, error) {
	if err := req.Validate(); err != nil {
		return SearchResponse{}, err
	}

	req = req.Normalize()

	if strings.TrimSpace(p.AccessToken) == "" {
		return SearchResponse{}, fmt.Errorf(
			"%w: Reddit access token is not configured",
			ErrProviderUnavailable,
		)
	}

	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 8
	}

	if maxResults > 50 {
		maxResults = 50
	}

	started := time.Now()

	endpoint, err := url.Parse(
		p.BaseURL,
	)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: invalid Reddit endpoint: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	if endpoint.Scheme == "" || endpoint.Host == "" {
		return SearchResponse{}, fmt.Errorf(
			"%w: Reddit base URL must include scheme and host",
			ErrProviderUnavailable,
		)
	}

	endpoint.Path = path.Join(
		endpoint.Path,
		"search",
	)
	endpoint.Fragment = ""

	query := endpoint.Query()
	query.Set("q", req.Query)
	query.Set("limit", strconv.Itoa(maxResults))
	query.Set("sort", "relevance")
	query.Set("t", "all")
	query.Set("type", "link")
	query.Set("raw_json", "1")
	query.Set("include_facets", "false")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint.String(),
		nil,
	)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"create Reddit request: %w",
			err,
		)
	}

	request.Header.Set(
		"Accept",
		"application/json",
	)

	request.Header.Set(
		"Authorization",
		"Bearer "+p.AccessToken,
	)

	request.Header.Set(
		"User-Agent",
		p.UserAgent,
	)

	response, err := p.Client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return SearchResponse{}, ctx.Err()
		}

		return SearchResponse{}, fmt.Errorf(
			"%w: Reddit request failed: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return SearchResponse{}, redditHTTPError(response)
	}

	// v1.1.4: +1 byte so an oversized payload is DETECTED instead of
	// silently truncated mid-JSON (the other providers already did this).
	const maxResponseBytes = 4 << 20

	limitedBody := io.LimitReader(
		response.Body,
		maxResponseBytes+1,
	)

	var payload redditSearchResponse

	if err := json.NewDecoder(limitedBody).Decode(&payload); err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: invalid Reddit response: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	results := make(
		[]Result,
		0,
		len(payload.Data.Children),
	)

	for index, child := range payload.Data.Children {
		if strings.TrimSpace(child.Data.Title) == "" {
			continue
		}

		permalink := strings.TrimSpace(
			child.Data.Permalink,
		)

		publicURL := strings.TrimSpace(
			child.Data.URL,
		)

		if permalink != "" {
			publicURL = "https://www.reddit.com" + permalink
		}

		if publicURL == "" {
			continue
		}

		snippet := compactRedditBody(
			child.Data.SelfText,
			700,
		)

		if snippet == "" {
			snippet = compactRedditBody(
				child.Data.Title,
				700,
			)
		}

		publishedAt := redditTimestamp(
			child.Data.CreatedUTC,
		)

		matchScore := redditPositionScore(
			index,
			len(payload.Data.Children),
		)

		hash := sha256.Sum256(
			[]byte(
				strings.Join(
					[]string{
						child.Data.Title,
						publicURL,
						snippet,
					},
					"\x00",
				),
			),
		)

		results = append(
			results,
			NormalizeResult(
				Result{
					Title:       child.Data.Title,
					URL:         publicURL,
					Snippet:     snippet,
					Source:      "Reddit",
					Provider:    p.Name(),
					PublishedAt: publishedAt,
					Authority:   AuthorityCommunity,
					MatchScore:  matchScore,
					ContentHash: hex.EncodeToString(
						hash[:],
					),
					Metadata: map[string]any{
						"id":              child.Data.ID,
						"name":            child.Data.Name,
						"author":          child.Data.Author,
						"subreddit":       child.Data.Subreddit,
						"score":           child.Data.Score,
						"ups":             child.Data.Ups,
						"downs":           child.Data.Downs,
						"num_comments":    child.Data.NumComments,
						"is_self":         child.Data.IsSelf,
						"over_18":         child.Data.Over18,
						"locked":          child.Data.Locked,
						"stickied":        child.Data.Stickied,
						"distinguished":   child.Data.Distinguished,
						"edited":          child.Data.Edited,
						"search_position": index,
					},
				},
				p.Name(),
			),
		)
	}

	responseResult := SearchResponse{
		Provider: p.Name(),
		Query:    req.Query,
		Results:  results,
		Duration: time.Since(started),
	}

	responseResult = NormalizeResponse(
		responseResult,
	)

	if err := responseResult.Validate(); err != nil {
		return responseResult, err
	}

	return responseResult, nil
}

func redditTimestamp(value float64) time.Time {
	if value <= 0 {
		return time.Time{}
	}

	seconds := int64(value)
	nanos := int64(
		(value - float64(seconds)) * 1e9,
	)

	return time.Unix(
		seconds,
		nanos,
	).UTC()
}

func redditPositionScore(
	index int,
	count int,
) float64 {
	if count <= 1 {
		return 1
	}

	if index < 0 {
		index = 0
	}

	if index >= count {
		index = count - 1
	}

	score := 1 -
		(float64(index) /
			float64(count))

	if score < 0 {
		score = 0
	}

	if score > 1 {
		score = 1
	}

	return score
}

func compactRedditBody(
	body string,
	maxLen int,
) string {
	body = strings.TrimSpace(body)

	if body == "" {
		return ""
	}

	body = strings.Join(
		strings.Fields(body),
		" ",
	)

	if maxLen <= 0 ||
		len(body) <= maxLen {
		return body
	}

	return body[:maxLen] + "…"
}

func redditHTTPError(
	response *http.Response,
) error {
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf(
			"%w: Reddit rejected the access token",
			ErrProviderUnavailable,
		)

	case http.StatusForbidden:
		return fmt.Errorf(
			"%w: Reddit denied API access",
			ErrProviderUnavailable,
		)

	case http.StatusTooManyRequests:
		return fmt.Errorf(
			"%w: Reddit rate limit exceeded",
			ErrProviderUnavailable,
		)

	case http.StatusNotFound:
		return fmt.Errorf(
			"%w: Reddit endpoint not found",
			ErrProviderUnavailable,
		)

	default:
		return fmt.Errorf(
			"%w: Reddit returned HTTP %d",
			ErrProviderUnavailable,
			response.StatusCode,
		)
	}
}
