//go:build !js && !keynope_sitegen

package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skip2/go-qrcode"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/sfnt"
)

// Passive input fixtures only. The companion browser acceptance creates and
// edits its entire test deck using MCP, not by modifying Markdown externally.
func TestMCPRecipeFixtures(t *testing.T) {
	out := os.Getenv("KEYNOPE_RECIPE_FIXTURES")
	if out == "" {
		t.Skip("set KEYNOPE_RECIPE_FIXTURES for browser input fixtures")
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"transparent", "white", "dark-lines"} {
		im := image.NewNRGBA(image.Rect(0, 0, 173, 113))
		for y := 0; y < 113; y++ {
			for x := 0; x < 173; x++ {
				p := color.NRGBA{}
				if mode != "transparent" {
					p = color.NRGBA{255, 255, 255, 255}
				}
				circle := math.Hypot(float64(x-49), float64(y-51))
				inside := circle < 32 || x >= 88 && x < 157 && y >= 25 && y < 92 || x >= 42 && x < 112 && y >= 75 && y < 99
				if inside {
					p = color.NRGBA{uint8(40 + x), uint8(180 + y/2), uint8(240 - y), 255}
				}
				if circle > 26 && circle < 31 || x >= 98 && x < 148 && y >= 42 && y < 47 || x >= 111 && x < 116 && y >= 34 && y < 83 {
					p = color.NRGBA{20, 24, 31, 255}
				}
				// A semitransparent corner and isolated fine strokes exercise thresholds.
				if x >= 158 && x <= 160 && y >= 8 && y < 101 {
					p = color.NRGBA{22, 28, 34, 255}
				}
				if x < 18 && y < 18 {
					p = color.NRGBA{255, 60, 100, 100}
				}
				im.SetNRGBA(x, y, p)
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, im); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, mode+".png"), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	code, err := qrcode.New("https://keynope.sh/join/Recipe01", qrcode.Medium)
	if err != nil {
		t.Fatal(err)
	}
	modules := code.Bitmap()
	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d"><rect width="%d" height="%d" fill="#ffffff"/>`, len(modules)*10, len(modules)*10, len(modules), len(modules), len(modules), len(modules))
	for y, row := range modules {
		for x, dark := range row {
			if dark {
				fmt.Fprintf(&svg, `<rect x="%d" y="%d" width="1" height="1" fill="#000000"/>`, x, y)
			}
		}
	}
	svg.WriteString(`</svg>`)
	if err := os.WriteFile(filepath.Join(out, "qr.svg"), []byte(svg.String()), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMCPRecipePanelContainment(t *testing.T) {
	a := automationTest(t)
	authoringCall(t, a, "deck_new", automationArguments{Discard: true})
	add := func(e Element) string {
		t.Helper()
		r := authoringCommand(t, a, map[string]any{"action": "add-element", "kind": "text", "elementData": e})
		return r["created"].([]automationObjectStamp)[0].ID
	}
	// Use actual native shape insertion rather than a second object model.
	authoringCommand(t, a, map[string]any{"action": "add-element", "kind": "shape", "name": "square"})
	objects := authoringCall(t, a, "deck_objects", automationArguments{})["objects"].([]map[string]any)
	panel := objects[0]["id"].(string)
	authoringCommand(t, a, map[string]any{"action": "update-element", "element": 0, "elementData": Element{Kind: "shape", Text: "[shape:square]", Query: "shape=square&left=10&top=10&width=220&height=36&fg=%23101828&modern-opacity=0.92"}})
	inside := add(Element{Kind: "text", Text: "Inside reading panel", Query: "render=truetype&modern-font=sans&modern-size=44&left=14&top=14&width=200&height=5"})
	authoringCommand(t, a, map[string]any{"action": "move-object-layer", "objectId": panel, "kind": "back"})
	measure := authoringCall(t, a, "slide_measure", automationArguments{Slide: 0, PanelID: panel})
	if len(measure["panel_outside"].([]string)) != 0 || len(measure["behind_panel"].([]string)) != 0 {
		t.Fatal(measure)
	}
	authoringCommand(t, a, map[string]any{"action": "move-object-layer", "objectId": panel, "kind": "front"})
	measure = authoringCall(t, a, "slide_measure", automationArguments{Slide: 0, PanelID: panel})
	if !containsString(measure["behind_panel"].([]string), inside) {
		t.Fatal("hidden content not reported", measure)
	}
	crossing := add(Element{Kind: "text", Text: "Crosses the top edge", Query: "render=truetype&modern-font=sans&modern-size=44&left=14&top=8&width=200&height=6"})
	measure = authoringCall(t, a, "slide_measure", automationArguments{Slide: 0, PanelID: panel})
	if !containsString(measure["panel_outside"].([]string), crossing) {
		t.Fatal("partial intersection not reported", measure)
	}
	if _, _, err := a.call(context.Background(), "slide_measure", automationArguments{Slide: 0, PanelID: "missing"}); err == nil {
		t.Fatal("missing panel accepted")
	}
}

func containsString(values []string, value string) bool {
	for _, s := range values {
		if s == value {
			return true
		}
	}
	return false
}

func TestMCPRecipeTextAcrossPageSizes(t *testing.T) {
	a := automationTest(t)
	authoringCall(t, a, "deck_new", automationArguments{Discard: true})
	for _, size := range []DeckPageSize{{Version: 1, Width: 1920, Height: 1080}, {Version: 1, Width: 1440, Height: 1080}, {Version: 1, Width: 1080, Height: 1920}} {
		a.editor.deck.PageSize = &size
		for _, font := range []string{"sans", "mono", "c64"} {
			for _, width := range []int{50, 100, 160} {
				e := Element{Kind: "text", Text: "Words wrap here\n\nUnicode: café → ✓", Query: fmt.Sprintf("render=truetype&modern-font=%s&modern-size=44&modern-width=%d&modern-line-height=1.2", font, width)}
				short := authoringCall(t, a, "text_measure", automationArguments{Element: &e, BoxWidth: 45})
				wide := authoringCall(t, a, "text_measure", automationArguments{Element: &e, BoxWidth: 210})
				if short["height"].(float64) < wide["height"].(float64) || short["font_size"] != float64(44) {
					t.Fatal(font, width, size, short, wide)
				}
			}
		}
	}
}

func TestMCPRecipeGlyphCoverage(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(trueTypeFontBase64))
	if err != nil {
		t.Fatal(err)
	}
	c64, err := sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	mono, err := sfnt.Parse(gomono.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var buffer sfnt.Buffer
	for ch := rune(32); ch <= 126; ch++ {
		id, err := c64.GlyphIndex(&buffer, ch)
		if err != nil || id == 0 {
			t.Fatalf("C64 missing printable ASCII %U: %v", ch, err)
		}
	}
	for _, ch := range "é→’“”€—–┌─┐│└┘╔═╗║╚╝░▒▓█" {
		id, err := mono.GlyphIndex(&buffer, ch)
		if err != nil || id == 0 {
			t.Fatalf("documented mono fallback missing %U: %v", ch, err)
		}
	}
	a := automationTest(t)
	authoringCall(t, a, "deck_new", automationArguments{Discard: true})
	e := Element{Kind: "text", Text: "Confirm ✓ ▆", Query: "render=truetype&modern-font=mono&modern-size=40"}
	result := authoringCall(t, a, "text_measure", automationArguments{Element: &e, BoxWidth: 100})
	warnings := result["missing_glyphs"].([]map[string]string)
	if len(warnings) != 2 || warnings[0]["codepoint"] != "U+2713" || warnings[1]["codepoint"] != "U+2586" {
		t.Fatal("missing checkmark and partial block were not reported", result)
	}
}
