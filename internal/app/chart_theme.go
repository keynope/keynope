package app

import (
	"encoding/json"
	"strings"
)

func validChartColorStyle(style ChartColorStyle) bool {
	return validTableColor(style.LabelColor) && validObjectColor(style.Color) && (style.ColorToken == "" || validThemeColorToken(style.ColorToken) && len(style.Color) == 7 && strings.HasPrefix(style.Color, "#"))
}
func (deck Deck) chartStyleColor(style ChartColorStyle, fallback string) string {
	if style.ColorToken != "" {
		if value := deck.themeTokenColor(style.ColorToken); value != "" {
			return value
		}
	}
	if style.Color != "" && style.Color != "inherit" {
		return style.Color
	}
	return fallback
}
func chartHasColorTokens(data ChartData) bool {
	if strings.HasPrefix(data.Options.GridColor, "theme:") {
		return true
	}
	for _, s := range data.Series {
		if s.ColorToken != "" || strings.HasPrefix(s.LabelColor, "theme:") {
			return true
		}
	}
	for _, s := range data.CategoryColors {
		if s.ColorToken != "" || strings.HasPrefix(s.LabelColor, "theme:") {
			return true
		}
	}
	return false
}
func mapChartColorTokens(data ChartData, deck Deck, flatten bool) ChartData {
	copy := cloneChart(data)
	if copy.Options.FontRole != "" {
		if flatten {
			copy.Options.FontRole = ""
		} else {
			copy.Options.FontID = deck.wordFontID(copy.Options.FontRole, copy.Options.FontID)
		}
	}
	mapStyle := func(color, token *string) {
		if *token == "" {
			return
		}
		if flatten {
			*token = ""
		} else {
			*color = deck.themeTokenColor(*token)
		}
	}
	for i := range copy.Series {
		mapStyle(&copy.Series[i].Color, &copy.Series[i].ColorToken)
	}
	for i := range copy.CategoryColors {
		mapStyle(&copy.CategoryColors[i].Color, &copy.CategoryColors[i].ColorToken)
	}
	return copy
}

func (value ChartColorStyle) MarshalJSON() ([]byte, error) {
	type stored ChartColorStyle
	extra := cloneJSONExtensions(value.Extra)
	if extra != nil {
		delete(*extra, "color")
		delete(*extra, "colorToken")
		delete(*extra, "labelColor")
	}
	return encodeJSONExtensions(stored(value), extra)
}
func (value *ChartColorStyle) UnmarshalJSON(data []byte) error {
	type stored ChartColorStyle
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "color", "colorToken", "labelColor")
	if err != nil {
		return err
	}
	*value = ChartColorStyle(decoded)
	value.Extra = extra
	return nil
}
