package research

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	BackendAuto       = "auto"
	BackendGitHub     = "github"
	BackendReddit     = "reddit"
	BackendWeb        = "web"
	BackendSearXNG    = "searxng"
	BackendDuckDuckGo = "duckduckgo"
)

// ServiceConfig controls the unified research service.
type ServiceConfig struct {
	Backend    string
	MaxResults int
	Timeout    time.Duration
}

// DefaultServiceConfig returns conservative research defaults.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		Backend:    BackendAuto,
		MaxResults: 8,
		Timeout:    20 * time.Second,
	}
}

// Service is the unified research orchestration layer.
//
// Providers are registered independently so GitHub, Reddit, SearXNG,
// DuckDuckGo, and future web providers can share one stable interface.
type Service struct {
	mu         sync.RWMutex
	providers  map[string]Provider
	backend    string
	maxResults int
	timeout    time.Duration
}

// NewService constructs a research service from the supplied configuration.
func NewService(cfg ServiceConfig) *Service {
	defaults := DefaultServiceConfig()

	backend := normalizeBackend(cfg.Backend)

	if backend == "" {
		backend = defaults.Backend
	}

	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaults.MaxResults
	}

	if maxResults > 50 {
		maxResults = 50
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaults.Timeout
	}

	return &Service{
		providers:  make(map[string]Provider),
		backend:    backend,
		maxResults: maxResults,
		timeout:    timeout,
	}
}

// Register adds or replaces a provider by its canonical name.
func (s *Service) Register(provider Provider) error {
	if provider == nil {
		return fmt.Errorf(
			"%w: nil provider",
			ErrProviderUnavailable,
		)
	}

	name := strings.ToLower(
		strings.TrimSpace(provider.Name()),
	)

	if name == "" {
		return fmt.Errorf(
			"%w: provider name is empty",
			ErrProviderUnavailable,
		)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.providers[name] = provider

	return nil
}

// Unregister removes a provider from the service.
func (s *Service) Unregister(name string) {
	name = canonicalBackendName(name)

	if name == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.providers, name)
}

// Provider returns a registered provider by name.
func (s *Service) Provider(name string) (Provider, bool) {
	name = canonicalBackendName(name)

	if name == "" {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	provider, ok := s.providers[name]

	return provider, ok
}

// ProviderNames returns all registered providers in deterministic order.
func (s *Service) ProviderNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make(
		[]string,
		0,
		len(s.providers),
	)

	for name := range s.providers {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// SetBackend changes the default routing mode.
//
// "auto" enables bounded multi-provider research.
// "web" is retained as a compatibility alias for DuckDuckGo.
// Any other value selects one registered provider explicitly.
func (s *Service) SetBackend(backend string) error {
	backend = normalizeBackend(backend)

	if backend == BackendWeb {
		backend = BackendDuckDuckGo
	}

	if backend != BackendAuto {
		s.mu.RLock()

		_, exists := s.providers[backend]

		s.mu.RUnlock()

		if !exists {
			return fmt.Errorf(
				"%w: provider %q is not registered",
				ErrProviderUnavailable,
				backend,
			)
		}
	}

	s.mu.Lock()

	s.backend = backend

	s.mu.Unlock()

	return nil
}

// Backend returns the current default routing mode.
func (s *Service) Backend() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.backend
}

// Search executes one unified research operation using the service's
// currently configured backend.
//
// Explicit backends use exactly one provider.
// Auto mode queries all currently registered providers concurrently and
// combines their successful results. Provider failures do not discard
// successful findings from other providers.
func (s *Service) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResponse, error) {
	return s.SearchWithBackend(
		ctx,
		s.Backend(),
		req,
	)
}

