package main

import (
	"fmt"
	"net/url"
)

type clipboardChartPaint struct {
	Color      string `json:"color"`
	LabelColor string `json:"labelColor"`
}

// Formatting only; chart data lives exactly once in the structured archive.
// The complete source theme, slide content and label strings never travel here.
type clipboardChartThemeFallback struct {
	FontID     string                `json:"fontId"`
	FontWidth  float64               `json:"fontWidth"`
	TextColor  string                `json:"textColor"`
	GridColor  string                `json:"gridColor"`
	Series     []clipboardChartPaint `json:"series"`
	Categories []clipboardChartPaint `json:"categories,omitempty"`
}

func captureClipboardChartTheme(deck Deck, owner Element, data ChartData) clipboardChartThemeFallback {
	theme := deck.resolvedDeckTheme()
	accent := theme.Colors.Accent
	if len(accent) == 0 {
		accent = []string{"#55aaff", "#ffaa55", "#55ffaa", "#ff55aa"}
	}
	q, _ := url.ParseQuery(owner.Query)
	fontID := deck.wordFontID(data.Options.FontRole, data.Options.FontID)
	if fontID == "" {
		fontID = q.Get("modern-font")
	}
	if fontID == "" || fontID == "theme" {
		fontID = data.Options.DefaultFontID
	}
	if fontID == "" {
		fontID = theme.Fonts.Body.FontID
	}
	width := theme.Fonts.Body.Width
	if data.Options.FontWidth != nil {
		width = *data.Options.FontWidth
	}
	color := deck.tableResolvedOwnerColor(Slide{}, owner)
	if color == "" {
		color = data.Options.DefaultColor
	}
	if color == "" {
		color = theme.Colors.Text
	}
	grid := theme.Colors.Line
	if data.Options.GridColor != "" {
		grid = deck.tableColor(data.Options.GridColor)
	}
	fallback := clipboardChartThemeFallback{FontID: fontID, FontWidth: width, TextColor: color, GridColor: grid}
	for i, series := range data.Series {
		fill := deck.chartStyleColor(ChartColorStyle{Color: series.Color, ColorToken: series.ColorToken}, accent[i%len(accent)])
		label := color
		if data.Options.Stacked {
			label = tableHeaderColor(theme, fill)
		}
		if series.LabelColor != "" {
			label = deck.tableColor(series.LabelColor)
		}
		fallback.Series = append(fallback.Series, clipboardChartPaint{Color: fill, LabelColor: label})
	}
	if data.Type == "pie" || data.Type == "doughnut" {
		for i := range data.Categories {
			fill := accent[i%len(accent)]
			style := ChartColorStyle{}
			if i < len(data.CategoryColors) {
				style = data.CategoryColors[i]
			}
			fill = deck.chartStyleColor(style, fill)
			label := tableHeaderColor(theme, fill)
			if style.LabelColor != "" {
				label = deck.tableColor(style.LabelColor)
			}
			fallback.Categories = append(fallback.Categories, clipboardChartPaint{Color: fill, LabelColor: label})
		}
	}
	return fallback
}

func (fallback clipboardChartThemeFallback) validate(data ChartData) error {
	if !validTextFont(fallback.FontID) {
		return fmt.Errorf("invalid clipboard chart font")
	}
	if err := (TableCellStyle{FontWidth: &fallback.FontWidth}).Validate(); err != nil {
		return err
	}
	if fallback.TextColor == "" || fallback.GridColor == "" || !validObjectColor(fallback.TextColor) || !validObjectColor(fallback.GridColor) {
		return fmt.Errorf("invalid clipboard chart paint")
	}
	count := 0
	if data.Type == "pie" || data.Type == "doughnut" {
		count = len(data.Categories)
	}
	if len(fallback.Series) != len(data.Series) || len(fallback.Categories) != count {
		return fmt.Errorf("clipboard chart palette does not match source")
	}
	for _, palette := range [][]clipboardChartPaint{fallback.Series, fallback.Categories} {
		for _, paint := range palette {
			if paint.Color == "" || paint.LabelColor == "" || !validObjectColor(paint.Color) || !validObjectColor(paint.LabelColor) {
				return fmt.Errorf("invalid clipboard chart palette")
			}
		}
	}
	return nil
}

func applyClipboardChartTheme(data ChartData, owner *Element, fallback clipboardChartThemeFallback) ChartData {
	data = cloneChart(data)
	data.Options.FontID = fallback.FontID
	data.Options.FontRole = ""
	width := fallback.FontWidth
	data.Options.FontWidth = &width
	data.Options.GridColor = fallback.GridColor
	for i, paint := range fallback.Series {
		data.Series[i].Color, data.Series[i].LabelColor, data.Series[i].ColorToken = paint.Color, paint.LabelColor, ""
	}
	for i, paint := range fallback.Categories {
		for len(data.CategoryColors) <= i {
			data.CategoryColors = append(data.CategoryColors, ChartColorStyle{})
		}
		data.CategoryColors[i].Color, data.CategoryColors[i].LabelColor, data.CategoryColors[i].ColorToken = paint.Color, paint.LabelColor, ""
	}
	q, _ := url.ParseQuery(owner.Query)
	q.Del("theme-fg")
	q.Set("fg", fallback.TextColor)
	owner.Query = q.Encode()
	return data
}
