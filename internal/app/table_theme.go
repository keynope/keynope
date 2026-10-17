package app

import (
	"math"
	"net/url"
	"strconv"
	"strings"
)

// Formatting is independent of track geometry and text content. Both scene
// projection and selection capabilities use these inputs; inspecting a range
// must not prepare emoji glyphs or lay out every cell in its table.
type tableCellFormat struct {
	Style       TableCellStyle
	TextValues  url.Values
	Color       string
	BorderColor string
	Padding     float64
	BorderWidth float64
}

// Authoring width is a percentage of the font's normalized baseline, not the
// final scene scale (which also includes font-family normalization).
func (deck Deck) tableCellFontWidth(format tableCellFormat) float64 {
	q := deck.resolveThemeTextValues(Element{Kind: "text"}, format.TextValues)
	for _, key := range []string{"modern-width", "ttf-width"} {
		if value, err := strconv.ParseFloat(q.Get(key), 64); err == nil && value >= 1 && value <= 200 {
			return value
		}
	}
	return 100
}

func (deck Deck) tableCellFormat(table TableData, row int, cell TableCell, color string) tableCellFormat {
	lightText := true
	if rgb, err := strconv.ParseUint(strings.TrimPrefix(color, "#"), 16, 24); err == nil {
		lightText = ((rgb>>16)&255)+((rgb>>8)&255)+(rgb&255) > 384
	}
	base := table.Style.Cell
	if table.Style.Header && row == 0 {
		if table.Style.HeaderFill != "" {
			base.Fill = table.Style.HeaderFill
		}
		if base.Fill == "" {
			base.Fill = "#314765"
			if !lightText {
				base.Fill = "#d4e0f2"
			}
			if deck.Theme != nil {
				base.Fill = deck.Theme.Colors.Accent[0]
			}
		}
	} else if table.Style.Banded && row%2 == 1 {
		if table.Style.BandFill != "" {
			base.Fill = table.Style.BandFill
		}
		if base.Fill == "" {
			base.Fill = "#222c3a"
			if !lightText {
				base.Fill = "#edf2f8"
			}
			if deck.Theme != nil {
				base.Fill = deck.Theme.Colors.Surface
			}
		}
	}
	if base.Fill == "" && deck.Theme != nil {
		base.Fill = deck.Theme.Colors.Background
	}
	style := mergeTableCellStyle(base, cell.Style)
	style.Fill, style.Color, style.BorderColor = deck.tableColor(style.Fill), deck.tableColor(style.Color), deck.tableColor(style.BorderColor)
	q := url.Values{"render": {"truetype"}, "modern-size": {"48"}, "modern-font": {"theme"}, "text-valign": {"middle"}}
	if deck.Theme != nil {
		q.Del("modern-size")
	}
	if style.FontID != "" {
		q.Set("modern-font", style.FontID)
	}
	if style.FontSize != nil {
		q.Set("modern-size", strconv.FormatFloat(*style.FontSize, 'f', -1, 64))
	}
	if style.FontWidth != nil {
		q.Set("modern-width", strconv.FormatFloat(*style.FontWidth, 'f', -1, 64))
	}
	if style.LineHeight != nil {
		q.Set("modern-line-height", strconv.FormatFloat(*style.LineHeight, 'f', -1, 64))
	}
	if style.Align != "" {
		q.Set("text-align", style.Align)
	}
	if style.Vertical != "" {
		q.Set("text-valign", style.Vertical)
	}
	format := tableCellFormat{Style: style, TextValues: q, Padding: 10, BorderWidth: 1, BorderColor: style.BorderColor, Color: style.Color}
	if style.Padding != nil {
		format.Padding = *style.Padding
	}
	if style.BorderWidth != nil {
		format.BorderWidth = *style.BorderWidth
	}
	if format.BorderColor == "" {
		format.BorderColor = "#586577"
		if !lightText {
			format.BorderColor = "#acb8c7"
		}
		if deck.Theme != nil {
			format.BorderColor = deck.Theme.Colors.Line
		}
	}
	if format.Color == "" {
		format.Color = color
		if color == "" && style.DefaultColor != "" {
			format.Color = style.DefaultColor
		}
		if deck.Theme != nil && color == "" && format.Color == "" {
			format.Color = deck.Theme.Colors.Text
			if table.Style.Header && row == 0 && len(style.Fill) == 7 {
				format.Color = tableHeaderColor(deck.Theme, style.Fill)
			}
		}
	}
	return format
}

// Semantic table colours stay in authored payloads. Only the projection sees
// concrete RGB values, so palette changes do not bake colour overrides.
func validTableColor(value string) bool {
	return validObjectColor(value) || strings.HasPrefix(value, "theme:") && validThemeColorToken(strings.TrimPrefix(value, "theme:"))
}

func (deck Deck) tableColor(value string) string {
	if strings.HasPrefix(value, "theme:") {
		return deck.themeTokenColor(strings.TrimPrefix(value, "theme:"))
	}
	return value
}

// Relative luminance/contrast is calculated in sRGB, not by averaging bytes.
// It is used only for inherited header text; authored colours are never changed.
func tableColorLuminance(color string) float64 {
	value, err := strconv.ParseUint(strings.TrimPrefix(color, "#"), 16, 24)
	if err != nil {
		return 0
	}
	channel := func(v uint64) float64 {
		c := float64(v&255) / 255
		if c <= .04045 {
			return c / 12.92
		}
		return math.Pow((c+.055)/1.055, 2.4)
	}
	return .2126*channel(value>>16) + .7152*channel(value>>8) + .0722*channel(value)
}

func tableColorContrast(a, b string) float64 {
	x, y := tableColorLuminance(a), tableColorLuminance(b)
	return (math.Max(x, y) + .05) / (math.Min(x, y) + .05)
}

func tableHeaderColor(theme *DeckTheme, fill string) string {
	color := theme.Colors.Text
	// Preserve the theme palette when possible; use black/white only if neither
	// theme foreground nor background supplies accessible header contrast.
	if tableColorContrast(color, fill) >= 4.5 {
		return color
	}
	if tableColorContrast(theme.Colors.Background, fill) >= 4.5 {
		return theme.Colors.Background
	}
	if tableColorContrast("#ffffff", fill) > tableColorContrast("#000000", fill) {
		return "#ffffff"
	}
	return "#000000"
}
