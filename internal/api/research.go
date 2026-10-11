package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/research"
)

type researchHTTPRequest struct {
	Query      string `json:"query"`
	Backend    string `json:"backend,omitempty"`
	MaxResults int    `json:"maxResults,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

type researchHTTPResult struct {
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

type researchHTTPResponse struct {
	OK        bool                 `json:"ok"`
	Provider  string               `json:"provider"`
	Query     string               `json:"query"`
	Duration  time.Duration        `json:"duration"`
	Results   []researchHTTPResult `json:"results"`
	Error     string               `json:"error,omitempty"`
	Providers []string             `json:"providers,omitempty"`
	Backend   string               `json:"backend,omitempty"`
}

func (s *Server) handleResearch(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.stack == nil || s.stack.Research == nil {
		writeErr(
			w,
			http.StatusServiceUnavailable,
			errors.New("research service is unavailable"),
		)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"backend":   s.stack.Research.Backend(),
			"providers": s.stack.Research.ProviderNames(),
		})
		return

	case http.MethodPost:
		var request researchHTTPRequest

		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		request.Query = strings.TrimSpace(request.Query)
		request.Backend = strings.TrimSpace(request.Backend)

		if request.Query == "" {
			writeErr(
				w,
				http.StatusBadRequest,
				research.ErrInvalidQuery,
			)
			return
		}

		ctx := r.Context()

		if request.TimeoutSec > 0 {
			timeout := time.Duration(
				request.TimeoutSec,
			) * time.Second

			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(
				ctx,
				timeout,
			)
			defer cancel()
		}

		response, err := s.stack.Research.SearchWithBackend(
			ctx,
			request.Backend,
			research.SearchRequest{
				Query:      request.Query,
				MaxResults: request.MaxResults,
			},
		)

		httpResponse := researchHTTPResponse{
			OK:       err == nil && len(response.Results) > 0,
			Provider: response.Provider,
			Query:    response.Query,
			Duration: response.Duration,
			Results: make(
				[]researchHTTPResult,
				0,
				len(response.Results),
			),
		}

		for _, result := range response.Results {
			httpResponse.Results = append(
				httpResponse.Results,
				researchHTTPResult{
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

		if err != nil {
			httpResponse.Error = err.Error()
		}

		writeJSON(w, httpResponse)
		return

	default:
		writeErr(
			w,
			http.StatusMethodNotAllowed,
			fmt.Errorf("method %s not allowed", r.Method),
		)
	}
}
