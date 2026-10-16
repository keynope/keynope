package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

type sceneChart struct {
	Data                ChartData         `json:"data"` // Referenced public data only, never registry extensions.
	Plot                sceneRect         `json:"plot"`
	Paths               []sceneChartPath  `json:"paths"`
	Labels              []sceneChartLabel `json:"labels"`
	Empty               bool              `json:"empty,omitempty"`
	SeriesColors        []string          `json:"seriesColors,omitempty"`
	CategoryColors      []string          `json:"categoryColors,omitempty"`
	SeriesLabelColors   []string          `json:"seriesLabelColors,omitempty"` // Resolved paints, shared by rendering and editable export.
	CategoryLabelColors []string          `json:"categoryLabelColors,omitempty"`
}
type sceneChartPath struct {
	D           string  `json:"d"`
	Fill        string  `json:"fill"`
	Stroke      string  `json:"stroke,omitempty"`
	StrokeWidth float64 `json:"strokeWidth,omitempty"`
	Opacity     float64 `json:"opacity"`
	Series      int     `json:"series"`
}
type sceneChartLabel struct {
	Text     *sceneText `json:"text"`
	Bounds   sceneRect  `json:"bounds"`
	Paint    scenePaint `json:"paint"`
	FullText string     `json:"fullText,omitempty"`
}

var chartMetricFonts = sync.OnceValue(func() map[string]*sfnt.Font {
	c64, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(trueTypeFontBase64))
	result := map[string]*sfnt.Font{}
	for id, data := range map[string][]byte{"sans": goregular.TTF, "mono": gomono.TTF, "c64": c64} {
		if f, err := sfnt.Parse(data); err == nil {
			result[id] = f
		}
	}
	return result
})

// Bundled font advances, including kerning, rather than character-count guesses.
// These same font files and width factors are used by the shared text painter.
func chartTextWidth(text string, style *sceneText) float64 {
	f := chartMetricFonts()[style.FontID]
	if f == nil {
		f = chartMetricFonts()["sans"]
	}
	var buffer sfnt.Buffer
	// Shape labels also use these shared font metrics. Their bounded strings
	// can be longer than a chart label; sum in 64 bits rather than overflowing
	// the font package's 26.6 int32 advance at large sizes.
	width := int64(0)
	previous := sfnt.GlyphIndex(0)
	first := true
	ppem := fixed.Int26_6(math.Round(style.Size * 64))
	for _, r := range text {
		index, err := f.GlyphIndex(&buffer, r)
		if err != nil {
			continue
		}
		if !first {
			if kern, err := f.Kern(&buffer, previous, index, ppem, font.HintingNone); err == nil {
				width += int64(kern)
			}
		}
		if advance, err := f.GlyphAdvance(&buffer, index, ppem, font.HintingNone); err == nil {
			width += int64(advance)
		}
		previous = index
		first = false
	}
	return float64(width) / 64 * style.WidthScale
}

func chartNumber(value float64, format string) string {
	if value == 0 {
		return "0"
	}
	if format == "percent" {
		if math.Abs(value) <= math.MaxFloat64/100 {
			return strconv.FormatFloat(value*100, 'g', 4, 64) + "%"
		}
		return strconv.FormatFloat(value, 'g', 4, 64) + " ×100%"
	}
	return strconv.FormatFloat(value, 'g', 4, 64)
}

type chartGeometryCache struct {
	sync.Mutex
	entries map[[32]byte]*sceneChart
	bytes   int
}

var sharedChartGeometry chartGeometryCache

// Geometry, cache keys and invalidation share these resolved inputs. No marks,
// font measurement, data cloning or text shaping are needed to inspect them.
type chartRenderInputs struct {
	Text           *sceneText
	Color          string
	GridColor      string
	SeriesColors   []string
	SeriesText     []string
	CategoryColors []string
	CategoryText   []string
}

