package app

import (
	"math"
	"strings"

	"github.com/rivo/uniseg"
)

type textWrapUnit struct {
	text                              string
	font                              string
	size, width                       float64
	breakAfter, space, newline, emoji bool
}

// Font-metric layout for implicit-height frames, not a replacement text
// painter. Browser shaping remains authoritative for caret/range interaction.
// Unicode line opportunities and grapheme fallback match the renderer's
// pre-wrap/overflow-wrap:anywhere policy without splitting emoji or marks.
func sceneTextWrappedHeight(text *sceneText, width float64) float64 {
	if text == nil {
		return 1
	}
	paragraphs := text.Paragraphs
	if len(paragraphs) == 0 {
		paragraphs = []sceneParagraph{{Runs: text.Runs}}
	}
	total := 0.
	type advanceKey struct {
		font, text string
		size       float64
	}
	advances := make(map[advanceKey]float64)
	measure := func(value string, style *sceneText) float64 {
		key := advanceKey{style.FontID, value, style.Size}
		if width, ok := advances[key]; ok {
			return width
		}
		width := chartTextWidth(value, style)
		if len(advances) >= 4096 {
			clear(advances)
		}
		advances[key] = width
		return width
	}
	base := *text
	if base.FontID == "" {
		base.FontID = "sans"
		if strings.Contains(base.Family, "Mono") {
			base.FontID = "mono"
		}
		if strings.Contains(base.Family, "C64") {
			base.FontID = "c64"
		}
	}
	tab := math.Max(1, measure("    ", &base))
	for _, paragraph := range paragraphs {
		var plain strings.Builder
		ends := make([]int, 0, len(paragraph.Runs))
		for _, run := range paragraph.Runs {
			plain.WriteString(run.Text)
			ends = append(ends, plain.Len())
		}
		graphemes := uniseg.NewGraphemes(plain.String())
		units := make([]textWrapUnit, 0, min(plain.Len(), 1024))
		runIndex := 0
		for graphemes.Next() {
			start, _ := graphemes.Positions()
			for runIndex < len(ends)-1 && start >= ends[runIndex] {
				runIndex++
			}
			metric := base
			if runIndex < len(paragraph.Runs) {
				run := paragraph.Runs[runIndex]
				if run.FontID != "" {
					metric.FontID = run.FontID
				}
				if run.FontRole != "" && text.FontRoles[run.FontRole] != "" {
					metric.FontID = text.FontRoles[run.FontRole]
				}
				if run.FontSize > 0 {
					metric.Size = run.FontSize
				}
			}
			value := graphemes.Str()
			unit := textWrapUnit{text: value, font: metric.FontID, size: metric.Size, breakAfter: graphemes.LineBreak() != uniseg.LineDontBreak, space: value == " " || value == "\t", newline: value == "\n" || value == "\r\n" || value == "\r" || value == "\u2028" || value == "\u2029"}
			if value != "\t" && !unit.newline {
				var tokens []emojiTextToken
				if len(value) > 1 || value[0] >= 0x80 {
					tokens = splitEmojiText(value)
				}
				if len(tokens) == 1 && tokens[0].assetKey != "" {
					unit.emoji = true
					scale := text.EmojiWidthScale
					if scale <= 0 {
						scale = 1
					}
					unit.width = metric.Size*.8*metric.WidthScale*scale + 2*math.Max(1, metric.Size/40)
				} else {
					unit.width = measure(value, &metric)
				}
			}
			units = append(units, unit)
		}
		available := math.Max(1, width)
		if paragraph.Bullet {
			available = math.Max(1, available-text.Size*1.1)
		}
		start, lastBreak, used := 0, 0, 0.
		lineSize := text.Size
		flush := func(end int) {
			size := text.Size
			for _, unit := range units[start:end] {
				if !unit.newline {
					size = math.Max(size, unit.size)
				}
			}
			total += size * text.LineHeight
			start = end
			lastBreak = end
			used = 0
			lineSize = text.Size
		}
		for i := 0; i < len(units); {
			unit := units[i]
			if unit.newline {
				flush(i + 1)
				i++
				continue
			}
			advance := unit.width
			if unit.text == "\t" {
				advance = (math.Floor(used/tab)+1)*tab - used
			} else if i > start {
				previous := units[i-1]
				if previous.font == unit.font && previous.size == unit.size && !previous.space && !unit.space && !previous.emoji && !unit.emoji {
					metric := base
					metric.FontID = unit.font
					metric.Size = unit.size
					// Kerning across paint/run boundaries remains a font dependency only.
					advance += measure(previous.text+unit.text, &metric) - previous.width - unit.width
				}
			}
			// CSS pre-wrap lets trailing whitespace hang beyond the wrap edge.
			if used+advance > available+1e-7 && !unit.space && i > start {
				end := i
				if lastBreak > start {
					end = lastBreak
				}
				flush(end)
				i = end
				continue
			}
			used += advance
			lineSize = math.Max(lineSize, unit.size)
			if unit.breakAfter {
				lastBreak = i + 1
			}
			i++
		}
		// Even an empty paragraph or a trailing newline retains a caret line.
		total += lineSize * text.LineHeight
		total += (text.ParagraphBefore + text.ParagraphAfter) * text.Size
	}
	return math.Max(1, math.Ceil(total))
}
