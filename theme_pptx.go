package main

import "keynope/internal/pptx"

func nativePPTXTheme(theme *DeckTheme, widths map[string]float64) *pptx.Theme {
	if theme == nil {
		return nil
	}
	face := func(font ThemeFont) string {
		scale := font.Width / 100
		if font.FontID == "c64" {
			widths[pptxC64FontFamily(scale)] = scale
		}
		return pptxFontFace(&sceneText{FontID: font.FontID, WidthScale: scale})
	}
	c := theme.Colors
	result := &pptx.Theme{Name: theme.Name, Dark1: c.Text, Light1: c.Background, Dark2: c.Heading, Light2: c.Surface, Hyperlink: c.Accent[0], FollowedHyperlink: c.Accent[4], HeadingFont: face(theme.Fonts.Heading), BodyFont: face(theme.Fonts.Body)}
	copy(result.Accent[:], c.Accent)
	return result
}
