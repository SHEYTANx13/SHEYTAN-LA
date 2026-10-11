package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Tool exposes the unified research service to the agent.
//
// The agent receives one stable "research" tool regardless of whether the
// underlying request is routed to GitHub, Reddit, SearXNG, DuckDuckGo, or
// future providers.
type Tool struct {
	Service *Service
}

// NewTool constructs an agent-facing research tool.
func NewTool(service *Service) (*Tool, error) {
	if service == nil {
		return nil, errors.New("research: service is nil")
	}

	return &Tool{
		Service: service,
	}, nil
}

// Name implements the agent.Tool interface.
func (t *Tool) Name() string {
	return "research"
}

// ShortDescription is the one-line UI label (v1.1.7). The full Description
// remains the model-facing spec.
func (t *Tool) ShortDescription() string {
	return "Search external sources with provenance."
}

// Description implements the agent.Tool interface.
func (t *Tool) Description() string {
	return `Use the unified research engine to search external technical knowledge.

The research engine can query registered providers such as GitHub and Reddit.
It normalizes, ranks, deduplicates, and bounds the returned evidence.

Use research when:
- an error or failure may have an existing public fix
- implementation details need external verification
- GitHub issues or pull requests may contain a known solution
- Reddit discussions may contain practical workarounds
- multiple external sources should be compared

Provider routing:
- auto: query all available providers and merge evidence
- github: GitHub issues and pull requests
- reddit: Reddit posts
- web: compatibility alias for DuckDuckGo
- searxng: SearXNG provider when registered
- duckduckgo: DuckDuckGo provider when registered

Important:
- Research findings are evidence, not proof.
- Local compilation, tests, and objective verification remain authoritative.
- Provider failures do not invalidate successful results from other providers.
- Do not treat community claims as equivalent to official or maintainer sources.`
}

// Parameters implements the agent.Tool interface.
func (t *Tool) Parameters() any {
	return researchParameters{
		Type: "object",
		Properties: map[string]researchParameter{
			"query": {
				Type:        "string",
				Description: "Technical question, error message, symbol, package, regression, or problem to research.",
			},
			"backend": {
				Type:        "string",
				Description: "Optional provider routing mode. Defaults to the service backend.",
				Enum: []string{
					BackendAuto,
					BackendGitHub,
					BackendReddit,
					BackendWeb,
					BackendSearXNG,
					BackendDuckDuckGo,
				},
			},
			"maxResults": {
				Type:        "integer",
				Description: "Maximum number of evidence results to return. The service enforces its own upper bound.",
			},
			"timeoutSec": {
				Type:        "integer",
				Description: "Optional research timeout in seconds for this operation.",
			},
		},
		Required: []string{
			"query",
		},
	}
}

// Run implements the agent.Tool interface.
func (t *Tool) Run(
	ctx context.Context,
	args json.RawMessage,
) (string, error) {
	if t == nil || t.Service == nil {
		return "", errors.New("research: tool is not initialized")
	}

	if len(args) == 0 {
		return "", errors.New("research: tool arguments are empty")
	}

	var request researchRequest

	if err := json.Unmarshal(args, &request); err != nil {
		return "", fmt.Errorf(
			"research: invalid tool arguments: %w",
			err,
		)
	}

	request.Query = strings.TrimSpace(request.Query)
	request.Backend = strings.TrimSpace(request.Backend)

	if request.Query == "" {
		return "", ErrInvalidQuery
	}

	searchContext := ctx

	if request.TimeoutSec > 0 {
		timeout := time.Duration(request.TimeoutSec) * time.Second

		var cancel context.CancelFunc
		searchContext, cancel = context.WithTimeout(
			ctx,
			timeout,
		)
		defer cancel()
	}

	response, err := t.Service.SearchWithBackend(
		searchContext,
		request.Backend,
		SearchRequest{
			Query:      request.Query,
			MaxResults: request.MaxResults,
		},
	)

	if encodeErr := validateToolResponse(response); encodeErr != nil {
		if err != nil {
			return "", errors.Join(err, encodeErr)
		}

		return "", encodeErr
	}

	encoded, encodeErr := encodeResearchResponse(
		response,
		err,
	)

	if encodeErr != nil {
		return "", encodeErr
	}

	return encoded, err
}

type researchRequest struct {
	Query string `json:"query"`

	Backend string `json:"backend,omitempty"`

	MaxResults int `json:"maxResults,omitempty"`

	TimeoutSec int `json:"timeoutSec,omitempty"`
}

type researchParameters struct {
	Type       string                       `json:"type"`
	Properties map[string]researchParameter `json:"properties"`
	Required   []string                     `json:"required"`
}

type researchParameter struct {
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type researchToolResponse struct {
	OK       bool                 `json:"ok"`
	Provider string               `json:"provider"`
	Query    string               `json:"query"`
	Duration time.Duration        `json:"duration"`
	Results  []researchToolResult `json:"results"`
	Error    string               `json:"error,omitempty"`
}

type researchToolResult struct {
	Title       string         `json:"title"`
	URL         string         `json:"url"`
	Snippet     string         `json:"snippet,omitempty"`
	Source      string         `json:"source"`
	Provider    string         `json:"provider"`
	PublishedAt time.Time      `json:"publishedAt,omitempty"`
	Authority   string         `json:"authority"`
	MatchScore  float64        `json:"matchScore"`
	ContentHash string         `json:"contentHash,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func encodeResearchResponse(
	response SearchResponse,
	searchErr error,
) (string, error) {
	output := researchToolResponse{
		OK:       searchErr == nil && len(response.Results) > 0,
		Provider: response.Provider,
		Query:    response.Query,
		Duration: response.Duration,
		Results: make(
			[]researchToolResult,
			0,
			len(response.Results),
		),
	}

	if searchErr != nil {
		output.Error = searchErr.Error()
	}

	for _, result := range response.Results {
		output.Results = append(
			output.Results,
			researchToolResult{
				Title:       result.Title,
				URL:         result.URL,
				Snippet:     result.Snippet,
				Source:      result.Source,
				Provider:    result.Provider,
				PublishedAt: result.PublishedAt,
				Authority:   result.Authority.String(),
				MatchScore:  result.MatchScore,
				ContentHash: result.ContentHash,
				Metadata:    result.Metadata,
			},
		)
	}

	data, err := json.MarshalIndent(
		output,
		"",
		"  ",
	)
	if err != nil {
		return "", fmt.Errorf(
			"research: encode response: %w",
			err,
		)
	}

	return string(data), nil
}

func validateToolResponse(
	response SearchResponse,
) error {
	if response.Provider == "" {
		return fmt.Errorf(
			"%w: research tool response provider is empty",
			ErrProviderUnavailable,
		)
	}

	if strings.TrimSpace(response.Query) == "" {
		return fmt.Errorf(
			"%w: research tool response query is empty",
			ErrInvalidQuery,
		)
	}

	return nil
}
