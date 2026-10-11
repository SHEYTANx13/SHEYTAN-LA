package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	defaultDuckDuckGoURL   = "https://html.duckduckgo.com"
	maxDuckDuckGoBodyBytes = 4 << 20
)

// DuckDuckGoProvider researches the public web through DuckDuckGo's
// non-JavaScript HTML search surface.
//
// No API key is required.
type DuckDuckGoProvider struct {
	Client  *http.Client
	BaseURL string
}

// NewDuckDuckGoProvider constructs a DuckDuckGo research provider.
func NewDuckDuckGoProvider(
	client *http.Client,
	baseURL string,
) *DuckDuckGoProvider {
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
		baseURL = defaultDuckDuckGoURL
	}

	return &DuckDuckGoProvider{
		Client:  client,
		BaseURL: baseURL,
	}
}

func (p *DuckDuckGoProvider) Name() string {
	return BackendDuckDuckGo
}

// Search executes a bounded DuckDuckGo HTML search.
func (p *DuckDuckGoProvider) Search(
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

	endpoint, err := duckDuckGoSearchEndpoint(
		p.BaseURL,
	)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: invalid DuckDuckGo endpoint: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	query := endpoint.Query()
	query.Set("q", req.Query)
	query.Set("kl", "wt-wt")
	query.Set("kp", "-2")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint.String(),
		nil,
	)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"create DuckDuckGo request: %w",
			err,
		)
	}

	request.Header.Set(
		"Accept",
		"text/html,application/xhtml+xml",
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
			"%w: DuckDuckGo request failed: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return SearchResponse{}, fmt.Errorf(
			"%w: DuckDuckGo returned HTTP %d",
			ErrProviderUnavailable,
			response.StatusCode,
		)
	}

	body, err := io.ReadAll(
		io.LimitReader(
			response.Body,
			maxDuckDuckGoBodyBytes+1,
		),
	)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: read DuckDuckGo response: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	if len(body) > maxDuckDuckGoBodyBytes {
		return SearchResponse{}, fmt.Errorf(
			"%w: DuckDuckGo response exceeds %d bytes",
			ErrProviderUnavailable,
			maxDuckDuckGoBodyBytes,
		)
	}

	items, err := parseDuckDuckGoHTML(body)
	if err != nil {
		return SearchResponse{}, fmt.Errorf(
			"%w: parse DuckDuckGo response: %v",
			ErrProviderUnavailable,
			err,
		)
	}

	results := make(
		[]Result,
		0,
		minInt(maxResults, len(items)),
	)

	started := time.Now()

	for index, item := range items {
		if strings.TrimSpace(item.Title) == "" ||
			strings.TrimSpace(item.URL) == "" {
			continue
		}

		item.Title = strings.TrimSpace(item.Title)
		item.URL = strings.TrimSpace(item.URL)
		item.Snippet = strings.TrimSpace(item.Snippet)

		matchScore := duckDuckGoPositionScore(
			index,
			len(items),
		)

		hash := sha256.Sum256(
			[]byte(
				strings.Join(
					[]string{
						item.Title,
						item.URL,
						item.Snippet,
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
					Snippet:     item.Snippet,
					Source:      "DuckDuckGo",
					Provider:    p.Name(),
					Authority:   AuthorityCommunity,
					MatchScore:  matchScore,
					ContentHash: hex.EncodeToString(hash[:]),
					Metadata: map[string]any{
						"search_position": index,
					},
				},
				p.Name(),
			),
		)

		if len(results) >= maxResults {
			break
		}
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

type duckDuckGoItem struct {
	Title   string
	URL     string
	Snippet string
}

func duckDuckGoSearchEndpoint(
	baseURL string,
) (*url.URL, error) {
	baseURL = strings.TrimSpace(baseURL)

	if baseURL == "" {
		baseURL = defaultDuckDuckGoURL
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	if base.Scheme == "" ||
		base.Host == "" {
		return nil, fmt.Errorf(
			"DuckDuckGo base URL must include scheme and host",
		)
	}

	base.Path = strings.TrimRight(
		base.Path,
		"/",
	) + "/html"

	base.RawQuery = ""
	base.Fragment = ""

	return base, nil
}

func parseDuckDuckGoHTML(
	body []byte,
) ([]duckDuckGoItem, error) {
	root, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	var items []duckDuckGoItem

	var visit func(*html.Node)

	visit = func(node *html.Node) {
		if node.Type == html.ElementNode &&
			node.Data == "div" &&
			hasClass(node, "result") {
			if item, ok := parseDuckDuckGoResult(node); ok {
				items = append(
					items,
					item,
				)
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}

	visit(root)

	return items, nil
}

func parseDuckDuckGoResult(
	node *html.Node,
) (duckDuckGoItem, bool) {
	var (
		item    duckDuckGoItem
		anchor  *html.Node
		snippet *html.Node
	)

	var find func(*html.Node)

	find = func(current *html.Node) {
		if anchor == nil &&
			current.Type == html.ElementNode &&
			current.Data == "a" &&
			hasClass(current, "result__a") {
			anchor = current
		}

		if snippet == nil &&
			current.Type == html.ElementNode &&
			hasClass(current, "result__snippet") {
			snippet = current
		}

		for child := current.FirstChild; child != nil; child = child.NextSibling {
			find(child)
		}
	}

	find(node)

	if anchor == nil {
		return duckDuckGoItem{}, false
	}

	rawURL := attributeValue(
		anchor,
		"href",
	)

	rawURL = normalizeDuckDuckGoURL(
		rawURL,
	)

	title := strings.TrimSpace(
		nodeText(anchor),
	)

	if title == "" ||
		rawURL == "" {
		return duckDuckGoItem{}, false
	}

	item.Title = title
	item.URL = rawURL

	if snippet != nil {
		item.Snippet = compactDuckDuckGoText(
			nodeText(snippet),
			700,
		)
	}

	return item, true
}

func normalizeDuckDuckGoURL(
	rawURL string,
) string {
	rawURL = strings.TrimSpace(rawURL)

	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(
		rawURL,
	)
	if err != nil {
		return ""
	}

	if parsed.Scheme != "" &&
		parsed.Host != "" {
		if strings.EqualFold(
			parsed.Host,
			"duckduckgo.com",
		) {
			target := strings.TrimSpace(
				parsed.Query().Get("uddg"),
			)

			if target != "" {
				if decoded, err := url.QueryUnescape(
					target,
				); err == nil {
					rawURL = decoded
				}
			}
		}

		return strings.TrimSpace(
			rawURL,
		)
	}

	if strings.HasPrefix(
		rawURL,
		"//",
	) {
		return "https:" + rawURL
	}

	return ""
}

func attributeValue(
	node *html.Node,
	name string,
) string {
	name = strings.TrimSpace(
		strings.ToLower(name),
	)

	for _, attr := range node.Attr {
		if strings.ToLower(attr.Key) == name {
			return attr.Val
		}
	}

	return ""
}

func hasClass(
	node *html.Node,
	className string,
) bool {
	className = strings.TrimSpace(
		className,
	)

	if className == "" {
		return false
	}

	classes := strings.Fields(
		attributeValue(node, "class"),
	)

	for _, class := range classes {
		if class == className {
			return true
		}
	}

	return false
}

func nodeText(
	node *html.Node,
) string {
	if node == nil {
		return ""
	}

	var builder strings.Builder

	var visit func(*html.Node)

	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(
				current.Data,
			)

			return
		}

		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}

	visit(node)

	return strings.Join(
		strings.Fields(
			builder.String(),
		),
		" ",
	)
}

func compactDuckDuckGoText(
	text string,
	maxLen int,
) string {
	text = strings.TrimSpace(text)

	if text == "" {
		return ""
	}

	text = strings.Join(
		strings.Fields(text),
		" ",
	)

	if maxLen <= 0 ||
		len(text) <= maxLen {
		return text
	}

	return text[:maxLen] + "…"
}

func duckDuckGoPositionScore(
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
		return 0
	}

	if score > 1 {
		return 1
	}

	// This is positional relevance, not an engine-provided probability.
	return score * 0.5
}