func chartVisibleOptions(data ChartData, width, height float64) ChartData {
	if width < 160 || height < 120 {
		data.Title = ""
		data.Options.Legend = false
		data.Options.DataLabels = false
		data.Options.HideAxes = true
		data.Options.XTitle = ""
		data.Options.YTitle = ""
	}
	return data
}

func (deck Deck) chartInputs(data ChartData, width, height float64, color, fontID string) chartRenderInputs {
	theme := deck.resolvedDeckTheme()
	if data.Options.FontID != "" {
		fontID = deck.wordFontID(data.Options.FontRole, data.Options.FontID)
	}
	if (fontID == "" || fontID == "theme") && data.Options.DefaultFontID != "" {
		fontID = data.Options.DefaultFontID
	}
	if fontID != "" && fontID != "theme" {
		theme = theme.Clone()
		theme.Fonts.Body.FontID = fontID
		deck.Theme = theme
	}
	if color == "" {
		color = data.Options.DefaultColor
	}
	if color == "" {
		color = theme.Colors.Text
	}
	size := math.Max(12, math.Min(32, math.Min(width/25, height/14)))
	q := url.Values{"render": {"truetype"}, "modern-font": {"theme"}, "modern-size": {strconv.FormatFloat(size, 'f', -1, 64)}, "text-align": {"center"}, "text-valign": {"middle"}}
	if data.Options.FontWidth != nil {
		q.Set("modern-width", strconv.FormatFloat(*data.Options.FontWidth, 'f', -1, 64))
	}
	base := deck.modernSceneTextFromValues(Element{Kind: "text"}, q)
	base.Size = size // Compact chart size is geometry-owned; family/width inherit.
	base.LineHeight = 1.1
	base.ParagraphBefore, base.ParagraphAfter = 0, 0
	inputs := chartRenderInputs{Text: base, Color: color}
	accent := theme.Colors.Accent
	if len(accent) == 0 {
		accent = []string{"#55aaff", "#ffaa55", "#55ffaa", "#ff55aa"}
	}
	for i, series := range data.Series {
		color := deck.chartStyleColor(ChartColorStyle{Color: series.Color, ColorToken: series.ColorToken}, accent[i%len(accent)])
		inputs.SeriesColors = append(inputs.SeriesColors, color)
		if chartVisibleOptions(data, width, height).Options.DataLabels {
			labelColor := inputs.Color
			if data.Options.Stacked {
				labelColor = tableHeaderColor(theme, color)
			}
			if series.LabelColor != "" {
				labelColor = deck.tableColor(series.LabelColor)
			}
			inputs.SeriesText = append(inputs.SeriesText, labelColor)
		}
	}
	if data.Type == "pie" || data.Type == "doughnut" {
		for i := range data.Categories {
			fill := accent[i%len(accent)]
			if i < len(data.CategoryColors) {
				fill = deck.chartStyleColor(data.CategoryColors[i], fill)
			}
			inputs.CategoryColors = append(inputs.CategoryColors, fill)
			if chartVisibleOptions(data, width, height).Options.DataLabels {
				labelColor := tableHeaderColor(theme, fill)
				if i < len(data.CategoryColors) && data.CategoryColors[i].LabelColor != "" {
					labelColor = deck.tableColor(data.CategoryColors[i].LabelColor)
				}
				inputs.CategoryText = append(inputs.CategoryText, labelColor)
			}
		}
	} else if !chartVisibleOptions(data, width, height).Options.HideAxes {
		inputs.GridColor = theme.Colors.Line
		if data.Options.GridColor != "" {
			inputs.GridColor = deck.tableColor(data.Options.GridColor)
		}
	}
	return inputs
}

