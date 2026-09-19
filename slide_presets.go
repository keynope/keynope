package main

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
)

// Presets create ordinary editable objects, not a second template format.
// Colours and typography are intentionally absent so paired theme defaults apply.
func slidePreset(name string, cols, rows int) (Slide, error) {
	slide := Slide{}
	if name == "" {
		return slide, nil
	}
	if cols < 1 || rows < 1 {
		return slide, errInvalidEditorAction
	}
	add := func(kind string, level int, text string, x, y, w, h float64) {
		q := url.Values{"render": {"truetype"}, "text-box": {"1"}}
		for key, value := range map[string]int{"left": int(math.Round(x * float64(cols))), "top": int(math.Round(y * float64(rows))), "width": max(1, int(math.Round(w*float64(cols)))), "height": max(1, int(math.Round(h*float64(rows))))} {
			q.Set(key, strconv.Itoa(value))
		}
		slide.Elements = append(slide.Elements, Element{ID: newStableID(kind), Kind: kind, Level: level, Text: text, Query: q.Encode()})
	}
	switch name {
	case "title":
		add("heading", 1, "Presentation title", .08, .12, .84, .35)
		add("text", 0, "Subtitle or speaker name", .08, .57, .84, .18)
	case "section":
		add("text", 0, "SECTION", .08, .2, .84, .12)
		add("heading", 1, "Section title", .08, .38, .84, .35)
	case "body":
		add("heading", 2, "Slide title", .08, .06, .84, .22)
		add("text", 0, "Add your key message here.\n\nUse the space below to explain it.", .08, .36, .84, .52)
	case "columns":
		add("heading", 2, "Two perspectives", .08, .06, .84, .22)
		add("text", 0, "First perspective\n\nAdd your supporting detail.", .08, .36, .39, .52)
		add("text", 0, "Second perspective\n\nAdd your supporting detail.", .53, .36, .39, .52)
	case "quote":
		add("heading", 2, "A memorable idea, in a few words.", .12, .22, .76, .38)
		add("text", 0, "Attribution", .12, .7, .76, .12)
	case "comparison":
		add("heading", 2, "Compare the options", .08, .06, .84, .22)
		add("text", 0, "Option A", .08, .33, .39, .12)
		add("bullet", 0, "First benefit\nSecond benefit\nTrade-off", .08, .52, .39, .36)
		add("text", 0, "Option B", .53, .33, .39, .12)
		add("bullet", 0, "First benefit\nSecond benefit\nTrade-off", .53, .52, .39, .36)
	default:
		return Slide{}, fmt.Errorf("unknown slide preset %q", name)
	}
	return slide, nil
}
