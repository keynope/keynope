package main

import (
	"net/url"
	"strings"
)

// Justification distributes terminal columns, not font-size changes. The same
// rows are used by the terminal, HTML export and both visual editors.
func renderJustifiedTextRows(element Element, width int) []string {
	if width <= 0 {
		return nil
	}
	values, _ := url.ParseQuery(element.Query)
	values.Del("align")
	element.Query = values.Encode()
	render := func(spans []styledTextSpan) []string {
		if rendersAsTextImage(element) {
			return renderTextImageStyledSpans(element, 1<<20, spans)
		}
		if element.Kind == "heading" {
			scale := 1
			if element.Level == 1 {
				scale = 2
			}
			return renderFullStyled(spans, scale)
		}
		return renderQuadStyled(spans)
	}
	space := max(1, editGlyphWidth(element))
	lineHeight := max(1, len(render([]styledTextSpan{{Text: "M"}})))
	var out []string
	longest := 0
	var drawLines []func()
	paragraph := func(text string, marker []string, indent int) {
		available := max(1, width-indent)
		words := styledWords(parseMarkdownStyledSpans(text))
		var chunks [][]string
		used := 0
		first := true
		flush := func() {
			lineChunks, lineUsed, lineFirst := chunks, used, first
			longest = max(longest, used)
			drawLines = append(drawLines, func() {
				chunks, used, first := lineChunks, lineUsed, lineFirst
				height := lineHeight
				for _, chunk := range chunks {
					height = max(height, len(chunk))
				}
				if first {
					height = max(height, len(marker))
				}
				gaps := max(0, len(chunks)-1)
				extra := max(0, available-used)
				if used*10 < longest*7 {
					extra = 0
				}
				for y := 0; y < height; y++ {
					prefix := ""
					if first && y < len(marker) {
						prefix = marker[y]
					}
					row := prefix + strings.Repeat(" ", max(0, indent-displayWidth(stripANSI(prefix))))
					for i, chunk := range chunks {
						part := ""
						if y < len(chunk) {
							part = chunk[y]
						}
						row += part + strings.Repeat(" ", max(0, maxLineDisplayWidth(chunk)-displayWidth(stripANSI(part))))
						if i < gaps {
							gap := space + extra/gaps
							if i < extra%gaps {
								gap++
							}
							row += strings.Repeat(" ", gap)
						}
					}
					row = cropANSIVisible(row, width)
					out = append(out, row)
				}
			})
			chunks, used, first = nil, 0, false
		}
		for len(words) > 0 {
			word := words[0]
			words = words[1:]
			chunk := render(word)
			w := maxLineDisplayWidth(chunk)
			// Long words must still wrap rather than losing their suffix to clipping.
			for w > available && styledSpansLen(word) > 1 {
				prefix, rest := splitStyledSpans(word, max(1, styledSpansLen(word)*available/max(1, w)), 1, 1)
				if styledSpansLen(rest) == 0 || styledSpansLen(prefix) == 0 {
					break
				}
				words = append([][]styledTextSpan{rest}, words...)
				word = prefix
				chunk = render(word)
				w = maxLineDisplayWidth(chunk)
			}
			if len(chunks) > 0 && used+space+w > available {
				flush()
			}
			if len(chunks) > 0 {
				used += space
			}
			chunks = append(chunks, chunk)
			used += w
		}
		if len(chunks) > 0 || first {
			flush()
		}
	}
	if element.Kind == "bullet" {
		marker := renderBulletMarkerRows()
		if rendersAsTextImage(element) {
			marker = render([]styledTextSpan{{Text: "·"}})
		}
		indent := min(max(0, width-1), maxLineDisplayWidth(marker)+space)
		for _, item := range splitBulletListItems(element.Text, true) {
			paragraph(item.Text, marker, indent)
			for _, continuation := range item.Continuations {
				paragraph(continuation, nil, indent)
			}
		}
	} else {
		for _, text := range strings.Split(strings.ReplaceAll(element.Text, "\r\n", "\n"), "\n") {
			paragraph(text, nil, 0)
		}
	}
	for _, draw := range drawLines {
		draw()
	}
	return out
}
