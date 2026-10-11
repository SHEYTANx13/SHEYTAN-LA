// Firewall rule management (v1.2.0, P1).
//
// SHEYTAN's local services (the llama.cpp engine, the local API) bind the
// loopback interface by default, which Windows Firewall does not filter —
// so NO rule is created unless the user actually needs one (e.g. a custom
// non-loopback bind). When rules ARE requested, they follow the product's
// security posture:
//
//   - one rule per logical purpose, named exactly (Parsaetak.SHEYTAN-LA …)
//     so they are discoverable, idempotent and removable;
//   - scoped to the EXACT process image path;
//   - minimum scope: the configured local port, TCP, inbound allow;
//   - never broad (no "any port", no "any program", no wildcard IPs);
//   - created via `netsh advfirewall firewall` with deterministic
//     argument vectors that are unit-tested byte-for-byte;
//   - verified after creation by re-reading the rule; removable by name.
//
// On non-Windows platforms every operation returns
// ErrUnsupportedPlatform — the UI reports that honestly instead of
// pretending a rule exists.
package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Firewall rule naming — stable identities for idempotent re-apply and
// clean removal.
const (
	FirewallRuleApp  = "Parsaetak.SHEYTAN-LA"
	FirewallRuleAPI  = "Parsaetak.SHEYTAN-LA Local API"
	FirewallRuleMCP  = "Parsaetak.SHEYTAN-LA MCP"
	FirewallRulePort = "Parsaetak.SHEYTAN-LA Engine"
)

// FirewallRule is one explicit, minimal firewall rule request.
type FirewallRule struct {
	// Name is the stable Windows rule name (one of the constants above).
	Name string
	// ExePath is the exact process image path the rule is scoped to.
	ExePath string
	// Port is the single local TCP port allowed (0 is rejected).
	Port int
	// Direction is "in" (the only direction this release creates).
	Direction string
	// Protocol is "TCP" (the only protocol this release creates).
	Protocol string
}

// Validate rejects malformed or overly-broad rules before any system call.
func (r FirewallRule) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("rule name is required")
	}
	if strings.TrimSpace(r.ExePath) == "" {
		return errors.New("rule program path is required (rules must be scoped to an exact executable)")
	}
	if r.Port <= 0 || r.Port > 65535 {
		return errors.New("rule requires a single explicit TCP port")
	}
	return nil
}

// addArgs builds the deterministic `netsh advfirewall firewall` argument
// vector for an idempotent rule add (run as "delete" + "add" pair by Apply
// so re-apply never duplicates).
func FirewallAddArgs(r FirewallRule) ([]string, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return []string{
		"advfirewall", "firewall", "rule", "add",
		"name=" + r.Name,
		"dir=in",
		"action=allow",
		"program=" + r.ExePath,
		"enable=yes",
		"profile=private,domain",
		"protocol=TCP",
		"localport=" + fmt.Sprint(r.Port),
		"description=SHEYTAN-LA managed rule — scoped to this executable and port; safe to remove via the app",
	}, nil
}

// FirewallDeleteArgs builds the removal argument vector (exact name).
func FirewallDeleteArgs(name string) ([]string, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("rule name is required")
	}
	return []string{
		"advfirewall", "firewall", "rule", "delete",
		"name=" + name,
	}, nil
}

// FirewallShowArgs builds the inspection argument vector used to verify a
// rule exists after creation.
func FirewallShowArgs(name string) ([]string, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("rule name is required")
	}
	return []string{
		"advfirewall", "firewall", "rule", "show",
		"name=" + name,
	}, nil
}

// firewallTimeout bounds every netsh invocation; firewall calls must never
// hang the API surface.
const firewallTimeout = 10 * time.Second

// FirewallApply creates (or re-creates) one rule and verifies it exists
// afterwards. Windows only.
func FirewallApply(ctx context.Context, r FirewallRule) error {
	if runtime.GOOS != "windows" {
		return ErrUnsupportedPlatform
	}
	del, err := FirewallDeleteArgs(r.Name)
	if err != nil {
		return err
	}
	add, err := FirewallAddArgs(r)
	if err != nil {
		return err
	}
	show, err := FirewallShowArgs(r.Name)
	if err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, firewallTimeout)
	defer cancel()

	if out, err := runNetsh(cctx, del...); err != nil {
		// A delete of a non-existent rule is not an error — netsh exits
		// nonzero with "No rules match". Treat only real failures.
		if !strings.Contains(strings.ToLower(out), "no rules match") {
			return fmt.Errorf("remove existing rule: %w: %s", err, out)
		}
	}
	if out, err := runNetsh(cctx, add...); err != nil {
		return fmt.Errorf("create rule: %w: %s", err, out)
	}
	if out, err := runNetsh(cctx, show...); err != nil {
		return fmt.Errorf("verify rule: %w: %s", err, out)
	}
	return nil
}

// FirewallRemove deletes one rule by exact name. Windows only.
func FirewallRemove(ctx context.Context, name string) error {
	if runtime.GOOS != "windows" {
		return ErrUnsupportedPlatform
	}
	del, err := FirewallDeleteArgs(name)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, firewallTimeout)
	defer cancel()
	if out, err := runNetsh(cctx, del...); err != nil {
		if strings.Contains(strings.ToLower(out), "no rules match") {
			return nil // already gone — removal is idempotent
		}
		return fmt.Errorf("delete rule: %w: %s", err, out)
	}
	return nil
}

// FirewallStatus reports whether a rule exists. Windows only; on other
// platforms state is "unsupported".
func FirewallStatus(ctx context.Context, name string) (exists bool, err error) {
	if runtime.GOOS != "windows" {
		return false, ErrUnsupportedPlatform
	}
	show, err := FirewallShowArgs(name)
	if err != nil {
		return false, err
	}
	cctx, cancel := context.WithTimeout(ctx, firewallTimeout)
	defer cancel()
	out, runErr := runNetsh(cctx, show...)
	return runErr == nil && strings.Contains(out, "Rule Name:"), nil
}

func runNetsh(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "netsh", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
