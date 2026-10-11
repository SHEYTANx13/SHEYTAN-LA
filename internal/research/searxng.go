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
	"path"
	"strconv"
	"strings"
	"time"
)

const maxSearXNGBodyBytes = 4 << 20

// SearXNGProvider researches the public web through a configured SearXNG
// instance. A self-hosted/local instance is fully supported.
type SearXNGProvider struct {
	Client  *http.Client
	BaseURL string
}

// NewSearXNGProvider constructs a SearXNG research provider.
//
// BaseURL should point at the root URL of a SearXNG instance, for example:
//
//	https://search.example.org
//
// The provider deliberately does not require authentication because SearXNG
// instances commonly expose their public search endpoint without an API key.
func NewSearXNGProvider(
	client *http.Client,
	baseURL string,
) *SearXNGProvider {
	if client == nil {
		client = &http.Client{
			Timeout: 20 * time.Second,
		}
	}

	baseURL = strings.TrimRight(
		strings.TrimSpace(baseURL),
		"/",
	)

	return &SearXNGProvider{
		Client:  client,
		BaseURL: baseURL,
	}
}

func (p *SearXNGProvider) Name() string {
	return BackendSearXNG
}

type searxngSearchResponse struct {
	Query           string              `json:"query"`
	NumberOfResults int                 `json:"number_of_results"`
	Results         []searxngSearchItem `json:"results"`
}

type searxngSearchItem struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Content       string   `json:"content"`
	Engine        string   `json:"engine"`
	Engines       []string `json:"engines"`
	Category      string   `json:"category"`
	PublishedDate string   `json:"publishedDate"`
	Score         float64  `json:"score"`
	Template      string   `json:"template"`
}

