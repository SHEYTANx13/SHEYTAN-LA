package research

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockResearchProvider struct {
	name      string
	response  SearchResponse
	err       error
	delay     time.Duration
	active    *atomic.Int32
	maxActive *atomic.Int32

	mu        sync.Mutex
	queries   []SearchRequest
	callCount int
}

func (m *mockResearchProvider) Name() string {
	return m.name
}

func (m *mockResearchProvider) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResponse, error) {
	if m.active != nil {
		current := m.active.Add(1)

		if m.maxActive != nil {
			updateAtomicMax(m.maxActive, current)
		}

		defer m.active.Add(-1)
	}

	m.mu.Lock()
	m.callCount++
	m.queries = append(m.queries, req)
	m.mu.Unlock()

	if m.delay > 0 {
		timer := time.NewTimer(m.delay)

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}

			return SearchResponse{}, ctx.Err()

		case <-timer.C:
		}
	}

	if m.err != nil {
		return m.response, m.err
	}

	return m.response, nil
}

func (m *mockResearchProvider) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.callCount
}

func (m *mockResearchProvider) LastQuery() SearchRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.queries) == 0 {
		return SearchRequest{}
	}

	return m.queries[len(m.queries)-1]
}

func updateAtomicMax(target *atomic.Int32, value int32) {
	for {
		current := target.Load()

		if value <= current {
			return
		}

		if target.CompareAndSwap(current, value) {
			return
		}
	}
}

func testResult(
	title string,
	url string,
	authority Authority,
	score float64,
	publishedAt time.Time,
) Result {
	return NormalizeResult(
		Result{
			Title:       title,
			URL:         url,
			Snippet:     title + " snippet",
			Source:      "test",
			Provider:    "test",
			Authority:   authority,
			MatchScore:  score,
			PublishedAt: publishedAt,
		},
		"test",
	)
}

func TestDefaultServiceConfig(t *testing.T) {
	cfg := DefaultServiceConfig()

	if cfg.Backend != BackendAuto {
		t.Fatalf("unexpected backend: %q", cfg.Backend)
	}

	if cfg.MaxResults != 8 {
		t.Fatalf("unexpected max results: %d", cfg.MaxResults)
	}

	if cfg.Timeout != 20*time.Second {
		t.Fatalf("unexpected timeout: %v", cfg.Timeout)
	}
}

func TestNewServiceDefaults(t *testing.T) {
	service := NewService(ServiceConfig{})

	if service == nil {
		t.Fatal("expected service")
	}

	if service.Backend() != BackendAuto {
		t.Fatalf("unexpected backend: %q", service.Backend())
	}

	if got := service.ProviderNames(); len(got) != 0 {
		t.Fatalf("expected no providers, got %v", got)
	}
}

func TestNewServiceBoundsConfiguration(t *testing.T) {
	service := NewService(ServiceConfig{
		Backend:    "  AUTO  ",
		MaxResults: 1000,
		Timeout:    5 * time.Second,
	})

	if service.Backend() != BackendAuto {
		t.Fatalf("unexpected backend: %q", service.Backend())
	}

	provider := &mockResearchProvider{
		name: "github",
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "test",
			Results: []Result{
				testResult(
					"test",
					"https://example.test/1",
					AuthorityTechnicalDiscussion,
					0.5,
					time.Now(),
				),
			},
		},
	}

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{
			Query:      "test",
			MaxResults: 1000,
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 1 {
		t.Fatalf("expected one result, got %d", len(response.Results))
	}

	request := provider.LastQuery()

	if request.MaxResults != 50 {
		t.Fatalf(
			"expected provider max results to be capped at 50, got %d",
			request.MaxResults,
		)
	}
}

func TestServiceRegisterProvider(t *testing.T) {
	service := NewService(ServiceConfig{})

	provider := &mockResearchProvider{
		name: "GitHub",
	}

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if names := service.ProviderNames(); len(names) != 1 || names[0] != "github" {
		t.Fatalf("unexpected provider names: %v", names)
	}

	resolved, ok := service.Provider("GITHUB")
	if !ok {
		t.Fatal("expected provider lookup to succeed")
	}

	if resolved != provider {
		t.Fatal("provider lookup returned different instance")
	}
}

