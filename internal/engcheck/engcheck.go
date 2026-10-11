// Package engcheck is the single authority for static executable
// evidence: file identity (SHA-256/size/mtime), architecture sniffing,
// and — for Windows PE images — the imported-DLL dependency closure.
//
// v1.3.6: both the engine preflight (internal/llm) and the transactional
// updater/discovery validation validate candidates through THIS package
// so the consumers can never drift apart. It intentionally uses only the
// standard library (debug/pe, debug/elf, crypto/sha256) — no third-party
// dependency was added for this (spec §4/§12).
package engcheck

import (
	"crypto/sha256"
	"debug/elf"
	"debug/pe"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"
)

// Identity is the immutable identity of one file on disk.
type Identity struct {
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTimeUnix"`
	Arch    string `json:"arch,omitempty"`
	Format  string `json:"format,omitempty"` // "pe" | "elf" | ""
}

func timeNowUnix() int64 {
	return time.Now().Unix()
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

// HashFile computes the SHA-256 of path in a bounded streaming read.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// IdentityOf returns the full identity of one file: hash, size, mtime and
// the sniffed architecture/format. A missing or unreadable file is an
// error; an unknown binary format is NOT an error (Arch/Format stay
// empty) — callers decide whether that is acceptable for their context.
func IdentityOf(path string) (Identity, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Identity{}, err
	}
	if st.IsDir() {
		return Identity{}, fmt.Errorf("%s is a directory", path)
	}

	sum, err := HashFile(path)
	if err != nil {
		return Identity{}, err
	}

	arch, format := SniffArch(path)

	return Identity{
		SHA256:  sum,
		Size:    st.Size(),
		ModTime: st.ModTime().Unix(),
		Arch:    arch,
		Format:  format,
	}, nil
}

// SniffArch inspects the file header and returns ("arch", "format").
// Supported: PE images ("pe") and ELF objects ("elf"). Anything else
// returns ("", "").
//
// The magic-byte sniff works on every host OS — a Windows engine
// candidate can be validated from the Linux test suite.
func SniffArch(path string) (arch, format string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return "", ""
	}

	switch {
	case head[0] == 'M' && head[1] == 'Z':
		peArch, err := peArchOf(path)
		if err != nil {
			return "", "pe"
		}
		return peArch, "pe"
	case head[0] == 0x7f && head[1] == 'E' && head[2] == 'L' && head[3] == 'F':
		elfArch, err := elfArchOf(path)
		if err != nil {
			return "", "elf"
		}
		return elfArch, "elf"
	default:
		return "", ""
	}
}

func peArchOf(path string) (string, error) {
	f, err := pe.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	switch f.FileHeader.Machine {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "amd64", nil
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "arm64", nil
	case pe.IMAGE_FILE_MACHINE_I386:
		return "386", nil
	case pe.IMAGE_FILE_MACHINE_ARMNT:
		return "arm", nil
	default:
		return fmt.Sprintf("pe-0x%04x", f.FileHeader.Machine), nil
	}
}

func elfArchOf(path string) (string, error) {
	f, err := elf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	switch f.Machine {
	case elf.EM_X86_64:
		return "amd64", nil
	case elf.EM_386:
		return "386", nil
	case elf.EM_AARCH64:
		return "arm64", nil
	case elf.EM_ARM:
		return "arm", nil
	default:
		return fmt.Sprintf("elf-0x%04x", f.Machine), nil
	}
}

// ArchMatchesHost reports whether the candidate architecture is usable on
// the running host. Unknown architectures ("") are NOT a match — the
// caller decides whether that is fatal for its context.
func ArchMatchesHost(arch string) bool {
	if arch == "" {
		return false
	}

	return strings.EqualFold(arch, runtime.GOARCH)
}

// PEImports returns the DLL names imported by a Windows PE image, in
// import order, lower-cased and de-duplicated. Non-PE files return an
// error — use SniffArch first when the format is unknown.
func PEImports(path string) ([]string, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	libs, err := f.ImportedLibraries()
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []string

	for _, lib := range libs {
		name := strings.ToLower(strings.TrimSpace(lib))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}

	return out, nil
}

