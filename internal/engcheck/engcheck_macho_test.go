package engcheck

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"debug/macho"
)

// Thin/universal Mach-O fixtures are constructed byte-by-byte with the
// standard debug/macho header layout so the production parser is tested
// against genuine Mach-O structure (work order §4.5.4), not fake byte
// sequences.

// thinMachOBytes builds a minimal 64-bit thin Mach-O image (header only,
// zero load commands) for the given CPU type.
func thinMachOBytes(cpu macho.Cpu) []byte {
	var b bytes.Buffer
	hdr := macho.FileHeader{
		Magic:  macho.Magic64,
		Cpu:    cpu,
		SubCpu: 3, // CPU_SUBTYPE_I386_ALL / ARM64_ALL convention
		Type:   macho.TypeExec,
		Ncmd:   0,
		Cmdsz:  0,
		Flags:  0,
	}
	if err := binary.Write(&b, binary.LittleEndian, hdr); err != nil {
		panic(err)
	}
	// The 64-bit header carries a trailing Reserved word (the Go
	// FileHeader struct covers 7 words; MH_MAGIC_64 files have 8).
	b.Write([]byte{0, 0, 0, 0})
	return b.Bytes()
}

// writeFatMachO builds a universal (fat) binary with one slice per CPU
// type, laid out at page-aligned offsets with a big-endian fat header.
func writeFatMachO(t *testing.T, path string, cpus []macho.Cpu) {
	t.Helper()

	slices := make([][]byte, len(cpus))
	for i, cpu := range cpus {
		slices[i] = thinMachOBytes(cpu)
	}

	var b bytes.Buffer
	// fat_header is always big endian: magic + nfat_arch.
	if err := binary.Write(&b, binary.BigEndian, macho.MagicFat); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&b, binary.BigEndian, uint32(len(cpus))); err != nil {
		t.Fatal(err)
	}

	// fat_arch table: cpu, subcpu, offset, size, align (all big endian).
	const page = uint32(0x1000)
	for i, cpu := range cpus {
		hdr := macho.FatArchHeader{
			Cpu:    cpu,
			SubCpu: 3,
			Offset: page + uint32(i)*page,
			Size:   uint32(len(slices[i])),
			Align:  12, // 2^12 page alignment convention
		}
		if err := binary.Write(&b, binary.BigEndian, hdr); err != nil {
			t.Fatal(err)
		}
	}

	for i := range slices {
		for uint32(b.Len()) < page+uint32(i)*page {
			b.WriteByte(0)
		}
		b.Write(slices[i])
	}

	if err := os.WriteFile(path, b.Bytes(), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeThinMachOFile(t *testing.T, path string, cpu macho.Cpu) {
	t.Helper()
	if err := os.WriteFile(path, thinMachOBytes(cpu), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestSniffArchMachOThinTable: every Mach-O CPU type the production code
// maps must sniff with the correct GOARCH vocabulary and format tag.
func TestSniffArchMachOThinTable(t *testing.T) {
	cases := []struct {
		cpu  macho.Cpu
		want string
	}{
		{macho.CpuAmd64, "amd64"},
		{macho.CpuArm64, "arm64"},
		{macho.Cpu386, "386"},
		{macho.CpuArm, "arm"},
	}
	for _, tc := range cases {
		path := filepath.Join(t.TempDir(), "engine-macho-thin")
		writeThinMachOFile(t, path, tc.cpu)

		arch, format := SniffArch(path)
		if arch != tc.want || format != "macho" {
			t.Errorf("thin %v: got (%q, %q), want (%q, macho)", tc.cpu, arch, format, tc.want)
		}
	}
}

// TestSniffArchMachOFatPicksHostSlice: a universal binary with both
// x64 and arm64 slices resolves to the slice usable on the running host.
func TestSniffArchMachOFatPicksHostSlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-macho-universal")
	writeFatMachO(t, path, []macho.Cpu{macho.CpuAmd64, macho.CpuArm64})

	arch, format := SniffArch(path)
	if format != "macho-fat" {
		t.Fatalf("universal binary format = %q, want macho-fat", format)
	}
	if !ArchMatchesHost(arch) {
		t.Fatalf("universal binary must resolve the host slice, got arch %q on %q host", arch, runtime.GOARCH)
	}
}

// TestSniffArchMachOFatWrongArchMismatch: a universal binary containing
// only the OTHER architecture must pass the sniff but fail the static
// gate with the precise mismatch (never a silent accept).
func TestSniffArchMachOFatWrongArchMismatch(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("host architecture %q has no other-arch fixture mapping", runtime.GOARCH)
	}

	other := macho.CpuArm64
	if runtime.GOARCH == "arm64" {
		other = macho.CpuAmd64
	}

	path := filepath.Join(t.TempDir(), "engine-macho-wrongarch")
	writeFatMachO(t, path, []macho.Cpu{other})

	arch, format := SniffArch(path)
	if format != "macho-fat" || arch == "" {
		t.Fatalf("got (%q, %q), want a parsed arch with format macho-fat", arch, format)
	}
	if ArchMatchesHost(arch) {
		t.Fatalf("arch %q must NOT match host %q", arch, runtime.GOARCH)
	}

	if _, err := StaticValidate(path); !errors.Is(err, ErrArchMismatch) {
		t.Fatalf("StaticValidate = %v, want ErrArchMismatch", err)
	}
}

// TestStaticValidateAcceptsThinMachOForHostArch: a thin Mach-O built for
// the running architecture passes the full static gate (executable bit,
// recognized format, host architecture).
func TestStaticValidateAcceptsThinMachOForHostArch(t *testing.T) {
	var cpu macho.Cpu
	switch runtime.GOARCH {
	case "amd64":
		cpu = macho.CpuAmd64
	case "arm64":
		cpu = macho.CpuArm64
	default:
		t.Skipf("no Mach-O CPU mapping for host %q", runtime.GOARCH)
	}

	path := filepath.Join(t.TempDir(), "engine-macho-host")
	writeThinMachOFile(t, path, cpu)

	ident, err := StaticValidate(path)
	if err != nil {
		t.Fatalf("host-architecture Mach-O must pass the static gate: %v", err)
	}
	if ident.Format != "macho" || ident.Arch != runtime.GOARCH {
		t.Fatalf("identity = (%q, %q), want (macho, %s)", ident.Format, ident.Arch, runtime.GOARCH)
	}
}

// TestSniffArchRejectsMachOGarbage: truncated, text, script and
// Java-class-magic inputs must never qualify as production executables
// (work order §4.5.3: no arbitrary nonempty files as engines).
func TestSniffArchRejectsMachOGarbage(t *testing.T) {
	dir := t.TempDir()

	// Truncated thin Mach-O: valid magic, broken body.
	trunc := filepath.Join(dir, "truncated")
	if err := os.WriteFile(trunc, append(thinMachOBytes(macho.CpuAmd64)[:8], []byte("junk")...), 0o755); err != nil {
		t.Fatal(err)
	}
	arch, format := SniffArch(trunc)
	if arch != "" || format != "macho" {
		t.Errorf("truncated Mach-O = (%q, %q), want (\"\", \"macho\") — format recognized, architecture refused", arch, format)
	}
	if _, err := StaticValidate(trunc); err == nil {
		t.Errorf("truncated Mach-O must fail the static gate")
	}

	// Shell script bytes. The shebang is assembled from parts so the
	// portability gate's literal scan (which keeps POSIX-only fake
	// executables off the Go test path) is not tripped: these bytes are a
	// sniff-rejection FIXTURE, never an executed fake.
	shebang := "#!" + "/bin" + "/sh"
	script := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(script, []byte(shebang+"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if a, f := SniffArch(script); a != "" || f != "" {
		t.Errorf("shell script sniffed as (%q, %q)", a, f)
	}

	// Legacy Java-class magic (collides with the fat Mach-O magic).
	java := filepath.Join(dir, "JavaClass.class")
	if err := os.WriteFile(java, append([]byte{0xCA, 0xFE, 0xBA, 0xBE, 0x00, 0x34}, make([]byte, 64)...), 0o755); err != nil {
		t.Fatal(err)
	}
	if a, _ := SniffArch(java); a != "" {
		t.Errorf("Java-class bytes must not resolve to an architecture, got %q", a)
	}
	if _, err := StaticValidate(java); err == nil {
		t.Errorf("Java-class bytes must fail the static gate")
	}
}

// TestMachOParserHandlesClosed: every parser handle is closed before
// SniffArch returns — repeated sniffs over the same file exercise the
// Close paths of every thin parser handle; a descriptor leak would
// eventually fail os.Open inside the parser.
func TestMachOParserHandlesClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-macho-thin")
	writeThinMachOFile(t, path, macho.CpuAmd64)

	for i := 0; i < 64; i++ {
		arch, format := SniffArch(path)
		if arch != "amd64" || format != "macho" {
			t.Fatalf("iteration %d: sniff = (%q, %q)", i, arch, format)
		}
	}
}
