// Package releasegate pins the repository-integrity lessons in CI as Go
// tests (see releasegate_test.go for the full gate). The package is
// test-only by design — it carries no runtime API — but a package with
// no non-test Go files makes `go build ./...` fail with "no non-test Go
// files", which broke the CodeQL Go build (run 44663205241) the moment
// the workflow started compiling every package of ./.... This doc.go is
// the minimal non-test file that keeps the gate package compilable
// while the tests themselves remain unchanged.
//
// v1.0.11 lesson (preserved verbatim from the original package comment):
// a .gitignore pattern that matches source paths silently drops packages
// from commits — which is precisely how internal/sessions and
// internal/sandbox went missing and broke the v1.0.9 and v1.0.10 builds
// on GitHub (the code compiled locally because the files were on disk;
// git simply never tracked them). The gate walks the real source tree
// and asks git itself (check-ignore) whether any file that exists on
// disk is invisible to git, so a re-broken .gitignore fails the build
// BEFORE a broken release can be pushed.
package releasegate
