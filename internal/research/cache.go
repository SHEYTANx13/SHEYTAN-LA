package research

import (
	"context"
	"strings"
	"sync"
	"time"
)

const defaultResearchCacheMaxEntries = 128

// CachedProvider wraps a research provider with a bounded in-memory TTL cache.
//
// Only successful responses containing at least one result are cached.
// Provider errors and empty-result responses are never cached.
type CachedProvider struct {
	provider   Provider
	ttl        time.Duration
	maxEntries int

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	response SearchResponse
	expires  time.Time
}

// NewCachedProvider wraps provider with an in-memory TTL cache.
//
// A non-positive ttl disables caching and returns the original provider.
func NewCachedProvider(
	provider Provider,
	ttl time.Duration,
) Provider {
	if provider == nil || ttl <= 0 {
		return provider
	}

	return &CachedProvider{
		provider:   provider,
		ttl:        ttl,
		maxEntries: defaultResearchCacheMaxEntries,
		entries:    make(map[string]cacheEntry),
	}
}

// Name preserves the wrapped provider's canonical identity.
func (c *CachedProvider) Name() string {
	return c.provider.Name()
}

// Search returns a cached successful response when available; otherwise it
// delegates to the wrapped provider and caches a successful non-empty result.
func (c *CachedProvider) Search(
	ctx context.Context,
	req SearchRequest,
) (SearchResponse, error) {
	if c == nil || c.provider == nil {
		return SearchResponse{}, ErrProviderUnavailable
	}

	key := researchCacheKey(req)

	if response, ok := c.get(key); ok {
		return response, nil
	}

	response, err := c.provider.Search(ctx, req)
	if err != nil {
		return response, err
	}

	response = NormalizeResponse(response)

	if len(response.Results) == 0 {
		return response, nil
	}

	c.put(
		key,
		response,
	)

	return response, nil
}

func researchCacheKey(req SearchRequest) string {
	query := strings.ToLower(
		strings.TrimSpace(req.Query),
	)

	return query + "\x00" + formatCacheInt(req.MaxResults)
}

func formatCacheInt(value int) string {
	// Avoid fmt just for a tiny deterministic integer encoding.
	if value == 0 {
		return "0"
	}

	negative := value < 0
	if negative {
		value = -value
	}

	var buf [20]byte
	index := len(buf)

	for value > 0 {
		index--
		buf[index] = byte('0' + value%10)
		value /= 10
	}

	if negative {
		index--
		buf[index] = '-'
	}

	return string(buf[index:])
}

func (c *CachedProvider) get(
	key string,
) (SearchResponse, bool) {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return SearchResponse{}, false
	}

	if !now.Before(entry.expires) {
		delete(c.entries, key)
		return SearchResponse{}, false
	}

	return cloneSearchResponse(entry.response), true
}

func (c *CachedProvider) put(
	key string,
	response SearchResponse,
) {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxEntries {
		c.pruneExpiredLocked(now)
	}

	if len(c.entries) >= c.maxEntries {
		c.evictOldestLocked()
	}

	c.entries[key] = cacheEntry{
		response: cloneSearchResponse(response),
		expires:  now.Add(c.ttl),
	}
}

func (c *CachedProvider) pruneExpiredLocked(
	now time.Time,
) {
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
		}
	}
}

func (c *CachedProvider) evictOldestLocked() {
	var (
		oldestKey    string
		oldestExpiry time.Time
	)

	for key, entry := range c.entries {
		if oldestKey == "" ||
			entry.expires.Before(oldestExpiry) {
			oldestKey = key
			oldestExpiry = entry.expires
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func cloneSearchResponse(
	response SearchResponse,
) SearchResponse {
	cloned := response
	cloned.Results = make(
		[]Result,
		len(response.Results),
	)

	for index, result := range response.Results {
		cloned.Results[index] = cloneResult(result)
	}

	return cloned
}

func cloneResult(
	result Result,
) Result {
	cloned := result

	if result.Metadata != nil {
		cloned.Metadata = make(
			map[string]any,
			len(result.Metadata),
		)

		for key, value := range result.Metadata {
			cloned.Metadata[key] = value
		}
	}

	return cloned
}
