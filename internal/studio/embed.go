package studio

import _ "embed"

// The complete viewer is embedded at build time: no CDN, no network
// fetch, no runtime download. The three.js files are the verbatim vendored
// release pinned in doc.go (ThreeJSVersion) with its MIT license text.
var (
	//go:embed view/index.html
	viewIndexHTML string

	//go:embed view/studio.js
	viewStudioJS string

	//go:embed view/studio.css
	viewStudioCSS string

	//go:embed assets/three.core.js
	threeCoreJS []byte

	//go:embed assets/three.module.js
	threeModuleJS []byte

	//go:embed assets/OrbitControls.js
	orbitControlsJS []byte
)
