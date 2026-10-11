package research

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type countingProvider struct {
	mu sync.Mutex

	name       string
	calls      int
	response   SearchResponse
	err        error
	mutateHook func(*SearchResponse)
}

func (p *countingProvider) Name() string {
	return p.name
}

func (p *countingProvider) Search(
	_ context.Context,
	_ SearchRequest,
) (SearchResponse, error) {
	p.mu.Lock()
	p.calls++
	response := cloneSearchResponse(p.response)
	err := p.err
	hook := p.mutateHook
	p.mu.Unlock()

	if hook != nil {
		hook(&response)
	}

	return response, err
}

func (p *countingProvider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.calls
}

func testResearchResponse() SearchResponse {
	return SearchResponse{
		Provider: "test",
		Query:    "golang repair",
		Results: []Result{
			{
				Title:       "Go Repair",
				URL:         "https://example.com/go-repair",
				Snippet:     "repair example",
				Source:      "example",
				Provider:    "test",
				Authority:   AuthorityTechnicalDiscussion,
				MatchScore:  0.8,
				ContentHash: "abc123",
				Metadata: map[string]any{
					"kind": "original",
				},
			},
		},
	}
}

func TestCachedProviderReturnsCachedResult(t *testing.T) {
	provider := &countingProvider{
		name:     "test",
		response: testResearchResponse(),
	}

	cached := NewCachedProvider(
		provider,
		time.Minute,
	)

	req := SearchRequest{
		Query:      "golang repair",
		MaxResults: 8,
	}

	first, err := cached.Search(
		context.Background(),
		req,
	)
	if err != nil {
		t.Fatalf("first search failed: %v", err)
	}

	second, err := cached.Search(
		context.Background(),
		req,
	)
	if err != nil {
		t.Fatalf("second search failed: %v", err)
	}

	if provider.Calls() != 1 {
		t.Fatalf(
			"expected exactly one provider call, got %d",
			provider.Calls(),
		)
	}

	if len(first.Results) != 1 ||
		len(second.Results) != 1 {
		t.Fatalf("expected one result from both searches")
	}

	if first.Results[0].URL != second.Results[0].URL {
		t.Fatalf("cached result differs from original result")
	}
}

func TestCachedProviderExpiresAfterTTL(t *testing.T) {
	provider := &countingProvider{
		name:     "test",
		response: testResearchResponse(),
	}

	cached := NewCachedProvider(
		provider,
		20*time.Millisecond,
	)

	req := SearchRequest{
		Query:      "ttl test",
		MaxResults: 4,
	}

	if _, err := cached.Search(
		context.Background(),
		req,
	); err != nil {
		t.Fatalf("first search failed: %v", err)
	}

	time.Sleep(40 * time.Millisecond)

	if _, err := cached.Search(
		context.Background(),
		req,
	); err != nil {
		t.Fatalf("post-expiry search failed: %v", err)
	}

	if provider.Calls() != 2 {
		t.Fatalf(
			"expected cache miss after TTL expiry, got %d provider calls",
			provider.Calls(),
		)
	}
}

func TestCachedProviderDoesNotCacheErrors(t *testing.T) {
	expectedErr := errors.New("provider temporarily unavailable")

	provider := &countingProvider{
		name:     "test",
		response: testResearchResponse(),
		err:      expectedErr,
	}

	cached := NewCachedProvider(
		provider,
		time.Minute,
	)

	req := SearchRequest{
		Query:      "error test",
		MaxResults: 4,
	}

	_, err := cached.Search(
		context.Background(),
		req,
	)
	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"expected original provider error, got %v",
			err,
		)
	}

	_, err = cached.Search(
		context.Background(),
		req,
	)
	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"expected original provider error on second call, got %v",
			err,
		)
	}

	if provider.Calls() != 2 {
		t.Fatalf(
			"expected errors not to be cached, got %d provider calls",
			provider.Calls(),
		)
	}
}

func TestCachedProviderDisabledDelegatesEveryCall(t *testing.T) {
	provider := &countingProvider{
		name:     "test",
		response: testResearchResponse(),
	}

	cached := NewCachedProvider(
		provider,
		0,
	)

	req := SearchRequest{
		Query:      "disabled cache",
		MaxResults: 4,
	}

	if _, err := cached.Search(
		context.Background(),
		req,
	); err != nil {
		t.Fatalf("first search failed: %v", err)
	}

	if _, err := cached.Search(
		context.Background(),
		req,
	); err != nil {
		t.Fatalf("second search failed: %v", err)
	}

	if provider.Calls() != 2 {
		t.Fatalf(
			"expected disabled cache to delegate twice, got %d calls",
			provider.Calls(),
		)
	}
}

func TestCachedProviderReturnsIndependentCopies(t *testing.T) {
	provider := &countingProvider{
		name:     "test",
		response: testResearchResponse(),
	}

	cached := NewCachedProvider(
		provider,
		time.Minute,
	)

	req := SearchRequest{
		Query:      "copy test",
		MaxResults: 4,
	}

	first, err := cached.Search(
		context.Background(),
		req,
	)
	if err != nil {
		t.Fatalf("first search failed: %v", err)
	}

	first.Results[0].Title = "MUTATED"
	first.Results[0].Metadata["kind"] = "mutated"

	second, err := cached.Search(
		context.Background(),
		req,
	)
	if err != nil {
		t.Fatalf("second search failed: %v", err)
	}

	if provider.Calls() != 1 {
		t.Fatalf(
			"expected second search to be served from cache, got %d provider calls",
			provider.Calls(),
		)
	}

	if second.Results[0].Title == "MUTATED" {
		t.Fatal("cached title was mutated through returned result")
	}

	if second.Results[0].Metadata["kind"] == "mutated" {
		t.Fatal("cached metadata was mutated through returned result")
	}
}

func TestCachedProviderSeparatesQueriesAndLimits(t *testing.T) {
	provider := &countingProvider{
		name:     "test",
		response: testResearchResponse(),
	}

	cached := NewCachedProvider(
		provider,
		time.Minute,
	)

	reqA := SearchRequest{
		Query:      "same query",
		MaxResults: 4,
	}

	reqB := SearchRequest{
		Query:      "same query",
		MaxResults: 8,
	}

	if _, err := cached.Search(
		context.Background(),
		reqA,
	); err != nil {
		t.Fatalf("request A failed: %v", err)
	}

	if _, err := cached.Search(
		context.Background(),
		reqB,
	); err != nil {
		t.Fatalf("request B failed: %v", err)
	}

	if provider.Calls() != 2 {
		t.Fatalf(
			"expected distinct MaxResults values to use distinct cache entries, got %d calls",
			provider.Calls(),
		)
	}
}

func TestCachedProviderNormalizesCacheKeyCaseAndWhitespace(t *testing.T) {
	provider := &countingProvider{
		name:     "test",
		response: testResearchResponse(),
	}

	cached := NewCachedProvider(
		provider,
		time.Minute,
	)

	reqA := SearchRequest{
		Query:      "  GoLang Repair  ",
		MaxResults: 4,
	}

	reqB := SearchRequest{
		Query:      "golang repair",
		MaxResults: 4,
	}

	if _, err := cached.Search(
		context.Background(),
		reqA,
	); err != nil {
		t.Fatalf("request A failed: %v", err)
	}

	if _, err := cached.Search(
		context.Background(),
		reqB,
	); err != nil {
		t.Fatalf("request B failed: %v", err)
	}

	if provider.Calls() != 1 {
		t.Fatalf(
			"expected normalized requests to share cache entry, got %d calls",
			provider.Calls(),
		)
	}
}
