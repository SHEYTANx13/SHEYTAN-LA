// Package tools — archive: zip/tar/gzip tool with chunk-streamed copies
// and zip-slip protection.
//
// Agents constantly need to bundle artifacts for the user or open data
// drops. The archive tool gives them:
//
//   - zip:   create a .zip from a list of files/dirs (recursive, 1 MB
//     chunked copies — never a whole file in memory)
//   - unzip: extract (path-traversal guard: ../ and absolute entries are
//     rejected; total-size cap; entry count cap)
//   - tar / untar (+ gzip via .gz / .tgz suffixes)
//   - list:  inventory an archive without extracting
package tools

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/humanize"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/updater"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ArchiveTool bundles and extracts archives.
type ArchiveTool struct{}

// Name implements the agent tool interface.
func (ArchiveTool) Name() string { return "archive" }

// Description implements the agent tool interface.
func (ArchiveTool) Description() string {
	return "Create and extract archives. " +
		`action=zip&sources=[...]&path="out.zip" bundles files/dirs (recursive); ` +
		`action=unzip&path="in.zip"&dest="out/" extracts (zip-slip safe); ` +
		"action=tar/untar handle .tar/.tar.gz/.tgz the same way; action=list inventories an archive. " +
		"Chunk-streamed — safe for large files. Pair with files (tree/list) to pick sources and verify results."
}

// Parameters implements the agent tool interface.
func (ArchiveTool) Parameters() any {
	return struct {
		Action  string   `json:"action"`
		Path    string   `json:"path,omitempty"`
		Dest    string   `json:"dest,omitempty"`
		Sources []string `json:"sources,omitempty"`
	}{}
}

const (
	archiveChunk  = 1 << 20 // 1 MB streaming buffer
	archiveMaxOut = 1 << 30 // 1 GB total extracted cap
	archiveMaxN   = 20000   // entry count cap
	// v1.11.1 (run 41933333132, work order §8.2.6/§8.2.7): the ZIP HEADER
	// UncompressedSize64 is attacker-controlled metadata — the streamed
	// copy is bounded BELOW it too, and the total decompressed size is
	// also bounded against a compression-ratio bomb.
	archiveEntryCap = 256 << 20 // single-member streamed cap
	archiveMaxRatio = 200       // max decompressed:archive-size ratio
	archiveRatioMin = 1 << 20   // ratio guard only above a 1 MiB floor
)

