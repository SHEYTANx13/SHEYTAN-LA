// gen-license: regenerates LICENSE.md from internal/brand so the shipped
// file can never drift from what the app displays.
//
// v1.7.2 single-license-file architecture: the generator writes EXACTLY
// ONE license artifact — LICENSE.md at the repository root. It must NEVER
// emit LICENSE, LICENSE-APACHE, LICENSE-PROPRIETARY, LICENSE-MAP.md,
// NOTICE.md, or any other licensing artifact; the whole-tree
// exact-one contract is enforced by internal/releasecontract.
//
// gen-license.go is also the guard against accidental reintroduction: if
// a legacy artifact appears next to LICENSE.md, the generator removes it
// (the consolidated document is the sole authority).
//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/brand"
)

// legacyArtifacts are the pre-consolidation license files. They were
// merged into LICENSE.md and must never exist again.
var legacyArtifacts = []string{
	"LICENSE",
	"LICENSE-APACHE",
	"LICENSE-PROPRIETARY",
	"LICENSE-MAP.md",
	"NOTICE.md",
}

func main() {
	if err := os.WriteFile("LICENSE.md", []byte(brand.LicenseText+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, legacy := range legacyArtifacts {
		if _, err := os.Stat(legacy); err == nil {
			if err := os.Remove(legacy); err != nil {
				fmt.Fprintf(os.Stderr, "legacy license artifact %s exists and could not be removed: %v\n", legacy, err)
				os.Exit(1)
			}
			fmt.Printf("removed legacy license artifact %s (merged into LICENSE.md)\n", legacy)
		}
	}

	fmt.Println("LICENSE.md regenerated from brand.LicenseText (" + brand.LicenseName + ")")
}
