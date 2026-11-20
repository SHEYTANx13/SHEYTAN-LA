package engine

import (
	"fmt"
	"math"
)

// This file is the CPU REFERENCE kernel set — scalar, allocation-honest,
// and deliberately unoptimized. It is the correctness reference for every
// future optimized or device kernel (work order section 5.6): optimized
// vector kernels are added ONLY after parity tests against these
// functions, and GPU backends only after the same parity discipline.

// matmulF32 computes C[m,n] = sum_k A[m,k]*B[k,n]. Shapes: A (m,k),
// B (k,n), C (m,n). All three must be f32; shapes are validated.
func matmulF32(a, b, c *Tensor) error {
	if a.DType != DTypeF32 || b.DType != DTypeF32 || c.DType != DTypeF32 {
		return fmt.Errorf("%w: matmul requires f32 operands", ErrUnsupportedDType)
	}
	if len(a.Shape) != 2 || len(b.Shape) != 2 || len(c.Shape) != 2 {
		return &ShapeError{Op: "matmul", Expected: "rank-2 (m,k) x (k,n) -> (m,n)", Actual: fmt.Sprintf("%s x %s -> %s", a.Shape, b.Shape, c.Shape)}
	}
	m, k1 := a.Shape[0], a.Shape[1]
	k2, n := b.Shape[0], b.Shape[1]
	if k1 != k2 || m != c.Shape[0] || n != c.Shape[1] {
		return &ShapeError{Op: "matmul", Expected: fmt.Sprintf("(%d,%d)x(%d,%d)", m, n, k2, c.Shape[1]), Actual: fmt.Sprintf("%s x %s -> %s", a.Shape, b.Shape, c.Shape)}
	}
	af, _ := a.Floats()
	bf, _ := b.Floats()
	cf, _ := c.Floats()
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			var sum float32
			for k := 0; k < k1; k++ {
				sum += af[i*k1+k] * bf[k*n+j]
			}
			cf[i*n+j] = sum
		}
	}
	// Write-back: Floats() is a detached typed copy; the kernel owns the
	// copy -> tensor materialization (no unsafe aliasing in the
	// reference path).
	copy(c.Bytes(), f32AsBytes(cf))
	return nil
}

// addF32 computes c = a + b elementwise (same shapes, f32).
func addF32(a, b, c *Tensor) error {
	if a.DType != DTypeF32 || b.DType != DTypeF32 || c.DType != DTypeF32 {
		return fmt.Errorf("%w: add requires f32 operands", ErrUnsupportedDType)
	}
	if !sameShape(a.Shape, b.Shape) || !sameShape(a.Shape, c.Shape) {
		return &ShapeError{Op: "add", Expected: "equal shapes", Actual: fmt.Sprintf("%s, %s, %s", a.Shape, b.Shape, c.Shape)}
	}
	af, _ := a.Floats()
	bf, _ := b.Floats()
	cf, _ := c.Floats()
	for i := range cf {
		cf[i] = af[i] + bf[i]
	}
	copy(c.Bytes(), f32AsBytes(cf))
	return nil
}

// mulF32 computes c = a * b elementwise (same shapes, f32).
func mulF32(a, b, c *Tensor) error {
	if a.DType != DTypeF32 || b.DType != DTypeF32 || c.DType != DTypeF32 {
		return fmt.Errorf("%w: mul requires f32 operands", ErrUnsupportedDType)
	}
	if !sameShape(a.Shape, b.Shape) || !sameShape(a.Shape, c.Shape) {
		return &ShapeError{Op: "mul", Expected: "equal shapes", Actual: fmt.Sprintf("%s, %s, %s", a.Shape, b.Shape, c.Shape)}
	}
	af, _ := a.Floats()
	bf, _ := b.Floats()
	cf, _ := c.Floats()
	for i := range cf {
		cf[i] = af[i] * bf[i]
	}
	copy(c.Bytes(), f32AsBytes(cf))
	return nil
}

