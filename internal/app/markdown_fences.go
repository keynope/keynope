package app

import (
	"regexp"
	"strings"
)

// One fence contract for metadata preprocessing, slide splitting and parsing.
// A shorter or opposite-marker fence inside a block is ordinary code content.
type markdownFenceState struct {
	marker byte
	width  int
}

func (state *markdownFenceState) boundary(line string) bool {
	line = strings.TrimSpace(line)
	if len(line) < 3 || line[0] != '`' && line[0] != '~' {
		return false
	}
	marker := line[0]
	width := 1
	for width < len(line) && line[width] == marker {
		width++
	}
	if width < 3 {
		return false
	}
	if state.width == 0 {
		state.marker, state.width = marker, width
		return true
	}
	if marker == state.marker && width >= state.width && strings.TrimSpace(line[width:]) == "" {
		state.marker, state.width = 0, 0
		return true
	}
	return false
}

// Search only unfenced spans, returning offsets in the original document.
// Never copy/mask a whole deck: embedded media archives can be large. Clipping
// spans also prevents a metadata regex's trailing \s* from consuming code.
func findDocumentMetadataIndices(text string, pattern *regexp.Regexp, count int) [][]int {
	if !strings.Contains(text, "```") && !strings.Contains(text, "~~~") {
		return pattern.FindAllStringSubmatchIndex(text, count)
	}
	if count == 0 {
		return nil
	}
	var matches [][]int
	add := func(start, end int) bool {
		limit := count
		if count > 0 {
			limit -= len(matches)
		}
		for _, match := range pattern.FindAllStringSubmatchIndex(text[start:end], limit) {
			for i := range match {
				if match[i] >= 0 {
					match[i] += start
				}
			}
			matches = append(matches, match)
		}
		return count > 0 && len(matches) >= count
	}
	state := markdownFenceState{}
	spanStart := 0
	for start := 0; start < len(text); {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start + 1
		}
		wasCode := state.width > 0
		boundary := state.boundary(text[start:end])
		if boundary && !wasCode {
			if add(spanStart, start) {
				return matches
			}
		} else if boundary && state.width == 0 {
			spanStart = end
		}
		start = end
	}
	if state.width == 0 {
		add(spanStart, len(text))
	}
	return matches
}

func findDocumentMetadata(text string, pattern *regexp.Regexp, count int) [][]string {
	var matches [][]string
	for _, positions := range findDocumentMetadataIndices(text, pattern, count) {
		match := make([]string, len(positions)/2)
		for i := range match {
			if positions[2*i] >= 0 {
				match[i] = text[positions[2*i]:positions[2*i+1]]
			}
		}
		matches = append(matches, match)
	}
	return matches
}

func firstDocumentMetadata(text string, pattern *regexp.Regexp) []string {
	if matches := findDocumentMetadata(text, pattern, 1); len(matches) > 0 {
		return matches[0]
	}
	return nil
}

func rewriteDocumentMetadata(text string, pattern *regexp.Regexp, rewrite func(string) string) string {
	matches := findDocumentMetadataIndices(text, pattern, -1)
	if len(matches) == 0 {
		return text
	}
	var output strings.Builder
	output.Grow(len(text))
	start := 0
	for _, match := range matches {
		output.WriteString(text[start:match[0]])
		if rewrite != nil {
			output.WriteString(rewrite(text[match[0]:match[1]]))
		}
		start = match[1]
	}
	output.WriteString(text[start:])
	return output.String()
}

func codeFenceForText(text string) string {
	width := 3
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		run := 0
		for run < len(line) && line[run] == '`' {
			run++
		}
		width = max(width, run+1)
	}
	return strings.Repeat("`", width)
}
