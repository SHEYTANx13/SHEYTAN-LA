package config

import "sync"

// Source is the concurrency-safe holder for the live runtime configuration.
//
// v1.1.4: prior versions mutated the single shared *Config in place from
// the HTTP config-patch handler (mergeConfigPatch did `*s.cfg = updated`)
// while agent runs, the LLM client and the engine manager concurrently
// read the same struct — a genuine data race (flagged under -race and by
// inspection). Source replaces that pattern with copy-on-write:
//
//   - Load() returns the current *Config pointer. Published values are
//     IMMUTABLE BY CONTRACT: nothing may mutate a Config obtained from
//     Load() after it has been stored.
//   - Update() copies the current value, applies the mutation to the
//     private copy, and atomically publishes the new pointer.
//   - Callers that need a consistent view for the duration of an
//     operation (an agent run, an engine start, one HTTP request) take a
//     single snapshot via Load() and use it throughout.
//
// Shallow copies are sufficient: slice/map fields are only ever replaced
// wholesale (JSON patches decode into fresh allocations), never mutated
// element-wise. Construction-time consumers (tools that captured a Config
// for their base directories) keep working unchanged — they simply observe
// the configuration as of their construction, which matches the previous
// behavior for every field they read.
type Source struct {
	mu  sync.RWMutex
	cfg *Config
}

// NewSource wraps an initial configuration value.
func NewSource(cfg *Config) *Source {
	if cfg == nil {
		cfg = Default()
	}
	return &Source{cfg: cfg}
}

// Load returns the current published configuration. The returned pointer
// must be treated as read-only.
func (s *Source) Load() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Store publishes a new configuration value.
func (s *Source) Store(cfg *Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

// Update copies the current configuration, applies mutate to the copy, and
// publishes the result. It returns the newly published value (read-only
// for the caller). This is the ONLY sanctioned way to change live config.
func (s *Source) Update(mutate func(*Config)) *Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := *s.cfg
	mutate(&next)
	s.cfg = &next
	return s.cfg
}

// UpdateErr is Update for mutators that can fail. The stored value is left
// untouched when mutate returns an error.
func (s *Source) UpdateErr(mutate func(*Config) error) (*Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := *s.cfg
	if err := mutate(&next); err != nil {
		return s.cfg, err
	}
	s.cfg = &next
	return s.cfg, nil
}
