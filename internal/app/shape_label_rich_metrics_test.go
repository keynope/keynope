package app

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestShapeLabelRichBrowserFixture(t *testing.T) {
	output := os.Getenv("KEYNOPE_RICH_LABEL_PROOF")
	if output == "" {
		t.Skip("optional browser fixture")
	}
	useTestAuthoredSize(t, 245, 56)
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{}}}
	for i, content := range []string{"Wi", "A\tB\n", "Line one\nLine two"} {
		labelQuery := "render=truetype&modern-font=sans&modern-size=32&modern-line-height=1.4&modern-paragraph-before=0.5&modern-paragraph-after=0.25&fg=%23ffffff"
		if i == 0 {
			runs, err := encodeModernRunsMetadata(content, "", []sceneRun{{Text: "W", FontID: "mono", FontSize: 160}, {Text: "i"}}, true)
			if err != nil {
				t.Fatal(err)
			}
			labelQuery = setQueryValue(labelQuery, "modern-runs", runs)
		}
		label, err := encodeShapeLabel(shapeLabelData{Text: content, Query: labelQuery})
		if err != nil {
			t.Fatal(err)
		}
		query := fmt.Sprintf("shape=square&left=60&top=%d&object-offset-x=0.123456&object-offset-y=0.234567&width=10.125&height=15.375&fg=%%230e7490", 3+i*17)
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: fmt.Sprintf("rich-%d", i), Kind: "shape", Query: setQueryValue(query, "shape-label", label)})
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(output, "labels.md"), deck); err != nil {
		t.Fatal(err)
	}
}

func TestShapeLabelRichMetricsMatchSharedText(t *testing.T) {
	for _, text := range []string{"Wi", "A\tB\n", "Line one\nLine two", "[color=#ff0000]Colour[/color]"} {
		label := Element{Kind: "text", Text: text, Query: "render=truetype&modern-font=sans&modern-size=32&modern-line-height=1.4&modern-paragraph-before=0.5&modern-paragraph-after=0.25"}
		if text == "Wi" {
			runs, err := encodeModernRunsMetadata(text, "", []sceneRun{{Text: "W", FontID: "mono", FontSize: 160}, {Text: "i"}}, true)
			if err != nil {
				t.Fatal(err)
			}
			label.Query = setQueryValue(label.Query, "modern-runs", runs)
		}
		style := (Deck{Appearance: &DeckAppearance{Version: 3}}).modernSceneText(label)
		physicalW, physicalH := sceneTextNaturalSize(style)
		for _, canvas := range []sceneRect{{Width: 1920, Height: 1080}, {Width: 1080, Height: 1920}} {
			for _, grid := range [][2]int{{245, 56}, {160, 90}} {
				w, h := shapeLabelSize(label, grid[0], grid[1], canvas)
				wantW := int(math.Ceil(physicalW*float64(grid[0])/canvas.Width)) + 2
				wantH := int(math.Ceil(physicalH*float64(grid[1])/canvas.Height)) + 2
				if w != wantW || h != wantH {
					t.Fatal("label diverges from shared rich text", text, canvas, grid, w, h, wantW, wantH)
				}
			}
		}
	}
}

func TestShapeTextGrowthKeepsFractionalOriginAndUntouchedAxis(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, page := range []DeckPageSize{{1, 1920, 1080}, {1, 1080, 1920}} {
		for _, axis := range []string{"width", "height"} {
			owner := Element{ID: "shape", Kind: "shape", Query: "shape=square&left=12&top=7&shape-offset-x=0.5&shape-offset-y=0.5&object-offset-x=0.123456&object-offset-y=0.234567&width=30.125&height=15.375"}
			deck := Deck{Appearance: &DeckAppearance{Version: 3}, PageSize: page.Clone(), Slides: []Slide{{Elements: []Element{owner}}}}
			before := cloneDeck(deck)
			scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
			if err != nil || len(scene.Objects) != 1 {
				t.Fatal(err)
			}
			old := scene.Objects[0].Bounds
			grown := old
			if axis == "width" {
				grown.Width += 25.125
			} else {
				grown.Height += 25.125
			}
			query, err := shapeTextGrowthQuery(deck, nativeEditorAction{Slide: 0, ObjectID: "shape", ShapeTextBounds: &grown}, owner)
			if err != nil || !reflect.DeepEqual(before, deck) {
				t.Fatal("growth changed source", err)
			}
			deck.Slides[0].Elements[0].Query = query
			after, err := buildDocumentSlideScene(deck, 0, 245, 56)
			if err != nil || len(after.Objects) != 1 {
				t.Fatal(err)
			}
			got := after.Objects[0].Bounds
			if math.Abs(got.X-old.X) > 1e-8 || math.Abs(got.Y-old.Y) > 1e-8 || axis == "width" && got.Height != old.Height || axis == "height" && got.Width != old.Width {
				t.Fatal("growth changed origin or untouched axis", page, axis, old, got)
			}
			if got.Width+1e-8 < grown.Width || got.Height+1e-8 < grown.Height {
				t.Fatal("growth did not accommodate request", got, grown)
			}
		}
	}
}
