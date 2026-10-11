package engine

// kv.go — native engine KV-cache concern (v1.1.5 Phase 1: TYPES ONLY).
//
// The future native engine will own its KV cache (cells, capacity,
// eviction, quantization). Phase 1 defines the data model; the metrics op
// reports the honest zero-state (strategy "none", no cells) because no
// model — hence no cache — exists.

// KVCacheStats is the KV-cache snapshot.
type KVCacheStats struct {
	// Strategy is the active cache strategy: "none" (no model), or a
	// future strategy id.
	Strategy string `json:"strategy,omitempty"`

	// Quantization of KV cells (e.g. "q8_0"); empty = unquantized or none.
	Quantization string `json:"quantization,omitempty"`

	// EstimatedCells is the number of allocated cache cells.
	EstimatedCells uint64 `json:"estimatedCells,omitempty"`

	// CapacityBytes is the cache capacity.
	CapacityBytes uint64 `json:"capacityBytes,omitempty"`
}
