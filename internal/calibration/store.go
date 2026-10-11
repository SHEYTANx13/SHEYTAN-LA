// store.go — retained verified calibration records (spec §4: "retain
// verified profile"), hardened in v1.5.1 (spec §6: safe invalidation).
//
// A record is keyed by a MODEL fingerprint (absolute path + size + mtime)
// AND a MACHINE fingerprint (HWKey) AND the runtime-profile (task) it was
// measured for. The machine fingerprint itself carries every identity
// dimension that can invalidate a measurement, where available:
//
//	OS/arch, CPU identity (model + core counts), total RAM, GPU identity
//	(vendor + name + VRAM + driver version), the accelerator/backend
//	posture (Vulkan engine backend), and the ENGINE build identity
//	(verified tag + engine binary size/mtime).
//
// A retained profile is only reused when the WHOLE key matches: a
// different model file, a different machine part, a different engine
// build or a different task invalidates the measurement and the record
// is treated as stale (evidence-based configuration is used instead).
// Corrupt or incomplete records fail closed: they are never reusable.
// The file lives at {DataDir}/calibration.json and is written atomically
// (tmp + rename), cross-platform.
package calibration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
)

// Record is one retained verified calibration result.
type Record struct {
	// ModelKey is the model fingerprint (ModelKey()).
	ModelKey string `json:"modelKey"`
	// HWKey is the machine + engine fingerprint (HWKey()).
	HWKey string `json:"hwKey"`
	// Task is the runtime profile the measurement was taken for (a
	// chat-calibrated profile must not silently serve an agent run —
	// spec §5: the same model may legitimately need different profiles).
	Task string `json:"task"`
	// ModelPath is the absolute model path at measurement time.
	ModelPath string `json:"modelPath"`
	// Winner is the winning candidate label.
	Winner string `json:"winner"`
	// Profile is the verified winning profile to re-apply.
	Profile recommendation.Recommendation `json:"profile"`
	// GenTokensPerSec / TTFTSeconds are the MEASURED values of the
	// winning run (never fabricated; surfaced as "measured on <date>").
	GenTokensPerSec float64   `json:"genTokensPerSec"`
	TTFTSeconds     float64   `json:"ttftSeconds"`
	MeasuredAt      time.Time `json:"measuredAt"`
}

// usable reports whether a loaded record is complete enough to reuse.
// Corrupt or incomplete records fail closed (spec §6): they are skipped
// at load time and replaced by the next Retain.
func (r Record) usable() bool {
	return r.ModelKey != "" &&
		r.HWKey != "" &&
		r.Winner != "" &&
		r.Task != "" &&
		recommendation.IsValidTask(r.Task) &&
		r.Profile.Context > 0 &&
		r.MeasuredAt.Unix() > 0
}

// storeFile is the on-disk shape.
type storeFile struct {
	Records []Record `json:"records"`
}

// StoreFileName is the canonical file name under the data dir.
const StoreFileName = "calibration.json"

// ModelKey fingerprints a model file: absolute path + size + mtime (the
// model identity AND its file state).
func ModelKey(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("model key: %w", err)
	}

	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("model key: %w", err)
	}

	return fmt.Sprintf("%s|%d|%d", abs, fi.Size(), fi.ModTime().UnixNano()), nil
}

