package engdiscovery

// The bounded --version probe for validated candidates (spec §12 step 5:
// a bounded safe probe only AFTER static checks). It uses the SAME
// subprocess runner as the engine launch path (internal/proc: hidden
// window + process-group kill on cancel) so probe behavior cannot drift
// from production launches, and a sanitized environment so a candidate
// can never observe secrets from the app's environment.

import (
	"context"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
)

// runBounded executes the candidate's --version flag under a hard
// context deadline and returns the combined output.
func runBounded(ctx context.Context, path string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := proc.CommandContext(runCtx, path, "--version")
	cmd.Env = proc.SanitizedEnvironment(nil)

	out, err := cmd.CombinedOutput()

	return strings.TrimSpace(string(out)), err
}
