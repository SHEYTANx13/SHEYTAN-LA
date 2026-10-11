// gen-syso builds the Windows resource object (rsrc_windows_amd64.syso)
// that `go build` embeds into sheytan-local-agent.exe automatically.
//
// It carries three things the exe never had before:
//
//  1. THE APP ICON — the brand flame (pre-rendered 512px PNG embedded from
//     logo-512.png, generated once from brand.LogoSVG) packed into a
//     multi-size Windows icon (256/128/64/48/32/16). Explorer, the taskbar,
//     and window listings now show SHEYTAN with its own face instead of the
//     generic Go placeholder.
//
//  2. VERSION INFO — FileVersion / ProductName / Company / Copyright, all
//     derived from internal/brand + internal/config so they can never drift
//     from what the app itself reports.
//
//  3. A DPI-AWARE MANIFEST — PerMonitorV2 + system fallback + Common
//     Controls v6 + long-path awareness. Before the manifest the process was
//     DPI-unaware, so Windows reported 96 DPI no matter what the display
//     scaling really was — on a 125-150% laptop the WHOLE interface rendered
//     at 1x and every dialog, tab and control looked miniaturized. With the
//     manifest, the Wails/WebView2 shell sees the true scale and renders
//     crisp at native size.
//
// Run from the module root:  go run ./scripts/gen-syso
package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/brand"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// logoPNG carries the pre-rendered brand flame. The PNG is generated once
// from brand.LogoSVG (512×512, transparent background) and committed, so
// this tool needs no SVG rasterization dependency at build time.
//
//go:embed logo-512.png
var logoPNG []byte

func main() {
	// 1) Decode the brand flame (same mark the Node/Vite UI ships in
	//    web/static/icons — one brand, everywhere).
	img, err := renderLogo()
	if err != nil {
		fatal("render logo: %v", err)
	}

	// Preview artifact so the icon can be eyeballed without a Windows box.
	if err := os.MkdirAll("build", 0o755); err != nil {
		fatal("mkdir build: %v", err)
	}
	if f, err := os.Create("build/icon-preview.png"); err == nil {
		_ = png.Encode(f, img)
		_ = f.Close()
	}

	// 2) Pack the multi-size icon (winres resizes with a high-quality filter
	//    internally and writes valid ICO-format resource entries).
	// 1.1.6 §12: the full resolution ladder incl. 24px (16 / 24 / 32 /
	// 48 / 64 / 128 / 256) so every shell surface — exe, taskbar, title
	// bar, switcher, packaged shortcut metadata — gets a crisp glyph.
	icon, err := winres.NewIconFromResizedImage(img, []int{256, 128, 64, 48, 32, 24, 16})
	if err != nil {
		fatal("build icon: %v", err)
	}

	rs := &winres.ResourceSet{}
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		fatal("set icon: %v", err)
	}

	// 3) Version info — derived from the app's own constants.
	ver := versionNumbers(config.AppVersion)
	vi := version.Info{
		FileVersion:    ver,
		ProductVersion: ver,
	}
	str := func(key, val string) {
		if err := vi.Set(version.LangDefault, key, val); err != nil {
			fatal("version info %s: %v", key, err)
		}
	}
	// v1.2.0 — the SHEYTAN-LA product identity. Per the Windows
	// metadata contract:
	//   ProductName     SHEYTAN-LA
	//   FileDescription SHEYTAN Local Agent
	//   CompanyName     Parsaetak
	//   InternalName    SHEYTAN-LA
	//   OriginalFilename SHEYTAN-LA.exe
	// NO Authenticode signature is claimed anywhere — signing happens
	// only through the real certificate pipeline.
	str(version.FileDescription, config.AppDescription)
	str(version.ProductName, config.AppShortName)
	// The CompanyName field carries the legal publisher (Parsaetak);
	// the author signature line stays in Comments so "right-click →
	// Properties → Details" still shows the signer attribution.
	str(version.CompanyName, config.AppPublisher)
	str(version.LegalCopyright, brand.Copyright()+" "+brand.TrademarkNotice+" Signed by "+brand.SignedBy+".")
	str(version.LegalTrademarks, brand.TrademarkNotice)
	str(version.Comments, brand.SignatureLine())
	str(version.OriginalFilename, config.ExecutableName)
	str(version.InternalName, config.AppShortName)
	str(version.FileVersion, versionString(config.AppVersion))
	str(version.ProductVersion, config.AppVersion)
	rs.SetVersionInfo(vi)

	// 4) Manifest: DPI awareness + common controls + long paths.
	rs.SetManifest(winres.AppManifest{
		Description:         config.AppName,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		LongPathAware:       true,
		ExecutionLevel:      winres.AsInvoker,
		Compatibility:       winres.Win10AndAbove,
	})

	// 5) Emit the .syso next to main.go — the Go toolchain links it into
	//    windows/amd64 builds automatically (other platforms ignore it).
	out, err := os.Create("rsrc_windows_amd64.syso")
	if err != nil {
		fatal("create syso: %v", err)
	}
	defer out.Close()
	if err := rs.WriteObject(out, winres.ArchAMD64); err != nil {
		fatal("write syso: %v", err)
	}

	// 6) 1.1.6 §12: also emit a standalone multi-resolution .ico for
	// packaged shortcut/application metadata (Wails NSIS/installer,
	// Windows Explorer property sheets). Same brand mark, same ladder.
	if err := writeICO("build/sheytan.ico", img); err != nil {
		fatal("write ico: %v", err)
	}

	fmt.Printf("rsrc_windows_amd64.syso + build/sheytan.ico written — icon + version %s + DPI-aware manifest embedded\n",
		config.AppVersion)
}

