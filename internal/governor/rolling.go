// rolling.go — v1.8.0: the bounded rolling signal behind the Governor's
// pressure model.
//
// Policy never reads ONE instantaneous sample: a single noisy CPU spike or
// one transient RAM dip must not flip the envelope. The rolling series is
// a bounded ring with an exponentially-weighted average (bounded memory,
// deterministic decay, no goroutines).
package governor

import "sync"

// rollingCap bounds the ring. With the shipped 15 s monitor cadence this
// covers ~7.5 minutes of history — long enough to smooth noise, short
// enough to stay responsive.
const rollingCap = 30

// alpha is the EWMA factor: 0.25 weights the newest sample meaningfully
// while old samples decay smoothly (after 8 samples an old value's weight
// is below 8%).
const alpha = 0.25

// rollingSeries is a bounded EWMA over the most recent samples. Safe for
// concurrent use (the Governor folds under its own lock, but the series
// guards itself so tests and future callers cannot race it).
type rollingSeries struct {
	mu   sync.Mutex
	vals []float64
	ewma float64
	has  bool
}

// observe folds one sample and returns the updated average.
func (r *rollingSeries) observe(v float64) float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.vals = append(r.vals, v)
	if len(r.vals) > rollingCap {
		r.vals = r.vals[len(r.vals)-rollingCap:]
	}

	if !r.has {
		r.ewma = v
		r.has = true
		return r.ewma
	}

	r.ewma = alpha*v + (1-alpha)*r.ewma
	return r.ewma
}

// average returns the current rolling average (0 with no samples).
func (r *rollingSeries) average() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ewma
}

// len reports how many samples the series holds (tests).
func (r *rollingSeries) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.vals)
}
