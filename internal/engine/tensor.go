package engine

import (
	"fmt"
	"math"
)

// DType enumerates the tensor element types the engine's kernels and
// validators implement. Only the dtypes with REAL CPU reference kernels
// are listed — a dtype absent from this enum is a typed unsupported
// answer, never a silent byte reinterpretation.
type DType uint8

const (
	DTypeF32 DType = iota + 1 // IEEE-754 binary32
	DTypeI32                  // signed 32-bit integer (token ids, indices)
)

// Size returns the element byte size. Every element count arithmetic in
// the engine goes through Size and checked multiplication.
func (d DType) Size() int {
	switch d {
	case DTypeF32:
		return 4
	case DTypeI32:
		return 4
	default:
		return 0
	}
}

func (d DType) String() string {
	switch d {
	case DTypeF32:
		return "f32"
	case DTypeI32:
		return "i32"
	default:
		return "unknown"
	}
}

// Shape is a row-major tensor shape. A scalar has len 0.
type Shape []int

// Numel returns the number of elements with OVERFLOW CHECKING: the
// multiplication is performed in int64 against a cap so a malicious or
// corrupt header can never wrap into a small allocation (work order
// section 5.2: "safe integer/shape multiplication and offset arithmetic").
func (s Shape) Numel() (int, error) {
	const maxElements = 1 << 40 // ~1T elements — far above every real tensor
	if len(s) == 0 {
		return 1, nil
	}
	n := int64(1)
	for _, dim := range s {
		if dim < 0 {
			return 0, fmt.Errorf("%w: negative dimension %d in shape %v", ErrShape, dim, []int(s))
		}
		if dim == 0 {
			return 0, nil
		}
		if n > maxElements/int64(dim) {
			return 0, fmt.Errorf("%w: shape %v exceeds the %d-element validation cap", ErrShape, []int(s), int64(maxElements))
		}
		n *= int64(dim)
	}
	return int(n), nil
}

func (s Shape) String() string {
	if len(s) == 0 {
		return "scalar"
	}
	out := "["
	for i, dim := range s {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprint(dim)
	}
	return out + "]"
}

// Tensor is a dense row-major tensor of one dtype. The layout contract:
//
//   - data holds Numel() elements (byte length == numel * dtype.Size());
//   - Strides are in ELEMENTS, computed by DefaultStrides, dense and
//     row-major;
//   - every index access is bounds-checked; offsets are computed with
//     checked arithmetic (checkedOffset below).
type Tensor struct {
	DType   DType
	Shape   Shape
	Strides []int
	data    []byte
}

// NewTensor allocates a validated dense tensor of the given shape. The
// data buffer is zero-initialized (never uninitialized memory).
func NewTensor(dt DType, shape Shape) (*Tensor, error) {
	t := &Tensor{DType: dt, Shape: append(Shape{}, shape...)}
	if err := t.validate(); err != nil {
		return nil, err
	}
	elem := dt.Size()
	if elem == 0 {
		return nil, &UnsupportedError{Kind: "dtype", Name: dt.String(), Details: "no reference kernel element size"}
	}
	numel, err := t.Shape.Numel()
	if err != nil {
		return nil, err
	}
	total, err := checkedMul(numel, elem)
	if err != nil {
		return nil, fmt.Errorf("%w: tensor allocation %s %s needs %d elements * %d bytes: %v", ErrMemory, dt, t.Shape, numel, elem, err)
	}
	t.data = make([]byte, total)
	t.Strides = DefaultStrides(t.Shape)
	return t, nil
}

// NewTensorFromBytes wraps existing bytes WITHOUT copying: the caller
// guarantees the slice stays alive and unmodified for the tensor's
// lifetime (mmap-style zero copy, used by the Safetensors adapter).
func NewTensorFromBytes(dt DType, shape Shape, data []byte) (*Tensor, error) {
	t := &Tensor{DType: dt, Shape: append(Shape{}, shape...), data: data}
	if err := t.validate(); err != nil {
		return nil, err
	}
	if dt.Size() == 0 {
		return nil, &UnsupportedError{Kind: "dtype", Name: dt.String(), Details: "no reference kernel element size"}
	}
	numel, err := t.Shape.Numel()
	if err != nil {
		return nil, err
	}
	want, err := checkedMul(numel, dt.Size())
	if err != nil {
		return nil, err
	}
	if len(data) != want {
		return nil, fmt.Errorf("%w: %s %s needs exactly %d data bytes, got %d", ErrMalformedContainer, dt, t.Shape, want, len(data))
	}
	t.Strides = DefaultStrides(t.Shape)
	return t, nil
}

