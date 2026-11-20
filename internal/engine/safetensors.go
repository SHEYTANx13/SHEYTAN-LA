package engine

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
)

// Safetensors support status: METADATA_INSPECTION_ONLY.
//
// Safetensors is a tensor CONTAINER, not a model architecture (work
// order section 5.3). This file implements STRICT header parsing and
// validation: dtype/shape/offset bounds checks, overflow checks, and
// overlap detection. It does NOT wire any pretrained architecture: a
// real model needs an architecture adapter, a tokenizer, a chat
// template and a complete required-tensor mapping before any inference
// claim may exist. Nothing here reads pickle .bin files — those are
// never trusted model data.

// safetensorsMaxHeaderBytes bounds the header allocation: a corrupt or
// hostile length field can never drive a huge allocation.
const safetensorsMaxHeaderBytes = 100 << 20 // 100 MiB of header JSON

// SafetensorsDType is the dtype vocabulary the header parser
// recognizes. Only F32/I32 have engine kernels; the wider dtype strings
// are RECOGNIZED for metadata honesty but produce a typed unsupported
// answer on any execution attempt.
type SafetensorsDType string

const (
	STF32  SafetensorsDType = "F32"
	STI32  SafetensorsDType = "I32"
	STF16  SafetensorsDType = "F16"
	STBF16 SafetensorsDType = "BF16"
	STF64  SafetensorsDType = "F64"
	STI64  SafetensorsDType = "I64"
	STI16  SafetensorsDType = "I16"
	STI8   SafetensorsDType = "I8"
	STU8   SafetensorsDType = "U8"
	STBOOL SafetensorsDType = "BOOL"
)

// sizeOf maps the safetensors dtype vocabulary to byte sizes (0 =
// unknown/unsupported by the parser's size table).
func (d SafetensorsDType) sizeOf() int {
	switch d {
	case STF32, STI32:
		return 4
	case STF16, STBF16, STI16:
		return 2
	case STF64, STI64:
		return 8
	case STI8, STU8, STBOOL:
		return 1
	default:
		return 0
	}
}

// toEngineDType maps container dtypes to engine dtypes (typed
// unsupported for everything without a reference kernel).
func (d SafetensorsDType) toEngineDType() (DType, error) {
	switch d {
	case STF32:
		return DTypeF32, nil
	case STI32:
		return DTypeI32, nil
	default:
		return 0, &UnsupportedError{Kind: "dtype", Name: string(d), Details: "no reference kernel for this safetensors dtype"}
	}
}

// SafetensorsTensorInfo is one validated header record.
type SafetensorsTensorInfo struct {
	Name        string
	DType       SafetensorsDType
	Shape       Shape
	DataOffsets [2]uint64
}

// Numel returns the validated element count.
func (info *SafetensorsTensorInfo) Numel() (int, error) {
	return info.Shape.Numel()
}

// SafetensorsMeta is the complete validated header.
type SafetensorsMeta struct {
	Tensors   []SafetensorsTensorInfo // sorted by DataOffsets[0] (spec order)
	Metadata  map[string]string       // the __metadata__ map, if present
	DataStart uint64                  // absolute offset of the data section
	DataEnd   uint64                  // == len(buffer) when validated against bytes
}

