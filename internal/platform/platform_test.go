package platform

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
)

// runtimeGOOS indirection keeps the skip condition readable.
func runtimeGOOS() string { return runtime.GOOS }

// TestFirewallAddArgs_DeterministicAndScoped locks the exact netsh
// argument vectors: any future change to scope (program/port/profile) must
// be a deliberate, reviewed decision — these rules touch the OS.
func TestFirewallAddArgs_DeterministicAndScoped(t *testing.T) {
	r := FirewallRule{
		Name:    FirewallRulePort,
		ExePath: `C:\Program Files\SHEYTAN-LA\SHEYTAN-LA.exe`,
		Port:    8080,
	}
	got, err := FirewallAddArgs(r)
	if err != nil {
		t.Fatalf("FirewallAddArgs: %v", err)
	}

	want := []string{
		"advfirewall", "firewall", "rule", "add",
		"name=Parsaetak.SHEYTAN-LA Engine",
		"dir=in",
		"action=allow",
		"program=C:\\Program Files\\SHEYTAN-LA\\SHEYTAN-LA.exe",
		"enable=yes",
		"profile=private,domain",
		"protocol=TCP",
		"localport=8080",
		"description=SHEYTAN-LA managed rule — scoped to this executable and port; safe to remove via the app",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args mismatch:\n got: %#v\nwant: %#v", got, want)
	}

	for _, a := range got {
		if a == "any" || a == "localport=any" {
			t.Fatalf("rule must never open any port: %v", got)
		}
	}
}

func TestFirewallAddArgs_RejectsBroadRules(t *testing.T) {
	cases := []struct {
		name string
		rule FirewallRule
	}{
		{"no program", FirewallRule{Name: FirewallRuleAPI, Port: 8080}},
		{"no port", FirewallRule{Name: FirewallRuleAPI, ExePath: `C:\x.exe`}},
		{"bad port", FirewallRule{Name: FirewallRuleAPI, ExePath: `C:\x.exe`, Port: -1}},
		{"no name", FirewallRule{ExePath: `C:\x.exe`, Port: 1}},
	}
	for _, tc := range cases {
		if _, err := FirewallAddArgs(tc.rule); err == nil {
			t.Errorf("%s: expected validation error, got nil", tc.name)
		}
	}
}

func TestFirewallDeleteAndShowArgs(t *testing.T) {
	del, err := FirewallDeleteArgs(FirewallRuleApp)
	if err != nil {
		t.Fatalf("delete args: %v", err)
	}
	if want := []string{"advfirewall", "firewall", "rule", "delete", "name=Parsaetak.SHEYTAN-LA"}; !reflect.DeepEqual(del, want) {
		t.Fatalf("delete args mismatch: got %#v", del)
	}
	show, err := FirewallShowArgs(FirewallRuleApp)
	if err != nil {
		t.Fatalf("show args: %v", err)
	}
	if !reflect.DeepEqual(show, []string{"advfirewall", "firewall", "rule", "show", "name=Parsaetak.SHEYTAN-LA"}) {
		t.Fatalf("show args mismatch: got %#v", show)
	}
	if _, err := FirewallDeleteArgs(""); err == nil {
		t.Fatal("empty name must be rejected")
	}
}

func TestFirewallOperations_NonWindowsUnsupported(t *testing.T) {
	if runtimeGOOS() == "windows" {
		t.Skip("platform-specific expectation inverted on windows")
	}
	if _, err := FirewallStatus(context.Background(), FirewallRuleApp); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("status: want ErrUnsupportedPlatform, got %v", err)
	}
	if err := FirewallApply(context.Background(), FirewallRule{
		Name: FirewallRuleApp, ExePath: "/x", Port: 1,
	}); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("apply: want ErrUnsupportedPlatform, got %v", err)
	}
	if err := FirewallRemove(context.Background(), FirewallRuleApp); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("remove: want ErrUnsupportedPlatform, got %v", err)
	}
}
