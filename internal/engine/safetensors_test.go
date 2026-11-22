package engine

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"
)

// buildSafetensors assembles a REAL safetensors container from records
// (the exact documented format: 8-byte LE header length + JSON header +
// data section).
func buildSafetensors(t *testing.T, records map[string][2]interface{}, metadata map[string]string) []byte {
	t.Helper()
	type rec struct {
		DType       string `json:"dtype"`
		Shape       []int  `json:"shape"`
		DataOffsets [2]int `json:"data_offsets"`
	}
	header := map[string]interface{}{}
	offset := 0
	data := []byte{}
	names := make([]string, 0, len(records))
	for name := range records {
		names = append(names, name)
	}
	// Deterministic order: sort names.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		dt := records[name][0].(string)
		shape := records[name][1].(Shape)
		numel, err := shape.Numel()
		if err != nil {
			t.Fatal(err)
		}
		elem := SafetensorsDType(dt).sizeOf()
		if elem == 0 {
			t.Fatalf("test record dtype %s has no size table entry", dt)
		}
		header[name] = rec{DType: dt, Shape: []int(shape), DataOffsets: [2]int{offset, offset + numel*elem}}
		// Payload: deterministic pseudo-bytes (index-derived, not random).
		for i := 0; i < numel*elem; i++ {
			data = append(data, byte((offset+i)%251))
		}
		offset += numel * elem
	}
	if metadata != nil {
		header["__metadata__"] = metadata
	}
	hj, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 8)
	binary.LittleEndian.PutUint64(out, uint64(len(hj)))
	out = append(out, hj...)
	out = append(out, data...)
	return out
}

func TestSafetensorsHeaderHappyPath(t *testing.T) {
	buf := buildSafetensors(t, map[string][2]interface{}{
		"a": {"F32", Shape{2, 2}},
		"b": {"I32", Shape{3}},
	}, map[string]string{"config": `{"architecture":"tiny-fixture-1"}`})

	meta, err := ParseSafetensorsHeader(buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Tensors) != 2 {
		t.Fatalf("want 2 tensors, got %d", len(meta.Tensors))
	}
	// Sorted by data offset.
	if meta.Tensors[0].Name != "a" || meta.Tensors[1].Name != "b" {
		t.Fatalf("order: %q then %q", meta.Tensors[0].Name, meta.Tensors[1].Name)
	}
	if meta.Metadata["config"] == "" {
		t.Fatal("__metadata__ must round-trip")
	}
	// Zero-copy materialization matches the container bytes.
	tA, err := TensorFromSafetensors(buf, meta.Tensors[0])
	if err != nil {
		t.Fatal(err)
	}
	if tA.DType != DTypeF32 || !sameShape(tA.Shape, Shape{2, 2}) {
		t.Fatalf("materialized tensor: %s %s", tA.DType, tA.Shape)
	}
}

func TestSafetensorsHeaderCorruption(t *testing.T) {
	good := buildSafetensors(t, map[string][2]interface{}{"a": {"F32", Shape{4}}}, nil)

	// Truncated buffer.
	if _, err := ParseSafetensorsHeader(good[:4]); !errors.Is(err, ErrMalformedContainer) {
		t.Fatalf("truncated: want ErrMalformedContainer, got %v", err)
	}
	// Hostile header length.
	hostile := append([]byte(nil), good...)
	binary.LittleEndian.PutUint64(hostile, uint64(1)<<40)
	if _, err := ParseSafetensorsHeader(hostile); !errors.Is(err, ErrMalformedContainer) {
		t.Fatalf("hostile length: want ErrMalformedContainer, got %v", err)
	}
	// Header JSON garbage.
	if _, err := ParseSafetensorsHeader([]byte{8, 0, 0, 0, 0, 0, 0, 0, '[', 'x', ']'}); !errors.Is(err, ErrMalformedContainer) {
		t.Fatalf("garbage JSON: want ErrMalformedContainer, got %v", err)
	}
	// Unknown dtype in the size table (hand-crafted: the builder cannot
	// produce a dtype without a size entry).
	unknown := []byte{8, 0, 0, 0, 0, 0, 0, 0}
	hjUnknown := []byte(`{"a":{"dtype":"Q12","shape":[4],"data_offsets":[0,4]}}`)
	binary.LittleEndian.PutUint64(unknown, uint64(len(hjUnknown)))
	unknown = append(unknown, hjUnknown...)
	unknown = append(unknown, make([]byte, 4)...)
	if _, err := ParseSafetensorsHeader(unknown); !errors.Is(err, ErrMalformedContainer) {
		t.Fatalf("unknown dtype: want ErrMalformedContainer, got %v", err)
	}
	// Shape/byte-count contradiction (span != numel*elem).
	bad := buildSafetensors(t, map[string][2]interface{}{"a": {"F32", Shape{4}}}, nil)
	// Overwrite the record's span by shifting a byte of the header JSON:
	// easier to hand-craft a small header with a wrong span.
	wrong := []byte{8, 0, 0, 0, 0, 0, 0, 0}
	hj := []byte(`{"a":{"dtype":"F32","shape":[4],"data_offsets":[0,3]}}`)
	binary.LittleEndian.PutUint64(wrong, uint64(len(hj)))
	wrong = append(wrong, hj...)
	wrong = append(wrong, make([]byte, 16)...)
	if _, err := ParseSafetensorsHeader(wrong); !errors.Is(err, ErrMalformedContainer) {
		t.Fatalf("span contradiction: want ErrMalformedContainer, got %v", err)
	}
	_ = bad
}

