package testfakes

// testlab.go — the re-exec helper for Coding Lab process-boundary tests.
//
// The lab runner executes command strings through the platform shell
// (/bin/sh -c on POSIX, cmd.exe /d /s /c on Windows). A test that needs a
// command to FAIL DETERMINISTICALLY through that real boundary must not
// spell a shell builtin ("exit 1") — the portability gate bans shell
// command strings in test files because the ONLY portable way to script
// a subprocess outcome is the re-exec mechanism this package provides.
//
// The helper: the test stages a copy of the test binary INSIDE the lab
// workspace (the lab policy forbids absolute and parent-escaping paths,
// so the fixture must live within the workspace and be invoked by
// relative path) and passes the mode variable through the production
// Command.Environment path (the runner merges it into the sanitized
// child environment). The re-executed copy dispatches here, reports a
// scripted diagnostic on stderr, and exits with the scripted code — a
// REAL process outcome observed by the REAL runner, on every platform.

import (
        "fmt"
        "os"
        "time"
)

// LabModeEnv is the environment variable that turns a re-executed test
// binary into a scripted lab subprocess. It travels through the
// production lab runner environment merge (Command.Environment), so the
// fixture proves the env path end-to-end.
const LabModeEnv = "SHEYTAN_FAKE_LAB_MODE"

// LabFailArg is the argv marker the scripted subprocess is invoked with.
// Gating on argv IN ADDITION to the mode variable means a stray mode
// variable in the outer environment can never hijack a normal
// `go test` invocation of the same binary.
const LabFailArg = "testfakes-lab-fail"

// RunLabHelper executes the scripted lab-subprocess behavior when this
// process was started as one: the mode variable is set AND argv carries
// the lab marker. It never returns normally — the scripted behavior
// terminates the process with the scripted exit code. Otherwise it
// returns false and the caller continues as a test binary.
func RunLabHelper(argv []string) bool {
        mode := os.Getenv(LabModeEnv)
        if mode == "" {
                return false
        }

        marker := false
        for _, a := range argv {
                if a == LabFailArg {
                        marker = true
                        break
                }
        }

        if !marker {
                return false
        }

        switch mode {
        case "fail":
                fmt.Fprintln(os.Stderr, "testfakes lab helper: scripted failure (exit 1)")
                os.Exit(1)
        case "hang":
                // A genuinely long-lived victim for cancellation tests: stays
                // alive until the caller's process-tree teardown kills it.
                for i := 0; i < 600; i++ {
                        sleep100ms()
                }
                os.Exit(0)
        default:
                fmt.Fprintf(os.Stderr, "testfakes lab helper: unknown mode %q\n", mode)
                os.Exit(64)
        }

        return true // unreachable: every behavior ends the process
}

// sleep100ms pauses briefly; the hang mode loops over it while waiting
// for the caller's bounded cancellation.
func sleep100ms() {
        time.Sleep(100 * time.Millisecond)
}
