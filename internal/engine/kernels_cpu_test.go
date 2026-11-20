package engine

import (
	"errors"
	"math"
	"testing"
)

func mustTensor(t *testing.T, dt DType, shape Shape) *Tensor {
	t.Helper()
	tensor, err := NewTensor(dt, shape)
	if err != nil {
		t.Fatalf("NewTensor %s %s: %v", dt, shape, err)
	}
	return tensor
}

func setF32(t *testing.T, tensor *Tensor, vals []float32) {
	t.Helper()
	// Floats() is a detached typed copy: the WRITE path must go through
	// the tensor bytes (the same discipline the kernels follow).
	copy(tensor.Bytes(), f32AsBytes(vals))
}

func getF32(t *testing.T, tensor *Tensor) []float32 {
	t.Helper()
	v, err := tensor.Floats()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }

func TestMatmulReference(t *testing.T) {
	// Hand-computed: A (2,3) x B (3,2).
	a := mustTensor(t, DTypeF32, Shape{2, 3})
	setF32(t, a, []float32{1, 2, 3, 4, 5, 6})
	b := mustTensor(t, DTypeF32, Shape{3, 2})
	setF32(t, b, []float32{7, 8, 9, 10, 11, 12})
	c := mustTensor(t, DTypeF32, Shape{2, 2})

	if err := matmulF32(a, b, c); err != nil {
		t.Fatal(err)
	}
	got := getF32(t, c)
	want := []float32{1*7 + 2*9 + 3*11, 1*8 + 2*10 + 3*12, 4*7 + 5*9 + 6*11, 4*8 + 5*10 + 6*12}
	for i := range want {
		if !approx(got[i], want[i]) {
			t.Fatalf("c[%d]: want %g, got %g", i, want[i], got[i])
		}
	}

	// Shape disagreement is a typed error.
	bad := mustTensor(t, DTypeF32, Shape{2, 1})
	if err := matmulF32(a, b, bad); !errors.Is(err, ErrShape) {
		t.Fatalf("shape mismatch: want ErrShape, got %v", err)
	}
}

func TestSoftmaxStable(t *testing.T) {
	x := mustTensor(t, DTypeF32, Shape{3})
	setF32(t, x, []float32{1000, 1001, 1002}) // e^1000 overflows without max-subtraction
	y := mustTensor(t, DTypeF32, Shape{3})
	if err := softmaxF32(x, y); err != nil {
		t.Fatal(err)
	}
	got := getF32(t, y)
	sum := got[0] + got[1] + got[2]
	if math.Abs(float64(sum-1)) > 1e-6 {
		t.Fatalf("softmax must sum to 1, got %g", sum)
	}
	if !approx(got[2], float32(1)/float32(1+math.Exp(-1)+math.Exp(-2))*(1)) {
		t.Fatalf("softmax[2] out of range: %g", got[2])
	}
	// Ascending inputs give ascending probabilities.
	if !(got[0] < got[1] && got[1] < got[2]) {
		t.Fatalf("softmax order broken: %v", got)
	}
}

func TestRmsNormReference(t *testing.T) {
	x := mustTensor(t, DTypeF32, Shape{4})
	setF32(t, x, []float32{1, 1, 1, 1})
	w := mustTensor(t, DTypeF32, Shape{4})
	setF32(t, w, []float32{2, 2, 2, 2})
	y := mustTensor(t, DTypeF32, Shape{4})
	if err := rmsNormF32(x, w, y, 1e-5); err != nil {
		t.Fatal(err)
	}
	got := getF32(t, y)
	// mean(x^2)=1, inv = 1/sqrt(1+eps) ~ 1 -> y ~ 2 for each element.
	for i, v := range got {
		if math.Abs(float64(v-2)) > 1e-3 {
			t.Fatalf("rmsnorm[%d]: want ~2, got %g", i, v)
		}
	}
}

