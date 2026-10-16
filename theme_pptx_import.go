package main

import (
	"net/url"
	"strings"

	"keynope/internal/pptx"
)

// A binding belongs to one native Read result. OPC source paths are not global
// identities and must never be compared across independently opened documents.
type pptxThemeBinding struct {
	source *pptx.Theme
	theme  *DeckTheme
}

func newPPTXThemeBinding(source *pptx.Theme) *pptxThemeBinding {
	if source == nil || source.SourcePart == "" {
		return nil
	}
	theme := builtinDeckTheme("clean-light")
	theme.Name = source.Name
	if theme.Name == "" {
		theme.Name = "Imported PowerPoint"
	}
	theme.Colors.Background, theme.Colors.Surface, theme.Colors.Text, theme.Colors.Heading = source.Light1, source.Light2, source.Dark1, source.Dark2
	theme.Colors.Muted, theme.Colors.Line = source.Dark2, source.Dark2
	theme.Colors.Accent = append([]string(nil), source.Accent[:]...)
	theme.Fonts.Heading.FontID, theme.Fonts.Body.FontID = importFont(source.HeadingFont), importFont(source.BodyFont)
	theme.Fonts.Heading.LineHeight, theme.Fonts.Body.LineHeight, theme.Fonts.Mono.LineHeight = 1.25, 1.25, 1.25
	theme.Defaults.ShapeFill, theme.Defaults.ShapeLine = theme.Colors.Surface, theme.Colors.Line
	if theme.Validate() != nil {
		return nil
	}
	return &pptxThemeBinding{source: source, theme: theme}
}

func pptxBinding(bindings []*pptxThemeBinding) *pptxThemeBinding {
	if len(bindings) > 0 {
		return bindings[0]
	}
	return nil
}

func (b *pptxThemeBinding) color(slot, owner, actual string) string {
	if b == nil || owner == "" || owner != b.source.SourcePart {
		return ""
	}
	role := map[string]string{"dk1": "text", "lt1": "background", "dk2": "heading", "lt2": "surface", "accent1": "accent-1", "accent2": "accent-2", "accent3": "accent-3", "accent4": "accent-4", "accent5": "accent-5", "accent6": "accent-6"}[slot]
	if role != "" && strings.EqualFold((Deck{Theme: b.theme}).themeTokenColor(role), actual) {
		return role
	}
	return "" // Hyperlink roles/transforms have no equivalent live colour role.
}

func (b *pptxThemeBinding) font(role, owner, actual string) string {
	if b == nil || owner == "" || owner != b.source.SourcePart {
		return ""
	}
	if role == "major" && actual == b.source.HeadingFont {
		return "heading"
	}
	if role == "minor" && actual == b.source.BodyFont {
		return "body"
	}
	return ""
}

func (b *pptxThemeBinding) run(object pptx.Object, source pptx.TextRun, converted sceneRun) sceneRun {
	if b == nil {
		return converted
	}
	// Preserve literal equal-value overrides before binding the object default.
	if source.Color != "" && b.color(object.ColorThemeSlot, object.ColorThemeSource, object.Color) != "" {
		converted.Color = strings.ToLower(source.Color)
	}
	if token := b.color(source.ColorThemeSlot, source.ColorThemeSource, source.Color); token != "" {
		converted.ColorToken = token
		converted.Color = strings.ToLower(source.Color)
	}
	if role := b.font(source.FontThemeRole, source.FontThemeSource, source.FontFace); role != "" {
		converted.FontRole = role
		converted.FontID = importFont(source.FontFace)
	}
	return converted
}

func freezePPTXTextDefaults(q url.Values) {
	for key, value := range map[string]string{"default-size": "97", "default-width": "100", "default-line-height": "1.25"} {
		if !q.Has(key) {
			q.Set(key, value)
		}
	}
}

func (b *pptxThemeBinding) element(source pptx.Object, element Element) Element {
	if b == nil {
		return element
	}
	q := mustQuery(element.Query)
	if element.Kind == "text" || element.Kind == "heading" || element.Kind == "bullet" || element.Kind == "code" {
		freezePPTXTextDefaults(q)
	}
	slot, owner, color := source.ColorThemeSlot, source.ColorThemeSource, source.Color
	if element.Kind == "shape" {
		slot, owner, color = source.FillThemeSlot, source.FillThemeSource, source.Fill
	}
	if source.Kind == "line" {
		slot, owner, color = source.StrokeThemeSlot, source.StrokeThemeSource, source.Stroke
	}
	if token := b.color(slot, owner, color); token != "" && q.Get("fg") != "" {
		q.Del("fg")
		q.Set("theme-fg", token)
	}
	if element.Kind == "shape" {
		if token := b.color(source.StrokeThemeSlot, source.StrokeThemeSource, source.Stroke); token != "" && q.Get("shape-stroke") != "none" {
			q.Set("shape-stroke", "theme:"+token)
		}
	}
	element.Query = q.Encode()
	return element
}

func (b *pptxThemeBinding) tableColor(value, slot, source string) string {
	if token := b.color(slot, source, value); token != "" {
		return "theme:" + token
	}
	return value
}

// Freeze missing chart defaults using the unthemed import, before attaching
// the imported primary theme. No text layout, rasterisation or scene traversal.
func (b *pptxThemeBinding) chart(deck Deck, source *pptx.Chart, owner *Element, data ChartData) (ChartData, error) {
	if b == nil {
		return data, nil
	}
	fallback := captureClipboardChartTheme(deck, *owner, data)
	if err := fallback.validate(data); err != nil {
		return ChartData{}, err
	}
	data = applyClipboardChartTheme(data, owner, fallback)
	for i, s := range source.Series {
		if token := b.color(s.ColorThemeSlot, s.ColorThemeSource, s.Color); token != "" {
			data.Series[i].ColorToken = token
		}
		data.Series[i].LabelColor = b.tableColor(data.Series[i].LabelColor, s.LabelThemeSlot, s.LabelThemeSource)
	}
	for i, ref := range source.CategoryColorSources {
		if i < len(data.CategoryColors) && i < len(source.CategoryColors) {
			data.CategoryColors[i].ColorToken = b.color(ref.Slot, ref.SourcePart, source.CategoryColors[i])
		}
	}
	for i, ref := range source.CategoryLabelSources {
		if i < len(data.CategoryColors) {
			data.CategoryColors[i].LabelColor = b.tableColor(data.CategoryColors[i].LabelColor, ref.Slot, ref.SourcePart)
		}
	}
	data.Options.GridColor = b.tableColor(data.Options.GridColor, source.GridThemeSlot, source.GridThemeSource)
	q := mustQuery(owner.Query)
	if token := b.color(source.ColorThemeSlot, source.ColorThemeSource, source.Color); token != "" {
		q.Del("fg")
		q.Set("theme-fg", token)
	}
	if role := b.font(source.FontThemeRole, source.FontThemeSource, source.FontFace); role != "" {
		data.Options.FontRole = role
		data.Options.FontID = importFont(source.FontFace)
		data.Options.DefaultFontID = ""
		q.Set("modern-font", "theme")
	}
	owner.Query = q.Encode()
	return data, data.Validate()
}

func freezePPTXTableDefaults(table *TableData) {
	width, spacing := 100.0, 1.25
	table.Style.Cell.FontWidth = &width
	table.Style.Cell.LineHeight = &spacing
}