// Run implements the agent tool interface.
func (t ArchiveTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Action  string   `json:"action"`
		Path    string   `json:"path"`
		Dest    string   `json:"dest"`
		Sources []string `json:"sources"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("bad args: %w", err)
	}
	switch strings.ToLower(p.Action) {
	case "zip":
		return t.zip_(p.Sources, p.Path)
	case "unzip":
		return t.unzip(p.Path, p.Dest)
	case "tar":
		return t.tar_(p.Sources, p.Path)
	case "untar":
		return t.untar(p.Path, p.Dest)
	case "list":
		return t.list(p.Path)
	default:
		return "", fmt.Errorf("unknown action %q (zip|unzip|tar|untar|list)", p.Action)
	}
}

func (t ArchiveTool) zip_(sources []string, dest string) (string, error) {
	if len(sources) == 0 {
		return "", fmt.Errorf("sources is required (list of files/dirs)")
	}
	if dest == "" {
		return "", fmt.Errorf("path (output archive) is required")
	}
	// v1.11.1 path-injection remediation (work order §7 — class (a)): the
	// output archive resolves through the fail-closed chokepoint; a
	// hostile dest fails here, before any MkdirAll/create.
	out, err := toolPath(dest)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	files, skipped := collectSources(sources)
	added := 0
	var total int64
	for _, src := range files {
		rel := relToBase(src)
		if err := addToZip(zw, src, rel, &total); err != nil {
			return "", err
		}
		added++
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("zipped %d file(s) -> %s (%s)", added, dest, humanize.Bytes(total))
	if skipped > 0 {
		msg += fmt.Sprintf(" (%d source path(s) not found, skipped)", skipped)
	}
	reportFileCreated(out)
	return msg, nil
}

func addToZip(zw *zip.Writer, src, rel string, total *int64) error {
	// v1.11.1 path-injection remediation (work order §7): the member
	// source re-resolves through the fail-closed chokepoint before the
	// stat/open pair — an unchecked caller can never make the archiver
	// read outside the base.
	src, err := toolPath(src)
	if err != nil {
		return err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(fi)
	if err != nil {
		return err
	}
	hdr.Name = rel
	hdr.Method = zip.Deflate
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.CopyBuffer(w, f, make([]byte, archiveChunk))
	*total += n
	return err
}

func collectSources(sources []string) (files []string, skipped int) {
	for _, s := range sources {
		// v1.11.1 path-injection remediation (work order §7): each
		// source resolves through the fail-closed chokepoint; a
		// hostile source is SKIPPED (and counted), exactly like a
		// missing one — it can never be opened outside the base.
		abs, err := toolPath(s)
		if err != nil {
			skipped++
			continue
		}
		fi, err := os.Stat(abs)
		if err != nil {
			skipped++
			continue
		}
		if !fi.IsDir() {
			files = append(files, abs)
			continue
		}
		_ = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				files = append(files, path)
			}
			return nil
		})
	}
	return files, skipped
}

func relToBase(abs string) string {
	base := BaseDir()
	if base != "" && strings.HasPrefix(abs, base) {
		rel, err := filepath.Rel(base, abs)
		if err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Base(abs))
}

func (t ArchiveTool) unzip(path, dest string) (string, error) {
	// v1.11.1 path-injection remediation (work order §7 — class (a)):
	// the archive AND its extraction root resolve through the fail-closed
	// chokepoint, so the rooted extraction below can never even SIT
	// outside the base.
	abs, err := toolPath(path)
	if err != nil {
		return "", err
	}
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > archiveMaxN {
		return "", fmt.Errorf("too many entries (%d > %d)", len(zr.File), archiveMaxN)
	}
	root, err := toolPath(dest)
	if err != nil {
		return "", err
	}
	if dest == "" {
		root, err = toolPath(strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs)))
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	// v1.11.1 (run 41901967798): ROOTED extraction. Every write goes
	// through an os.Root handle bound to the destination root, so the OS
	// resolves the final path inside the root — a hostile member can
	// neither escape lexically nor redirect a later write through a
	// symlink planted in the extraction tree. Symlink members are
	// rejected outright, and member names are validated by the ONE
	// authoritative lexical validator shared with the updater
	// (updater.SafeArchiveRel): absolute/rooted/drive/UNC members,
	// traversal and NUL-bearing names all fail closed.
	//
	// v1.11.1 (run 41933333132, work order §8.2.4/§8.2.6/§8.2.7): the
	// extraction is also an exhaustive LEDGER — duplicate members,
	// case-fold collisions (the Windows loader's and filesystem's
	// matching rule) and file-vs-dir conflicts fail closed BEFORE any
	// overwrite; every member copy is bounded by a streamed byte cap
	// (the header's UncompressedSize64 is attacker metadata, never
	// trusted alone); and the total decompressed size is additionally
	// bounded against a compression-ratio bomb.
	rooted, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer rooted.Close()

	archiveSize := archiveRatioFloor()
	if fi, statErr := os.Stat(abs); statErr == nil && fi.Size() > 0 {
		archiveSize = fi.Size()
	}

	ledger := newExtractionLedger()

	// v1.11.1 (run 42127523923, work order §6.5/§6.6): STAGED
	// transactional extraction. Every member is extracted into a
	// unique staging directory INSIDE the root (exclusive creates —
	// an in-flight extraction can never truncate a pre-existing
	// file), and only after ALL members have been copied and
	// verified are they moved to their final locations. On any
	// failure the stage is removed (Root.RemoveAll) and the
	// destination's prior content is untouched.
	stageRel, err := freshStageDir(rooted)
	if err != nil {
		return "", err
	}

	committed := false

	defer func() {
		if !committed {
			_ = rooted.RemoveAll(stageRel) // rollback: remove ONLY the stage
		}
	}()

	stagePath := func(rel string) string {
		return filepath.Join(stageRel, rel)
	}

	var total uint64

	var stagedFiles []string

	var stagedDirs []string

	extracted := 0
	for _, zf := range zr.File {
		if zf.UncompressedSize64 > archiveMaxOut || total+zf.UncompressedSize64 > archiveMaxOut {
			return "", fmt.Errorf("archive too large (cap %d MB)", archiveMaxOut>>20)
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("unsafe entry %q (symlink members are not allowed)", zf.Name)
		}
		rel, err := updater.SafeArchiveRel(zf.Name)
		if err != nil {
			return "", err
		}
		if err := ledger.claim(rel, zf.FileInfo().IsDir()); err != nil {
			return "", err
		}
		if zf.FileInfo().IsDir() {
			if err := rooted.MkdirAll(stagePath(rel), 0o755); err != nil {
				return "", err
			}
			stagedDirs = append(stagedDirs, rel)
			continue
		}
		if dirRel := filepath.Dir(rel); dirRel != "." {
			if err := rooted.MkdirAll(stagePath(dirRel), 0o755); err != nil {
				return "", err
			}
		}
		src, err := zf.Open()
		if err != nil {
			return "", err
		}
		// Exclusive create inside the stage: a member can never
		// collide with, or truncate, pre-existing content.
		dst, err := rooted.OpenFile(stagePath(rel), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
		if err != nil {
			src.Close()
			return "", err
		}
		// Streamed cap: never trust the header's size alone.
		n, err := io.CopyBuffer(dst, io.LimitReader(src, archiveEntryCap+1), make([]byte, archiveChunk))
		total += uint64(n)
		src.Close()
		dst.Close()
		if err != nil {
			return "", err
		}
		if n > archiveEntryCap {
			return "", fmt.Errorf("zip member %q exceeds the streamed %d MB entry cap", zf.Name, archiveEntryCap>>20)
		}
		if total > archiveMaxOut {
			return "", fmt.Errorf("archive too large (cap %d MB)", archiveMaxOut>>20)
		}
		if total > archiveRatioMin && total > uint64(archiveSize)*archiveMaxRatio {
			return "", fmt.Errorf("archive decompression ratio exceeds %dx (%d bytes from a %d byte archive) — refusing suspected decompression bomb", archiveMaxRatio, total, archiveSize)
		}
		stagedFiles = append(stagedFiles, rel)
		extracted++
	}

	// Commit (§6.6): extraction + verification complete — move the
	// staged tree into its final locations. Directory members first,
	// then files (an intra-root rename, atomic per member).
	for _, dirRel := range stagedDirs {
		if err := rooted.MkdirAll(dirRel, 0o755); err != nil {
			return "", err
		}
	}

	for _, rel := range stagedFiles {
		if dirRel := filepath.Dir(rel); dirRel != "." {
			if err := rooted.MkdirAll(dirRel, 0o755); err != nil {
				return "", err
			}
		}

		if err := rooted.Rename(stagePath(rel), rel); err != nil {
			return "", err
		}

		reportFileCreated(filepath.Join(root, rel))
	}

	committed = true
	_ = rooted.RemoveAll(stageRel) // drop the now-empty stage tree

	return fmt.Sprintf("unzipped %d file(s) from %s -> %s", extracted, path, destOrAuto(dest, root)), nil
}

// archiveRatioFloor keeps the ratio guard from dividing by a zero/absent
// size when the archive cannot be stat'ed.
func archiveRatioFloor() int64 { return 1 }

// freshStageDir creates a UNIQUE staging directory directly inside the
// extraction root (work order §6.5, run 42127523923): an exclusive Mkdir
// with a random token, so two concurrent extractions into the same root
// never share a stage, and every descendant operation stays under the
// same rooted handle as the final writes. The stage lives INSIDE the
// root (never beside it) so the os.Root boundary covers the whole
// transaction.
func freshStageDir(rooted *os.Root) (string, error) {
	var token [8]byte

	for attempt := 0; attempt < 5; attempt++ {
		if _, err := cryptorand.Read(token[:]); err != nil {
			return "", fmt.Errorf("stage token: %w", err)
		}

		rel := ".archive-stage-" + hex.EncodeToString(token[:])

		err := rooted.Mkdir(rel, 0o700)
		if err == nil {
			return rel, nil
		}

		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}

	return "", errors.New("could not allocate a unique extraction staging directory")
}

// displayArchiveMember renders an archive member name for the LISTING
// output (and every other place a member name is displayed).
//
// v1.11.1 (run 42502043636 work order §4.3, zipslip alerts #175/#176 at
// the list sinks): the rendered value is PROVABLY NOT a live filesystem
// path. The raw member name is first put through the ONE shared lexical
// archive-name validator (updater.SafeArchiveRel — the same rule set the
// extractors use): absolute/Windows-rooted/UNC/drive forms, any ".."
// traversal, ADS-colon spellings and NUL-bearing names FAIL validation
// and are rendered as a Go-quoted escape of the raw bytes instead of the
// name itself. Control characters and overlong names are escaped or
// truncated as well, so no member name can smuggle terminal/log content
// or an unbounded line into the tool result. Display-only: the listing
// never touches the filesystem with the member name.
func displayArchiveMember(name string) string {
	const maxMemberDisplay = 200
	render := func(s string) string {
		if len(s) > maxMemberDisplay {
			s = s[:maxMemberDisplay] + "…(truncated)"
		}
		return s
	}
	_, err := updater.SafeArchiveRel(name)
	if err != nil {
		return render(strconv.Quote(name))
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return render(strconv.Quote(name))
		}
	}
	return render(name)
}

// extractionLedger is the §8.2.4 overwrite/conflict authority for one
// extraction: no duplicate members, no case-fold collisions, and no
// file-vs-dir boundary conflicts — the first spelling of a path wins
// its KIND, and any contradicting member fails the whole extraction.
type extractionLedger struct {
	files map[string]string // lowercased rel -> first original spelling
	dirs  map[string]bool   // lowercased directory rel paths
}

func newExtractionLedger() *extractionLedger {
	return &extractionLedger{files: map[string]string{}, dirs: map[string]bool{}}
}

func (l *extractionLedger) claim(rel string, isDir bool) error {
	key := strings.ToLower(filepath.ToSlash(rel))

	// Every ANCESTOR directory of this member must not be an existing
	// file ("a" extracted as a file cannot later host "a/b").
	dir := filepath.ToSlash(filepath.Dir(rel))
	for dir != "." && dir != "/" && dir != "" {
		if l.files[dir] != "" {
			return fmt.Errorf("archive entry %q conflicts with the file %q (a file cannot become a directory)", rel, l.files[dir])
		}
		parent := filepath.ToSlash(filepath.Dir(dir))
		if parent == dir {
			break
		}
		dir = parent
	}

	if isDir {
		if _, exists := l.files[key]; exists {
			return fmt.Errorf("archive entry %q conflicts with the file %q (a directory cannot replace a file)", rel, l.files[key])
		}
		l.dirs[key] = true
		return nil
	}

	if _, exists := l.files[key]; exists {
		return fmt.Errorf("archive contains duplicate or case-colliding entries for %q (first seen as %q)", rel, l.files[key])
	}
	if l.dirs[key] {
		return fmt.Errorf("archive entry %q conflicts with an existing directory of the same path", rel)
	}
	l.files[key] = rel
	return nil
}

func destOrAuto(dest, root string) string {
	if dest != "" {
		return dest
	}
	return filepath.Base(root) + "/ (auto)"
}

func (t ArchiveTool) tar_(sources []string, dest string) (string, error) {
	if len(sources) == 0 {
		return "", fmt.Errorf("sources is required")
	}
	if dest == "" {
		return "", fmt.Errorf("path (output archive) is required")
	}
	// v1.11.1 path-injection remediation (work order §7 — class (a)): the
	// output archive resolves through the fail-closed chokepoint; a
	// hostile dest fails here, before any MkdirAll/create.
	out, err := toolPath(dest)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var w io.WriteCloser = f
	gz := strings.HasSuffix(strings.ToLower(dest), ".gz") || strings.HasSuffix(strings.ToLower(dest), ".tgz")
	if gz {
		w = gzip.NewWriter(f)
	}
	tw := tar.NewWriter(w)

	files, skipped := collectSources(sources)
	added := 0
	var total int64
	for _, src := range files {
		// v1.11.1 (work order §7): re-gate every member source before
		// the stat/open pair (collectSources already returned
		// builder-derived paths — this is the sink-adjacent guard).
		if _, err := toolPath(src); err != nil {
			return "", err
		}
		fi, err := os.Stat(src)
		if err != nil {
			continue
		}
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return "", err
		}
		hdr.Name = relToBase(src)
		if err := tw.WriteHeader(hdr); err != nil {
			return "", err
		}
		sf, err := os.Open(src)
		if err != nil {
			return "", err
		}
		n, err := io.CopyBuffer(tw, sf, make([]byte, archiveChunk))
		sf.Close()
		total += n
		if err != nil {
			return "", err
		}
		added++
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if gz {
		if err := w.Close(); err != nil {
			return "", err
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("tarred %d file(s) -> %s (%s)", added, dest, humanize.Bytes(total))
	if skipped > 0 {
		msg += fmt.Sprintf(" (%d source path(s) skipped)", skipped)
	}
	reportFileCreated(out)
	return msg, nil
}

func (t ArchiveTool) untar(path, dest string) (string, error) {
	// v1.11.1 path-injection remediation (work order §7 — class (a)):
	// the archive AND its extraction root resolve through the fail-closed
	// chokepoint (same shape as unzip).
	abs, err := toolPath(path)
	if err != nil {
		return "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("open tar: %w", err)
	}
	defer f.Close()

	var r io.Reader = f
	if strings.HasSuffix(strings.ToLower(path), ".gz") || strings.HasSuffix(strings.ToLower(path), ".tgz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer gz.Close()
		r = gz
	}
	root, err := toolPath(dest)
	if err != nil {
		return "", err
	}
	if dest == "" {
		root, err = toolPath(strings.TrimSuffix(strings.TrimSuffix(filepath.Base(abs), ".gz"), ".tar"))
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	rooted, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer rooted.Close()

	archiveSize := archiveRatioFloor()
	if fi, statErr := os.Stat(abs); statErr == nil && fi.Size() > 0 {
		archiveSize = fi.Size()
	}

	ledger := newExtractionLedger()

	// v1.11.1 (run 42127523923, work order §6.5/§6.6): the same STAGED
	// transactional extraction as unzip — unique stage inside the
	// root, exclusive creates, commit only after the whole archive
	// verified, rollback removes ONLY the stage.
	stageRel, err := freshStageDir(rooted)
	if err != nil {
		return "", err
	}

	committed := false

	defer func() {
		if !committed {
			_ = rooted.RemoveAll(stageRel) // rollback: remove ONLY the stage
		}
	}()

	stagePath := func(rel string) string {
		return filepath.Join(stageRel, rel)
	}

	tr := tar.NewReader(r)
	extracted := 0
	var total uint64

	var stagedFiles []string

	var stagedDirs []string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if extracted > archiveMaxN || total > archiveMaxOut {
			return "", fmt.Errorf("archive too large (entry/size cap)")
		}
		// v1.11.1 (run 41901967798): rooted extraction + link-member
		// rejection + the shared lexical validator (updater.SafeArchiveRel).
		// v1.11.1 (run 41933333132, §8.2.4/§8.2.7): the same extraction
		// ledger and streamed caps as unzip — duplicate/case-colliding/
		// conflicting members and header-lying sizes fail closed.
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return "", fmt.Errorf("unsafe entry %q (link members are not allowed)", hdr.Name)
		}

		rel, err := updater.SafeArchiveRel(hdr.Name)
		if err != nil {
			return "", err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := ledger.claim(rel, true); err != nil {
				return "", err
			}
			if err := rooted.MkdirAll(stagePath(rel), 0o755); err != nil {
				return "", err
			}
			stagedDirs = append(stagedDirs, rel)
		case tar.TypeReg:
			if err := ledger.claim(rel, false); err != nil {
				return "", err
			}
			if dirRel := filepath.Dir(rel); dirRel != "." {
				if err := rooted.MkdirAll(stagePath(dirRel), 0o755); err != nil {
					return "", err
				}
			}
			// Exclusive create inside the stage (§6.5).
			dst, err := rooted.OpenFile(stagePath(rel), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
			if err != nil {
				return "", err
			}
			// Streamed cap: the tar header's Size is metadata too.
			n, err := io.CopyBuffer(dst, io.LimitReader(tr, archiveEntryCap+1), make([]byte, archiveChunk))
			total += uint64(n)
			dst.Close()
			if err != nil {
				return "", err
			}
			if n > archiveEntryCap {
				return "", fmt.Errorf("tar member %q exceeds the streamed %d MB entry cap", hdr.Name, archiveEntryCap>>20)
			}
			if total > archiveMaxOut {
				return "", fmt.Errorf("archive too large (entry/size cap)")
			}
			if total > archiveRatioMin && total > uint64(archiveSize)*archiveMaxRatio {
				return "", fmt.Errorf("archive decompression ratio exceeds %dx (%d bytes from a %d byte archive) — refusing suspected decompression bomb", archiveMaxRatio, total, archiveSize)
			}
			stagedFiles = append(stagedFiles, rel)
			extracted++
		}
	}

	// Commit (§6.6): the staged tree moves into its final locations —
	// directory members first, then files, all intra-root.
	for _, dirRel := range stagedDirs {
		if err := rooted.MkdirAll(dirRel, 0o755); err != nil {
			return "", err
		}
	}

	for _, rel := range stagedFiles {
		if dirRel := filepath.Dir(rel); dirRel != "." {
			if err := rooted.MkdirAll(dirRel, 0o755); err != nil {
				return "", err
			}
		}

		if err := rooted.Rename(stagePath(rel), rel); err != nil {
			return "", err
		}

		reportFileCreated(filepath.Join(root, rel))
	}

	committed = true
	_ = rooted.RemoveAll(stageRel) // drop the now-empty stage tree

	return fmt.Sprintf("untarred %d file(s) from %s -> %s", extracted, path, destOrAuto(dest, root)), nil
}

func (t ArchiveTool) list(path string) (string, error) {
	// v1.11.1 path-injection remediation (work order §7 — class (a)): the
	// archive resolves through the fail-closed chokepoint — list opens
	// archives with zip.OpenReader/os.Open, so a hostile path must fail
	// before either sink.
	abs, err := toolPath(path)
	if err != nil {
		return "", err
	}
	lower := strings.ToLower(path)
	var b strings.Builder
	if strings.HasSuffix(lower, ".zip") {
		zr, err := zip.OpenReader(abs)
		if err != nil {
			return "", err
		}
		defer zr.Close()
		fmt.Fprintf(&b, "%d entr(ies) in %s:\n", len(zr.File), path)
		var total uint64
		for i, zf := range zr.File {
			if i >= 50 {
				fmt.Fprintf(&b, "… %d more\n", len(zr.File)-50)
				break
			}
			total += zf.UncompressedSize64
			fmt.Fprintf(&b, "  %-40s %10s\n", displayArchiveMember(zf.Name), humanize.Bytes(int64(zf.UncompressedSize64)))
		}
		fmt.Fprintf(&b, "total uncompressed: %s\n", humanize.Bytes(int64(total)))
		return b.String(), nil
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".tgz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		defer gz.Close()
		r = gz
	}
	tr := tar.NewReader(r)
	n := 0
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		n++
		total += hdr.Size
		if n <= 50 {
			fmt.Fprintf(&b, "  %-40s %10s\n", displayArchiveMember(hdr.Name), humanize.Bytes(hdr.Size))
		}
	}
	if n > 50 {
		fmt.Fprintf(&b, "… %d more\n", n-50)
	}
	return fmt.Sprintf("%d entr(ies), total %s:\n%s", n, humanize.Bytes(total), b.String()), nil
}

// reportFileCreated routes through the shared artifact hook (nil in CLI
// mode; the GUI tracks created files for the Files view).
func reportFileCreated(path string) {
	if OnFileCreated != nil {
		OnFileCreated(path)
	}
}
