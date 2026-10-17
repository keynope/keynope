package app

import (
	"strings"

	"keynope/internal/pptx"
)

func sceneChartToPPTX(source *sceneChart) *pptx.Chart {
	d := source.Data
	c := &pptx.Chart{Type: d.Type, Title: d.Title, Categories: append([]string(nil), d.Categories...), Options: pptx.ChartOptions{HideAxes: d.Options.HideAxes, Legend: d.Options.Legend, DataLabels: d.Options.DataLabels, ValueFormat: d.Options.ValueFormat, XTitle: d.Options.XTitle, YTitle: d.Options.YTitle, Minimum: d.Options.Minimum, Maximum: d.Options.Maximum}}
	for _, s := range d.Series {
		label := s.LabelColor
		if strings.HasPrefix(label, "theme:") {
			// Visible labels receive the renderer's resolved paint below. An
			// inactive label must not leak an application token into native RGB.
			label = ""
		}
		c.Series = append(c.Series, pptx.ChartSeries{Name: s.Name, Values: append([]float64(nil), s.Values...), Color: s.Color, LabelColor: label})
	}
	c.Options.Stacked = d.Options.Stacked
	for _, path := range source.Paths {
		if path.Series < 0 && path.Stroke != "" {
			c.GridColor = path.Stroke
			break
		}
	}
	for _, label := range source.Labels {
		if label.Text != nil {
			c.FontFace = pptxFontFace(label.Text)
			c.FontSize = pptxTextFontSize(label.Text)
			c.Color = label.Paint.Color
			break
		}
	}
	c.CategoryColors = append([]string(nil), source.CategoryColors...)
	for i, style := range d.CategoryColors {
		if style.LabelColor != "" && !strings.HasPrefix(style.LabelColor, "theme:") {
			for len(c.CategoryLabelColors) <= i {
				c.CategoryLabelColors = append(c.CategoryLabelColors, "")
			}
			c.CategoryLabelColors[i] = style.LabelColor
		}
	}
	for i, color := range source.SeriesColors {
		if i < len(c.Series) {
			c.Series[i].Color = color
		}
	}
	for i, color := range source.SeriesLabelColors {
		if i < len(c.Series) {
			c.Series[i].LabelColor = color
		}
	}
	if source.CategoryLabelColors != nil {
		c.CategoryLabelColors = append([]string(nil), source.CategoryLabelColors...)
	}
	for i := range c.Series {
		if c.Series[i].Color == "inherit" || c.Series[i].Color == "" {
			c.Series[i].Color = "#55aaff"
		}
	}
	return c
}
func pptxChartToData(source *pptx.Chart) (ChartData, []pptx.Warning, error) {
	c := ChartData{Version: 1, Type: source.Type, Title: source.Title, Categories: append([]string(nil), source.Categories...), Options: ChartOptions{HideAxes: source.Options.HideAxes, Legend: source.Options.Legend, DataLabels: source.Options.DataLabels, ValueFormat: source.Options.ValueFormat, XTitle: source.Options.XTitle, YTitle: source.Options.YTitle, Minimum: source.Options.Minimum, Maximum: source.Options.Maximum}}
	for _, s := range source.Series {
		c.Series = append(c.Series, ChartSeries{Name: s.Name, Values: append([]float64(nil), s.Values...), Color: s.Color, LabelColor: s.LabelColor})
	}
	c.Options.Stacked = source.Options.Stacked
	c.Options.GridColor = source.GridColor
	warnings := []pptx.Warning{}
	if source.FontFace != "" || source.Color != "" {
		warnings = append(warnings, pptx.Warning{Code: "chart-label-style-approximated", Message: "Chart labels retain their color and mapped font family, but use Keynope's responsive sizing rather than the imported Office size."})
	}
	if len(source.CategoryColors) > 0 {
		for i, color := range source.CategoryColors {
			if i >= len(c.Categories) {
				break
			}
			c.CategoryColors = append(c.CategoryColors, ChartColorStyle{Color: color})
		}
	}
	for i, color := range source.CategoryLabelColors {
		if i >= len(c.Categories) {
			break
		}
		if color != "" {
			for len(c.CategoryColors) <= i {
				c.CategoryColors = append(c.CategoryColors, ChartColorStyle{})
			}
			c.CategoryColors[i].LabelColor = color
		}
	}
	return c, warnings, c.Validate()
}
