package presentation

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func mustPut(t *testing.T, s *Store, b Block) Block {
	t.Helper()
	got, err := s.Put(b)
	if err != nil {
		t.Fatalf("Put(%s): %v", b.ID, err)
	}
	return got
}

func TestValidateCanonicalizesEnvelope(t *testing.T) {
	b := Block{ID: "run1/resp-final", Kind: KindMarkdown, Payload: []byte("# Hello")}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if b.V != Version {
		t.Errorf("V = %d, want %d", b.V, Version)
	}
	if b.Encoding != "utf8" {
		t.Errorf("Encoding = %q, want utf8", b.Encoding)
	}
	if b.CreatedAt.IsZero() {
		t.Error("CreatedAt not stamped")
	}
	if b.Degrade != nil {
		t.Errorf("known kind must not degrade: %+v", b.Degrade)
	}
}

func TestValidateUnknownKindDegradesNotFails(t *testing.T) {
	b := Block{ID: "b1", Kind: Kind("hologram"), Payload: []byte("raw future data")}
	if err := b.Validate(); err != nil {
		t.Fatalf("unknown kind must be accepted: %v", err)
	}
	if b.Degrade == nil || b.Degrade.Reason != DegradeUnknownKind {
		t.Fatalf("Degrade = %+v, want kind-unknown record", b.Degrade)
	}
	if SelectSurface(b.Kind) != SurfaceNative {
		t.Errorf("unknown kind must select the native surface, got %q", SelectSurface(b.Kind))
	}
}

func TestValidateRefusals(t *testing.T) {
	cases := []struct {
		name string
		b    Block
		want error
	}{
		{"empty id", Block{Kind: KindText}, ErrID},
		{"id with space", Block{ID: "bad id", Kind: KindText}, ErrID},
		{"id too long", Block{ID: strings.Repeat("x", MaxIDLen+1), Kind: KindText}, ErrID},
		{"title too long", Block{ID: "b", Kind: KindText, Title: strings.Repeat("t", MaxTitleLen+1)}, ErrTitle},
		{"source too long", Block{ID: "b", Kind: KindText, Source: strings.Repeat("s", MaxSourceLen+1)}, ErrSource},
		{"text oversize", Block{ID: "b", Kind: KindText, Payload: make([]byte, MaxTextPayload+1)}, ErrPayload},
		{"invalid utf8", Block{ID: "b", Kind: KindText, Payload: []byte{0xff, 0xfe, 0x01}}, ErrPayload},
		{"table not json", Block{ID: "b", Kind: KindTable, Payload: []byte("not json")}, ErrPayload},
		{"table rows not arrays", Block{ID: "b", Kind: KindTable, Payload: []byte(`{"columns":["a"],"rows":[42]}`)}, ErrPayload},
		{"table too many rows", Block{ID: "b", Kind: KindTable, Payload: tablePayload(MaxTableRows+1, 1)}, ErrPayload},
		{"table too many cells", Block{ID: "b", Kind: KindTable, Payload: tablePayload(60, 90)}, ErrPayload},
		{"chart oversize", Block{ID: "b", Kind: KindChart, Payload: make([]byte, MaxJSONPayload+1)}, ErrPayload},
		{"artifact without path", Block{ID: "b", Kind: KindArtifact}, ErrArtifact},
		{"artifact with content", Block{ID: "b", Kind: KindArtifact, Path: "a.csv", Payload: make([]byte, MaxMetaPayload+1)}, ErrPayload},
		{"3d oversize", Block{ID: "b", Kind: Kind3D, Payload: make([]byte, MaxMetaPayload+1)}, ErrPayload},
		{"oversize unknown kind", Block{ID: "b", Kind: Kind("x"), Payload: make([]byte, MaxJSONPayload+1)}, ErrPayload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.b.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate err = %v, want %v", err, tc.want)
			}
		})
	}
}

func tablePayload(rows, cols int) []byte {
	var sb strings.Builder
	sb.WriteString(`{"columns":[`)
	for c := 0; c < cols; c++ {
		if c > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`"c"`)
	}
	sb.WriteString(`],"rows":[`)
	for r := 0; r < rows; r++ {
		if r > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('[')
		for c := 0; c < cols; c++ {
			if c > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(`"x"`)
		}
		sb.WriteByte(']')
	}
	sb.WriteString(`]}`)
	return []byte(sb.String())
}

