package main

import (
	"math"
	"net/url"
	"strings"
)

func sceneOnlyEmojiText(text *sceneText) bool {
	if text == nil || len(text.EmojiFonts) == 0 {
		return false
	}
	found := false
	for _, token := range splitEmojiText(sceneTextPlain(text)) {
		if token.assetKey != "" {
			found = true
		} else if strings.TrimSpace(token.text) != "" {
			return false
		}
	}
	return found
}

// Natural (unwrapped) physical text extents. This is deliberately not a second
// browser line-breaking implementation: explicit width-bound boxes retain their
// authored wrapping. Resolve theme/word fonts before measuring this projection.
func sceneTextNaturalSize(text *sceneText) (float64, float64) {
	if text == nil {
		return 1, 1
	}
	paragraphs := text.Paragraphs
	if len(paragraphs) == 0 {
		paragraphs = []sceneParagraph{{Runs: text.Runs}}
	}
	width, height := 0., 0.
	for _, p := range paragraphs {
		lineWidth, lineSize := 0., text.Size
		indent := 0.
		if p.Bullet {
			indent = text.Size * 1.1
		}
		flushLine := func() {
			width = math.Max(width, lineWidth+indent)
			height += lineSize * text.LineHeight
			lineWidth = 0
			lineSize = text.Size
		}
		// Join colour/emphasis-only boundaries before measuring advances so a
		// formatting span does not accidentally remove font kerning.
		metric := *text
		if metric.FontID == "" {
			metric.FontID = "sans"
			if strings.Contains(metric.Family, "Mono") {
				metric.FontID = "mono"
			}
			if strings.Contains(metric.Family, "C64") {
				metric.FontID = "c64"
			}
		}
		baseFont := metric.FontID
		tab := math.Max(1, chartTextWidth("    ", &metric))
		var pending strings.Builder
		flushText := func() {
			for _, token := range splitEmojiText(pending.String()) {
				if token.assetKey != "" {
					scale := text.EmojiWidthScale
					if scale <= 0 {
						scale = 1
					}
					lineWidth += metric.Size*.8*metric.WidthScale*scale + 2*math.Max(1, metric.Size/40)
				} else {
					for i, part := range strings.Split(token.text, "\t") {
						if i > 0 {
							lineWidth = (math.Floor(lineWidth/tab) + 1) * tab
						}
						lineWidth += chartTextWidth(part, &metric)
					}
				}
			}
			pending.Reset()
		}
		for _, run := range p.Runs {
			font, size := baseFont, text.Size
			if run.FontID != "" {
				font = run.FontID
			}
			if run.FontRole != "" && text.FontRoles[run.FontRole] != "" {
				font = text.FontRoles[run.FontRole]
			}
			if run.FontSize > 0 {
				size = run.FontSize
			}
			if font != metric.FontID || size != metric.Size {
				flushText()
				metric.FontID, metric.Size = font, size
			}
			for i, part := range strings.Split(run.Text, "\n") {
				if i > 0 {
					flushText()
					flushLine()
				}
				if part != "" {
					lineSize = math.Max(lineSize, size)
				}
				pending.WriteString(part)
			}
		}
		flushText()
		flushLine()
		height += (text.ParagraphBefore + text.ParagraphAfter) * text.Size
	}
	return math.Max(1, math.Ceil(width+1)), math.Max(1, math.Ceil(height))
}

// Replace only missing natural-width dimensions. Keep layout's flow origin,
// while preserving authored centre/right/bottom edges when the frame changes.
func naturalSceneTextBounds(bounds sceneRect, natural sceneRect, q url.Values) sceneRect {
	if q.Has("width") {
		return bounds
	}
	oldW, oldH := bounds.Width, bounds.Height
	bounds.Width, bounds.Height = natural.Width, natural.Height
	if q.Has("height") {
		bounds.Height = oldH
	}
	p := imagePlacementFromValues(q)
	if p.left == nil && p.leftPct == nil {
		switch {
		case p.right != nil || p.rightPct != nil || p.align == "right":
			bounds.X += oldW - bounds.Width
		case p.align == "center":
			bounds.X += (oldW - bounds.Width) / 2
		}
	}
	if p.top == nil && p.rowDelta == nil {
		switch {
		case p.bottom != nil || p.verticalAlign == "bottom":
			bounds.Y += oldH - bounds.Height
		case p.verticalAlign == "middle":
			bounds.Y += (oldH - bounds.Height) / 2
		}
	}
	return bounds
}
