// Package tools — fire-themed SVG chart renderer for the dataAnalysis tool.
//
// Renders publication-quality dark charts (bar / line / scatter / pie) with
// the SHEYTAN ember palette. Output goes to <app folder>/charts/<name>.svg.
package tools

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Fire palette — dark background, ember-to-flame gradient family.
var firePalette = []string{
	"#FF3B30", // ember red
	"#FF6B1A", // flame orange
	"#FFA02E", // amber
	"#FFC53D", // gold
	"#E5484D", // deep red
	"#FF8355", // light coral
	"#D9381E", // blood orange
	"#FFB56B", // peach
	"#C9184A", // crimson
	"#F77F00", // burnt orange
}

// actionChart renders a chart from a dataset.
func (t *DataTool) actionChart(p *dataParams) (string, error) {
	if p.Path == "" {
		return "", fmt.Errorf("path is required (the dataset to chart)")
	}
	d, err := t.load(p.Path)
	if err != nil {
		return "", err
	}
	kind := strings.ToLower(p.Chart)
	if kind == "" {
		kind = "bar"
	}

	var svg string
	var summary string
	switch kind {
	case "bar":
		svg, summary, err = t.renderBarLine(d, p, false)
	case "line":
		svg, summary, err = t.renderBarLine(d, p, true)
	case "scatter":
		svg, summary, err = t.renderScatter(d, p)
	case "pie":
		svg, summary, err = t.renderPie(d, p)
	default:
		return "", fmt.Errorf("unknown chart type %q (bar|line|scatter|pie)", p.Chart)
	}
	if err != nil {
		return "", err
	}

	name := p.Name
	if name == "" {
		name = fmt.Sprintf("%s-%s-%d", strings.TrimSuffix(filepath.Base(d.Path), filepath.Ext(filepath.Base(d.Path))), kind, time.Now().Unix())
	}
	name = sanitizeName(name)
	if !strings.HasSuffix(name, ".svg") {
		name += ".svg"
	}
	if err := os.MkdirAll(t.cfg.ChartsDir(), 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(t.cfg.ChartsDir(), name)
	if err := os.WriteFile(out, []byte(svg), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("Chart rendered → %s\n%s\n(Open the Data view in the GUI to preview, or any browser to view the SVG.)", out, summary), nil
}

// chartData extracts label/value pairs for bar/line/pie charts.
func (t *DataTool) chartData(d *dataset, p *dataParams) ([]string, []float64, error) {
	if p.ValueCol == "" {
		return nil, nil, fmt.Errorf("'valueCol' is required (the numeric column to plot)")
	}
	vi, err := d.colIndex(p.ValueCol)
	if err != nil {
		return nil, nil, err
	}
	if d.Types[vi] != typeNumber {
		return nil, nil, fmt.Errorf("valueCol %q is not numeric", p.ValueCol)
	}
	li := -1
	if p.LabelCol != "" {
		li, err = d.colIndex(p.LabelCol)
		if err != nil {
			return nil, nil, err
		}
	}
	var labels []string
	var values []float64
	for r := range d.Rows {
		v := parseNumber(d.Rows[r][vi])
		if math.IsNaN(v) {
			continue
		}
		if li >= 0 {
			labels = append(labels, clipStr(strings.TrimSpace(d.Rows[r][li]), 16))
		} else {
			labels = append(labels, strconv.Itoa(r+1))
		}
		values = append(values, v)
	}
	if len(values) == 0 {
		return nil, nil, fmt.Errorf("no numeric values found in %q", p.ValueCol)
	}
	if len(values) > 60 { // keep charts readable
		labels = labels[:60]
		values = values[:60]
	}
	return labels, values, nil
}

// renderBarLine renders a bar or line chart.
func (t *DataTool) renderBarLine(d *dataset, p *dataParams, line bool) (string, string, error) {
	labels, values, err := t.chartData(d, p)
	if err != nil {
		return "", "", err
	}

	title := chartTitle(p, line)
	w, h := 920.0, 520.0
	ml, mr, mt, mb := 84.0, 24.0, 56.0, 84.0 // margins
	pw := w - ml - mr
	ph := h - mt - mb

	vmin, vmax := values[0], values[0]
	for _, v := range values {
		if v < vmin {
			vmin = v
		}
		if v > vmax {
			vmax = v
		}
	}
	if vmin == vmax {
		vmin -= 1
		vmax += 1
	}
	if vmin > 0 && vmin < vmax*0.3 {
		vmin = 0 // nicer baseline when data is far from zero
	}
	padY := (vmax - vmin) * 0.08
	lo, hi := vmin-padY, vmax+padY

	n := len(values)
	stepX := pw / float64(n)
	barW := stepX * 0.62
	if line {
		barW = 0
	}

	var b strings.Builder
	b.WriteString(svgOpen(w, h))
	b.WriteString(svgTitle(w/2, 30, title))
	b.WriteString(svgText(ml-14, mt+ph/2, p.ValueCol, "end", 13, "#B8A9A9", 90))
	b.WriteString(svgText(ml+pw/2, h-22, p.LabelCol, "middle", 13, "#B8A9A9", 0))

	// gridlines + y ticks
	for i := 0; i <= 5; i++ {
		yy := mt + ph - ph*float64(i)/5
		val := lo + (hi-lo)*float64(i)/5
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#2A1712" stroke-width="1"/>`, ml, yy, ml+pw, yy))
		b.WriteString(svgText(ml-10, yy+4, fmtNum(val), "end", 12, "#8A7B7B", 0))
	}

	if line {
		var pts []string
		for i, v := range values {
			x := ml + stepX*(float64(i)+0.5)
			y := mt + ph - (v-lo)/(hi-lo)*ph
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", x, y))
		}
		// glow under the line
		b.WriteString(fmt.Sprintf(`<polyline points="%s" fill="none" stroke="#FF3B30" stroke-width="5" stroke-linejoin="round" stroke-linecap="round" opacity="0.18"/>`, strings.Join(pts, " ")))
		b.WriteString(fmt.Sprintf(`<polyline points="%s" fill="none" stroke="url(#fireGrad)" stroke-width="2.5" stroke-linejoin="round" stroke-linecap="round"/>`, strings.Join(pts, " ")))
		for i, v := range values {
			x := ml + stepX*(float64(i)+0.5)
			y := mt + ph - (v-lo)/(hi-lo)*ph
			b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="4" fill="#FF6B1A" stroke="#0D0707" stroke-width="1.5"/>`, x, y))
		}
	} else {
		for i, v := range values {
			x := ml + stepX*(float64(i)+0.5) - barW/2
			y := mt + ph - (v-lo)/(hi-lo)*ph
			bh := mt + ph - y
			if bh < 1 {
				bh = 1
			}
			gradID := fmt.Sprintf("barGrad%d", i%len(firePalette))
			b.WriteString(fmt.Sprintf(`<defs><linearGradient id="%s" x1="0" y1="0" x2="0" y2="1">
<stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s" stop-opacity="0.35"/></linearGradient></defs>`,
				gradID, firePalette[i%len(firePalette)], firePalette[i%len(firePalette)]))
			b.WriteString(fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="3" fill="url(#%s)"/>`, x, y, barW, bh, gradID))
		}
	}

	// x labels (thinned to ≤ 20)
	labelEvery := 1
	if n > 20 {
		labelEvery = n/20 + 1
	}
	for i := 0; i < n; i += labelEvery {
		x := ml + stepX*(float64(i)+0.5)
		b.WriteString(svgText(x, mt+ph+18, labels[i], "middle", 11, "#8A7B7B", -35))
	}
	b.WriteString("</svg>")

	summary := fmt.Sprintf("%s — %d points, %s range %s…%s", title, n, p.ValueCol, fmtNum(vmin), fmtNum(vmax))
	return b.String(), summary, nil
}

// renderScatter renders a scatter plot from two numeric columns.
func (t *DataTool) renderScatter(d *dataset, p *dataParams) (string, string, error) {
	if p.Column == "" || p.Column2 == "" {
		return "", "", fmt.Errorf("scatter needs two numeric columns: 'column' (x) and 'column2' (y)")
	}
	xi, err := d.colIndex(p.Column)
	if err != nil {
		return "", "", err
	}
	yi, err := d.colIndex(p.Column2)
	if err != nil {
		return "", "", err
	}
	type pt struct{ x, y float64 }
	var pts []pt
	for r := range d.Rows {
		x := parseNumber(d.Rows[r][xi])
		y := parseNumber(d.Rows[r][yi])
		if !math.IsNaN(x) && !math.IsNaN(y) {
			pts = append(pts, pt{x, y})
		}
	}
	if len(pts) < 2 {
		return "", "", fmt.Errorf("not enough numeric pairs in %q vs %q", p.Column, p.Column2)
	}
	if len(pts) > 800 {
		pts = pts[:800]
	}

	xs := make([]float64, len(pts))
	ys := make([]float64, len(pts))
	for i, q := range pts {
		xs[i], ys[i] = q.x, q.y
	}
	r := pearson(xs, ys)

	title := fmt.Sprintf("%s vs %s · r = %.2f", p.Column, p.Column2, r)
	w, h := 920.0, 520.0
	ml, mr, mt, mb := 84.0, 24.0, 56.0, 60.0
	pw := w - ml - mr
	ph := h - mt - mb

	xmin, xmax := xs[0], xs[0]
	ymin, ymax := ys[0], ys[0]
	for i := range pts {
		xmin = math.Min(xmin, xs[i])
		xmax = math.Max(xmax, xs[i])
		ymin = math.Min(ymin, ys[i])
		ymax = math.Max(ymax, ys[i])
	}
	if xmin == xmax {
		xmax = xmin + 1
	}
	if ymin == ymax {
		ymax = ymin + 1
	}

	var b strings.Builder
	b.WriteString(svgOpen(w, h))
	b.WriteString(svgTitle(w/2, 30, title))
	b.WriteString(svgText(ml-14, mt+ph/2, p.Column2, "end", 13, "#B8A9A9", 90))
	b.WriteString(svgText(ml+pw/2, h-20, p.Column, "middle", 13, "#B8A9B9", 0))

	for i := 0; i <= 5; i++ {
		yy := mt + ph - ph*float64(i)/5
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#2A1712"/>`, ml, yy, ml+pw, yy))
		b.WriteString(svgText(ml-10, yy+4, fmtNum(ymin+(ymax-ymin)*float64(i)/5), "end", 12, "#8A7B7B", 0))
	}
	for i := 0; i <= 5; i++ {
		xx := ml + pw*float64(i)/5
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#2A1712"/>`, xx, mt, xx, mt+ph))
		b.WriteString(svgText(xx, mt+ph+18, fmtNum(xmin+(xmax-xmin)*float64(i)/5), "middle", 12, "#8A7B7B", 0))
	}
	for _, q := range pts {
		x := ml + (q.x-xmin)/(xmax-xmin)*pw
		y := mt + ph - (q.y-ymin)/(ymax-ymin)*ph
		b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="4" fill="#FF6B1A" fill-opacity="0.8" stroke="#FF3B30" stroke-width="1"/>`, x, y))
	}
	b.WriteString("</svg>")
	summary := fmt.Sprintf("Scatter — %d points, Pearson r = %.3f", len(pts), r)
	return b.String(), summary, nil
}

// renderPie renders a pie chart with a legend.
func (t *DataTool) renderPie(d *dataset, p *dataParams) (string, string, error) {
	labels, values, err := t.chartData(d, p)
	if err != nil {
		return "", "", err
	}
	// Aggregate top slices + "other" beyond 8.
	type slice struct {
		label string
		val   float64
	}
	var slices []slice
	for i := range values {
		slices = append(slices, slice{labels[i], values[i]})
	}
	sort.SliceStable(slices, func(i, j int) bool { return slices[i].val > slices[j].val })
	if len(slices) > 8 {
		other := 0.0
		for _, s := range slices[8:] {
			other += s.val
		}
		slices = append(slices[:8], slice{"other", other})
	}
	total := 0.0
	for _, s := range slices {
		total += math.Abs(s.val)
	}
	if total == 0 {
		return "", "", fmt.Errorf("all values are zero — cannot build a pie")
	}

	title := chartTitle(p, false)
	w, h := 920.0, 520.0
	cx, cy, R := 250.0, 260.0, 170.0

	var b strings.Builder
	b.WriteString(svgOpen(w, h))
	b.WriteString(svgTitle(w/2, 30, title))

	start := -math.Pi / 2
	for i, s := range slices {
		frac := math.Abs(s.val) / total
		end := start + frac*2*math.Pi
		x0, y0 := cx+R*math.Cos(start), cy+R*math.Sin(start)
		x1, y1 := cx+R*math.Cos(end), cy+R*math.Sin(end)
		large := 0
		if end-start > math.Pi {
			large = 1
		}
		color := firePalette[i%len(firePalette)]
		b.WriteString(fmt.Sprintf(`<path d="M %.1f %.1f A %.1f %.1f 0 %d 1 %.1f %.1f L %.1f %.1f Z" fill="%s" stroke="#0D0707" stroke-width="2"/>`,
			x0, y0, R, R, large, x1, y1, cx, cy, color))
		pct := frac * 100
		if pct >= 4 {
			mid := (start + end) / 2
			lx := cx + R*0.62*math.Cos(mid)
			ly := cy + R*0.62*math.Sin(mid)
			b.WriteString(svgText(lx, ly+4, fmt.Sprintf("%.0f%%", pct), "middle", 12, "#FFFFFF", 0))
		}
		start = end
	}
	// legend
	ly := 120.0
	for i, s := range slices {
		color := firePalette[i%len(firePalette)]
		b.WriteString(fmt.Sprintf(`<rect x="520" y="%.1f" width="16" height="16" rx="3" fill="%s"/>`, ly, color))
		b.WriteString(svgText(546, ly+13, fmt.Sprintf("%s (%s)", s.label, fmtNum(s.val)), "start", 13, "#D8CFCF", 0))
		ly += 30
	}
	b.WriteString("</svg>")
	summary := fmt.Sprintf("Pie — %d slices, total %s", len(slices), fmtNum(total))
	return b.String(), summary, nil
}

func chartTitle(p *dataParams, line bool) string {
	kind := "Bar"
	if line {
		kind = "Line"
	}
	if p.ValueCol != "" {
		return fmt.Sprintf("%s chart — %s", kind, p.ValueCol)
	}
	return fmt.Sprintf("%s chart", kind)
}

// --- SVG primitives ---

func svgOpen(w, h float64) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f">`+
		`<defs><linearGradient id="fireGrad" x1="0" y1="0" x2="1" y2="0">`+
		`<stop offset="0%%" stop-color="#FF3B30"/><stop offset="100%%" stop-color="#FFA02E"/></linearGradient></defs>`+
		`<rect width="100%%" height="100%%" fill="#0D0707"/>`, w, h, w, h)
}

func svgTitle(x, y float64, text string) string {
	return svgText(x, y, text, "middle", 17, "#F5EDED", 0)
}

// svgText emits a text element; angle rotates it (used for y-axis labels).
func svgText(x, y float64, text, anchor string, size int, color string, angle float64) string {
	text = xmlEscape(text)
	rot := ""
	if angle != 0 {
		rot = fmt.Sprintf(` transform="rotate(%.0f %.1f %.1f)"`, angle, x, y)
	}
	return fmt.Sprintf(`<text x="%.1f" y="%.1f" font-family="Segoe UI, Arial, sans-serif" font-size="%d" fill="%s" text-anchor="%s"%s>%s</text>`,
		x, y, size, color, anchor, rot, text)
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}
