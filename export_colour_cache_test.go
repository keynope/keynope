package main

import "testing"

func TestExportLinkAndBackgroundPassIsolation(t *testing.T) {
	for _, count := range []int{1, 3, 1} {
		for _, query := range []string{"", "slide=2&bg=%2355aaff", "link=https%3A%2F%2Fkeynope.sh&bg=red", "slide=2&bg=red&bad=%zz"} {
			var lines []Line
			for _, role := range []string{"text", "code", "outline", "code", "text"} {
				lines = append(lines, Line{Row: len(lines), Text: "X", Role: role, Query: query, Element: -1})
			}
			out := exportLines(lines, Slide{}, 80, 25, count)
			if len(out) != len(lines) {
				t.Fatalf("got %d rows", len(out))
			}
			for i, line := range lines {
				link := ""
				if target, ok := linkTargetFromQuery(query, count); ok && line.Role != "outline" {
					link = target.Value
				}
				background := ""
				if line.Role == "code" {
					background = ansiCSSColour("100")
					if bg := elementBG(query); bg != "" {
						background = ansiCSSColour(bg)
					}
				}
				if out[i].Link != link || len(out[i].Parts) != 1 || out[i].Parts[0].Background != background {
					t.Fatalf("count %d query %q role %s: %+v", count, query, line.Role, out[i])
				}
			}
		}
	}
}

func TestExportForegroundPassIsolation(t *testing.T) {
	for _, slide := range []Slide{{FG: "31", HeaderFG: "32"}, {FG: "34", HeaderFG: "35"}} {
		var lines []Line
		for _, query := range []string{"", "fg=%2355aaff&header=%23ffaa55", "fg=%2355aaff&bad=%zz"} {
			for _, role := range []string{"text", "heading", "text", "heading"} {
				lines = append(lines, Line{Row: len(lines), Text: "X", Role: role, Query: query, Element: -1})
			}
		}
		out := exportLines(lines, slide, 80, 25, 1)
		if len(out) != len(lines) {
			t.Fatalf("got %d rows", len(out))
		}
		for i, line := range lines {
			fg := slideFG(slide)
			if line.Role == "heading" {
				fg = slideHeaderFG(slide)
			}
			if explicit := elementFG(line.Query, line.Role == "heading"); explicit != "" {
				fg = explicit
			}
			if len(out[i].Parts) != 1 || out[i].Parts[0].Color != ansiCSSColour(fg) {
				t.Fatalf("row %d: foreground differs for role/query/defaults: %+v", i, out[i])
			}
		}
	}
}
