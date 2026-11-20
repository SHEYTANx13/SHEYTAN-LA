package engine

import (
	"errors"
	"math"
	"testing"
)

func TestShapeNumelOverflow(t *testing.T) {
	// A hostile shape must never wrap into a small allocation.
	huge := Shape{1 << 20, 1 << 20, 1 << 20}
	if _, err := huge.Numel(); !errors.Is(err, ErrShape) {
		t.Fatalf("huge shape: want ErrShape, got %v", err)
	}
	neg := Shape{4, -1}
	if _, err := neg.Numel(); !errors.Is(err, ErrShape) {
		t.Fatalf("negative dim: want ErrShape, got %v", err)
	}
	zero := Shape{4, 0}
	n, err := zero.Numel()
	if err != nil || n != 0 {
		t.Fatalf("zero dim: want (0,nil), got (%d,%v)", n, err)
	}
	if n, err := (Shape{}).Numel(); err != nil || n != 1 {
		t.Fatalf("scalar: want (1,nil), got (%d,%v)", n, err)
	}
	if n, err := (Shape{1024, 1024}).Numel(); err != nil || n != 1024*1024 {
		t.Fatalf("plain: want (1048576,nil), got (%d,%v)", n, err)
	}
}

func TestCheckedArithmetic(t *testing.T) {
	if _, err := checkedMul(math.MaxInt32, 2); err == nil {
		t.Fatal("checkedMul overflow must fail")
	}
	if _, err := checkedMul(-1, 4); err == nil {
		t.Fatal("checkedMul negative operand must fail")
	}
	if v, err := checkedMul(1024, 1024); err != nil || v != 1048576 {
		t.Fatalf("checkedMul: got (%d,%v)", v, err)
	}
	if _, err := checkedAdd(math.MaxInt32, 1); err == nil {
		t.Fatal("checkedAdd overflow must fail")
	}
}

func TestTensorValidation(t *testing.T) {
	if _, err := NewTensor(DType(99), Shape{2}); err == nil {
		t.Fatal("unknown dtype must be a typed unsupported answer")
	}
	t4, err := NewTensor(DTypeF32, Shape{2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(t4.Bytes()); got != 24 {
		t.Fatalf("f32 (2,3) buffer: want 24 bytes, got %d", got)
	}
	want := []int{3, 1}
	for i, s := range t4.Strides {
		if s != want[i] {
			t.Fatalf("stride %d: want %d, got %d", i, want[i], s)
		}
	}
	if _, err := NewTensorFromBytes(DTypeF32, Shape{2}, make([]byte, 5)); err == nil {
		t.Fatal("byte-length mismatch must fail (container discipline)")
	}
	zero, err := NewTensor(DTypeF32, Shape{0, 4})
	if err != nil {
		t.Fatalf("zero-size tensor: %v", err)
	}
	if len(zero.Bytes()) != 0 {
		t.Fatal("zero-size tensor must have an empty buffer")
	}
}

func TestCheckedOffsetBounds(t *testing.T) {
	shape := Shape{3, 4}
	strides := DefaultStrides(shape)
	off, err := checkedOffset(shape, strides, []int{2, 3}, 12)
	if err != nil || off != 11 {
		t.Fatalf("offset(2,3): want (11,nil), got (%d,%v)", off, err)
	}
	if _, err := checkedOffset(shape, strides, []int{3, 0}, 12); !errors.Is(err, ErrShape) {
		t.Fatalf("out-of-range index: want ErrShape, got %v", err)
	}
	if _, err := checkedOffset(shape, strides, []int{1}, 12); !errors.Is(err, ErrShape) {
		t.Fatalf("rank mismatch: want ErrShape, got %v", err)
	}
}