// softmaxF32 applies the numerically-stable softmax over the LAST
// dimension of x (rank >= 1), writing y (same shape).
func softmaxF32(x, y *Tensor) error {
	if x.DType != DTypeF32 || y.DType != DTypeF32 {
		return fmt.Errorf("%w: softmax requires f32 tensors", ErrUnsupportedDType)
	}
	if !sameShape(x.Shape, y.Shape) || len(x.Shape) == 0 {
		return &ShapeError{Op: "softmax", Expected: "equal rank>=1 shapes", Actual: fmt.Sprintf("%s, %s", x.Shape, y.Shape)}
	}
	xf, _ := x.Floats()
	yf, _ := y.Floats()
	last := x.Shape[len(x.Shape)-1]
	rows := 1
	for _, d := range x.Shape[:len(x.Shape)-1] {
		rows *= d
	}
	for r := 0; r < rows; r++ {
		row := xf[r*last : (r+1)*last]
		out := yf[r*last : (r+1)*last]
		maxV := row[0]
		for _, v := range row[1:] {
			if v > maxV {
				maxV = v
			}
		}
		sum := 0.0
		for i, v := range row {
			e := math.Exp(float64(v - maxV))
			out[i] = float32(e)
			sum += e
		}
		if sum == 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
			return fmt.Errorf("softmax row %d produced a non-finite denominator %g", r, sum)
		}
		inv := 1.0 / sum
		for i := range out {
			out[i] *= float32(inv)
		}
	}
	copy(y.Bytes(), f32AsBytes(yf))
	return nil
}

// rmsNormF32 computes y = x / sqrt(mean(x^2) + eps) * weight over the
// LAST dimension (the documented RMSNorm formulation).
func rmsNormF32(x, weight, y *Tensor, eps float64) error {
	if x.DType != DTypeF32 || weight.DType != DTypeF32 || y.DType != DTypeF32 {
		return fmt.Errorf("%w: rmsnorm requires f32 tensors", ErrUnsupportedDType)
	}
	if len(x.Shape) == 0 || !sameShape(x.Shape, y.Shape) {
		return &ShapeError{Op: "rmsnorm", Expected: "equal rank>=1 shapes", Actual: fmt.Sprintf("%s, %s", x.Shape, y.Shape)}
	}
	last := x.Shape[len(x.Shape)-1]
	if len(weight.Shape) != 1 || weight.Shape[0] != last {
		return &ShapeError{Op: "rmsnorm", Expected: fmt.Sprintf("weight (%d,)", last), Actual: weight.Shape.String()}
	}
	xf, _ := x.Floats()
	wf, _ := weight.Floats()
	yf, _ := y.Floats()
	rows := 1
	for _, d := range x.Shape[:len(x.Shape)-1] {
		rows *= d
	}
	for r := 0; r < rows; r++ {
		row := xf[r*last : (r+1)*last]
		var ss float64
		for _, v := range row {
			ss += float64(v) * float64(v)
		}
		ms := ss / float64(last)
		inv := 1.0 / math.Sqrt(ms+eps)
		for i, v := range row {
			yf[r*last+i] = float32(float64(v)*inv) * wf[i]
		}
	}
	copy(y.Bytes(), f32AsBytes(yf))
	return nil
}

// embeddingF32 gathers vocab rows: ids (n,) i32 -> out (n,d) f32, where
// table is (vocab,d). Ids are strictly bounds-checked — a negative or
// out-of-range id is a typed error, never a wild read.
func embeddingF32(table, ids, out *Tensor) error {
	if table.DType != DTypeF32 || out.DType != DTypeF32 || ids.DType != DTypeI32 {
		return fmt.Errorf("%w: embedding requires f32 table/out and i32 ids", ErrUnsupportedDType)
	}
	if len(table.Shape) != 2 || len(ids.Shape) != 1 || !sameShape(out.Shape, Shape{ids.Shape[0], table.Shape[1]}) {
		return &ShapeError{Op: "embedding", Expected: "table (vocab,d) x ids (n,) -> (n,d)", Actual: fmt.Sprintf("%s x %s -> %s", table.Shape, ids.Shape, out.Shape)}
	}
	vocab, d := table.Shape[0], table.Shape[1]
	tf, _ := table.Floats()
	idv, _ := ids.Ints()
	of, _ := out.Floats()
	for i, id := range idv {
		if id < 0 || int(id) >= vocab {
			return fmt.Errorf("%w: embedding id %d outside vocab %d", ErrShape, id, vocab)
		}
		copy(of[i*d:(i+1)*d], tf[int(id)*d:(int(id)+1)*d])
	}
	copy(out.Bytes(), f32AsBytes(of))
	return nil
}