func TestEmbeddingBounds(t *testing.T) {
	table := mustTensor(t, DTypeF32, Shape{4, 2})
	setF32(t, table, []float32{1, 2, 3, 4, 5, 6, 7, 8})
	ids := mustTensor(t, DTypeI32, Shape{3})
	copy(ids.Bytes(), i32AsBytes([]int32{3, 0, 1}))
	out := mustTensor(t, DTypeF32, Shape{3, 2})
	if err := embeddingF32(table, ids, out); err != nil {
		t.Fatal(err)
	}
	got := getF32(t, out)
	want := []float32{7, 8, 1, 2, 3, 4}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("embedding[%d]: want %g, got %g", i, want[i], got[i])
		}
	}
	copy(ids.Bytes(), i32AsBytes([]int32{0, 4, 1}))
	if err := embeddingF32(table, ids, out); !errors.Is(err, ErrShape) {
		t.Fatalf("out-of-vocab id: want ErrShape, got %v", err)
	}
	copy(ids.Bytes(), i32AsBytes([]int32{0, -2, 1}))
	if err := embeddingF32(table, ids, out); !errors.Is(err, ErrShape) {
		t.Fatalf("negative id: want ErrShape, got %v", err)
	}
}

func TestCausalAttentionReference(t *testing.T) {
	// t=2, d=2, hand-computed: q0=k0=v0=(1,0), q1=k1=v1=(0,1).
	q := mustTensor(t, DTypeF32, Shape{2, 2})
	setF32(t, q, []float32{1, 0, 0, 1})
	k := mustTensor(t, DTypeF32, Shape{2, 2})
	setF32(t, k, []float32{1, 0, 0, 1})
	v := mustTensor(t, DTypeF32, Shape{2, 2})
	setF32(t, v, []float32{1, 0, 0, 1})
	out := mustTensor(t, DTypeF32, Shape{2, 2})
	if err := causalAttentionF32(q, k, v, out); err != nil {
		t.Fatal(err)
	}
	got := getF32(t, out)
	// Row 0 attends only to itself: (1,0). Row 1: scores = q1.k / sqrt(2)
	// -> [0*0.707, 1*0.707] -> softmax = [0.33024, 0.66976] ->
	// 0.33024*(1,0) + 0.66976*(0,1).
	if !approx(got[0], 1) || !approx(got[1], 0) {
		t.Fatalf("attention row 0: %v", got[:2])
	}
	if !approx(got[2], 0.33023846) || !approx(got[3], 0.66976154) {
		t.Fatalf("attention row 1: %v", got[2:4])
	}
}

func TestSiluReference(t *testing.T) {
	x := mustTensor(t, DTypeF32, Shape{3})
	setF32(t, x, []float32{0, 1, -1})
	y := mustTensor(t, DTypeF32, Shape{3})
	if err := siluF32(x, y); err != nil {
		t.Fatal(err)
	}
	got := getF32(t, y)
	// silu(0)=0; silu(1)=1/(1+e^-1)~0.7311; silu(-1)=-1/(1+e^1)~-0.2689.
	if !approx(got[0], 0) || !approx(got[1], 0.73106) || !approx(got[2], -0.26894) {
		t.Fatalf("silu: %v", got)
	}
}

func TestArgmaxDeterministicTies(t *testing.T) {
	x := mustTensor(t, DTypeF32, Shape{2, 3})
	setF32(t, x, []float32{1, 3, 3, 2, 2, 9})
	idx, err := argmaxF32(x)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := idx.Ints()
	// Ties resolve to the LOWEST index: 1 for row 0, 2 for row 1.
	if got[0] != 1 || got[1] != 2 {
		t.Fatalf("argmax: want [1 2], got %v", got)
	}
}

func TestOperationRegistryMiss(t *testing.T) {
	if _, err := lookupKernel(OpKey{Op: "matmul", DType: DTypeI32, BackendClass: "cpu"}); !errors.Is(err, ErrUnsupportedOperator) {
		t.Fatalf("registry miss must be typed, got %v", err)
	}
}

func TestGraphValidationOrder(t *testing.T) {
	// A node consuming an undefined tensor fails validation.
	g := NewGraph([]string{"a"}, []string{"out"})
	g.Append(Node{Op: "add", Inputs: map[string]string{"a": "a", "b": "undefined"}, Output: "out", DType: DTypeF32, Shape: Shape{2}})
	if err := g.Validate(); err == nil {
		t.Fatal("graph with an undefined operand must fail validation")
	}
	// A registry-missing operator fails validation.
	g2 := NewGraph([]string{"a"}, []string{"out"})
	g2.Append(Node{Op: "quantum_fft", Inputs: map[string]string{"a": "a"}, Output: "out", DType: DTypeF32, Shape: Shape{2}})
	if err := g2.Validate(); !errors.Is(err, ErrUnsupportedOperator) {
		t.Fatalf("registry miss in graph: want ErrUnsupportedOperator, got %v", err)
	}
}
