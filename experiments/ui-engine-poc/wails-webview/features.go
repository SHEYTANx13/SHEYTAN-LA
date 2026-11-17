// features.go — the WebView capability DETECTION contract (§3.4/§6).
//
// A WebView is NOT equivalent to shipping the newest Chrome/Edge/Safari on
// every OS: the HOST engine controls what exists. The workspace therefore
// PROBES features at runtime and degrades visibly — an unsupported feature
// either offers an "Open in system browser" fallback or renders an honest
// unavailable state. This file pins the probe manifest (the full list the
// work order requires) so the fixture page and the Go side agree on ONE
// capability vocabulary; detection itself runs IN the WebView (the asset
// page), and the results flow back through the bridge with this manifest's
// shape.
package main

// Feature is one probed WebView capability.
type Feature struct {
	Name     string `json:"name"`
	Probe    string `json:"probe"`    // the JS probe expression family
	Fallback string `json:"fallback"` // "system-browser" | "unavailable" | "none"
	Note     string `json:"note,omitempty"`
}

// FeatureManifest is the full probed vocabulary.
type FeatureManifest struct {
	Schema  int      `json:"schema"`
	Feature []string `json:"features"`
	Probes  []Feature
}

// features — the complete v1.11.1 probe list (work order §3.4):
// HTML5, modern CSS, JS modules, WebGL/WebGL2, WebGPU (where supported),
// Canvas, WebAudio, video/audio codecs, WebRTC, MediaDevices, IndexedDB,
// service workers, filesystem access, downloads, clipboard and permission
// prompts.
var features = []Feature{
	{Name: "html5", Probe: "document.doctype", Fallback: "unavailable"},
	{Name: "modern-css", Probe: "CSS.supports('display','grid')", Fallback: "unavailable"},
	{Name: "js-modules", Probe: "import.meta", Fallback: "unavailable"},
	{Name: "canvas", Probe: "getContext('2d')", Fallback: "unavailable"},
	{Name: "webgl", Probe: "getContext('webgl')", Fallback: "system-browser", Note: "varies by host engine and driver"},
	{Name: "webgl2", Probe: "getContext('webgl2')", Fallback: "system-browser"},
	{Name: "webgpu", Probe: "navigator.gpu", Fallback: "system-browser", Note: "WebGPU exists only where the host engine ships it — never claimed from detection"},
	{Name: "webaudio", Probe: "AudioContext", Fallback: "unavailable"},
	{Name: "video-codecs", Probe: "canPlayType", Fallback: "system-browser"},
	{Name: "audio-codecs", Probe: "canPlayType", Fallback: "system-browser"},
	{Name: "webrtc", Probe: "RTCPeerConnection", Fallback: "system-browser"},
	{Name: "media-devices", Probe: "navigator.mediaDevices", Fallback: "unavailable", Note: "camera/mic/screen require explicit user approval"},
	{Name: "indexeddb", Probe: "indexedDB", Fallback: "unavailable"},
	{Name: "service-workers", Probe: "serviceWorker", Fallback: "unavailable"},
	{Name: "filesystem-access", Probe: "showSaveFilePicker", Fallback: "system-browser"},
	{Name: "downloads", Probe: "a[download]", Fallback: "unavailable"},
	{Name: "clipboard", Probe: "navigator.clipboard", Fallback: "unavailable", Note: "clipboard writes require user approval"},
}

// FeatureNames returns the vocabulary in probe order.
func FeatureNames() []string {
	out := make([]string, 0, len(features))
	for _, f := range features {
		out = append(out, f.Name)
	}
	return out
}

// FeatureByName resolves one probe entry.
func FeatureByName(name string) (Feature, bool) {
	for _, f := range features {
		if f.Name == name {
			return f, true
		}
	}
	return Feature{}, false
}

// EveryFeatureHasFallback reports whether every probe carries a valid
// degradation policy — the acceptance rule for the manifest.
func EveryFeatureHasFallback() bool {
	for _, f := range features {
		switch f.Fallback {
		case "system-browser", "unavailable", "none":
		default:
			return false
		}
	}
	return true
}
