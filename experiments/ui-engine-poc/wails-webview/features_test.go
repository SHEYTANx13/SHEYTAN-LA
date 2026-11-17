// features_test.go — the feature-detection manifest: the complete probe
// vocabulary, every probe carrying an honest fallback, WebGPU never
// claimed from detection.
package main

import (
	"testing"
)

// TestFeatureVocabularyIsComplete pins the full v1.11.1 probe list (the
// work order's §3.4 enumeration) — a missing probe is a contract break.
func TestFeatureVocabularyIsComplete(t *testing.T) {
	want := []string{
		"html5", "modern-css", "js-modules", "canvas", "webgl", "webgl2",
		"webgpu", "webaudio", "video-codecs", "audio-codecs", "webrtc",
		"media-devices", "indexeddb", "service-workers",
		"filesystem-access", "downloads", "clipboard",
	}
	got := FeatureNames()
	if len(got) != len(want) {
		t.Fatalf("feature vocabulary has %d probes, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("probe order/content drifted at %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestEveryFeatureHasFallback(t *testing.T) {
	if !EveryFeatureHasFallback() {
		t.Fatal("every probe must carry a valid degradation policy — an unsupported feature must never dead-end")
	}
}

// TestWebGPUFallsBackToSystemBrowser: WebGPU is probed and DEGRADED —
// never claimed from detection (the host engine decides).
func TestWebGPUFallsBackToSystemBrowser(t *testing.T) {
	f, ok := FeatureByName("webgpu")
	if !ok {
		t.Fatal("the webgpu probe is missing")
	}
	if f.Fallback != "system-browser" {
		t.Fatalf("webgpu fallback is %q, want system-browser", f.Fallback)
	}
}

func TestMediaDevicesAndClipboardCarryApprovalNotes(t *testing.T) {
	for _, name := range []string{"media-devices", "clipboard"} {
		f, ok := FeatureByName(name)
		if !ok {
			t.Fatalf("the %s probe is missing", name)
		}
		if f.Note == "" {
			t.Errorf("the %s probe must document its approval requirement (camera/mic/clipboard writes are user-confirmed)", name)
		}
	}
}

func TestFeatureByNameRejectsUnknown(t *testing.T) {
	if _, ok := FeatureByName("telepathy"); ok {
		t.Fatal("an unknown feature resolved — the vocabulary is closed")
	}
}