// writeICO packs img into a classic multi-image .ico file: BMP entries
// for the small sizes (maximum shell compatibility) and a PNG entry for
// 256px (Vista+ standard). All seven resolutions of the §12 ladder.
func writeICO(path string, src image.Image) error {
	sizes := []int{16, 24, 32, 48, 64, 128, 256}

	type entry struct {
		width  int
		height int
		data   []byte
		isPNG  bool
	}

	entries := make([]entry, 0, len(sizes))
	for _, s := range sizes {
		if s == 256 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, src); err != nil {
				return err
			}
			entries = append(entries, entry{256, 256, buf.Bytes(), true})
			continue
		}
		bmp, err := bmpFromImage(src, s)
		if err != nil {
			return err
		}
		entries = append(entries, entry{s, s, bmp, false})
	}

	// ICONDIR + ICONDIRENTRY table + image blobs.
	total := 6 + 16*len(entries)
	out := bytes.NewBuffer(make([]byte, 0, total))
	out.WriteByte(0) // reserved
	out.WriteByte(0)
	out.WriteByte(1) // type: icon
	out.WriteByte(0)
	out.WriteByte(byte(len(entries)))
	out.WriteByte(0)

	offset := total
	for _, e := range entries {
		w := byte(e.width)
		if e.width == 256 {
			w = 0
		}
		out.WriteByte(w)
		out.WriteByte(byte(e.height))
		out.WriteByte(0) // palette
		out.WriteByte(0) // reserved
		out.WriteByte(1) // planes
		out.WriteByte(0)
		out.WriteByte(32) // bpp
		out.WriteByte(0)

		size := uint32(len(e.data))
		out.WriteByte(byte(size))
		out.WriteByte(byte(size >> 8))
		out.WriteByte(byte(size >> 16))
		out.WriteByte(byte(size >> 24))

		off := uint32(offset)
		out.WriteByte(byte(off))
		out.WriteByte(byte(off >> 8))
		out.WriteByte(byte(off >> 16))
		out.WriteByte(byte(off >> 24))

		offset += len(e.data)
	}

	for _, e := range entries {
		out.Write(e.data)
	}

	return os.WriteFile(path, out.Bytes(), 0o644)
}

// bmpFromImage renders src at size×size and encodes it as a Windows
// BITMAPINFOHEADER bitmap with 32-bit BGRA pixels + AND mask (the ICO
// BMP layout: double height, top-down pixels, bottom-up mask).
func bmpFromImage(src image.Image, size int) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dst.Set(x, y, src.At(x*src.Bounds().Dx()/size, y*src.Bounds().Dy()/size))
		}
	}

	header := make([]byte, 40)
	put := func(i int, v uint32) {
		header[i] = byte(v)
		header[i+1] = byte(v >> 8)
		header[i+2] = byte(v >> 16)
		header[i+3] = byte(v >> 24)
	}
	put(0, 40)
	put(4, uint32(size))
	put(8, uint32(size*2)) // double height (XOR + AND)
	put(12, 1)
	put(14, 32) // bpp
	// compression 0, sizeImage may be 0 for BI_RGB.

	pixels := make([]byte, 0, size*size*4)
	for y := size - 1; y >= 0; y-- { // bottom-up rows
		for x := 0; x < size; x++ {
			r, g, b, a := dst.At(x, y).RGBA()
			pixels = append(pixels, byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8))
		}
	}

	// AND mask: 1bpp, bottom-up, rows padded to 32 bits. Fully opaque
	// images use all-zero masks; alpha is honored from the XOR plane.
	rowBytes := ((size + 31) / 32) * 4
	mask := make([]byte, rowBytes*size)

	out := make([]byte, 0, 40+len(pixels)+len(mask))
	out = append(out, header...)
	out = append(out, pixels...)
	out = append(out, mask...)
	return out, nil
}

// renderLogo decodes the embedded, pre-rendered brand flame.
func renderLogo() (image.Image, error) {
	return png.Decode(bytes.NewReader(logoPNG))
}

// versionNumbers turns "1.0.6" into [4]uint16{1, 0, 6, 0}.
func versionNumbers(v string) [4]uint16 {
	var out [4]uint16
	for i, part := range strings.SplitN(strings.TrimSpace(v), ".", 4) {
		if i >= 4 {
			break
		}
		if n, err := strconv.ParseUint(part, 10, 16); err == nil {
			out[i] = uint16(n)
		}
	}
	return out
}

// versionString renders "1.0.6" as the canonical "1.0.6.0".
func versionString(v string) string {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 4)
	for len(parts) < 4 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".")
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "gen-syso: "+format+"\n", args...)
	os.Exit(1)
}
