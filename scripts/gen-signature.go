// gen-signature: regenerates the SIGNATURE file from internal/brand so the
// shipped signature can never drift from what the app displays. The
// application is SIGNED UNDER THE NAME "Parsa Tak" (v1.0.8+): the same
// signature is embedded in the exe version resource, printed in the About
// dialog, and carried here.
//
// The SIGNATURE file ships in BOTH distribution zips (full app + GitHub
// source) next to the LICENSE.
//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/brand"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func main() {
	version := config.AppVersion
	if len(os.Args) > 1 {
		version = os.Args[1]
	}
	body := brand.SignatureBlock(version) + "\n" +
		"This file is regenerated from internal/brand at build time; the\n" +
		"signature embedded in the exe, the About dialog and this file are\n" +
		"one and the same source of truth.\n"
	if err := os.WriteFile("SIGNATURE", []byte(body), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("SIGNATURE regenerated (signed by %s, v%s)\n", brand.SignedBy, version)
}
