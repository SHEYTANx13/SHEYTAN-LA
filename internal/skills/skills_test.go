package skills

import (
	"testing"
	"time"
)

func verifiedCandidate() Candidate {
	return Candidate{
		ID:   "c1",
		Name: "Build Verification Flow",
		Steps: []string{
			"Inspect the build system files",
			"Run the build command",
			"Run the test command",
			"Confirm zero failures",
		},
		Tools: []string{"shell", "files"},
		RunID: "run-42",
		Evidence: Evidence{
			Verification: "verified",
			Checks:       []string{"go build ./... (exit 0)", "go test ./... (0 fail)"},
			ObservedAt:   time.Now(),
		},
	}
}

// TestPromotionRequiresVerifiedEvidence is the core rule of the validated
// learning mechanism: successful-but-unverified executions NEVER become
// skills.
func TestPromotionRequiresVerifiedEvidence(t *testing.T) {
	// No evidence at all.
	c := verifiedCandidate()
	c.Evidence = Evidence{}
	if _, ok, reason := PromoteCandidate(c); ok {
		t.Fatal("candidate without evidence must not promote")
	} else if reason == "" {
		t.Fatal("rejection must carry a reason")
	}

	// Failed evidence.
	c = verifiedCandidate()
	c.Evidence.Verification = "failed"
	if _, ok, _ := PromoteCandidate(c); ok {
		t.Fatal("failed verification must not promote")
	}

	// Partially verified is not enough.
	c = verifiedCandidate()
	c.Evidence.Verification = "partially_verified"
	if _, ok, _ := PromoteCandidate(c); ok {
		t.Fatal("partially_verified must not promote")
	}

	// Model-claimed success ("not_verified") is not evidence.
	c = verifiedCandidate()
	c.Evidence.Verification = "not_verified"
	if _, ok, _ := PromoteCandidate(c); ok {
		t.Fatal("not_verified must not promote")
	}

	// Verified promotes.
	sk, ok, _ := PromoteCandidate(verifiedCandidate())
	if !ok {
		t.Fatal("verified evidence must promote")
	}
	if len(sk.Steps) == 0 || sk.Identity.ID == "" || sk.Identity.Version != 1 {
		t.Fatalf("promoted skill malformed: %+v", sk)
	}
	if sk.Evidence.Verification != "verified" {
		t.Fatal("promoted skill must carry its evidence")
	}
}

func TestStoreMatchAndRender(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	sk, ok, _ := PromoteWithTrigger(verifiedCandidate(), []string{"build", "verify"})
	if !ok {
		t.Fatal("promote failed")
	}
	if err := store.SaveSkill(sk); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload from disk.
	store2 := NewStore(dir)
	if err := store2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if store2.Count() != 1 {
		t.Fatalf("count = %d, want 1", store2.Count())
	}

	matched := store2.MatchTask("please build and verify the parser", 2)
	if len(matched) != 1 {
		t.Fatalf("matched = %d, want 1", len(matched))
	}

	unrelated := store2.MatchTask("water the plants", 2)
	if len(unrelated) != 0 {
		t.Fatalf("unrelated task matched %d skills", len(unrelated))
	}

	block := RenderBlock(matched, 400)
	if block == "" {
		t.Fatal("render block empty for a match")
	}
	if len(block) > 400*4+64 {
		t.Fatalf("render block exceeds budget: %d chars", len(block))
	}
}

func TestRenderBlockRespectsBudget(t *testing.T) {
	matched := []Skill{}
	for i := 0; i < 10; i++ {
		sk, ok, _ := PromoteCandidate(verifiedCandidate())
		if !ok {
			t.Fatal("promote failed")
		}
		sk.Identity.ID = string(rune('a'+i)) + "-skill"
		matched = append(matched, *sk)
	}
	block := RenderBlock(matched, 100)
	if len(block) > 100*4+128 {
		t.Fatalf("block exceeds budget: %d", len(block))
	}
}

func TestVersionBumpOnSave(t *testing.T) {
	store := NewStore(t.TempDir())

	sk, ok, _ := PromoteCandidate(verifiedCandidate())
	if !ok {
		t.Fatal("promote failed")
	}
	if err := store.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}
	sk2, ok, _ := PromoteCandidate(verifiedCandidate())
	if !ok {
		t.Fatal("promote failed")
	}
	sk2.Identity.ID = sk.Identity.ID
	if err := store.SaveSkill(sk2); err != nil {
		t.Fatal(err)
	}

	if store.Count() != 1 {
		t.Fatalf("same-id save must replace, count = %d", store.Count())
	}
	if store.All()[0].Identity.Version != 2 {
		t.Fatalf("version = %d, want 2", store.All()[0].Identity.Version)
	}
}