// SearchWithBackend executes one research operation using a request-scoped
// routing mode.
//
// Unlike SetBackend, this method never mutates the service's shared backend.
// It is therefore safe for concurrent agent requests that need different
// providers.
func (s *Service) SearchWithBackend(
	ctx context.Context,
	backend string,
	req SearchRequest,
) (SearchResponse, error) {
	req = req.Normalize()

	if err := req.Validate(); err != nil {
		return SearchResponse{}, err
	}

	backend = normalizeBackend(backend)

	s.mu.RLock()

	defaultMaxResults := s.maxResults
	timeout := s.timeout

	providers := make(
		map[string]Provider,
		len(s.providers),
	)

	for name, provider := range s.providers {
		providers[name] = provider
	}

	s.mu.RUnlock()

	maxResults := req.MaxResults

	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}

	if maxResults > 50 {
		maxResults = 50
	}

	req.MaxResults = maxResults

	if backend == BackendWeb {
		backend = BackendDuckDuckGo
	}

	if backend != BackendAuto {
		provider, ok := providers[backend]

		if !ok {
			return SearchResponse{}, fmt.Errorf(
				"%w: provider %q is not registered",
				ErrProviderUnavailable,
				backend,
			)
		}

		return s.searchProvider(
			ctx,
			provider,
			req,
			timeout,
		)
	}

	return s.searchAuto(
		ctx,
		providers,
		req,
		timeout,
	)
}

func normalizeBackend(backend string) string {
	backend = strings.ToLower(
		strings.TrimSpace(backend),
	)

	if backend == "" {
		return BackendAuto
	}

	return backend
}

func canonicalBackendName(backend string) string {
	backend = normalizeBackend(backend)

	if backend == BackendWeb {
		return BackendDuckDuckGo
	}

	return backend
}

func (s *Service) searchProvider(
	ctx context.Context,
	provider Provider,
	req SearchRequest,
	timeout time.Duration,
) (SearchResponse, error) {
	ctx, cancel := context.WithTimeout(
		ctx,
		timeout,
	)

	defer cancel()

	started := time.Now()

	response, err := provider.Search(
		ctx,
		req,
	)

	if err != nil {
		if ctx.Err() != nil {
			return SearchResponse{}, ctx.Err()
		}

		return response, err
	}

	// Set the canonical registry name first so result-level provider fields
	// inherit it during normalization instead of trusting whatever label the
	// provider payload carried.
	response.Provider = provider.Name()

	response = NormalizeResponse(response)

	response.Duration = time.Since(started)

	if err := response.Validate(); err != nil {
		return response, err
	}

	return response, nil
}

type providerSearchResult struct {
	name     string
	response SearchResponse
	err      error
}

type collectedResult struct {
	result Result
}

func (s *Service) searchAuto(
	ctx context.Context,
	providers map[string]Provider,
	req SearchRequest,
	timeout time.Duration,
) (SearchResponse, error) {
	started := time.Now()

	if len(providers) == 0 {
		return SearchResponse{
			Provider: BackendAuto,
			Query:    req.Query,
			Duration: time.Since(started),
		}, fmt.Errorf(
			"%w: no research providers are registered",
			ErrProviderUnavailable,
		)
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		timeout,
	)

	defer cancel()

	names := make(
		[]string,
		0,
		len(providers),
	)

	for name := range providers {
		names = append(names, name)
	}

	sort.Strings(names)

	resultsCh := make(
		chan providerSearchResult,
		len(names),
	)

	var wg sync.WaitGroup

	for _, name := range names {
		name := name
		provider := providers[name]

		wg.Add(1)

		go func() {
			defer wg.Done()

			response, err := provider.Search(
				ctx,
				req,
			)

			resultsCh <- providerSearchResult{
				name:     name,
				response: response,
				err:      err,
			}
		}()
	}

	wg.Wait()
	close(resultsCh)

	allResults := make(
		[]collectedResult,
		0,
	)

	var providerErrors []error
	var successfulProviders int

	for providerResult := range resultsCh {
		if providerResult.err != nil {
			if !errors.Is(
				providerResult.err,
				ErrNoResults,
			) &&
				!errors.Is(
					providerResult.err,
					context.Canceled,
				) &&
				!errors.Is(
					providerResult.err,
					context.DeadlineExceeded,
				) {
				providerErrors = append(
					providerErrors,
					fmt.Errorf(
						"%s: %w",
						providerResult.name,
						providerResult.err,
					),
				)
			}

			continue
		}

		response := NormalizeResponse(
			providerResult.response,
		)

		if len(response.Results) == 0 {
			continue
		}

		successfulProviders++

		for _, result := range response.Results {
			allResults = append(
				allResults,
				collectedResult{
					result: NormalizeResult(
						result,
						providerResult.name,
					),
				},
			)
		}
	}

	merged := mergeResearchResults(
		allResults,
		req.MaxResults,
	)

	response := SearchResponse{
		Provider: BackendAuto,
		Query:    req.Query,
		Results:  merged,
		Duration: time.Since(started),
	}

	if len(response.Results) > 0 {
		response = NormalizeResponse(response)
		return response, nil
	}

	if ctx.Err() != nil {
		return response, ctx.Err()
	}

	if successfulProviders == 0 &&
		len(providerErrors) > 0 {
		return response, fmt.Errorf(
			"%w: all providers failed: %v",
			ErrProviderUnavailable,
			errors.Join(providerErrors...),
		)
	}

	return response, ErrNoResults
}

