package presentation

import "sync"

// DefaultStoreMax is the default bounded capacity of one Store. The store
// is a presentation cache, NOT a session authority: the durable transcript
// lives in the session store, artifacts live in the artifact authority.
const DefaultStoreMax = 256

// Store is the ONE bounded, Go-owned block registry of the hybrid shell.
//
// Semantics:
//   - Upsert by ID: putting a block with an existing ID replaces it — one
//     ID is one logical artifact, and streaming updates never duplicate.
//   - Bounded: the oldest blocks evict first (insertion-order LRU); the
//     bound keeps memory flat regardless of session length.
//   - Concurrency-safe: producers (run settlement paths) and the
//     workspace server (snapshot reads) run on different goroutines.
type Store struct {
	mu    sync.RWMutex
	order []string         // insertion order, oldest first
	byID  map[string]Block // canonical blocks
	max   int
}

// NewStore builds a store with the given bound (≤0 → DefaultStoreMax).
func NewStore(max int) *Store {
	if max <= 0 {
		max = DefaultStoreMax
	}
	return &Store{byID: make(map[string]Block, 16), max: max}
}

// Put validates (canonicalizing in place) and stores b. A block whose ID
// already exists is replaced in place (one ID → one logical artifact).
// Invalid blocks are refused with the typed error and NOT stored — the
// native shell rendering is unaffected, so refusal loses nothing.
func (s *Store) Put(b Block) (Block, error) {
	if err := b.Validate(); err != nil {
		return Block{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byID[b.ID]; !exists {
		s.order = append(s.order, b.ID)
		for len(s.order) > s.max {
			old := s.order[0]
			s.order = s.order[1:]
			delete(s.byID, old)
		}
	}
	s.byID[b.ID] = b
	return b, nil
}

// Get returns the canonical block by ID.
func (s *Store) Get(id string) (Block, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.byID[id]
	return b, ok
}

// List returns the bounded metadata snapshot, oldest first. The slice is
// a copy; the caller cannot mutate the store through it.
func (s *Store) List() []Meta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Meta, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, MetaOf(s.byID[id]))
	}
	return out
}

// Len is the current block count.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byID)
}