// Search executes a bounded SearXNG web search.
func (p *SearXNGProvider) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResponse, error) {
	if err := req.Validate(); err != nil {
		return SearchResponse{}, err
	}

	req = req.Normalize()

	if strings.TrimSpace(p.BaseURL) == "" {
		return SearchResponse{}, fmt.Errorf(
			"%w: SearXNG base URL is not configured",
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

	endpoint, err := searxngSearchEndpoint(p.BaseURL)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: invalid SearXNG endpoint: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	query := endpoint.Query()
	query.Set("q", req.Query)
	query.Set("format", "json")
	query.Set("categories", "general")
	query.Set("pageno", "1")
	query.Set("safesearch", "0")
	query.Set("language", "all")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint.String(),
		nil,
	)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"create SearXNG request: %w",
			err,
		)
	}

	request.Header.Set(
		"Accept",
		"application/json",
	)

	request.Header.Set(
		"User-Agent",
		DefaultUserAgent(),
	)

	response, err := p.Client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return SearchResponse{}, ctx.Err()
		}

		return SearchResponse{}, fmt.Errorf(
			"%w: SearXNG request failed: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return SearchResponse{}, fmt.Errorf(
			"%w: SearXNG returned HTTP %d",
			ErrProviderUnavailable,
			response.StatusCode,
		)
	}

	body, err := io.ReadAll(
		io.LimitReader(
			response.Body,
			maxSearXNGBodyBytes+1,
		),
	)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: read SearXNG response: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	if len(body) > maxSearXNGBodyBytes {
		return SearchResponse{}, fmt.Errorf(
			"%w: SearXNG response exceeds %d bytes",
			ErrProviderUnavailable,
			maxSearXNGBodyBytes,
		)
	}

	var payload searxngSearchResponse

	if err := json.Unmarshal(body, &payload); err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: invalid SearXNG JSON response: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	results := make(
		[]Result,
		0,
		minInt(maxResults, len(payload.Results)),
	)

	for index, item := range payload.Results {
		item.Title = strings.TrimSpace(item.Title)
		item.URL = strings.TrimSpace(item.URL)
		item.Content = strings.TrimSpace(item.Content)

		if item.Title == "" || item.URL == "" {
			continue
		}

		matchScore := normalizeSearXNGScore(
			item.Score,
			index,
			len(payload.Results),
		)

		publishedAt := parseSearXNGTime(
			item.PublishedDate,
		)

		source := "SearXNG"

		if item.Engine != "" {
			source = "SearXNG/" + strings.TrimSpace(item.Engine)
		}

		engine := strings.TrimSpace(item.Engine)

		if engine == "" && len(item.Engines) > 0 {
			engine = strings.TrimSpace(item.Engines[0])
		}

		authority := searxngAuthority(
			engine,
			item.URL,
		)

		snippet := compactSearXNGContent(
			item.Content,
			700,
		)

		hash := sha256.Sum256(
			[]byte(
				strings.Join(
					[]string{
						item.Title,
						item.URL,
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
					URL:         item.URL,
					Snippet:     snippet,
					Source:      source,
					Provider:    p.Name(),
					PublishedAt: publishedAt,
					Authority:   authority,
					MatchScore:  matchScore,
					ContentHash: hex.EncodeToString(hash[:]),
					Metadata: map[string]any{
						"engine":          item.Engine,
						"engines":         item.Engines,
						"category":        item.Category,
						"template":        item.Template,
						"score_raw":       item.Score,
						"search_position": index,
					},
				},
				p.Name(),
			),
		)
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	responseResult := SearchResponse{
		Provider: p.Name(),
		Query:    req.Query,
		Results:  results,
		Duration: time.Since(started),
	}

	responseResult = NormalizeResponse(responseResult)

	if err := responseResult.Validate(); err != nil {
		return responseResult, err
	}

	return responseResult, nil
}

func searxngSearchEndpoint(
	baseURL string,
) (*url.URL, error) {
	baseURL = strings.TrimSpace(baseURL)

	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	if base.Scheme == "" ||
		base.Host == "" {
		return nil, fmt.Errorf(
			"SearXNG base URL must include scheme and host",
		)
	}

	base.Path = path.Join(
		base.Path,
		"search",
	)

	base.RawQuery = ""
	base.Fragment = ""

	return base, nil
}

func normalizeSearXNGScore(
	score float64,
	index int,
	count int,
) float64 {
	if score > 0 {
		// SearXNG scores are instance/engine dependent rather than a stable
		// probability. Convert positive values monotonically into [0,1].
		normalized := score / (score + 1)

		if normalized < 0 {
			return 0
		}

		if normalized > 1 {
			return 1
		}

		return normalized
	}

	// Some engines/instances omit scores. Preserve the provider's returned
	// ordering as a conservative fallback rather than pretending there was a
	// measured relevance score.
	if count <= 1 {
		return 1
	}

	if index < 0 {
		index = 0
	}

	if index >= count {
		index = count - 1
	}

	fallback := 1 -
		(float64(index) /
			float64(count))

	if fallback < 0 {
		return 0
	}

	if fallback > 1 {
		return 1
	}

	return fallback * 0.5
}

func searxngAuthority(
	engine string,
	rawURL string,
) Authority {
	engine = strings.ToLower(
		strings.TrimSpace(engine),
	)

	rawURL = strings.ToLower(
		strings.TrimSpace(rawURL),
	)

	// Search engine metadata is only a ranking hint. Do not classify a result
	// as official merely because it came through SearXNG.
	switch engine {
	case "github":
		return AuthorityProjectSource

	case "stackoverflow":
		return AuthorityTechnicalDiscussion

	case "arxiv",
		"pubmed",
		"wikipedia":
		return AuthorityTechnicalDiscussion
	}

	if strings.Contains(rawURL, ".gov/") ||
		strings.Contains(rawURL, ".gov.") {
		return AuthorityOfficial
	}

	if strings.Contains(rawURL, ".edu/") ||
		strings.Contains(rawURL, ".edu.") {
		return AuthorityTechnicalDiscussion
	}

	return AuthorityCommunity
}

func parseSearXNGTime(
	value string,
) time.Time {
	value = strings.TrimSpace(value)

	if value == "" {
		return time.Time{}
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if parsed, err := time.Parse(
			layout,
			value,
		); err == nil {
			return parsed
		}
	}

	return time.Time{}
}

func compactSearXNGContent(
	content string,
	maxLen int,
) string {
	content = strings.TrimSpace(content)

	if content == "" {
		return ""
	}

	content = strings.Join(
		strings.Fields(content),
		" ",
	)

	if maxLen <= 0 ||
		len(content) <= maxLen {
		return content
	}

	return content[:maxLen] + "…"
}

func searxngMaxResults(
	value string,
) int {
	value = strings.TrimSpace(value)

	if value == "" {
		return 0
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0
	}

	if parsed > 50 {
		return 50
	}

	return parsed
}