// ParseSafetensorsHeader validates a complete safetensors byte buffer:
// the 8-byte little-endian header length, the JSON header, every tensor
// record (dtype, shape, offsets, byte counts), monotonic non-overlapping
// data ranges inside the data section.
//
// The result is METADATA ONLY: it names what the container carries. It
// is NOT model support (see the package comment).
func ParseSafetensorsHeader(data []byte) (*SafetensorsMeta, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("%w: buffer of %d bytes is shorter than the 8-byte header length field", ErrMalformedContainer, len(data))
	}
	n := binary.LittleEndian.Uint64(data[:8])
	if n < 2 || n > safetensorsMaxHeaderBytes {
		return nil, fmt.Errorf("%w: header length %d outside the [%d,%d] validation window", ErrMalformedContainer, n, 2, int64(safetensorsMaxHeaderBytes))
	}
	if uint64(len(data)) < 8+n {
		return nil, fmt.Errorf("%w: header length %d exceeds the %d-byte buffer", ErrMalformedContainer, n, len(data))
	}
	headerJSON := data[8 : 8+n]

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(headerJSON, &raw); err != nil {
		return nil, fmt.Errorf("%w: header JSON: %v", ErrMalformedContainer, err)
	}

	meta := &SafetensorsMeta{
		DataStart: 8 + n,
		DataEnd:   uint64(len(data)),
	}

	// __metadata__ is the reserved key carrying free-form provenance.
	if md, ok := raw["__metadata__"]; ok {
		meta.Metadata = map[string]string{}
		if err := json.Unmarshal(md, &meta.Metadata); err != nil {
			return nil, fmt.Errorf("%w: __metadata__ is not a string map: %v", ErrMalformedContainer, err)
		}
		delete(raw, "__metadata__")
	}

	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: header carries no tensor records", ErrMalformedContainer)
	}

	for name, body := range raw {
		var rec struct {
			DType       string   `json:"dtype"`
			Shape       []int64  `json:"shape"`
			DataOffsets []uint64 `json:"data_offsets"`
		}
		if err := json.Unmarshal(body, &rec); err != nil {
			return nil, fmt.Errorf("%w: tensor %q record: %v", ErrMalformedContainer, name, err)
		}
		dt := SafetensorsDType(rec.DType)
		elem := dt.sizeOf()
		if elem == 0 {
			return nil, fmt.Errorf("%w: tensor %q: dtype %q has no byte-size table entry", ErrMalformedContainer, name, rec.DType)
		}
		shape := make(Shape, len(rec.Shape))
		for i, dim := range rec.Shape {
			if dim < 0 || dim > int64(^uint(0)>>1) {
				return nil, fmt.Errorf("%w: tensor %q: dimension %d = %d is not a machine int", ErrMalformedContainer, name, i, dim)
			}
			shape[i] = int(dim)
		}
		numel, err := shape.Numel()
		if err != nil {
			return nil, fmt.Errorf("%w: tensor %q: %v", ErrMalformedContainer, name, err)
		}
		want, err := checkedMul(numel, elem)
		if err != nil {
			return nil, fmt.Errorf("%w: tensor %q: byte count: %v", ErrMalformedContainer, name, err)
		}
		if len(rec.DataOffsets) != 2 {
			return nil, fmt.Errorf("%w: tensor %q: data_offsets must have exactly 2 entries, got %d", ErrMalformedContainer, name, len(rec.DataOffsets))
		}
		start, end := rec.DataOffsets[0], rec.DataOffsets[1]
		if end < start {
			return nil, fmt.Errorf("%w: tensor %q: data_offsets [%d,%d] are reversed", ErrMalformedContainer, name, start, end)
		}
		if uint64(want) != end-start {
			return nil, fmt.Errorf("%w: tensor %q: data_offsets span %d bytes but %s %v needs exactly %d", ErrMalformedContainer, name, end-start, dt, []int(shape), want)
		}
		if meta.DataStart+start > meta.DataEnd || meta.DataStart+end > meta.DataEnd {
			return nil, fmt.Errorf("%w: tensor %q: data_offsets [%d,%d] escape the %d-byte data section", ErrMalformedContainer, name, start, end, meta.DataEnd-meta.DataStart)
		}
		meta.Tensors = append(meta.Tensors, SafetensorsTensorInfo{
			Name:        name,
			DType:       dt,
			Shape:       shape,
			DataOffsets: [2]uint64{meta.DataStart + start, meta.DataStart + end},
		})
	}

	sort.Slice(meta.Tensors, func(i, j int) bool {
		return meta.Tensors[i].DataOffsets[0] < meta.Tensors[j].DataOffsets[0]
	})

	// Overlap detection: adjacent (sorted) ranges must not intersect.
	for i := 1; i < len(meta.Tensors); i++ {
		prev := meta.Tensors[i-1]
		cur := meta.Tensors[i]
		if cur.DataOffsets[0] < prev.DataOffsets[1] {
			return nil, fmt.Errorf("%w: tensors %q and %q have overlapping data ranges", ErrMalformedContainer, prev.Name, cur.Name)
		}
	}

	return meta, nil
}

// TensorFromSafetensors materializes ONE validated container record as
// an engine tensor over the ORIGINAL bytes (zero copy; the caller keeps
// the buffer alive and read-only). The dtype must have a reference
// kernel; everything else is a typed unsupported answer.
func TensorFromSafetensors(data []byte, info SafetensorsTensorInfo) (*Tensor, error) {
	dt, err := info.DType.toEngineDType()
	if err != nil {
		return nil, err
	}
	if info.DataOffsets[1] > uint64(len(data)) {
		return nil, fmt.Errorf("%w: tensor %q offsets escape the buffer", ErrMalformedContainer, info.Name)
	}
	return NewTensorFromBytes(dt, info.Shape, data[info.DataOffsets[0]:info.DataOffsets[1]])
}