func (deck Deck) projectChart(data ChartData, bounds sceneRect, color string, fontIDs ...string) (*sceneChart, error) {
	if err := data.Validate(); err != nil {
		return nil, err
	}
	if data.Options.Stacked && data.Type != "column" && data.Type != "bar" {
		return nil, fmt.Errorf("stacked chart layout is not supported yet; disable stacking")
	}
	if !finitePositive(bounds.Width) || !finitePositive(bounds.Height) {
		return nil, fmt.Errorf("chart bounds must be positive and finite")
	}
	fontID := ""
	if len(fontIDs) > 0 {
		fontID = fontIDs[0]
	}
	inputs := deck.chartInputs(data, bounds.Width, bounds.Height, color, fontID)
	key, err := chartGeometryKey(data, bounds.Width, bounds.Height, inputs)
	if err != nil {
		return nil, fmt.Errorf("chart render inputs: %w", err)
	}
	sharedChartGeometry.Lock()
	cached := sharedChartGeometry.entries[key]
	sharedChartGeometry.Unlock()
	if cached != nil {
		return cached, nil
	} // Immutable render data; world position is not part of this key.
	result := layoutChart(clonePublicChart(data), bounds.Width, bounds.Height, inputs)
	encoded, _ := json.Marshal(result)
	cost := len(encoded) + len(key)
	// A 16k-point grouped chart can exceed 1 MiB even with one path per
	// series. Keep it eligible for retained movement while bounding the entire
	// cache to 4 MiB; larger projections deliberately remain uncached.
	if cost <= 4<<20 {
		sharedChartGeometry.Lock()
		if sharedChartGeometry.entries == nil || len(sharedChartGeometry.entries) >= 32 || sharedChartGeometry.bytes+cost > 4<<20 {
			sharedChartGeometry.entries = map[[32]byte]*sceneChart{}
			sharedChartGeometry.bytes = 0
		}
		if sharedChartGeometry.entries[key] == nil {
			sharedChartGeometry.entries[key] = result
			sharedChartGeometry.bytes += cost
		}
		sharedChartGeometry.Unlock()
	}
	return result, nil
}

