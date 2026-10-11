package lab

import "testing"

// TestPolicyBlocksExpandedPathEscapes pins the v1.1.4Z jail fix: $HOME/x,
// ~/x and ${VAR}/x tokens passed the lexical workspace-escape check while
// the real shell expanded them OUTSIDE the workspace.
func TestPolicyBlocksExpandedPathEscapes(t *testing.T) {
	p := DefaultPolicy()

	blocked := []string{
		"cat $HOME/secret.txt",
		"cat ~/secret.txt",
		"cp ${HOME}/key.bin .",
		"echo x > $USERPROFILE/escape.txt",
		"type %USERPROFILE%\\escape.txt",
	}

	for _, cmd := range blocked {
		err := p.Evaluate(cmd)
		if err == nil {
			t.Errorf("command %q must be blocked as a workspace escape", cmd)
		}
	}

	// Non-path variable usage stays legal (echo $PATH is not an escape).
	if err := p.Evaluate("echo $PATH"); err != nil {
		t.Errorf("echo $PATH must stay allowed: %v", err)
	}
}
