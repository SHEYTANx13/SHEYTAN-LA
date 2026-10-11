package engine

// memory.go — native engine memory concern (v1.1.5 Phase 1: TYPES ONLY).
//
// The future native engine will budget memory explicitly across weights,
// KV cache and compute buffers before loading a model. Phase 1 defines the
// data model; MemoryUsage.NativeRSSBytes is the one field measured today
// (reported by the C++ metrics op from its own process).

// MemoryUsage is the native engine's memory snapshot.
type MemoryUsage struct {
	// WeightsBytes: resident model weights. Zero until a model loads.
	WeightsBytes uint64 `json:"weightsBytes,omitempty"`

	// KVCacheBytes: KV-cache cells. Zero until a model loads.
	KVCacheBytes uint64 `json:"kvCacheBytes,omitempty"`

	// ComputeBuffersBytes: scratch/compute buffers. Zero until inference
	// allocates them.
	ComputeBuffersBytes uint64 `json:"computeBuffersBytes,omitempty"`

	// NativeRSSBytes: MEASURED resident set of the engine process.
	NativeRSSBytes uint64 `json:"nativeRssBytes,omitempty"`
}

// MemoryPlan is the forward-looking allocation plan the future loader
// will produce before loading a model (bounded by the hardware profile's
// available RAM/VRAM). Phase 1 defines the shape only — no planner exists.
type MemoryPlan struct {
	// BudgetBytes is the total envelope granted to the engine.
	BudgetBytes uint64 `json:"budgetBytes,omitempty"`

	// WeightsBytes / KVCacheBytes / ComputeBuffersBytes are the planned
	// allocations. Zero until a planner exists.
	WeightsBytes        uint64 `json:"weightsBytes,omitempty"`
	KVCacheBytes        uint64 `json:"kvCacheBytes,omitempty"`
	ComputeBuffersBytes uint64 `json:"computeBuffersBytes,omitempty"`

	// Device names which device each allocation targets ("cpu", a GPU id,
	// an accelerator id) once multi-device placement exists.
	WeightsDevice string `json:"weightsDevice,omitempty"`
	KVDevice      string `json:"kvDevice,omitempty"`
}