// validate checks the dtype is implemented and the shape is sane.
func (t *Tensor) validate() error {
	if t.DType != DTypeF32 && t.DType != DTypeI32 {
		return &UnsupportedError{Kind: "dtype", Name: t.DType.String(), Details: "tensor dtype has no implemented reference kernel"}
	}
	if _, err := t.Shape.Numel(); err != nil {
		return err
	}
	return nil
}

// DefaultStrides returns dense row-major strides in ELEMENTS.
func DefaultStrides(shape Shape) []int {
	strides := make([]int, len(shape))
	acc := 1
	for i := len(shape) - 1; i >= 0; i-- {
		strides[i] = acc
		acc *= shape[i]
	}
	return strides
}

// Bytes exposes the raw backing bytes (read-mostly; adapters and tests).
func (t *Tensor) Bytes() []byte { return t.data }

// Floats views the data as f32 elements (dtype-checked).
func (t *Tensor) Floats() ([]float32, error) {
	if t.DType != DTypeF32 {
		return nil, fmt.Errorf("%w: Floats() on %s tensor", ErrUnsupportedDType, t.DType)
	}
	return bytesAsF32(t.data), nil
}

// Ints views the data as i32 elements (dtype-checked).
func (t *Tensor) Ints() ([]int32, error) {
	if t.DType != DTypeI32 {
		return nil, fmt.Errorf("%w: Ints() on %s tensor", ErrUnsupportedDType, t.DType)
	}
	return bytesAsI32(t.data), nil
}

// bytesAsF32 / bytesAsI32 / f32AsBytes / i32AsBytes: explicit
// little-endian element codecs (architecture-independent byte order —
// the container adapters are responsible for byte-order declarations;
// the reference kernels use this fixed order).
func bytesAsF32(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		bits := uint32(b[4*i]) | uint32(b[4*i+1])<<8 | uint32(b[4*i+2])<<16 | uint32(b[4*i+3])<<24
		out[i] = math.Float32frombits(bits)
	}
	return out
}

func bytesAsI32(b []byte) []int32 {
	out := make([]int32, len(b)/4)
	for i := range out {
		bits := uint32(b[4*i]) | uint32(b[4*i+1])<<8 | uint32(b[4*i+2])<<16 | uint32(b[4*i+3])<<24
		out[i] = int32(bits)
	}
	return out
}

func f32AsBytes(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		bits := math.Float32bits(x)
		out[4*i] = byte(bits)
		out[4*i+1] = byte(bits >> 8)
		out[4*i+2] = byte(bits >> 16)
		out[4*i+3] = byte(bits >> 24)
	}
	return out
}

func i32AsBytes(v []int32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		bits := uint32(x)
		out[4*i] = byte(bits)
		out[4*i+1] = byte(bits >> 8)
		out[4*i+2] = byte(bits >> 16)
		out[4*i+3] = byte(bits >> 24)
	}
	return out
}

// checkedMul multiplies two non-negative ints with an overflow check.
func checkedMul(a, b int) (int, error) {
	if a < 0 || b < 0 {
		return 0, fmt.Errorf("%w: negative operand in checked multiplication (%d, %d)", ErrShape, a, b)
	}
	prod := int64(a) * int64(b)
	if prod > math.MaxInt32 {
		return 0, fmt.Errorf("checked multiplication overflow: %d * %d = %d", a, b, prod)
	}
	return int(prod), nil
}

// checkedAdd adds two non-negative ints with an overflow check.
func checkedAdd(a, b int) (int, error) {
	sum := int64(a) + int64(b)
	if sum > math.MaxInt32 {
		return 0, fmt.Errorf("checked addition overflow: %d + %d", a, b)
	}
	return int(sum), nil
}

// checkedOffset computes the flat element offset of a multi-dimensional
// index against the strides, bounds-checked against numel.
func checkedOffset(shape, strides []int, index []int, numel int) (int, error) {
	if len(index) != len(shape) {
		return 0, fmt.Errorf("%w: index rank %d does not match shape rank %d", ErrShape, len(index), len(shape))
	}
	off := 0
	for i, ix := range index {
		if ix < 0 || ix >= shape[i] {
			return 0, fmt.Errorf("%w: index[%d]=%d outside dimension %d of shape %v", ErrShape, i, ix, shape[i], shape)
		}
		o, err := checkedMul(ix, strides[i])
		if err != nil {
			return 0, err
		}
		off, err = checkedAdd(off, o)
		if err != nil {
			return 0, err
		}
	}
	if off >= numel {
		return 0, fmt.Errorf("%w: flat offset %d outside %d elements", ErrShape, off, numel)
	}
	return off, nil
}
