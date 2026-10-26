// Package studio implements the optional 3D Studio: a read-only
// system-topology visualizer rendered with a locally-bundled Three.js
// release inside the user's browser.
//
// Boundary contract (v1.11.0):
//   - The studio is a VIEWER. It exposes only minimum read-only view data
//     (one schema'd topology DTO) and zero commands. Mutations are
//     impossible from this surface by construction.
//   - The viewer server binds 127.0.0.1 on an ephemeral port — never an
//     external interface — and is started/stopped on demand by the ONE
//     core authority (internal/api), not by the UI.
//   - Static viewer assets carry no secrets; the topology endpoint
//     requires a per-launch bearer token delivered out-of-band (URL
//     fragment, never a query parameter, never logged).
//   - The renderer degrades honestly: without WebGL the page says so and
//     the textual topology equivalent remains fully usable.
package studio

import "errors"

// Typed studio failures — the API layer maps them to honest 4xx text.
var (
	errNotRunning     = errors.New("the 3D Studio viewer is not running")
	errAlreadyRunning = errors.New("the 3D Studio viewer is already running")
	errEmptyTopology  = errors.New("topology has no nodes")
	errInvalidNode    = errors.New("topology node missing id or label")
	errDuplicateNode  = errors.New("topology has duplicate node ids")
	errDanglingEdge   = errors.New("topology edge references an unknown node")
)

// Pinned Three.js release (the vendored copy in assets/, served offline).
// Update procedure: replace assets/three.core.js, assets/three.module.js
// and assets/OrbitControls.js from the same npm release tag, update the
// version here, and re-run the studio tests.
const (
	ThreeJSVersion = "0.182.0" // three.js r182, MIT — see assets/LICENSE-THREE.txt
	ThreeJSLicense = "MIT"
)