// systemDLL reports whether an imported DLL is a Windows system library
// the OS resolves itself (never expected beside the engine binary).
// The list is the well-known kernel/API-set subset, not an attempt to
// enumerate every Windows DLL (spec §4: never claim a specific DLL is
// defective without evidence).
func systemDLL(name string) bool {
	switch name {
	case
		"kernel32.dll", "user32.dll", "gdi32.dll", "shell32.dll",
		"advapi32.dll", "ole32.dll", "oleaut32.dll", "ws2_32.dll",
		"ntdll.dll", "msvcrt.dll", "ucrtbase.dll", "comdlg32.dll",
		"comctl32.dll", "shlwapi.dll", "secur32.dll", "crypt32.dll",
		"bcrypt.dll", "ncrypt.dll", "userenv.dll", "winmm.dll",
		"setupapi.dll", "version.dll", "wintrust.dll", "psapi.dll",
		"dwmapi.dll", "uxtheme.dll", "imm32.dll", "rpcrt4.dll",
		"msimg32.dll", "opengl32.dll", "glu32.dll",
		"win32u.dll", "kernelbase.dll":
		return true
	default:
		return strings.HasPrefix(name, "api-ms-win-") ||
			strings.HasPrefix(name, "ext-ms-")
	}
}

// DependencyReport walks the imported-DLL closure of a Windows PE binary
// and reports which non-system imports are MISSING beside the executable
// (the standard DLL search order: the executable's own directory first).
//
// On non-PE files the report is empty with format "" — nothing to check.
// The report is evidence only: it never names a DLL "defective", only
// present/missing (spec §4).
type DependencyReport struct {
	Format      string   `json:"format"`
	Arch        string   `json:"arch,omitempty"`
	Imports     []string `json:"imports,omitempty"`
	Missing     []string `json:"missing,omitempty"`
	Present     []string `json:"present,omitempty"`
	SystemOnly  bool     `json:"systemOnlyImports,omitempty"`
	InspectedAt int64    `json:"inspectedAtUnix"`
}

// CheckDependencies inspects the binary at path and returns the import
// evidence. For ELF/unknown formats the report carries the format only.
func CheckDependencies(path string) (DependencyReport, error) {
	arch, format := SniffArch(path)

	rep := DependencyReport{
		Format:      format,
		Arch:        arch,
		InspectedAt: timeNowUnix(),
	}

	if format != "pe" {
		return rep, nil
	}

	imports, err := PEImports(path)
	if err != nil {
		return rep, err
	}

	rep.Imports = imports

	dir := dirOf(path)
	system := 0

	for _, imp := range imports {
		if systemDLL(imp) {
			system++
			continue
		}

		if _, err := os.Stat(dir + string(os.PathSeparator) + imp); err == nil {
			rep.Present = append(rep.Present, imp)
			continue
		}

		rep.Missing = append(rep.Missing, imp)
	}

	rep.SystemOnly = system == len(imports)

	return rep, nil
}

// ErrNotExecutable is returned when a candidate lacks any executable
// permission bit on Unix.
var ErrNotExecutable = errors.New("candidate is not executable")

// ErrArchMismatch is returned when a candidate targets another machine.
var ErrArchMismatch = errors.New("candidate architecture does not match this machine")

// StaticValidate performs the OS-independent static gate every engine
// candidate must pass BEFORE it is ever executed (spec §12: never run an
// unknown binary merely because its filename matches):
//
//  1. the file exists, is a regular file and is non-empty;
//  2. on Unix it carries an executable bit;
//  3. its format is recognizable (PE or ELF);
//  4. its architecture matches the running host.
func StaticValidate(path string) (Identity, error) {
	ident, err := IdentityOf(path)
	if err != nil {
		return ident, err
	}

	if ident.Size == 0 {
		return ident, fmt.Errorf("%s is empty", path)
	}

	if runtime.GOOS != "windows" {
		st, statErr := os.Stat(path)
		if statErr == nil && st.Mode()&0o111 == 0 {
			return ident, ErrNotExecutable
		}
	}

	if ident.Format == "" {
		return ident, fmt.Errorf("%w: unrecognized executable format", ErrArchMismatch)
	}

	if !ArchMatchesHost(ident.Arch) {
		return ident, fmt.Errorf("%w: %s binary on %s host", ErrArchMismatch, ident.Arch, runtime.GOARCH)
	}

	return ident, nil
}
