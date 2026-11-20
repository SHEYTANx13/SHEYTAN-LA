package engine

import (
	"math"
	"math/rand"
	"testing"
)

func TestGreedySamplingDeterministic(t *testing.T) {
	logits := []float32{0.1, 2.0, 1.999, -1, 0.5}
	rng := rand.New(rand.NewSource(1))
	id, err := SampleToken(logits, SamplingConfig{}, rng)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Fatalf("greedy: want 1 (2.0 > 1.999), got %d", id)
	}
	// Ties resolve to the LOWEST index (deterministic documented rule).
	id, _ = SampleToken([]float32{1, 1, 1}, SamplingConfig{}, rng)
	if id != 0 {
		t.Fatalf("greedy tie: want 0, got %d", id)
	}
}

func TestSeededSamplingReproducibility(t *testing.T) {
	logits := make([]float32, 64)
	for i := range logits {
		logits[i] = float32(math.Sin(float64(i))) * 3
	}
	cfg := SamplingConfig{Seed: 42, Temperature: 0.8, TopK: 16, TopP: 0.9, MaxNewTokens: 16}

	// Two independent RNGs with the same seed produce IDENTICAL token
	// streams — reproducibility is contractual.
	rngA := rand.New(rand.NewSource(cfg.Seed))
	rngB := rand.New(rand.NewSource(cfg.Seed))
	for i := 0; i < 32; i++ {
		a, err := SampleToken(logits, cfg, rngA)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := SampleToken(logits, cfg, rngB)
		if a != b {
			t.Fatalf("step %d: seeded sampling diverged (%d vs %d)", i, a, b)
		}
	}
}

func TestTopKTruncation(t *testing.T) {
	// One dominant logit, tiny noise elsewhere: top-k=1 must always
	// return the dominant index regardless of seed.
	logits := []float32{0, 0, 0, 10, 0, 0}
	for seed := int64(0); seed < 16; seed++ {
		id, err := SampleToken(logits, SamplingConfig{Seed: seed, Temperature: 1.5, TopK: 1}, rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatal(err)
		}
		if id != 3 {
			t.Fatalf("seed %d: top-k=1 must pin index 3, got %d", seed, id)
		}
	}
}

func TestTopPMass(t *testing.T) {
	// top-p=1.0 keeps everything; p=0.5 keeps only the dominant mass.
	logits := []float32{0, 0, 0, 10, 0, 0}
	id, err := SampleToken(logits, SamplingConfig{Seed: 7, Temperature: 1.0, TopP: 0.5}, rand.New(rand.NewSource(7)))
	if err != nil || id != 3 {
		t.Fatalf("top-p=0.5 must sample the dominant index, got (%d,%v)", id, err)
	}
}

func TestEmptyLogits(t *testing.T) {
	if _, err := SampleToken(nil, SamplingConfig{}, rand.New(rand.NewSource(1))); err == nil {
		t.Fatal("empty logits must fail")
	}
}
