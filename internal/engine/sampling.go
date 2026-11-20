package engine

import (
	"container/heap"
	"fmt"
	"math"
	"math/rand"
)

// SamplingConfig is the request-local decoding configuration. The
// zero value is greedy decoding (temperature 0) — the most deterministic
// answer, and the one correctness tests pin.
type SamplingConfig struct {
	Seed         int64   // explicit seed: reproducibility is contractual
	Temperature  float32 // 0 = greedy (argmax)
	TopK         int     // 0 = disabled
	TopP         float32 // 0 = disabled; (0,1) nucleus mass
	MaxNewTokens int     // hard generation cap (bounded requests)
}

// StopCondition terminates generation BEFORE the token cap.
type StopCondition func(tokenID int32) bool

// StopOnToken stops when one of the ids is produced (EOS-style).
func StopOnToken(ids ...int32) StopCondition {
	set := make(map[int32]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return func(t int32) bool { return set[t] }
}

// SampleToken selects the next token id from logits (vocab,)
// deterministically. The output is a pure function of (logits, config):
//
//   - greedy: argmax, ties to the lowest index (documented in argmaxF32);
//   - temperature>0: softmax(logits/temperature) then seeded sampling
//     over the top-k / top-p truncated distribution.
//
// The same seed, logits and config reproduce the same token — the
// reproducibility tests pin this bit-for-bit.
func SampleToken(logits []float32, cfg SamplingConfig, rng *rand.Rand) (int32, error) {
	if len(logits) == 0 {
		return 0, fmt.Errorf("%w: empty logits", ErrShape)
	}
	if cfg.Temperature <= 0 {
		best := 0
		for i, v := range logits[1:] {
			if v > logits[best] {
				best = i + 1
			}
		}
		return int32(best), nil
	}

	// Temperature-scaled probabilities (numerically stable).
	inv := 1.0 / float64(cfg.Temperature)
	maxV := float64(logits[0])
	for _, v := range logits[1:] {
		if float64(v) > maxV {
			maxV = float64(v)
		}
	}
	probs := make([]float64, len(logits))
	sum := 0.0
	for i, v := range logits {
		e := math.Exp(float64(v)*inv - maxV*inv)
		probs[i] = e
		sum += e
	}
	for i := range probs {
		probs[i] /= sum
	}

	// Top-k truncation (keep the k highest-probability candidates).
	candidates := make([]int, len(probs))
	for i := range candidates {
		candidates[i] = i
	}
	if cfg.TopK > 0 && cfg.TopK < len(probs) {
		// Partial selection via a bounded max-heap of size k.
		h := &probHeap{}
		heap.Init(h)
		for _, idx := range candidates {
			if h.Len() < cfg.TopK {
				heap.Push(h, probItem{idx: idx, p: probs[idx]})
			} else if probs[idx] > (*h)[0].p {
				(*h)[0] = probItem{idx: idx, p: probs[idx]}
				heap.Fix(h, 0)
			}
		}
		candidates = candidates[:0]
		for _, item := range *h {
			candidates = append(candidates, item.idx)
		}
	}

	// Top-p truncation: sort by probability desc, keep the smallest
	// prefix whose cumulative mass reaches TopP.
	if cfg.TopP > 0 && cfg.TopP < 1 {
		sortByProb(candidates, probs)
		cum := 0.0
		keep := len(candidates)
		for i, idx := range candidates {
			cum += probs[idx]
			if cum >= float64(cfg.TopP) {
				keep = i + 1
				break
			}
		}
		candidates = candidates[:keep]
	}

	// Seeded categorical draw over the truncated candidate set.
	total := 0.0
	for _, idx := range candidates {
		total += probs[idx]
	}
	if total <= 0 {
		return 0, fmt.Errorf("%w: truncated sampling distribution is empty", ErrShape)
	}
	r := rng.Float64() * total
	var acc float64
	for _, idx := range candidates {
		acc += probs[idx]
		if r < acc {
			return int32(idx), nil
		}
	}
	return int32(candidates[len(candidates)-1]), nil // float rounding guard
}

type probItem struct {
	idx int
	p   float64
}

type probHeap []probItem

func (h probHeap) Len() int            { return len(h) }
func (h probHeap) Less(i, j int) bool  { return h[i].p < h[j].p } // min-heap
func (h probHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *probHeap) Push(x interface{}) { *h = append(*h, x.(probItem)) }
func (h *probHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func sortByProb(candidates []int, probs []float64) {
	// Insertion sort — candidate sets are tiny (top-k <= vocab of small
	// models; determinism beats asymptotics here, and ties keep the
	// original (index-ascending) order, so the truncation is stable).
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && probs[candidates[j]] > probs[candidates[j-1]]; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
}
