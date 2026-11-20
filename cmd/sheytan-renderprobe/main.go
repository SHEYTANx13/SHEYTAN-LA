//go:build !android && !ios

// sheytan-renderprobe — the combined-link regression probe
// (run 44547031698 §3.7.4/§3.7.5: "run an actual linked production app
// build that imports both Go-Gui and GoGPU. A successful build of each
// package separately does not prove the combined link works").
//
// ONE binary importing BOTH presentation/service stacks that historically
// collided at link time:
//
//   - the Go-Gui shell stack (ui/gogui -> go-gui -> ebitengine/purego,
//     whose CGO_ENABLED=0 build carries internal/fakecgo); and
//   - the GoGPU render service (internal/render -> gogpu -> goffi, whose
//     CGO_ENABLED=0 build carries its own internal/fakecgo).
//
// Under CGO_ENABLED=0 both fakecgo copies define _cgo_init and the link
// fails ("duplicated definition of symbol _cgo_init") - goffi v0.6.8's
// documented Workaround A is the -tags nofakecgo build, which disables
// goffi's copy and relies on purego's. This probe is the regression
// target that PROVES the documented configuration still links on every
// CI run and CodeQL build:
//
//	CGO_ENABLED=0 go build -tags nofakecgo ./cmd/sheytan-renderprobe
//
// It is a build/CI verification target, not a released surface: it never
// owns a product window, never starts a shell loop, and never connects
// to a core. The blank imports force both packages' link graphs (package
// inits are always reachable, so neither fakecgo copy can be dead-stripped).
//
// The optional -render flag executes the REAL software-path verification
// job through the render service facade (headless, no display, no GPU
// claims - the software renderer is a real path of the pinned gogpu
// stack) and prints the observation as JSON. A failure is reported as a
// failure, never masked (work order §10.1).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/render"

	_ "github.com/SHEYTANx13/SHEYTAN-LA/internal/renderboot" // render-service startup wiring (renderstate contract)
	_ "github.com/SHEYTANx13/SHEYTAN-LA/ui/gogui"            // Go-Gui/purego link graph (purego fakecgo)
)

func main() {
	renderJob := flag.Bool("render", false, "run the real software-path render verification job (headless) and print the observation")
	flag.Parse()

	fmt.Printf("%s %s combined-link probe: go-gui+gogpu link graph resolved (purego+goffi)\n", config.AppName, config.AppVersion)

	if *renderJob {
		runRenderVerification()
		return
	}

	fmt.Println("link probe OK (no render job requested; pass -render for the headless software-path verification)")
}

// runRenderVerification executes the render service's REAL verification
// job on the software path and prints the discovery snapshot and the job
// result as JSON. Every failure is printed and exits nonzero - never
// masked (evidence honesty: a software-path execution is CPU-class
// evidence, never a GPU/backend claim).
func runRenderVerification() {
	svc := render.Default()

	snap := svc.Discover()

	enc := json.NewEncoder(os.Stdout)

	if err := enc.Encode(snap); err != nil {
		fmt.Fprintf(os.Stderr, "renderprobe: snapshot encode failed: %v\n", err)
		os.Exit(1)
	}

	res, err := svc.RunVerificationJob(1, 240, 120)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render verification job failed (surfaced honestly, never masked): %v\n", err)
		os.Exit(1)
	}

	if err := enc.Encode(res); err != nil {
		fmt.Fprintf(os.Stderr, "renderprobe: result encode failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("render verification job verified: id=%s backend=%s sha256=%.16s duration=%s\n",
		res.ID, res.Backend, res.SHA256, res.Duration)
}
