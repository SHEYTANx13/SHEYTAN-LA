package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGitHubAPIURL = "https://api.github.com"
	maxGitHubBodyBytes  = 4 << 20
)

// GitHubProvider researches public GitHub issues and pull requests.
type GitHubProvider struct {
	Client  *http.Client
	BaseURL string
	Token   string
}

// NewGitHubProvider constructs a GitHub research provider.
//
// A token is optional. When present, it is read from GITHUB_TOKEN unless an
// explicit token is supplied by the caller.
func NewGitHubProvider(client *http.Client, baseURL, token string) *GitHubProvider {
	if client == nil {
		client = &http.Client{
			Timeout: 20 * time.Second,
		}
	}

	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultGitHubAPIURL
	}

	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}

	return &GitHubProvider{
		Client:  client,
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
	}
}

func (p *GitHubProvider) Name() string {
	return "github"
}

type githubSearchResponse struct {
	TotalCount int                `json:"total_count"`
	Items      []githubSearchItem `json:"items"`
}

type githubSearchItem struct {
	Title     string `json:"title"`
	HTMLURL   string `json:"html_url"`
	Body      string `json:"body"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	User struct {
		Login string `json:"login"`
	} `json:"user"`

	RepositoryURL string  `json:"repository_url"`
	Comments      int     `json:"comments"`
	Score         float64 `json:"score"`

	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request,omitempty"`
}

// Search executes a bounded GitHub issue/PR search.
//
// GitHub's /search/issues endpoint covers both issues and pull requests,
// making it a useful first research layer for:
//   - exact error messages
//   - package/module names
//   - symbols
//   - regressions
//   - maintainer responses
//   - known workarounds
func (p *GitHubProvider) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResponse, error) {
	if err := req.Validate(); err != nil {
		return SearchResponse{}, err
	}

	req = req.Normalize()

	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 8
	}

	if maxResults > 50 {
		maxResults = 50
	}

	started := time.Now()

	endpoint, err := githubSearchEndpoint(p.BaseURL)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: invalid GitHub endpoint: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	query := endpoint.Query()
	query.Set("q", req.Query)
	query.Set("per_page", strconv.Itoa(maxResults))
	query.Set("page", "1")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint.String(),
		nil,
	)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"create GitHub request: %w",
			err,
		)
	}

	request.Header.Set(
		"Accept",
		"application/vnd.github+json",
	)
	request.Header.Set(
		"User-Agent",
		DefaultUserAgent(),
	)
	request.Header.Set(
		"X-GitHub-Api-Version",
		"2022-11-28",
	)

	if p.Token != "" {
		request.Header.Set(
			"Authorization",
			"Bearer "+p.Token,
		)
	}

	response, err := p.Client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return SearchResponse{}, ctx.Err()
		}

		return SearchResponse{}, fmt.Errorf(
			"%w: GitHub request failed: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return SearchResponse{}, githubHTTPError(response)
	}

	body, err := io.ReadAll(
		io.LimitReader(
			response.Body,
			maxGitHubBodyBytes+1,
		),
	)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: read GitHub response: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	if len(body) > maxGitHubBodyBytes {
		return SearchResponse{}, fmt.Errorf(
			"%w: GitHub response exceeds %d bytes",
			ErrProviderUnavailable,
			maxGitHubBodyBytes,
		)
	}

	var payload githubSearchResponse

	if err := json.Unmarshal(body, &payload); err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: invalid GitHub response: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	results := make(
		[]Result,
		0,
		len(payload.Items),
	)

	for _, item := range payload.Items {
		if strings.TrimSpace(item.HTMLURL) == "" {
			continue
		}

		publishedAt := parseGitHubTime(
			item.CreatedAt,
		)

		authority := AuthorityCommunity

		if item.User.Login != "" {
			// GitHub search results originate from project-hosted discussions.
			// Maintainer/project-source detection is refined later when the
			// repository-specific metadata provider is implemented.
			authority = AuthorityTechnicalDiscussion
		}

		snippet := compactGitHubBody(
			item.Body,
			700,
		)

		matchScore := normalizeGitHubScore(
			item.Score,
		)

		hash := sha256.Sum256(
			[]byte(
				strings.Join(
					[]string{
						item.Title,
						item.HTMLURL,
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
					Title:       item.Title,
					URL:         item.HTMLURL,
					Snippet:     snippet,
					Source:      "GitHub",
					Provider:    p.Name(),
					PublishedAt: publishedAt,
					Authority:   authority,
					MatchScore:  matchScore,
					ContentHash: hex.EncodeToString(
						hash[:],
					),
					Metadata: map[string]any{
						"state":            item.State,
						"author":           item.User.Login,
						"comments":         item.Comments,
						"repository":       item.RepositoryURL,
						"is_pull_request":  item.PullRequest != nil,
						"created_at":       item.CreatedAt,
						"updated_at":       item.UpdatedAt,
						"github_score_raw": item.Score,
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

func githubSearchEndpoint(
	baseURL string,
) (*url.URL, error) {
	baseURL = strings.TrimSpace(baseURL)

	if baseURL == "" {
		baseURL = defaultGitHubAPIURL
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	if base.Scheme == "" ||
		base.Host == "" {
		return nil, fmt.Errorf(
			"GitHub base URL must include scheme and host",
		)
	}

	base.Path = path.Join(
		base.Path,
		"search/issues",
	)
	base.Fragment = ""

	return base, nil
}

func normalizeGitHubScore(
	score float64,
) float64 {
	if math.IsNaN(score) ||
		math.IsInf(score, 0) ||
		score <= 0 {
		return 0
	}

	// GitHub's score is an opaque ranking value rather than a [0,1]
	// probability. Convert it monotonically without pretending that every
	// score >= 1 means identical relevance.
	normalized := score / (score + 1)

	if normalized < 0 {
		return 0
	}

	if normalized > 1 {
		return 1
	}

	return normalized
}

func parseGitHubTime(
	value string,
) time.Time {
	value = strings.TrimSpace(value)

	if value == "" {
		return time.Time{}
	}

	t, err := time.Parse(
		time.RFC3339,
		value,
	)
	if err != nil {
		return time.Time{}
	}

	return t
}

func compactGitHubBody(
	body string,
	maxLen int,
) string {
	body = strings.TrimSpace(body)

	if body == "" {
		return ""
	}

	// Collapse common whitespace without attempting to parse Markdown.
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

func githubHTTPError(
	response *http.Response,
) error {
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf(
			"%w: GitHub rejected authentication",
			ErrProviderUnavailable,
		)

	case http.StatusForbidden:
		return fmt.Errorf(
			"%w: GitHub rate limit or access policy rejected the request",
			ErrProviderUnavailable,
		)

	case http.StatusNotFound:
		return fmt.Errorf(
			"%w: GitHub endpoint not found",
			ErrProviderUnavailable,
		)

	default:
		return fmt.Errorf(
			"%w: GitHub returned HTTP %d",
			ErrProviderUnavailable,
			response.StatusCode,
		)
	}
}
