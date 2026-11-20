package engine

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
)

// Backend is the device capability contract (work order section 5.6).
// Handles are opaque; resource ownership is explicit: everything a
// backend returns is released by its Close/Free paths, never by
// finalizers (GPU memory is deterministic, not GC-driven).
type Backend interface {
	// Info names the backend implementation and its driver/runtime
	// identity (for evidence records, not for capability inference).
	Info() BackendInfo

	// Capabilities declares the operators/dtypes this backend executes
	// for real — the registry consults this before planning; a missing
	// capability is a typed unsupported answer, never a silent CPU
	// fallback with a misleading label.
	Capabilities() BackendCapabilities

	// Allocate reserves device/host memory for a dense tensor.
	Allocate(dt DType, shape Shape) (*Buffer, error)

	// Upload copies host tensor bytes into a backend buffer.
	Upload(buf *Buffer, t *Tensor) error

	// Download copies a backend buffer back into a host tensor.
	Download(buf *Buffer, t *Tensor) error

	// Compile prepares an execution plan for a graph fragment (the CPU
	// reference backend validates and returns the graph unchanged).
	Compile(g *Graph) (*Plan, error)

	// Submit enqueues a compiled plan against named buffers.
	Submit(p *Plan, tensors map[string]*Tensor) error

	// Synchronize blocks until every submitted operation completes.
	Synchronize() error

	// MemoryInfo reports current and limit memory (evidence for the
	// device-truth chain; the CPU backend reports process-scoped truth).
	MemoryInfo() MemoryStats

	// Close releases every backend resource in deterministic order.
	Close() error
}

// BackendInfo is the identity evidence of a backend implementation.
type BackendInfo struct {
	Name           string // "cpu-reference"
	Device         string // "host"
	Driver         string // runtime detail (empty for the pure Go CPU path)
	RuntimeVersion string // Go runtime version (runtime.Version())
}

// BackendCapabilities declares what the backend REALLY executes.
type BackendCapabilities struct {
	Operators map[string]bool // "matmul", "add", ...
	DTypes    map[DType]bool
}

// MemoryStats is the memory evidence triple.
type MemoryStats struct {
	UsedBytes  uint64
	FreeBytes  uint64
	LimitBytes uint64
}

// Buffer is an opaque backend allocation. The CPU reference backend
// stores the bytes directly; device backends wrap their own handles.
// Release is EXPLICIT (Free); Close releases everything outstanding.
type Buffer struct {
	backend *CPUBackend
	dt      DType
	shape   Shape
	bytes   []byte
}

// Free releases one buffer explicitly (deterministic, no finalizer).
func (b *Buffer) Free() error {
	if b == nil || b.backend == nil {
		return nil
	}
	return b.backend.release(b)
}

// Plan is an opaque compiled execution plan.
type Plan struct {
	backend *CPUBackend
	graph   *Graph
}

// CPUBackend is the scalar CPU reference implementation — the
// correctness baseline of the whole engine.
type CPUBackend struct {
	mu      sync.Mutex
	buffers map[*Buffer]struct{}
	closed  bool
}

// NewCPUBackend constructs the reference backend.
func NewCPUBackend() *CPUBackend {
	return &CPUBackend{buffers: make(map[*Buffer]struct{})}
}

func (c *CPUBackend) Info() BackendInfo {
	return BackendInfo{
		Name:           "cpu-reference",
		Device:         "host",
		Driver:         "",
		RuntimeVersion: runtime.Version(),
	}
}

func (c *CPUBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{
		Operators: map[string]bool{
			"matmul": true, "add": true, "mul": true, "softmax": true,
			"rmsnorm": true, "embedding": true, "attention": true,
			"silu": true, "argmax": true,
		},
		DTypes: map[DType]bool{DTypeF32: true, DTypeI32: true},
	}
}

func (c *CPUBackend) Allocate(dt DType, shape Shape) (*Buffer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrEngineClosed
	}
	if !c.Capabilities().DTypes[dt] {
		return nil, &UnsupportedError{Kind: "dtype", Name: dt.String(), Details: "cpu-reference backend implements f32 and i32 only"}
	}
	t, err := NewTensor(dt, shape)
	if err != nil {
		return nil, err
	}
	buf := &Buffer{backend: c, dt: dt, shape: append(Shape{}, shape...), bytes: t.Bytes()}
	c.buffers[buf] = struct{}{}
	return buf, nil
}

func (c *CPUBackend) Upload(buf *Buffer, t *Tensor) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.buffers[buf]; !ok {
		return fmt.Errorf("%w: upload into a foreign or freed buffer", ErrMalformedContainer)
	}
	if t.DType != buf.dt || !sameShape(t.Shape, buf.shape) {
		return &ShapeError{Op: "upload", Expected: buf.dt.String() + " " + buf.shape.String(), Actual: t.DType.String() + " " + t.Shape.String()}
	}
	copy(buf.bytes, t.Bytes())
	return nil
}

func (c *CPUBackend) Download(buf *Buffer, t *Tensor) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.buffers[buf]; !ok {
		return fmt.Errorf("%w: download from a foreign or freed buffer", ErrMalformedContainer)
	}
	if t.DType != buf.dt || !sameShape(t.Shape, buf.shape) {
		return &ShapeError{Op: "download", Expected: buf.dt.String() + " " + buf.shape.String(), Actual: t.DType.String() + " " + t.Shape.String()}
	}
	copy(t.Bytes(), buf.bytes)
	return nil
}