// causalAttentionF32 is the single-head reference attention: given Q, K,
// V as (t,d) f32 tensors it computes softmax(QK^T / sqrt(d)) masked
// causally, then the weighted V sum (t,d). This is the correctness
// reference for the fixture architecture's attention path.
func causalAttentionF32(q, k, v, out *Tensor) error {
	if q.DType != DTypeF32 || k.DType != DTypeF32 || v.DType != DTypeF32 || out.DType != DTypeF32 {
		return fmt.Errorf("%w: attention requires f32 tensors", ErrUnsupportedDType)
	}
	t := q.Shape[0]
	d := q.Shape[1]
	if len(q.Shape) != 2 || !sameShape(k.Shape, Shape{t, d}) || !sameShape(v.Shape, Shape{t, d}) || !sameShape(out.Shape, Shape{t, d}) {
		return &ShapeError{Op: "attention", Expected: "(t,d) x (t,d) x (t,d) -> (t,d)", Actual: fmt.Sprintf("%s x %s x %s -> %s", q.Shape, k.Shape, v.Shape, out.Shape)}
	}
	qf, _ := q.Floats()
	kf, _ := k.Floats()
	vf, _ := v.Floats()
	of, _ := out.Floats()

	scale := 1.0 / math.Sqrt(float64(d))
	scores := make([]float32, t)
	for i := 0; i < t; i++ {
		// scores[j] = q_i . k_j / sqrt(d) for j <= i (causal mask).
		maxV := float32(math.Inf(-1))
		for j := 0; j <= i; j++ {
			var s float32
			for dd := 0; dd < d; dd++ {
				s += qf[i*d+dd] * kf[j*d+dd]
			}
			scores[j] = float32(float64(s) * scale)
			if scores[j] > maxV {
				maxV = scores[j]
			}
		}
		sum := 0.0
		weights := make([]float32, i+1)
		for j := 0; j <= i; j++ {
			e := float32(math.Exp(float64(scores[j] - maxV)))
			weights[j] = e
			sum += float64(e)
		}
		for dd := 0; dd < d; dd++ {
			var acc float32
			for j := 0; j <= i; j++ {
				acc += weights[j] * float32(float64(vf[j*d+dd])/sum)
			}
			of[i*d+dd] = acc
		}
	}
	copy(out.Bytes(), f32AsBytes(of))
	return nil
}

// siluF32 computes y = x * sigmoid(x) elementwise.
func siluF32(x, y *Tensor) error {
	if x.DType != DTypeF32 || y.DType != DTypeF32 {
		return fmt.Errorf("%w: silu requires f32 tensors", ErrUnsupportedDType)
	}
	if !sameShape(x.Shape, y.Shape) {
		return &ShapeError{Op: "silu", Expected: "equal shapes", Actual: fmt.Sprintf("%s, %s", x.Shape, y.Shape)}
	}
	xf, _ := x.Floats()
	yf, _ := y.Floats()
	for i, v := range xf {
		yf[i] = v / (1 + float32(math.Exp(-float64(v))))
	}
	copy(y.Bytes(), f32AsBytes(yf))
	return nil
}

// argmaxF32 returns the index of the maximum value over the LAST
// dimension of x (i32 indices). Ties resolve to the LOWEST index —
// deterministic and documented.
func argmaxF32(x *Tensor) (*Tensor, error) {
	if x.DType != DTypeF32 {
		return nil, fmt.Errorf("%w: argmax requires f32 input", ErrUnsupportedDType)
	}
	if len(x.Shape) == 0 {
		return nil, &ShapeError{Op: "argmax", Expected: "rank>=1", Actual: x.Shape.String()}
	}
	last := x.Shape[len(x.Shape)-1]
	rows := 1
	for _, d := range x.Shape[:len(x.Shape)-1] {
		rows *= d
	}
	out, err := NewTensor(DTypeI32, Shape{rows})
	if err != nil {
		return nil, err
	}
	xf, _ := x.Floats()
	ov, _ := out.Ints()
	for r := 0; r < rows; r++ {
		row := xf[r*last : (r+1)*last]
		best := 0
		for i, v := range row[1:] {
			if v > row[best] {
				best = i + 1
			}
		}
		ov[r] = int32(best)
	}
	copy(out.Bytes(), i32AsBytes(ov))
	out.Shape = append(Shape{}, x.Shape[:len(x.Shape)-1]...)
	out.Strides = DefaultStrides(out.Shape)
	return out, nil
}

func sameShape(a, b Shape) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
