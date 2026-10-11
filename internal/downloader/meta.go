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
	"os"
	"path/filepath"
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

// sourceCacheFile layout: <CacheDir>/sheytan-source-cache/<safe-key>.json
func sourceCachePath(cacheDir, key string) string {
	name := sanitizeKey(key)
	return filepath.Join(cacheDir, "sheytan-source-cache", name+".json")
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

func loadSourceCache(cacheDir, key string) *cachedSource {
	if cacheDir == "" || key == "" {
		return nil
	}
	raw, err := os.ReadFile(sourceCachePath(cacheDir, key))
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
	dir := filepath.Dir(sourceCachePath(cacheDir, key))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp := sourceCachePath(cacheDir, key) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, sourceCachePath(cacheDir, key))
}