func (c *CPUBackend) Compile(g *Graph) (*Plan, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	return &Plan{backend: c, graph: g}, nil
}

// Submit executes the plan on the host immediately (the reference
// backend is synchronous); Synchronize then returns instantly.
func (c *CPUBackend) Submit(p *Plan, tensors map[string]*Tensor) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrEngineClosed
	}
	return executeGraph(p.graph, tensors, c)
}

func (c *CPUBackend) Synchronize() error { return nil }

func (c *CPUBackend) MemoryInfo() MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return MemoryStats{
		UsedBytes:  m.Alloc,
		FreeBytes:  0,
		LimitBytes: 0, // 0 = process-scoped, no hard limit declared
	}
}

func (c *CPUBackend) release(buf *Buffer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.buffers[buf]; !ok {
		return nil // already released — idempotent by contract
	}
	delete(c.buffers, buf)
	buf.bytes = nil
	return nil
}

func (c *CPUBackend) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	for buf := range c.buffers {
		buf.bytes = nil
	}
	c.buffers = make(map[*Buffer]struct{})
	return nil
}

// OpKey is the operation-registry key: operator, dtype, and the
// backend-kind class (work order section 5.6: keyed by operation,
// dtype, shape, layout, quantization and backend capability).
type OpKey struct {
	Op           string
	DType        DType
	BackendClass string // "cpu" — device backends register their own classes
}

// kernelFn is the reference kernel signature: named operands resolved
// from the executor's tensor map.
type kernelFn func(tensors map[string]*Tensor, attrs map[string]string, out string) error

// opRegistry maps OpKey -> kernel. The CPU reference implementations
// are registered once at package init.
var opRegistry = map[OpKey]kernelFn{}

func init() {
	register("matmul", DTypeF32, "cpu", func(ts map[string]*Tensor, attrs map[string]string, out string) error {
		return matmulF32(ts["a"], ts["b"], ts[out])
	})
	register("add", DTypeF32, "cpu", func(ts map[string]*Tensor, attrs map[string]string, out string) error {
		return addF32(ts["a"], ts["b"], ts[out])
	})
	register("mul", DTypeF32, "cpu", func(ts map[string]*Tensor, attrs map[string]string, out string) error {
		return mulF32(ts["a"], ts["b"], ts[out])
	})
	register("softmax", DTypeF32, "cpu", func(ts map[string]*Tensor, attrs map[string]string, out string) error {
		return softmaxF32(ts["x"], ts[out])
	})
	register("rmsnorm", DTypeF32, "cpu", func(ts map[string]*Tensor, attrs map[string]string, out string) error {
		eps := 1e-5
		if v, ok := attrs["eps"]; ok {
			if _, err := fmt.Sscan(v, &eps); err != nil {
				return fmt.Errorf("rmsnorm attr eps %q: %v", v, err)
			}
		}
		return rmsNormF32(ts["x"], ts["weight"], ts[out], eps)
	})
	register("embedding", DTypeF32, "cpu", func(ts map[string]*Tensor, attrs map[string]string, out string) error {
		return embeddingF32(ts["table"], ts["ids"], ts[out])
	})
	register("attention", DTypeF32, "cpu", func(ts map[string]*Tensor, attrs map[string]string, out string) error {
		return causalAttentionF32(ts["q"], ts["k"], ts["v"], ts[out])
	})
	register("silu", DTypeF32, "cpu", func(ts map[string]*Tensor, attrs map[string]string, out string) error {
		return siluF32(ts["x"], ts[out])
	})
	register("argmax", DTypeF32, "cpu", func(ts map[string]*Tensor, attrs map[string]string, out string) error {
		idx, err := argmaxF32(ts["x"])
		if err != nil {
			return err
		}
		want := ts[out]
		if want.DType != DTypeI32 || !sameShape(want.Shape, idx.Shape) {
			return &ShapeError{Op: "argmax", Expected: want.DType.String() + " " + want.Shape.String(), Actual: idx.DType.String() + " " + idx.Shape.String()}
		}
		src, _ := idx.Ints()
		copy(want.Bytes(), i32AsBytes(src))
		return nil
	})
}

func register(op string, dt DType, class string, fn kernelFn) {
	opRegistry[OpKey{Op: op, DType: dt, BackendClass: class}] = fn
}

// lookupKernel resolves the registry (typed miss, never a guess).
func lookupKernel(key OpKey) (kernelFn, error) {
	if fn, ok := opRegistry[key]; ok {
		return fn, nil
	}
	registered := make([]string, 0, len(opRegistry))
	for k := range opRegistry {
		registered = append(registered, fmt.Sprintf("%s/%s/%s", k.Op, k.DType, k.BackendClass))
	}
	sort.Strings(registered)
	return nil, &UnsupportedError{
		Kind:    "operator",
		Name:    key.Op + "/" + key.DType.String() + "/" + key.BackendClass,
		Details: "registered reference kernels: " + fmt.Sprint(registered),
	}
}
