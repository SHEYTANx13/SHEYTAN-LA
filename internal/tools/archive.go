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
	"encoding/json"
	"fmt"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/humanize"
	"io"
	"os"
	"path/filepath"
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

// safeJoin resolves entry names inside the extraction root, refusing
// absolute paths and .. traversal (zip-slip).
func safeJoin(root, name string) (string, error) {
	name = strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/")
	clean := filepath.Clean(name)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("unsafe entry %q (path traversal)", name)
	}
	return filepath.Join(root, clean), nil
}

func (t ArchiveTool) zip_(sources []string, dest string) (string, error) {
	if len(sources) == 0 {
		return "", fmt.Errorf("sources is required (list of files/dirs)")
	}
	if dest == "" {
		return "", fmt.Errorf("path (output archive) is required")
	}
	out := ResolvePath(dest)
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
		abs := ResolvePath(s)
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
	abs := ResolvePath(path)
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > archiveMaxN {
		return "", fmt.Errorf("too many entries (%d > %d)", len(zr.File), archiveMaxN)
	}
	root := ResolvePath(dest)
	if dest == "" {
		root = ResolvePath(strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs)))
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	var total uint64
	extracted := 0
	for _, zf := range zr.File {
		if zf.UncompressedSize64 > archiveMaxOut || total+zf.UncompressedSize64 > archiveMaxOut {
			return "", fmt.Errorf("archive too large (cap %d MB)", archiveMaxOut>>20)
		}
		target, err := safeJoin(root, zf.Name)
		if err != nil {
			return "", err
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		src, err := zf.Open()
		if err != nil {
			return "", err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			src.Close()
			return "", err
		}
		n, err := io.CopyBuffer(dst, src, make([]byte, archiveChunk))
		total += uint64(n)
		src.Close()
		dst.Close()
		if err != nil {
			return "", err
		}
		reportFileCreated(target)
		extracted++
	}
	return fmt.Sprintf("unzipped %d file(s) from %s -> %s", extracted, path, destOrAuto(dest, root)), nil
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
	out := ResolvePath(dest)
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
	abs := ResolvePath(path)
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
	root := ResolvePath(dest)
	if dest == "" {
		root = ResolvePath(strings.TrimSuffix(strings.TrimSuffix(filepath.Base(abs), ".gz"), ".tar"))
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	tr := tar.NewReader(r)
	extracted := 0
	var total uint64
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
		target, err := safeJoin(root, hdr.Name)
		if err != nil {
			return "", err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return "", err
			}
			n, err := io.CopyBuffer(dst, tr, make([]byte, archiveChunk))
			total += uint64(n)
			dst.Close()
			if err != nil {
				return "", err
			}
			reportFileCreated(target)
			extracted++
		}
	}
	return fmt.Sprintf("untarred %d file(s) from %s -> %s", extracted, path, destOrAuto(dest, root)), nil
}

func (t ArchiveTool) list(path string) (string, error) {
	abs := ResolvePath(path)
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
			fmt.Fprintf(&b, "  %-40s %10s\n", zf.Name, humanize.Bytes(int64(zf.UncompressedSize64)))
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
			fmt.Fprintf(&b, "  %-40s %10s\n", hdr.Name, humanize.Bytes(hdr.Size))
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