func mergeResearchResults(
	input []collectedResult,
	maxResults int,
) []Result {
	if len(input) == 0 ||
		maxResults <= 0 {
		return nil
	}

	type rankedResult struct {
		result Result
		index  int
	}

	ranked := make(
		[]rankedResult,
		0,
		len(input),
	)

	for index, item := range input {
		result := NormalizeResult(
			item.result,
			item.result.Provider,
		)

		ranked = append(
			ranked,
			rankedResult{
				result: result,
				index:  index,
			},
		)
	}

	sort.SliceStable(
		ranked,
		func(i, j int) bool {
			left := ranked[i].result
			right := ranked[j].result

			leftScore := combinedResearchScore(left)
			rightScore := combinedResearchScore(right)

			if leftScore != rightScore {
				return leftScore > rightScore
			}

			leftAuthority := left.Authority.Rank()
			rightAuthority := right.Authority.Rank()

			if leftAuthority != rightAuthority {
				return leftAuthority > rightAuthority
			}

			if !left.PublishedAt.Equal(
				right.PublishedAt,
			) {
				return left.PublishedAt.After(
					right.PublishedAt,
				)
			}

			leftURL := left.URL
			rightURL := right.URL

			if leftURL != rightURL {
				return leftURL < rightURL
			}

			return ranked[i].index < ranked[j].index
		},
	)

	seen := make(
		map[string]struct{},
		len(ranked),
	)

	results := make(
		[]Result,
		0,
		minInt(maxResults, len(ranked)),
	)

	for _, item := range ranked {
		result := item.result

		key := researchResultKey(result)

		if key == "" {
			continue
		}

		if _, exists := seen[key]; exists {
			continue
		}

		seen[key] = struct{}{}

		results = append(
			results,
			result,
		)

		if len(results) >= maxResults {
			break
		}
	}

	return results
}

func researchResultKey(result Result) string {
	if hash := strings.TrimSpace(
		result.ContentHash,
	); hash != "" {
		return "hash:" + strings.ToLower(hash)
	}

	if rawURL := strings.TrimSpace(
		result.URL,
	); rawURL != "" {
		return "url:" + strings.ToLower(rawURL)
	}

	title := strings.ToLower(
		strings.TrimSpace(result.Title),
	)

	snippet := strings.ToLower(
		strings.TrimSpace(result.Snippet),
	)

	if title == "" && snippet == "" {
		return ""
	}

	return "content:" +
		title +
		"\x00" +
		snippet
}

func combinedResearchScore(
	result Result,
) float64 {
	match := result.MatchScore

	if match < 0 {
		match = 0
	}

	if match > 1 {
		match = 1
	}

	authority := result.Authority.Rank()

	// Relevance is weighted more heavily than authority.
	// Authority remains a secondary trust signal.
	return (match * 0.70) +
		(authority * 0.30)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}

	return right
}
