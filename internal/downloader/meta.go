// meta.go — the source cache: remembers which source last produced a
// VERIFIED download for a given cache key, so routine startup tries the
// known-good endpoint first instead of probing every mirror.
//
// The cache is deliberately advisory-only: a cached source is reordered
// to the front, never added, and only while fresh (SourceCacheTTL). Any
// failure falls through to the normal ordered list.
package downloader

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// cachedSource is one remembered source selection.
type cachedSource struct {
	URL   string `json:"url"`
	Label string `json:"label,omitempty"`
	Trust string `json:"trust,omitempty"`
	OKAt  string `json:"okAt"` // RFC3339 of the last verified success
}

func (c *cachedSource) fresh() bool {
	t, err := time.Parse(time.RFC3339, c.OKAt)
	if err != nil {
		return false
	}
	return time.Since(t) < SourceCacheTTL
}

// errUnsafeCacheKey marks a cache key that cannot be mapped to a safe
// on-disk file name (v1.11.1 path-injection remediation). Cache keys
// embed REMOTE identifiers (release tags, manifest versions, platform
// ids, asset names), so they are treated as hostile data.
var errUnsafeCacheKey = errors.New("downloader: cache key does not map to a safe cache file name")

// maxCacheKeyLen bounds a cache key (and therefore its slugged file
// name) long before NAME_MAX.
const maxCacheKeyLen = 200

// sourceCachePath maps a cache key to its on-disk location
// "<CacheDir>/sheytan-source-cache/<safe-key>.json" and FAILS CLOSED for
// keys that cannot be mapped to a safe single path component
// (v1.11.1 path-injection remediation): sanitizeKey slugs the key into a
// generated local name, and the RESULT is re-validated against the
// strict single-component grammar before any path is built. A hostile
// key produces an error, never a path.
func sourceCachePath(cacheDir, key string) (string, error) {
	if cacheDir == "" || key == "" {
		return "", fmt.Errorf("%w (empty)", errUnsafeCacheKey)
	}
	if len(key) > maxCacheKeyLen {
		return "", fmt.Errorf("%w (length %d exceeds %d)", errUnsafeCacheKey, len(key), maxCacheKeyLen)
	}
	name := sanitizeKey(key)
	if !validCacheName(name) {
		return "", fmt.Errorf("%w %q", errUnsafeCacheKey, key)
	}
	return filepath.Join(cacheDir, "sheytan-source-cache", name+".json"), nil
}

func sanitizeKey(key string) string {
	out := make([]rune, 0, len(key))
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		out = append(out, 'k')
	}
	return string(out)
}

// validCacheName is the strict single-component grammar for the
// SANITIZED cache-file stem: no separators (POSIX and Windows), no NUL
// or volume/ADS colons, no leading dots (a hostile all-dot key such as
// ".." slugs to a dotted stem that must never become a hidden or
// traversal name), no whitespace padding and no non-grammar characters.
func validCacheName(name string) bool {
	if name == "" || len(name) > maxCacheKeyLen {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if strings.ContainsAny(name, "\x00:") {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	if strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func loadSourceCache(cacheDir, key string) *cachedSource {
	if cacheDir == "" || key == "" {
		return nil
	}
	path, err := sourceCachePath(cacheDir, key)
	if err != nil {
		return nil // hostile key: no cache hit — fall through to the normal order
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c cachedSource
	if err := json.Unmarshal(raw, &c); err != nil || c.URL == "" {
		return nil
	}
	if !c.fresh() {
		return nil
	}
	return &c
}

func recordSourceCache(cacheDir, key string, src Source) {
	if cacheDir == "" || key == "" || src.URL == "" {
		return
	}
	path, err := sourceCachePath(cacheDir, key)
	if err != nil {
		return // hostile key: the cache entry is never written anywhere
	}
	c := cachedSource{
		URL:   src.URL,
		Label: src.Label,
		Trust: string(src.Trust),
		OKAt:  time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