func layoutChart(data ChartData, width, height float64, inputs chartRenderInputs) *sceneChart {
	c := &sceneChart{Data: data, Paths: []sceneChartPath{}, Labels: []sceneChartLabel{}, SeriesLabelColors: append([]string(nil), inputs.SeriesText...), CategoryLabelColors: append([]string(nil), inputs.CategoryText...)}
	// Small thumbnails still show the marks; suppress decorations rather than
	// placing oversized labels outside the authored box. Keep Data untouched.
	data = chartVisibleOptions(data, width, height)
	base, color := inputs.Text, inputs.Color
	size := base.Size
	pad := math.Min(size*.6, math.Min(width, height)*.1)
	top, bottom := pad, height-pad
	addLabel := func(text string, b sceneRect, align string, paintColor string) {
		if paintColor == "none" {
			paintColor = "transparent"
		}
		if b.Width <= 2 || b.Height < size*1.1 {
			return
		}
		full := text
		style := *base
		style.Align = align
		style.Vertical = "middle"
		available := b.Width - 2
		if chartTextWidth(text, &style) > available {
			if available < chartTextWidth("…", &style) {
				return
			}
			runes := []rune(text)
			lo, hi := 0, len(runes)
			for lo < hi {
				mid := (lo + hi + 1) / 2
				if chartTextWidth(string(runes[:mid])+"…", &style) <= available {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			text = string(runes[:lo]) + "…"
		}
		style.Runs = []sceneRun{{Text: text}}
		style.Paragraphs = nil
		c.Labels = append(c.Labels, sceneChartLabel{Text: &style, Bounds: b, Paint: scenePaint{Color: paintColor}, FullText: full})
	}
	if data.Title != "" {
		addLabel(data.Title, sceneRect{pad, top, width - 2*pad, size * 1.5}, "center", color)
		top += size * 1.8
	}
	circular := data.Type == "pie" || data.Type == "doughnut"
	seriesColor := func(index int) string {
		return inputs.SeriesColors[index]
	}
	c.SeriesColors, c.CategoryColors = inputs.SeriesColors, inputs.CategoryColors
	if data.Options.Legend {
		count := len(data.Series)
		if circular {
			count = len(data.Categories)
		}
		columns := max(1, int(width/(size*8)))
		shown := min(count, min(columns*2, 12))
		rows := (shown + columns - 1) / columns
		bottom -= float64(rows) * size * 1.5
		for i := 0; i < shown; i++ {
			text, col := data.Series[min(i, len(data.Series)-1)].Name, seriesColor(min(i, len(data.Series)-1))
			if circular {
				text, col = data.Categories[i], inputs.CategoryColors[i]
			}
			if i == shown-1 && count > shown {
				text += " (" + strconv.Itoa(count-shown) + " more)"
			}
			x := float64(i%columns)*width/float64(columns) + pad
			y := bottom + float64(i/columns)*size*1.5
			c.Paths = append(c.Paths, sceneChartPath{D: chartRectPath(x, y+size*.4, size*.55, size*.55), Fill: col, Opacity: 1, Series: -1})
			addLabel(text, sceneRect{x + size*.8, y, width/float64(columns) - size*1.4, size * 1.4}, "start", color)
		}
		bottom -= pad
	}
	if circular {
		c.Plot = sceneRect{pad, top, math.Max(1, width-2*pad), math.Max(1, bottom-top)}
		cx, cy := width/2, (top+bottom)/2
		r := math.Max(1, math.Min(c.Plot.Width, c.Plot.Height)*.44)
		maximum := 0.0
		for _, v := range data.Series[0].Values {
			maximum = math.Max(maximum, v)
		}
		if maximum == 0 {
			c.Empty = true
			addLabel("No values yet", sceneRect{pad, cy - size, width - 2*pad, 2 * size}, "center", color)
			return c
		}
		total := 0.0
		for _, v := range data.Series[0].Values {
			total += v / maximum
		}
		angle := -math.Pi / 2
		for i, v := range data.Series[0].Values {
			if v == 0 {
				continue
			}
			part := (v / maximum) / total
			sweep := part * 2 * math.Pi
			inner := 0.0
			if data.Type == "doughnut" {
				inner = r * .56
			}
			c.Paths = append(c.Paths, sceneChartPath{D: chartSectorPath(cx, cy, r, inner, angle, sweep), Fill: inputs.CategoryColors[i], Opacity: 1, Series: 0})
			if data.Options.DataLabels && part >= .07 && len(data.Categories) <= 12 {
				mid := angle + sweep/2
				rr := r * .72
				if inner > 0 {
					rr = r * .78
				}
				x, y := cx+math.Cos(mid)*rr, cy+math.Sin(mid)*rr
				label := strconv.FormatFloat(part*100, 'f', 0, 64) + "%"
				lc := inputs.CategoryText[i]
				addLabel(label, sceneRect{x - size*1.6, y - size*.6, size * 3.2, size * 1.2}, "center", lc)
			}
			angle += sweep
		}
		return c
	}
	bar := data.Type == "bar"
	maximum := 0.0
	for _, s := range data.Series {
		for _, v := range s.Values {
			maximum = math.Max(maximum, math.Abs(v))
		}
	}
	for _, v := range []*float64{data.Options.Minimum, data.Options.Maximum} {
		if v != nil {
			maximum = math.Max(maximum, math.Abs(*v))
		}
	}
	if maximum == 0 {
		maximum = 1
	}
	lo, hi := 0.0, 0.0
	for _, s := range data.Series {
		for _, v := range s.Values {
			lo = math.Min(lo, v/maximum)
			hi = math.Max(hi, v/maximum)
		}
	}
	// Sum normalized values to avoid overflowing finite large input values.
	stackStarts, stackEnds := make([][]float64, len(data.Series)), make([][]float64, len(data.Series))
	if data.Options.Stacked {
		positive, negative := make([]float64, len(data.Categories)), make([]float64, len(data.Categories))
		for si, s := range data.Series {
			stackStarts[si], stackEnds[si] = make([]float64, len(s.Values)), make([]float64, len(s.Values))
			for i, v := range s.Values {
				stack := positive
				if v < 0 {
					stack = negative
				}
				stackStarts[si][i] = stack[i]
				stack[i] += v / maximum
				stackEnds[si][i] = stack[i]
				lo, hi = math.Min(lo, stack[i]), math.Max(hi, stack[i])
			}
		}
	}
	if data.Options.Minimum != nil {
		lo = *data.Options.Minimum / maximum
	}
	if data.Options.Maximum != nil {
		hi = *data.Options.Maximum / maximum
	}
	if hi <= lo {
		if data.Options.Minimum != nil {
			hi = lo + 1
		} else {
			lo = hi - 1
		}
	}
	targetTicks := math.Max(2, math.Min(8, height/(size*2.5)))
	raw := (hi - lo) / targetTicks
	power := math.Pow(10, math.Floor(math.Log10(raw)))
	step := power
	for _, factor := range []float64{1, 2, 2.5, 5, 10} {
		if factor*power >= raw {
			step = factor * power
			break
		}
	}
	if !finitePositive(step) {
		step = 1
	}
	if data.Options.Minimum == nil {
		lo = math.Floor(lo/step) * step
	}
	if data.Options.Maximum == nil {
		hi = math.Ceil(hi/step) * step
	}
	left, right := pad, width-pad
	labelWidth := size * 3.5
	if bar {
		labelWidth = math.Min(width*.28, size*10)
	}
	if !data.Options.HideAxes {
		left += labelWidth
		bottom -= size * 1.7
	}
	if data.Options.XTitle != "" {
		bottom -= size * 1.5
		addLabel(data.Options.XTitle, sceneRect{left, bottom + size*1.8, right - left, size * 1.3}, "center", color)
	}
	if data.Options.YTitle != "" {
		addLabel(data.Options.YTitle, sceneRect{pad, top, width - 2*pad, size * 1.4}, "start", color)
		top += size * 1.6
	}
	c.Plot = sceneRect{left, top, math.Max(1, right-left), math.Max(1, bottom-top)}
	plot := c.Plot
	normalizedPosition := func(value float64) float64 {
		ratio := (value - lo) / (hi - lo)
		return math.Max(0, math.Min(1, ratio))
	}
	position := func(value float64) float64 { return normalizedPosition(value / maximum) }
	baseline := position(0)
	if !data.Options.HideAxes {
		// Integer-indexed ticks avoid accumulated addition error at zero.
		// The tiny index tolerance also keeps a rounded lower bound from
		// accidentally skipping its first tick on division by the same step.
		start := math.Ceil(lo/step - 1e-8)
		for n := 0; n < 12; n++ {
			v := (start + float64(n)) * step
			if v > hi+step*1e-8 {
				break
			}
			ratio := (v - lo) / (hi - lo)
			label := chartNumber(v*maximum, data.Options.ValueFormat)
			if math.IsInf(v*maximum, 0) {
				continue
			}
			if bar {
				x := plot.X + ratio*plot.Width
				c.Paths = append(c.Paths, sceneChartPath{D: fmt.Sprintf("M%g %gV%g", x, plot.Y, plot.Y+plot.Height), Fill: "none", Stroke: inputs.GridColor, StrokeWidth: 1, Opacity: .45, Series: -1})
				w := math.Min(size*4, width-2*pad)
				addLabel(label, sceneRect{math.Max(pad, math.Min(width-pad-w, x-w/2)), plot.Y + plot.Height + pad, w, size * 1.2}, "center", color)
			} else {
				y := plot.Y + (1-ratio)*plot.Height
				c.Paths = append(c.Paths, sceneChartPath{D: fmt.Sprintf("M%g %gH%g", plot.X, y, plot.X+plot.Width), Fill: "none", Stroke: inputs.GridColor, StrokeWidth: 1, Opacity: .45, Series: -1})
				addLabel(label, sceneRect{pad, y - size*.6, labelWidth - pad, size * 1.2}, "end", color)
			}
		}
		capacity := max(1, int(plot.Width/(size*5)))
		if bar {
			capacity = max(1, int(plot.Height/(size*1.6)))
		}
		stride := max(1, (len(data.Categories)+capacity-1)/capacity)
		for i, category := range data.Categories {
			if i%stride != 0 {
				continue
			}
			if bar {
				slot := plot.Height / float64(len(data.Categories))
				addLabel(category, sceneRect{pad, plot.Y + float64(i)*slot, labelWidth - pad, slot}, "end", color)
			} else {
				slot := plot.Width / float64(len(data.Categories))
				addLabel(category, sceneRect{plot.X + float64(i)*slot, plot.Y + plot.Height + pad, slot*float64(stride) - pad, size * 1.2}, "center", color)
			}
		}
	}
	for si, s := range data.Series {
		var path strings.Builder
		if data.Type == "column" || bar {
			for i, v := range s.Values {
				slot := plot.Width / float64(len(s.Values))
				if bar {
					slot = plot.Height / float64(len(s.Values))
				}
				thickness := slot * .8 / float64(len(data.Series))
				offset := slot*.1 + float64(si)*thickness
				start, end := baseline, position(v)
				if data.Options.Stacked {
					thickness, offset = slot*.8, slot*.1
					start, end = normalizedPosition(stackStarts[si][i]), normalizedPosition(stackEnds[si][i])
				}
				if bar {
					path.WriteString(chartRectPath(plot.X+math.Min(start, end)*plot.Width, plot.Y+float64(i)*slot+offset, math.Abs(end-start)*plot.Width, thickness*.94))
				} else {
					path.WriteString(chartRectPath(plot.X+float64(i)*slot+offset, plot.Y+(1-math.Max(start, end))*plot.Height, thickness*.94, math.Abs(end-start)*plot.Height))
				}
			}
			c.Paths = append(c.Paths, sceneChartPath{D: path.String(), Fill: seriesColor(si), Opacity: 1, Series: si})
		} else {
			point := func(i int, v float64) (float64, float64) {
				return plot.X + (float64(i)+.5)*plot.Width/float64(len(s.Values)), plot.Y + (1-position(v))*plot.Height
			}
			for i, v := range s.Values {
				x, y := point(i, v)
				if i == 0 {
					fmt.Fprintf(&path, "M%g %g", x, y)
				} else {
					fmt.Fprintf(&path, "L%g %g", x, y)
				}
			}
			if data.Type == "area" {
				x, _ := point(len(s.Values)-1, 0)
				x0, _ := point(0, 0)
				fmt.Fprintf(&path, "L%g %gL%g %gZ", x, plot.Y+(1-baseline)*plot.Height, x0, plot.Y+(1-baseline)*plot.Height)
				c.Paths = append(c.Paths, sceneChartPath{D: path.String(), Fill: seriesColor(si), Stroke: seriesColor(si), StrokeWidth: 2, Opacity: .35, Series: si})
			} else {
				c.Paths = append(c.Paths, sceneChartPath{D: path.String(), Fill: "none", Stroke: seriesColor(si), StrokeWidth: 3, Opacity: 1, Series: si})
				if len(s.Values) == 1 {
					x, y := point(0, s.Values[0])
					c.Paths = append(c.Paths, sceneChartPath{D: chartSectorPath(x, y, 4, 0, 0, 2*math.Pi), Fill: seriesColor(si), Opacity: 1, Series: si})
				}
			}
		}
		if data.Options.DataLabels && len(data.Categories)*len(data.Series) <= 24 {
			for i, v := range s.Values {
				slot := plot.Width / float64(len(s.Values))
				x, y := plot.X+(float64(i)+.5)*slot, plot.Y+(1-position(v))*plot.Height-size*1.2
				if bar {
					slot = plot.Height / float64(len(s.Values))
					x, y = plot.X+position(v)*plot.Width, plot.Y+(float64(i)+.5)*slot-size*.6
				}
				if data.Type == "column" {
					x = plot.X + float64(i)*slot + slot*.1 + (float64(si)+.5)*slot*.8/float64(len(data.Series))
				}
				if bar {
					y = plot.Y + float64(i)*slot + slot*.1 + (float64(si)+.5)*slot*.8/float64(len(data.Series)) - size*.6
				}
				label := chartNumber(v, data.Options.ValueFormat)
				w := chartTextWidth(label, base) + 4
				labelColor := color
				if si < len(inputs.SeriesText) {
					labelColor = inputs.SeriesText[si]
				}
				if data.Options.Stacked {
					start, end := normalizedPosition(stackStarts[si][i]), normalizedPosition(stackEnds[si][i])
					mid := (start + end) / 2
					// Keep labels inside visible segments; a thin/zero or clipped
					// segment must not borrow room from its neighbouring series.
					if bar && (math.Abs(end-start)*plot.Width < w+4 || slot*.8*.94 < size*1.2) || !bar && (math.Abs(end-start)*plot.Height < size*1.2+4 || slot*.8*.94 < w) {
						continue
					}
					if si < len(inputs.SeriesText) {
						labelColor = inputs.SeriesText[si]
					}
					if bar {
						x, y = plot.X+mid*plot.Width, plot.Y+(float64(i)+.5)*slot-size*.6
					} else {
						x, y = plot.X+(float64(i)+.5)*slot, plot.Y+(1-mid)*plot.Height-size*.6
					}
				}
				box := sceneRect{math.Max(plot.X, math.Min(plot.X+plot.Width-w, x-w/2)), math.Max(plot.Y, math.Min(plot.Y+plot.Height-size*1.2, y)), w, size * 1.2}
				collision := false
				for _, existing := range c.Labels {
					b := existing.Bounds
					if box.X < b.X+b.Width+2 && box.X+box.Width+2 > b.X && box.Y < b.Y+b.Height+2 && box.Y+box.Height+2 > b.Y {
						collision = true
						break
					}
				}
				if !collision && box.Width <= plot.Width && box.Height <= plot.Height {
					addLabel(label, box, "center", labelColor)
				}
			}
		}
	}
	return c
}

func chartRectPath(x, y, w, h float64) string { return fmt.Sprintf("M%g %gh%gv%gh%gZ", x, y, w, h, -w) }
func chartSectorPath(cx, cy, r, inner, start, sweep float64) string {
	// A full circle needs two arcs; a single coincident endpoint arc is empty.
	if sweep >= 2*math.Pi-1e-10 {
		d := fmt.Sprintf("M%g %gA%g %g 0 1 1 %g %gA%g %g 0 1 1 %g %gZ", cx+r, cy, r, r, cx-r, cy, r, r, cx+r, cy)
		if inner > 0 {
			d += fmt.Sprintf("M%g %gA%g %g 0 1 0 %g %gA%g %g 0 1 0 %g %gZ", cx+inner, cy, inner, inner, cx-inner, cy, inner, inner, cx+inner, cy)
		}
		return d
	}
	x1, y1 := cx+r*math.Cos(start), cy+r*math.Sin(start)
	x2, y2 := cx+r*math.Cos(start+sweep), cy+r*math.Sin(start+sweep)
	large := 0
	if sweep > math.Pi {
		large = 1
	}
	d := fmt.Sprintf("M%g %gA%g %g 0 %d 1 %g %g", x1, y1, r, r, large, x2, y2)
	if inner > 0 {
		d += fmt.Sprintf("L%g %gA%g %g 0 %d 0 %g %gZ", cx+inner*math.Cos(start+sweep), cy+inner*math.Sin(start+sweep), inner, inner, large, cx+inner*math.Cos(start), cy+inner*math.Sin(start))
	} else {
		d += fmt.Sprintf("L%g %gZ", cx, cy)
	}
	return d
}
