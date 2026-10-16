package pptx

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Theme is the representable DrawingML palette and major/minor Latin faces.
// Object paint/fonts remain concrete fallbacks; this does not imply that every
// object is rebound to scheme tokens. Extra application roles have no direct
// theme slot and remain in the object's resolved formatting.
type Theme struct {
	SourcePart                   string // Read-only OPC owner within this presentation, not a global theme identity.
	Name                         string
	Dark1, Light1, Dark2, Light2 string
	Accent                       [6]string
	Hyperlink, FollowedHyperlink string
	HeadingFont, BodyFont        string
}

func validateTheme(theme *Theme) error {
	if theme == nil {
		return nil
	}
	if len(theme.Name) > 100 || !utf8.ValidString(theme.Name) || strings.IndexFunc(theme.Name, unicode.IsControl) >= 0 {
		return fmt.Errorf("invalid PowerPoint theme name")
	}
	for _, value := range append([]string{theme.Dark1, theme.Light1, theme.Dark2, theme.Light2, theme.Hyperlink, theme.FollowedHyperlink}, theme.Accent[:]...) {
		if len(value) != 7 || value[0] != '#' {
			return fmt.Errorf("invalid PowerPoint theme colour")
		}
		for _, r := range value[1:] {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return fmt.Errorf("invalid PowerPoint theme colour")
			}
		}
	}
	for _, face := range []string{theme.HeadingFont, theme.BodyFont} {
		if strings.TrimSpace(face) == "" || len(face) > 128 || !utf8.ValidString(face) || strings.IndexFunc(face, unicode.IsControl) >= 0 {
			return fmt.Errorf("invalid PowerPoint theme font")
		}
	}
	return nil
}

func themeHeader(theme *Theme) string {
	if theme == nil {
		theme = &Theme{Name: "Keynope", Dark1: "#000000", Light1: "#ffffff", Dark2: "#1f1f24", Light2: "#f4f1e8", Accent: [6]string{"#55aaff", "#ff55aa", "#55ffaa", "#ffcc55", "#aa55ff", "#55ffff"}, Hyperlink: "#55aaff", FollowedHyperlink: "#aa55ff", HeadingFont: "Arial", BodyFont: "Arial"}
	}
	name := theme.Name
	if name == "" {
		name = "Keynope"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="%s"><a:themeElements><a:clrScheme name="%s">`, escapeAttr(name), escapeAttr(name))
	keys := []string{"dk1", "lt1", "dk2", "lt2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hlink", "folHlink"}
	values := append([]string{theme.Dark1, theme.Light1, theme.Dark2, theme.Light2}, theme.Accent[:]...)
	values = append(values, theme.Hyperlink, theme.FollowedHyperlink)
	for i, key := range keys {
		fmt.Fprintf(&b, `<a:%s><a:srgbClr val="%s"/></a:%s>`, key, strings.ToUpper(strings.TrimPrefix(values[i], "#")), key)
	}
	fmt.Fprintf(&b, `</a:clrScheme><a:fontScheme name="%s"><a:majorFont><a:latin typeface="%s"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="%s"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>`, escapeAttr(name), escapeAttr(theme.HeadingFont), escapeAttr(theme.BodyFont))
	return b.String()
}
