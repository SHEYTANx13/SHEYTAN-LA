package cmd

import (
	"fmt"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/brand"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// License prints the SHEYTAN™ trademark + the consolidated license text.
//
// v1.7.2: the repository carries exactly ONE licensing artifact —
// LICENSE.md — and brand.LicenseText is that complete document (the
// mixed-license model, the component classification, the third-party
// notices, and the FULL Apache-2.0 and Parsaetak Proprietary License
// v1.1 texts). The CLI therefore prints truthful legal and trademark
// information with no routing to files that no longer exist.
func License(cfg *config.Config) int {
	fmt.Printf("%s — %s\n", brand.Trademark, brand.FullName)
	fmt.Println(brand.Copyright())
	fmt.Println(brand.TrademarkNotice)
	fmt.Print("\n")
	fmt.Print(brand.LicenseText)
	fmt.Println()
	return 0
}
