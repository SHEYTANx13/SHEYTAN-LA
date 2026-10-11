package config

import (
	"errors"
	"sync"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

func TestSourcePublishesNewValue(t *testing.T) {
	src := NewSource(Default())
	first := src.Load()

	next := src.Update(func(c *Config) { c.MaxIterations = 42 })

	if src.Load().MaxIterations != 42 {
		t.Fatalf("published value not visible: %d", src.Load().MaxIterations)
	}
	if next.MaxIterations != 42 {
		t.Fatalf("Update return value stale: %d", next.MaxIterations)
	}
	if first.MaxIterations == 42 {
		t.Fatal("the ORIGINAL snapshot must stay immutable")
	}
}

func TestSourceUpdateErrLeavesValueUntouched(t *testing.T) {
	src := NewSource(Default())

	_, err := src.UpdateErr(func(c *Config) error {
		c.Port = 9999
		return errBoom
	})

	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if src.Load().Port == 9999 {
		t.Fatal("failed mutation must not be published")
	}
}

// TestSourceConcurrentReadWrite is the regression test for the v1.1.3
// data race: the HTTP config patcher mutated the shared *Config in place
// while run goroutines read it. Under -race this test would previously
// fail; with copy-on-write it must pass.
func TestSourceConcurrentReadWrite(t *testing.T) {
	src := NewSource(Default())

	var wg sync.WaitGroup

	for w := 0; w < 4; w++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < 200; i++ {
				cfg := src.Load()
				_ = cfg.MaxIterations + cfg.Port + len(cfg.EnabledTools)
				_ = cfg.ToolEnabled("shell")
			}
		}()
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			src.Update(func(c *Config) {
				c.MaxIterations = 10 + i%20
			})
		}(i)
	}

	wg.Wait()
}

func TestEffectiveRunTimeoutBounds(t *testing.T) {
	cases := []struct {
		minutes int
		want    int // minutes, 0 = unbounded
	}{
		{0, 0},
		{-5, 1},
		{30, 30},
		{99999, 1440},
	}

	for _, tc := range cases {
		cfg := Default()
		cfg.RunTimeoutMinutes = tc.minutes

		got := cfg.EffectiveRunTimeout()
		want := timeMinutes(tc.want)

		if got != want {
			t.Errorf("RunTimeoutMinutes=%d: got %v want %v", tc.minutes, got, want)
		}
	}
}

func TestEffectiveSandboxMemory(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"512m", 512},
		{"512", 512},
		{"1g", 1024},
		{"2G", 2048},
		{"64k", 64}, // below floor clamps up
		{"", 512},
		{"garbage", 512},
	}

	for _, tc := range cases {
		cfg := Default()
		cfg.SandboxMemory = tc.in

		if got := cfg.EffectiveSandboxMemoryMB(); got != tc.want {
			t.Errorf("SandboxMemory=%q: got %d want %d", tc.in, got, tc.want)
		}
	}
}

func TestEffectiveSandboxCPUPercent(t *testing.T) {
	cfg := Default()
	cfg.SandboxCPU = 0
	if got := cfg.EffectiveSandboxCPUPercent(); got != 25 {
		t.Errorf("default CPU percent = %d, want 25", got)
	}

	cfg.SandboxCPU = 3
	if got := cfg.EffectiveSandboxCPUPercent(); got != 5 {
		t.Errorf("below-floor CPU percent = %d, want 5", got)
	}

	cfg.SandboxCPU = 400
	if got := cfg.EffectiveSandboxCPUPercent(); got != 100 {
		t.Errorf("above-ceiling CPU percent = %d, want 100", got)
	}
}

func TestSaveIsAtomicAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"

	cfg := Default()
	cfg.DataDir = dir
	cfg.MaxIterations = 17

	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.MaxIterations != 17 {
		t.Fatalf("round trip lost MaxIterations: %d", loaded.MaxIterations)
	}

	// A failed save must not destroy the previous file: rename is atomic.
	cfg2 := *cfg
	cfg2.Port = 99
	if err := Save(path, &cfg2); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("load after overwrite: %v", err)
	}
}

func timeMinutes(m int) time.Duration {
	return time.Duration(m) * time.Minute
}
