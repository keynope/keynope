package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// Public proof decks contain only project-owned synthetic data. This generator
// is intentionally independent of private customer/workshop fixtures.
func powerupProofCorpus(t testing.TB) map[string]Deck {
	t.Helper()
	blank := func(id string) Deck {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: id}}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{}, Charts: map[string]ChartData{}, Builds: map[string]SlideBuild{}}}
		d.EnsureDefaultMasters()
		canonicalizeConcreteAppearance(&d)
		return d
	}
	text := func(id, content, query string) Element {
		values, _ := url.ParseQuery(query)
		values.Set("render", "truetype")
		if !values.Has("modern-size") {
			values.Set("modern-size", "48")
		}
		return Element{ID: id, Kind: "text", Text: content, Query: values.Encode()}
	}
	out := map[string]Deck{}
	themes := blank("theme-proof")
	for i, font := range []string{"c64", "sans", "mono"} {
		themes.Slides[0].Elements = append(themes.Slides[0].Elements, text("font-"+font, "Theme-linked "+font, fmt.Sprintf("top=%d&left=12&width=210&height=12&modern-font=%s&theme-fg=accent-%d", 4+i*15, font, i+1)))
	}
	themes.Masters.Layouts[0].Slide.FG = "#445566"
	themes.Masters.Layouts[0].Slide.FGSet = true
	themes.Slides = append(themes.Slides, Slide{ID: "theme-override", LayoutID: themes.Masters.Layouts[0].ID, Elements: []Element{text("override", "Explicit colour overrides the theme", "top=8&left=12&width=210&height=20&fg=%23ff0055")}})
	out["themes-and-overrides.md"] = themes
	tables := blank("table-empty")
	tables.Slides = nil
	for i, caseName := range []string{"empty", "basic", "merged", "rotated", "header", "dense"} {
		count := 3
		if caseName == "dense" {
			count = 20
		}
		data, err := newTableData(count, count)
		if err != nil {
			t.Fatal(err)
		}
		for r := range data.Cells {
			for c := range data.Cells[r] {
				if caseName != "empty" {
					data.Cells[r][c].Text = fmt.Sprintf("%d:%d", r+1, c+1)
				}
			}
		}
		if caseName == "merged" {
			data.Cells[0][0].ColSpan = 3
			data.Cells[0][1] = TableCell{Hidden: true}
			data.Cells[0][2] = TableCell{Hidden: true}
		}
		if caseName == "header" {
			data.Style.Header = true
			data.Style.Banded = true
		}
		if caseName == "dense" {
			size, padding := 14.0, 2.0
			data.Style.Cell.FontSize = &size
			data.Style.Cell.Padding = &padding
		}
		id := "table-" + caseName
		tables.Objects.Tables[id] = data
		query := "top=5&left=15&width=215&height=46"
		if caseName == "rotated" {
			query += "&object-rotation=6"
		}
		tables.Slides = append(tables.Slides, Slide{ID: fmt.Sprintf("table-page-%d", i), Elements: []Element{{ID: id, Kind: "table", ObjectID: id, Query: query}}})
	}
	out["tables.md"] = tables
	charts := blank("chart-proof")
	charts.Slides = nil
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		data := newChartData(kind)
		data.Title = "Synthetic " + kind
		data.Categories = []string{"Zero", "Positive", "Long category label", "Negative"}
		data.Series = []ChartSeries{{Name: "First", Values: []float64{0, 8, 3, -4}}, {Name: "Second", Values: []float64{0, 2, 6, -1}, Color: "#ff0055"}}
		if kind == "pie" || kind == "doughnut" {
			data.Series = data.Series[:1]
			data.Series[0].Values = []float64{0, 8, 3, 4}
		}
		data.Options.Legend = true
		data.Options.DataLabels = true
		id := "chart-" + kind
		charts.Objects.Charts[id] = data
		charts.Slides = append(charts.Slides, Slide{ID: id + "-page", Elements: []Element{{ID: id, Kind: "chart", ObjectID: id, Query: "top=4&left=12&width=220&height=48"}}})
	}
	out["charts.md"] = charts
	diagrams := blank("diagram-proof")
	diagrams.Slides[0].Elements = []Element{
		{ID: "from", Kind: "shape", Query: "top=8&left=15&width=65&height=16&shape=rounded&shape-radius=0.25&fg=%2355aaff&group=diagram-group"},
		{ID: "to", Kind: "shape", Query: "top=32&left=165&width=65&height=16&shape=diamond&fg=%23ff0055"},
		{ID: "obstacle", Kind: "shape", Query: "top=16&left=100&width=40&height=25&shape=hexagon&shape-depth=0.3&object-rotation=12&group=diagram-group"},
		{ID: "route", Kind: "connector", Query: "connector-from=from&connector-from-side=right&connector-to=to&connector-to-side=left&connector-mode=elbow&connector-arrows=end&connector-width=1&connector-arrow-width=6"},
	}
	for i := 0; i < 3; i++ {
		labelColor := "%23ffffff"
		if i == 2 {
			labelColor = "%23263445"
		}
		encoded, err := json.Marshal(shapeLabelData{Text: []string{"Start", "Finish", "Obstacle"}[i], Query: "render=truetype&modern-size=48&fg=" + labelColor})
		if err != nil {
			t.Fatal(err)
		}
		diagrams.Slides[0].Elements[i].Query = setQueryValue(diagrams.Slides[0].Elements[i].Query, "shape-label", base64.StdEncoding.EncodeToString(encoded))
	}
	out["diagrams.md"] = diagrams
	media := blank("media-proof")
	img := image.NewNRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 8), G: uint8(y * 10), B: 160, A: uint8(100 + x*4)})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	imageID, asset, _, err := embeddedImageAsset(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	svgID, vector, _, err := embeddedSVGAsset(context.Background(), []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="32" height="24" viewBox="0 0 32 24"><rect x="2" y="2" width="28" height="20" fill="#55aaff"/><circle cx="16" cy="12" r="8" fill="#ff0055"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	palette := color.Palette{color.Transparent, color.NRGBA{R: 255, G: 80, B: 160, A: 255}, color.NRGBA{R: 80, G: 160, B: 255, A: 255}}
	frames := []*image.Paletted{image.NewPaletted(img.Rect, palette), image.NewPaletted(img.Rect, palette)}
	for index, frame := range frames {
		for y := 4; y < 20; y++ {
			for x := 4; x < 28; x++ {
				frame.SetColorIndex(x, y, uint8(index+1))
			}
		}
	}
	var gifBytes bytes.Buffer
	if err := gif.EncodeAll(&gifBytes, &gif.GIF{Image: frames, Delay: []int{12, 24}, LoopCount: 0}); err != nil {
		t.Fatal(err)
	}
	animationID, animation, _, err := embeddedImageAsset(gifBytes.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	media.Assets = map[string]DeckAsset{imageID: asset, svgID: vector, animationID: animation}
	for i, item := range []struct{ id, asset, query string }{{"alpha", imageID, "modern-mask=ellipse"}, {"vector", svgID, "modern-mask=rounded"}, {"animation", animationID, "modern-crop=0.1,0.1,0.1,0.1"}} {
		media.Slides[0].Elements = append(media.Slides[0].Elements, Element{ID: item.id, Kind: "image", AssetID: item.asset, Path: embeddedAssetPath(item.asset, media.Assets[item.asset]), Query: fmt.Sprintf("top=10&left=%d&width=65&height=35&image-fit=cover&%s", 8+i*80, item.query)})
	}
	media.Slides = append(media.Slides, Slide{ID: "media-retro", Elements: []Element{{ID: "retro-image", Kind: "image", AssetID: imageID, Path: embeddedAssetPath(imageID, asset), Query: "top=6&left=18&width=210&height=42&image-fit=cover&image-style=retro&glyph=braille"}}})
	media.Slides = append(media.Slides, Slide{ID: "media-background", BackgroundMediaSet: true, BackgroundMedia: &slideBackgroundMedia{AssetID: imageID, Fit: "cover", Opacity: .6, OpacitySet: true, Query: "modern-crop=0.1,0,0.1,0&brightness=0.8"}, Elements: []Element{text("background-caption", "Portable adjusted background", "top=20&left=15&width=215&height=15&fg=%23ffffff")}})
	out["media.md"] = media
	builds := blank("build-proof")
	builds.Slides[0].Elements = []Element{text("first", "First: click to appear", "top=8&left=12&width=210&height=12&group=build-group"), text("second", "Then: automatic fade", "top=24&left=12&width=210&height=12&group=build-group")}
	builds.Objects.Builds["build-proof"] = SlideBuild{Version: 1, Transition: SlideTransition{Effect: "fade", DurationMS: 400}, Steps: []BuildStep{{TargetIDs: []string{"first"}, Effect: "appear", Advance: "click"}, {TargetIDs: []string{"second"}, Effect: "fade", Advance: "after", DelayMS: 200, DurationMS: 400}}}
	out["builds.md"] = builds
	layout := blank("layout-proof")
	layout.Slides[0].Elements = []Element{text("squeezed", "Squeezed C64 text", "top=3&left=4&width=230&height=14&modern-font=c64&modern-size=120&modern-width=60"), text("mono", "Monospaced code\nprint(42)", "top=24&left=4&width=110&height=25&modern-font=mono"), {ID: "clipped", Kind: "shape", Query: "top=42&left=215&width=60&height=30&shape=circle&fg=%23ff0055"}}
	out["export-layout.md"] = layout
	for name, deck := range out {
		canonicalizeConcreteAppearance(&deck)
		standardizeDeckText(&deck)
		out[name] = deck
	}
	return out
}

func TestPowerupProofCorpusExplicitTextSize(t *testing.T) {
	deck := powerupProofCorpus(t)["export-layout.md"]
	scene, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range scene.Objects {
		if object.ID == "squeezed" {
			if object.Text == nil || object.Text.Size != 120 {
				t.Fatal("explicit proof font size was replaced by fixture default")
			}
			return
		}
	}
	t.Fatal("explicit-size proof object missing")
}

func TestPowerupPublicProofCorpus(t *testing.T) {
	var manifest struct {
		Version        int
		AuthoredCanvas struct{ Columns, Rows int }
		Decks          []struct {
			File  string
			Pages int
		}
	}
	data, err := os.ReadFile(testRepositoryPath("testdata", "powerup", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || manifest.AuthoredCanvas.Columns != 245 || manifest.AuthoredCanvas.Rows != 56 {
		t.Fatal("unsupported proof corpus context")
	}
	pages := map[string]int{}
	for _, entry := range manifest.Decks {
		if entry.Pages < 1 || pages[entry.File] != 0 {
			t.Fatal("invalid proof corpus manifest")
		}
		pages[entry.File] = entry.Pages
	}
	oldW, oldH := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	defer func() { authoredTerminalWidth, authoredTerminalHeight = oldW, oldH }()
	corpus := powerupProofCorpus(t)
	if len(pages) != len(corpus) {
		t.Fatal("manifest and public corpus diverged")
	}
	for name, deck := range corpus {
		t.Run(name, func(t *testing.T) {
			if len(deck.Slides) != pages[name] {
				t.Fatal("manifest page count does not match proof deck")
			}
			file := testRepositoryPath("testdata", "powerup", name)
			expected, err := serializeDeck(file, deck)
			if err != nil {
				t.Fatal(err)
			}
			if os.Getenv("KEYNOPE_UPDATE_POWERUP_FIXTURES") == "1" {
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, expected, 0644); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored, expected) {
				t.Fatal("proof fixture differs from deterministic generator")
			}
			loaded, err := parseDeckData(file, stored)
			if err != nil {
				t.Fatal(err)
			}
			second, err := serializeDeck(file, loaded)
			if err != nil || !bytes.Equal(stored, second) {
				t.Fatal("proof fixture save/reopen is unstable", err)
			}
			for index := range loaded.Slides {
				scene, err := buildSlideScene(loaded, index, 245, 56)
				if err != nil || len(scene.Objects) == 0 {
					t.Fatal("proof page has no renderable objects", index, err)
				}
			}
		})
	}
}