func TestServiceRegisterRejectsInvalidProvider(t *testing.T) {
	service := NewService(ServiceConfig{})

	if err := service.Register(nil); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}

	emptyName := &mockResearchProvider{name: "  "}

	if err := service.Register(emptyName); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestServiceRegisterReplacesExistingProvider(t *testing.T) {
	service := NewService(ServiceConfig{})

	first := &mockResearchProvider{name: "github"}
	second := &mockResearchProvider{name: "github"}

	if err := service.Register(first); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}

	if err := service.Register(second); err != nil {
		t.Fatalf("second Register returned error: %v", err)
	}

	resolved, ok := service.Provider("github")
	if !ok {
		t.Fatal("expected provider")
	}

	if resolved != second {
		t.Fatal("expected second provider to replace first")
	}

	if names := service.ProviderNames(); len(names) != 1 {
		t.Fatalf("expected one provider, got %v", names)
	}
}

func TestServiceUnregister(t *testing.T) {
	service := NewService(ServiceConfig{})

	provider := &mockResearchProvider{name: "github"}

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	service.Unregister("GITHUB")

	if _, ok := service.Provider("github"); ok {
		t.Fatal("expected provider to be removed")
	}

	service.Unregister("")
	service.Unregister("missing")
}

func TestServiceProviderNamesDeterministic(t *testing.T) {
	service := NewService(ServiceConfig{})

	for _, name := range []string{"web", "github", "reddit", "searxng"} {
		if err := service.Register(&mockResearchProvider{name: name}); err != nil {
			t.Fatalf("Register(%q) returned error: %v", name, err)
		}
	}

	got := service.ProviderNames()
	expected := []string{
		"github",
		"reddit",
		"searxng",
		"web",
	}

	if len(got) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}

	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf(
				"provider order mismatch at %d: expected %q, got %q",
				i,
				expected[i],
				got[i],
			)
		}
	}
}

func TestServiceSetBackendAuto(t *testing.T) {
	service := NewService(ServiceConfig{})

	if err := service.SetBackend(""); err != nil {
		t.Fatalf("SetBackend returned error: %v", err)
	}

	if service.Backend() != BackendAuto {
		t.Fatalf("expected auto backend, got %q", service.Backend())
	}

	if err := service.SetBackend("AUTO"); err != nil {
		t.Fatalf("SetBackend returned error: %v", err)
	}

	if service.Backend() != BackendAuto {
		t.Fatalf("expected auto backend, got %q", service.Backend())
	}
}

func TestServiceSetBackendRequiresRegisteredProvider(t *testing.T) {
	service := NewService(ServiceConfig{})

	err := service.SetBackend(BackendGitHub)

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}

	if service.Backend() != BackendAuto {
		t.Fatalf("backend changed unexpectedly to %q", service.Backend())
	}
}

func TestServiceSetBackendExplicit(t *testing.T) {
	service := NewService(ServiceConfig{})

	provider := &mockResearchProvider{
		name: BackendGitHub,
	}

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if err := service.SetBackend("GITHUB"); err != nil {
		t.Fatalf("SetBackend returned error: %v", err)
	}

	if service.Backend() != BackendGitHub {
		t.Fatalf("expected github backend, got %q", service.Backend())
	}
}