// HWKey fingerprints the machine + the runtime identity a measurement
// was taken against (spec §6). Every available identity dimension is
// included; when a dimension's evidence is unavailable it renders as a
// distinct "unknown" value — so an earlier record taken when the
// evidence WAS available no longer matches (fail closed), and a record
// never matches across different machines or engine builds.
func HWKey(hw hardware.Profile) string {
	gpu := "unknown"
	if g := hw.PrimaryGPU(); g != nil {
		gpu = fmt.Sprintf("%s/%s/%d/drv=%s",
			clean(g.Vendor), clean(g.Name), g.VRAMBytes, clean(g.DriverVer))
	}

	engine := "tag=" + clean(hw.Backend.EngineTag)
	if hw.Backend.EngineBinary != "" {
		if fi, err := os.Stat(hw.Backend.EngineBinary); err == nil {
			engine += fmt.Sprintf(";bin=%d|%d", fi.Size(), fi.ModTime().UnixNano())
		} else {
			// The engine binary that backed a measured profile is gone —
			// the identity evidence is unavailable, so this key can never
			// match a record retained against a present binary.
			engine += ";bin=missing"
		}
	} else {
		engine += ";bin=missing"
	}

	return fmt.Sprintf(
		"os=%s/%s;cpu=%s/%d/%d;ram=%d;gpu=%s;backend=vulkan=%s;%s",
		clean(hw.OS), clean(hw.Arch),
		clean(hw.CPU.Name), hw.CPU.PhysicalCores, hw.CPU.LogicalCores,
		hw.RAM.TotalBytes,
		gpu,
		strconv.FormatBool(hw.Backend.Vulkan),
		engine,
	)
}

// clean normalizes an identity string for the fingerprint: whitespace
// is collapsed and empties render as "unknown" (a stable, comparable
// value — never a silent wildcard).
func clean(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if s == "" {
		return "unknown"
	}
	return s
}

// Load reads the usable retained records from {dataDir}/calibration.json.
// A missing file is an empty store, not an error. Records that fail the
// completeness check are dropped (fail closed) — they are never reused.
func Load(dataDir string) (map[string]Record, error) {
	path := filepath.Join(dataDir, StoreFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Record{}, nil
		}
		return nil, fmt.Errorf("calibration store read: %w", err)
	}

	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("calibration store parse: %w", err)
	}

	out := make(map[string]Record, len(sf.Records))
	for _, r := range sf.Records {
		if !r.usable() {
			// Corrupt/incomplete: fail closed, never reused.
			continue
		}
		out[r.ModelKey+"@"+r.HWKey+"@"+r.Task] = r
	}

	return out, nil
}

// Save persists the records atomically (tmp + rename, cross-platform).
func Save(dataDir string, records map[string]Record) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("calibration store dir: %w", err)
	}

	sf := storeFile{Records: make([]Record, 0, len(records))}
	for _, r := range records {
		sf.Records = append(sf.Records, r)
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("calibration store encode: %w", err)
	}

	path := filepath.Join(dataDir, StoreFileName)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("calibration store write: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("calibration store rename: %w", err)
	}

	return nil
}

// Retained looks up a verified record for the model fingerprint on this
// machine, for the requested runtime profile. The bool reports a usable
// hit — any mismatch (machine part, engine identity, task) or a corrupt
// store is a miss, never a partial reuse.
func Retained(dataDir, modelKey, hwKey, task string) (Record, bool) {
	records, err := Load(dataDir)
	if err != nil {
		// A store that cannot be parsed fails closed.
		return Record{}, false
	}

	if !recommendation.IsValidTask(task) {
		return Record{}, false
	}

	r, ok := records[modelKey+"@"+hwKey+"@"+task]
	return r, ok
}

// Retain stores a verified result. The record's task comes from the
// profile itself (the recommendation engine stamps it), so a record can
// only ever be reused for the profile it was measured with.
func Retain(dataDir, modelKey, hwKey string, rec Record) error {
	records, err := Load(dataDir)
	if err != nil {
		// A corrupt store is replaced, not trusted.
		records = map[string]Record{}
	}

	if rec.Profile.Task != "" {
		rec.Task = string(rec.Profile.Task)
	}
	if !recommendation.IsValidTask(rec.Task) {
		return fmt.Errorf("calibration store retain: profile has no valid task profile")
	}

	rec.ModelKey = modelKey
	rec.HWKey = hwKey
	records[modelKey+"@"+hwKey+"@"+rec.Task] = rec

	return Save(dataDir, records)
}