func TestSafetensorsOverlapDetection(t *testing.T) {
	// Hand-crafted overlapping records.
	hj := []byte(`{"a":{"dtype":"F32","shape":[2],"data_offsets":[0,8]},"b":{"dtype":"F32","shape":[2],"data_offsets":[4,12]}}`)
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(len(hj)))
	buf = append(buf, hj...)
	buf = append(buf, make([]byte, 16)...)
	if _, err := ParseSafetensorsHeader(buf); !errors.Is(err, ErrMalformedContainer) {
		t.Fatalf("overlap: want ErrMalformedContainer, got %v", err)
	}
}

func TestSafetensorsOffsetsEscapeDataSection(t *testing.T) {
	hj := []byte(`{"a":{"dtype":"F32","shape":[4],"data_offsets":[0,16]}}`)
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(len(hj)))
	buf = append(buf, hj...)
	buf = append(buf, make([]byte, 8)...) // only 8 bytes of data, record wants 16
	if _, err := ParseSafetensorsHeader(buf); !errors.Is(err, ErrMalformedContainer) {
		t.Fatalf("escaping offsets: want ErrMalformedContainer, got %v", err)
	}
}

func TestSafetensorsExecutionDTypeGate(t *testing.T) {
	// F16 is recognized in metadata but has NO reference kernel: the
	// execution gate must answer typed-unsupported, never reinterpret.
	buf := buildSafetensors(t, map[string][2]interface{}{"a": {"F16", Shape{4}}}, nil)
	meta, err := ParseSafetensorsHeader(buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TensorFromSafetensors(buf, meta.Tensors[0]); !errors.Is(err, ErrUnsupportedDType) {
		t.Fatalf("f16 gate: want ErrUnsupportedDType, got %v", err)
	}
}

// TestSafetensorsOffsetsNearUint64MaxRejected (run 44835425398 engine
// pass): the previous escape check compared meta.DataStart+start against
// DataEnd — a uint64 ADDITION that wraps for offsets near 2^64, so a
// hostile container was ACCEPTED and its tensor materialized over
// aliased header bytes. The check now compares each offset against the
// data-section length (no addition, no wrap): such a container must be
// rejected with the typed malformed-container error, never materialized.
func TestSafetensorsOffsetsNearUint64MaxRejected(t *testing.T) {
	startOff := uint64(18446744073709551608) // 2^64 - 8
	endOff := startOff + 4                   // exactly the 4 bytes a 1-element F32 record needs

	hj, err := json.Marshal(map[string]json.RawMessage{
		"a": mustJSON(t, map[string]interface{}{
			"dtype":        "F32",
			"shape":        []int{1},
			"data_offsets": []uint64{startOff, endOff},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(len(hj)))
	buf = append(buf, hj...)
	buf = append(buf, make([]byte, 8)...) // an 8-byte data section the offsets claim to escape

	meta, perr := ParseSafetensorsHeader(buf)
	if !errors.Is(perr, ErrMalformedContainer) {
		t.Fatalf("near-2^64 offsets must be REJECTED with ErrMalformedContainer (fail closed), got meta=%v err=%v", meta, perr)
	}
}

// TestTensorFromSafetensorsRejectsReversedDirectOffsets: the exported
// materializer validates directly-constructed info defensively — a
// reversed offset range is a typed error, never a Go slice panic.
func TestTensorFromSafetensorsRejectsReversedDirectOffsets(t *testing.T) {
	buf := buildSafetensors(t, map[string][2]interface{}{
		"a": {"F32", Shape{2}},
	}, nil)

	info := SafetensorsTensorInfo{
		Name:        "a",
		DType:       STF32,
		Shape:       Shape{2},
		DataOffsets: [2]uint64{16, 8}, // reversed
	}

	if _, err := TensorFromSafetensors(buf, info); !errors.Is(err, ErrMalformedContainer) {
		t.Fatalf("reversed direct offsets must fail typed ErrMalformedContainer, got %v", err)
	}
}

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return b
}