func TestServiceSearchExplicitProvider(t *testing.T) {
	provider := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "permission denied",
			Results: []Result{
				testResult(
					"GitHub fix",
					"https://github.com/example/repo/issues/1",
					AuthorityTechnicalDiscussion,
					0.9,
					time.Now(),
				),
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend:    BackendGitHub,
		MaxResults: 8,
		Timeout:    2 * time.Second,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{
			Query: "permission denied",
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Provider != BackendGitHub {
		t.Fatalf("unexpected response provider: %q", response.Provider)
	}

	if response.Query != "permission denied" {
		t.Fatalf("unexpected query: %q", response.Query)
	}

	if len(response.Results) != 1 {
		t.Fatalf("expected one result, got %d", len(response.Results))
	}

	if provider.Calls() != 1 {
		t.Fatalf("expected one provider call, got %d", provider.Calls())
	}

	if request := provider.LastQuery(); request.MaxResults != 8 {
		t.Fatalf("unexpected provider max results: %d", request.MaxResults)
	}
}

func TestServiceSearchExplicitProviderMissing(t *testing.T) {
	service := NewService(ServiceConfig{
		Backend: BackendGitHub,
	})

	_, err := service.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestServiceSearchRejectsInvalidQuery(t *testing.T) {
	service := NewService(ServiceConfig{})

	_, err := service.Search(
		context.Background(),
		SearchRequest{Query: "   "},
	)

	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("expected ErrInvalidQuery, got %v", err)
	}
}

func TestServiceSearchExplicitProviderFailure(t *testing.T) {
	providerErr := errors.New("provider exploded")

	provider := &mockResearchProvider{
		name: BackendGitHub,
		err:  providerErr,
	}

	service := NewService(ServiceConfig{
		Backend: BackendGitHub,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	_, err := service.Search(
		context.Background(),
		SearchRequest{Query: "test"},
	)

	if !errors.Is(err, providerErr) {
		t.Fatalf("expected provider error, got %v", err)
	}
}

func TestServiceSearchExplicitProviderTimeout(t *testing.T) {
	provider := &mockResearchProvider{
		name:  BackendGitHub,
		delay: 200 * time.Millisecond,
	}

	service := NewService(ServiceConfig{
		Backend: BackendGitHub,
		Timeout: 20 * time.Millisecond,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	_, err := service.Search(
		context.Background(),
		SearchRequest{Query: "timeout"},
	)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline, got %v", err)
	}
}

func TestServiceSearchExplicitProviderParentCancellation(t *testing.T) {
	provider := &mockResearchProvider{
		name:  BackendGitHub,
		delay: 200 * time.Millisecond,
	}

	service := NewService(ServiceConfig{
		Backend: BackendGitHub,
		Timeout: 2 * time.Second,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan error, 1)

	go func() {
		_, err := service.Search(
			ctx,
			SearchRequest{Query: "cancel"},
		)
		resultCh <- err
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Search did not stop after cancellation")
	}
}

func TestServiceAutoSearchCombinesProviders(t *testing.T) {
	active := atomic.Int32{}
	maxActive := atomic.Int32{}

	now := time.Now()

	github := &mockResearchProvider{
		name:      BackendGitHub,
		delay:     50 * time.Millisecond,
		active:    &active,
		maxActive: &maxActive,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "compile error",
			Results: []Result{
				testResult(
					"GitHub result",
					"https://github.com/example/repo/issues/1",
					AuthorityMaintainer,
					0.80,
					now,
				),
			},
		},
	}

	reddit := &mockResearchProvider{
		name:      BackendReddit,
		delay:     50 * time.Millisecond,
		active:    &active,
		maxActive: &maxActive,
		response: SearchResponse{
			Provider: BackendReddit,
			Query:    "compile error",
			Results: []Result{
				testResult(
					"Reddit result",
					"https://reddit.com/r/test/comments/1",
					AuthorityCommunity,
					0.95,
					now.Add(time.Minute),
				),
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend:    BackendAuto,
		MaxResults: 8,
		Timeout:    2 * time.Second,
	})

	if err := service.Register(github); err != nil {
		t.Fatalf("Register github returned error: %v", err)
	}

	if err := service.Register(reddit); err != nil {
		t.Fatalf("Register reddit returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{
			Query: "compile error",
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Provider != BackendAuto {
		t.Fatalf("unexpected provider: %q", response.Provider)
	}

	if len(response.Results) != 2 {
		t.Fatalf("expected 2 merged results, got %d", len(response.Results))
	}

	if github.Calls() != 1 {
		t.Fatalf("expected one GitHub call, got %d", github.Calls())
	}

	if reddit.Calls() != 1 {
		t.Fatalf("expected one Reddit call, got %d", reddit.Calls())
	}

	if maxActive.Load() < 2 {
		t.Fatalf(
			"expected concurrent provider execution, maximum active providers was %d",
			maxActive.Load(),
		)
	}
}

func TestServiceAutoSearchDeduplicatesByContentHash(t *testing.T) {
	first := testResult(
		"same",
		"https://example.test/shared",
		AuthorityCommunity,
		0.8,
		time.Now(),
	)

	second := first
	second.Provider = BackendReddit
	second.Source = "Reddit"

	providerA := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "duplicate",
			Results:  []Result{first},
		},
	}

	providerB := &mockResearchProvider{
		name: BackendReddit,
		response: SearchResponse{
			Provider: BackendReddit,
			Query:    "duplicate",
			Results:  []Result{second},
		},
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(providerA); err != nil {
		t.Fatalf("Register providerA returned error: %v", err)
	}

	if err := service.Register(providerB); err != nil {
		t.Fatalf("Register providerB returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{
			Query: "duplicate",
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 1 {
		t.Fatalf("expected one deduplicated result, got %d", len(response.Results))
	}
}

func TestServiceAutoSearchDeduplicatesByURLWhenHashMissing(t *testing.T) {
	first := Result{
		Title:      "same",
		URL:        "https://example.test/shared",
		Snippet:    "first",
		Provider:   BackendGitHub,
		Authority:  AuthorityCommunity,
		MatchScore: 0.8,
	}

	second := first
	second.Provider = BackendReddit
	second.Snippet = "second"

	providerA := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "duplicate",
			Results:  []Result{first},
		},
	}

	providerB := &mockResearchProvider{
		name: BackendReddit,
		response: SearchResponse{
			Provider: BackendReddit,
			Query:    "duplicate",
			Results:  []Result{second},
		},
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(providerA); err != nil {
		t.Fatalf("Register providerA returned error: %v", err)
	}

	if err := service.Register(providerB); err != nil {
		t.Fatalf("Register providerB returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "duplicate"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 1 {
		t.Fatalf("expected one deduplicated result, got %d", len(response.Results))
	}
}

func TestServiceAutoSearchDeduplicatesByContent(t *testing.T) {
	first := Result{
		Title:      "same title",
		Snippet:    "same snippet",
		Provider:   BackendGitHub,
		Authority:  AuthorityCommunity,
		MatchScore: 0.8,
	}

	second := first
	second.Provider = BackendReddit

	providerA := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "duplicate",
			Results:  []Result{first},
		},
	}

	providerB := &mockResearchProvider{
		name: BackendReddit,
		response: SearchResponse{
			Provider: BackendReddit,
			Query:    "duplicate",
			Results:  []Result{second},
		},
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(providerA); err != nil {
		t.Fatalf("Register providerA returned error: %v", err)
	}

	if err := service.Register(providerB); err != nil {
		t.Fatalf("Register providerB returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "duplicate"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 1 {
		t.Fatalf("expected one deduplicated result, got %d", len(response.Results))
	}
}

func TestServiceAutoSearchRanksByCombinedScore(t *testing.T) {
	now := time.Now()

	low := testResult(
		"low",
		"https://example.test/low",
		AuthorityOfficial,
		0.10,
		now,
	)

	high := testResult(
		"high",
		"https://example.test/high",
		AuthorityCommunity,
		0.95,
		now,
	)

	provider := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "rank",
			Results: []Result{
				low,
				high,
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "rank"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(response.Results))
	}

	if response.Results[0].Title != "high" {
		t.Fatalf(
			"expected high relevance result first, got %q",
			response.Results[0].Title,
		)
	}
}

func TestServiceAutoSearchUsesAuthorityAsSecondaryRankingSignal(t *testing.T) {
	now := time.Now()

	official := testResult(
		"official",
		"https://example.test/official",
		AuthorityOfficial,
		0.50,
		now,
	)

	community := testResult(
		"community",
		"https://example.test/community",
		AuthorityCommunity,
		0.50,
		now,
	)

	provider := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "authority",
			Results: []Result{
				community,
				official,
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "authority"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Results[0].Title != "official" {
		t.Fatalf(
			"expected official result first, got %q",
			response.Results[0].Title,
		)
	}
}

func TestServiceAutoSearchRespectsMaximumResults(t *testing.T) {
	provider := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "many",
			Results: []Result{
				testResult(
					"one",
					"https://example.test/1",
					AuthorityCommunity,
					0.9,
					time.Now(),
				),
				testResult(
					"two",
					"https://example.test/2",
					AuthorityCommunity,
					0.8,
					time.Now(),
				),
				testResult(
					"three",
					"https://example.test/3",
					AuthorityCommunity,
					0.7,
					time.Now(),
				),
				testResult(
					"four",
					"https://example.test/4",
					AuthorityCommunity,
					0.6,
					time.Now(),
				),
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend:    BackendAuto,
		MaxResults: 2,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "many"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(response.Results))
	}
}

func TestServiceAutoSearchContinuesAfterOneProviderFails(t *testing.T) {
	failing := &mockResearchProvider{
		name: BackendGitHub,
		err:  errors.New("GitHub unavailable"),
	}

	working := &mockResearchProvider{
		name: BackendReddit,
		response: SearchResponse{
			Provider: BackendReddit,
			Query:    "fallback",
			Results: []Result{
				testResult(
					"working result",
					"https://reddit.com/r/test/comments/1",
					AuthorityCommunity,
					0.7,
					time.Now(),
				),
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(failing); err != nil {
		t.Fatalf("Register failing provider returned error: %v", err)
	}

	if err := service.Register(working); err != nil {
		t.Fatalf("Register working provider returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "fallback"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 1 {
		t.Fatalf("expected one result, got %d", len(response.Results))
	}

	if response.Results[0].Title != "working result" {
		t.Fatalf(
			"unexpected fallback result: %q",
			response.Results[0].Title,
		)
	}
}

func TestServiceAutoSearchAllProvidersFail(t *testing.T) {
	providerA := &mockResearchProvider{
		name: BackendGitHub,
		err:  errors.New("GitHub down"),
	}

	providerB := &mockResearchProvider{
		name: BackendReddit,
		err:  errors.New("Reddit down"),
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(providerA); err != nil {
		t.Fatalf("Register providerA returned error: %v", err)
	}

	if err := service.Register(providerB); err != nil {
		t.Fatalf("Register providerB returned error: %v", err)
	}

	_, err := service.Search(
		context.Background(),
		SearchRequest{Query: "failure"},
	)

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestServiceAutoSearchAllProvidersReturnNoResults(t *testing.T) {
	providerA := &mockResearchProvider{
		name: BackendGitHub,
		err:  ErrNoResults,
	}

	providerB := &mockResearchProvider{
		name: BackendReddit,
		err:  ErrNoResults,
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(providerA); err != nil {
		t.Fatalf("Register providerA returned error: %v", err)
	}

	if err := service.Register(providerB); err != nil {
		t.Fatalf("Register providerB returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "empty"},
	)

	if !errors.Is(err, ErrNoResults) {
		t.Fatalf("expected ErrNoResults, got %v", err)
	}

	if len(response.Results) != 0 {
		t.Fatalf("expected no results, got %d", len(response.Results))
	}
}

func TestServiceAutoSearchNoProviders(t *testing.T) {
	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "none"},
	)

	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}

	if response.Provider != BackendAuto {
		t.Fatalf("unexpected provider: %q", response.Provider)
	}

	if response.Query != "none" {
		t.Fatalf("unexpected query: %q", response.Query)
	}
}

func TestServiceAutoSearchParentCancellation(t *testing.T) {
	provider := &mockResearchProvider{
		name:  BackendGitHub,
		delay: 500 * time.Millisecond,
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
		Timeout: 2 * time.Second,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan error, 1)

	go func() {
		_, err := service.Search(
			ctx,
			SearchRequest{Query: "cancel all"},
		)
		resultCh <- err
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Search did not stop after cancellation")
	}
}

func TestServiceAutoSearchTimeout(t *testing.T) {
	provider := &mockResearchProvider{
		name:  BackendGitHub,
		delay: 300 * time.Millisecond,
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
		Timeout: 20 * time.Millisecond,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	_, err := service.Search(
		context.Background(),
		SearchRequest{Query: "timeout"},
	)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestServiceAutoSearchIgnoresProviderNoResults(t *testing.T) {
	noResults := &mockResearchProvider{
		name: BackendGitHub,
		err:  ErrNoResults,
	}

	working := &mockResearchProvider{
		name: BackendReddit,
		response: SearchResponse{
			Provider: BackendReddit,
			Query:    "mixed",
			Results: []Result{
				testResult(
					"working",
					"https://reddit.com/r/test/comments/2",
					AuthorityCommunity,
					0.8,
					time.Now(),
				),
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(noResults); err != nil {
		t.Fatalf("Register no-results returned error: %v", err)
	}

	if err := service.Register(working); err != nil {
		t.Fatalf("Register working returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "mixed"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 1 {
		t.Fatalf("expected one result, got %d", len(response.Results))
	}
}

func TestResearchResultKey(t *testing.T) {
	hashResult := Result{
		ContentHash: "ABC123",
		URL:         "https://example.test/hash",
	}

	if got := researchResultKey(hashResult); got != "hash:abc123" {
		t.Fatalf("unexpected hash key: %q", got)
	}

	urlResult := Result{
		URL: "  HTTPS://Example.Test/Path  ",
	}

	if got := researchResultKey(urlResult); got != "url:https://example.test/path" {
		t.Fatalf("unexpected URL key: %q", got)
	}

	contentResult := Result{
		Title:   "  Hello  ",
		Snippet: "  World  ",
	}

	if got := researchResultKey(contentResult); got != "content:hello\x00world" {
		t.Fatalf("unexpected content key: %q", got)
	}

	emptyResult := Result{}

	if got := researchResultKey(emptyResult); got != "" {
		t.Fatalf("expected empty key, got %q", got)
	}
}

func TestCombinedResearchScoreClampsValues(t *testing.T) {
	result := Result{
		MatchScore: 100,
		Authority:  AuthorityOfficial,
	}

	if got := combinedResearchScore(result); got != 1 {
		t.Fatalf("expected score 1, got %v", got)
	}

	result.MatchScore = -100

	if got := combinedResearchScore(result); got != 0.30 {
		t.Fatalf("expected score 0.30, got %v", got)
	}
}

func TestMinInt(t *testing.T) {
	if got := minInt(2, 3); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}

	if got := minInt(3, 2); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}

func TestMergeResearchResultsDeterministicTieBreak(t *testing.T) {
	published := time.Date(
		2026,
		time.September,
		2,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	input := []collectedResult{
		{
			result: Result{
				Title:       "B",
				URL:         "https://example.test/b",
				Snippet:     "B",
				Provider:    BackendGitHub,
				Authority:   AuthorityCommunity,
				MatchScore:  0.5,
				PublishedAt: published,
			},
		},
		{
			result: Result{
				Title:       "A",
				URL:         "https://example.test/a",
				Snippet:     "A",
				Provider:    BackendReddit,
				Authority:   AuthorityCommunity,
				MatchScore:  0.5,
				PublishedAt: published,
			},
		},
	}

	results := mergeResearchResults(input, 8)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].URL != "https://example.test/a" {
		t.Fatalf(
			"expected deterministic URL tie-break to select A first, got %q",
			results[0].URL,
		)
	}
}

func TestMergeResearchResultsZeroLimit(t *testing.T) {
	input := []collectedResult{
		{
			result: testResult(
				"test",
				"https://example.test/1",
				AuthorityCommunity,
				0.5,
				time.Now(),
			),
		},
	}

	results := mergeResearchResults(input, 0)

	if len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
}

func TestServiceSearchNormalizesProviderName(t *testing.T) {
	provider := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: " GITHUB ",
			Query:    "normalize",
			Results: []Result{
				{
					Title:      "result",
					URL:        "https://example.test/normalize",
					MatchScore: 0.5,
					Authority:  AuthorityCommunity,
				},
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend: BackendGitHub,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{Query: "normalize"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Provider != BackendGitHub {
		t.Fatalf("unexpected provider: %q", response.Provider)
	}

	if response.Results[0].Provider != BackendGitHub {
		t.Fatalf(
			"unexpected normalized result provider: %q",
			response.Results[0].Provider,
		)
	}
}

func TestServiceSearchPreservesContextDeadlineFromCaller(t *testing.T) {
	provider := &mockResearchProvider{
		name:  BackendGitHub,
		delay: 500 * time.Millisecond,
	}

	service := NewService(ServiceConfig{
		Backend: BackendGitHub,
		Timeout: 2 * time.Second,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Millisecond,
	)
	defer cancel()

	_, err := service.Search(
		ctx,
		SearchRequest{Query: "deadline"},
	)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestServiceAutoSearchDeterministicProviderSnapshot(t *testing.T) {
	firstCalls := atomic.Int32{}
	secondCalls := atomic.Int32{}

	first := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "snapshot",
			Results: []Result{
				testResult(
					"first",
					"https://example.test/first",
					AuthorityCommunity,
					0.5,
					time.Now(),
				),
			},
		},
	}

	second := &mockResearchProvider{
		name: BackendReddit,
		response: SearchResponse{
			Provider: BackendReddit,
			Query:    "snapshot",
			Results: []Result{
				testResult(
					"second",
					"https://example.test/second",
					AuthorityCommunity,
					0.5,
					time.Now(),
				),
			},
		},
	}

	first.active = (*atomic.Int32)(&firstCalls)
	second.active = (*atomic.Int32)(&secondCalls)

	service := NewService(ServiceConfig{
		Backend: BackendAuto,
	})

	if err := service.Register(first); err != nil {
		t.Fatalf("Register first returned error: %v", err)
	}

	if err := service.Register(second); err != nil {
		t.Fatalf("Register second returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{
			Query: "snapshot",
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(response.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(response.Results))
	}

	if first.Calls() != 1 || second.Calls() != 1 {
		t.Fatalf(
			"expected one call per provider, got %d and %d",
			first.Calls(),
			second.Calls(),
		)
	}
}

func TestServiceSearchErrorWrapping(t *testing.T) {
	baseErr := fmt.Errorf("underlying provider error")

	provider := &mockResearchProvider{
		name: BackendGitHub,
		err:  baseErr,
	}

	service := NewService(ServiceConfig{
		Backend: BackendGitHub,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	_, err := service.Search(
		context.Background(),
		SearchRequest{Query: "wrapped"},
	)
	if !errors.Is(err, baseErr) {
		t.Fatalf("expected original error to be preserved, got %v", err)
	}
}

func TestServiceDoesNotRequireProviderIdentity(t *testing.T) {
	service := NewService(ServiceConfig{})

	if service == nil {
		t.Fatal("expected service")
	}

	if service.Backend() != BackendAuto {
		t.Fatalf("expected default backend %q, got %q", BackendAuto, service.Backend())
	}
}

func TestServiceSearchNormalizesRequestedQuery(t *testing.T) {
	provider := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "test query",
			Results: []Result{
				testResult(
					"result",
					"https://example.test/result",
					AuthorityCommunity,
					0.8,
					time.Now(),
				),
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend: BackendGitHub,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	response, err := service.Search(
		context.Background(),
		SearchRequest{
			Query: "   test query   ",
		},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if response.Query != "test query" {
		t.Fatalf("unexpected normalized query: %q", response.Query)
	}

	request := provider.LastQuery()

	if request.Query != "test query" {
		t.Fatalf("provider received unnormalized query: %q", request.Query)
	}
}

func TestServiceSearchZeroRequestUsesServiceDefault(t *testing.T) {
	provider := &mockResearchProvider{
		name: BackendGitHub,
		response: SearchResponse{
			Provider: BackendGitHub,
			Query:    "default",
			Results: []Result{
				testResult(
					"default",
					"https://example.test/default",
					AuthorityCommunity,
					0.8,
					time.Now(),
				),
			},
		},
	}

	service := NewService(ServiceConfig{
		Backend:    BackendGitHub,
		MaxResults: 3,
	})

	if err := service.Register(provider); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	_, err := service.Search(
		context.Background(),
		SearchRequest{Query: "default"},
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	request := provider.LastQuery()

	if request.MaxResults != 3 {
		t.Fatalf(
			"expected service default max results 3, got %d",
			request.MaxResults,
		)
	}
}