func TestValidateTableWithinBounds(t *testing.T) {
	b := Block{ID: "tbl", Kind: KindTable, Payload: []byte(`{"columns":["name","value"],"rows":[["a",1],["b",2]]}`)}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if b.Encoding != "json" {
		t.Errorf("Encoding = %q, want json", b.Encoding)
	}
}

func TestSelectSurfaceDeterministic(t *testing.T) {
	// The policy is a pure function of the kind — same input, same output,
	// forever. Pinned here so no future change can make it stateful.
	want := map[Kind]Surface{
		KindText:     SurfaceNative,
		KindStatus:   SurfaceNative,
		KindArtifact: SurfaceNative,
		KindMarkdown: SurfaceWorkspace,
		KindCode:     SurfaceWorkspace,
		KindDiff:     SurfaceWorkspace,
		KindTable:    SurfaceWorkspace,
		KindChart:    SurfaceWorkspace,
		Kind3D:       SurfaceStudio,
		Kind("??"):   SurfaceNative,
	}
	for k, s := range want {
		for i := 0; i < 3; i++ {
			if got := SelectSurface(k); got != s {
				t.Fatalf("SelectSurface(%q)[%d] = %q, want %q", k, i, got, s)
			}
		}
	}
}

func TestStoreUpsertOneIDOneArtifact(t *testing.T) {
	s := NewStore(0)
	mustPut(t, s, Block{ID: "b1", Kind: KindText, Payload: []byte("v1")})
	mustPut(t, s, Block{ID: "b1", Kind: KindMarkdown, Payload: []byte("v2 — replaced")})
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (one ID is one logical artifact)", s.Len())
	}
	got, ok := s.Get("b1")
	if !ok || string(got.Payload) != "v2 — replaced" || got.Kind != KindMarkdown {
		t.Fatalf("Get = %+v ok=%v, want replaced markdown v2", got, ok)
	}
	// Provenance survives replacement when the producer re-issues it.
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt lost on replacement")
	}
}

func TestStoreBoundedLRU(t *testing.T) {
	s := NewStore(3)
	for i := 0; i < 5; i++ {
		mustPut(t, s, Block{ID: string(rune('a' + i)), Kind: KindText, Payload: []byte("x")})
	}
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3", s.Len())
	}
	list := s.List()
	if len(list) != 3 || list[0].ID != "c" || list[2].ID != "e" {
		t.Fatalf("List = %+v, want oldest-evicted [c d e]", list)
	}
	if _, ok := s.Get("a"); ok {
		t.Error("evicted block 'a' still present")
	}
	if _, ok := s.Get("e"); !ok {
		t.Error("newest block 'e' missing")
	}
}

func TestStoreRefusesInvalid(t *testing.T) {
	s := NewStore(0)
	if _, err := s.Put(Block{Kind: KindText}); !errors.Is(err, ErrID) {
		t.Fatalf("Put without ID: err = %v, want ErrID", err)
	}
	if s.Len() != 0 {
		t.Fatalf("invalid block stored (Len=%d)", s.Len())
	}
}

func TestStoreListSnapshotIsCopy(t *testing.T) {
	s := NewStore(0)
	mustPut(t, s, Block{ID: "b1", Kind: KindText, Payload: []byte("x")})
	list := s.List()
	list[0].ID = "mutated"
	if again := s.List(); again[0].ID != "b1" {
		t.Fatal("List leaked the internal snapshot")
	}
}

func TestStoreConcurrentPutGet(t *testing.T) {
	s := NewStore(64)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = s.Put(Block{ID: "shared", Kind: KindText, Payload: []byte("w")})
				_, _ = s.Get("shared")
				_ = s.List()
				_ = s.Len()
			}
		}(w)
	}
	wg.Wait()
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.Len())
	}
}

func TestMetaOfBoundedDescriptor(t *testing.T) {
	ts := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	b := Block{ID: "b1", Kind: KindDiff, Title: "t", RunID: "r", SessionID: "s",
		CreatedAt: ts, Payload: []byte("--- a\n+++ b\n")}
	m := MetaOf(b)
	if m.Bytes != len(b.Payload) || m.Kind != KindDiff || !m.CreatedAt.Equal(ts) {
		t.Fatalf("Meta = %+v", m)
	}
	if m.Title != "t" || m.RunID != "r" || m.SessionID != "s" {
		t.Fatalf("Meta provenance lost: %+v", m)
	}
}
