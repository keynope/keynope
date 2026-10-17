package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCuratedShapeGeometryCatalogue(t *testing.T) {
	if len(shapeDefinitions) != 29 {
		t.Fatal("curated catalogue is incomplete")
	}
	ids := map[string]bool{}
	for _, d := range shapeDefinitions {
		t.Run(d.ID, func(t *testing.T) {
			if ids[d.ID] || d.Title == "" || d.Preset == "" || d.Category == "" {
				t.Fatal("invalid catalogue entry")
			}
			ids[d.ID] = true
			for _, aspect := range []float64{.25, 1, 4} {
				g := shapeGeometry(ShapeSpec{Kind: d.ID}, aspect)
				if g.Path == "" || strings.Contains(g.Path, "NaN") || strings.Contains(g.Path, "Inf") {
					t.Fatal("invalid path")
				}
				if g.TextRegion.Width <= 0 || g.TextRegion.Height <= 0 || g.TextRegion.X < 0 || g.TextRegion.Y < 0 || g.TextRegion.X+g.TextRegion.Width > 1.00001 || g.TextRegion.Y+g.TextRegion.Height > 1.00001 {
					t.Fatalf("invalid label region: %+v", g.TextRegion)
				}
				count := 0
				for _, c := range g.Contours {
					count += len(c)
					for _, p := range c {
						if math.IsNaN(p.X) || math.IsNaN(p.Y) || p.X < -.01 || p.Y < -.01 || p.X > 1.01 || p.Y > 1.01 {
							t.Fatalf("unbounded geometry: %+v", p)
						}
					}
				}
				if count < 3 || count > 512 {
					t.Fatalf("unexpected geometry complexity %d", count)
				}
				// Scanline rasterization and point hit testing consume identical paths.
				for y := 0; y < 21; y++ {
					v := (float64(y) + .5) / 21
					xs := g.rowIntersections(v)
					if len(xs)%2 != 0 {
						t.Fatal("unclosed scanline")
					}
					for x := 0; x < 37; x++ {
						u := (float64(x) + .5) / 37
						inside := false
						for n := 0; n+1 < len(xs); n += 2 {
							if u >= xs[n] && u < xs[n+1] {
								inside = true
							}
						}
						if inside != g.contains(u, v) {
							t.Fatal("raster and hit coverage differ")
						}
					}
				}
			}
			q := url.Values{"shape": {d.ID}, "width": {"30"}, "height": {"15"}}
			ports := shapePortsFromValues("shape", q, 3, 4)
			if len(ports) != 4 {
				t.Fatalf("missing ports: %v", ports)
			}
			for _, p := range ports {
				if p.X < 3 || p.X > 33 || p.Y < 4 || p.Y > 19 {
					t.Fatal("port outside bounds", p)
				}
			}
			if pptxShape(d.ID) != d.Preset {
				t.Fatal("native preset omitted")
			}
			if rows := renderShapeRows(Element{Kind: "shape", Query: q.Encode()}, 245); len(rows) == 0 {
				t.Fatal("missing Retro coverage")
			}
		})
	}
}

func TestShapeSpecBoundedAdjustmentsAndAliases(t *testing.T) {
	for alias, want := range map[string]string{"rectangle": "square", "rect": "square", "ellipse": "circle", "roundRect": "rounded", "speech-bubble": "speech-bubble"} {
		if got := shapeSpecFromValues(url.Values{"shape": {alias}}).Kind; got != want {
			t.Fatalf("%s => %s", alias, got)
		}
	}
	for _, raw := range []string{"NaN", "Inf", "-Inf", "bogus", "1e300", "-100"} {
		q := url.Values{"shape": {"arrow-right"}, "shape-depth": {raw}, "shape-neck": {raw}}
		s := shapeSpecFromValues(q)
		if s.Adjustments["depth"] < .1 || s.Adjustments["depth"] > .65 || s.Adjustments["neck"] < .1 || s.Adjustments["neck"] > .8 {
			t.Fatal("unbounded imported parameters")
		}
		if validateShapeAdjustments(Element{Kind: "shape", Query: q.Encode()}) == nil {
			t.Fatal("invalid authored adjustment accepted")
		}
	}
}

func TestShapeGeometryUpdatePersistenceHistoryAndRouting(t *testing.T) {
	for _, master := range []bool{false, true} {
		e := Element{ID: "adjusted", Kind: "shape", Query: "shape=arrow-right&shape-depth=0.2&shape-neck=0.4&left=5&top=4&width=30&height=15"}
		s := newNativeEditorSession("Shapes.md", Deck{Slides: []Slide{{Elements: []Element{e}}}})
		s.masterMode = master
		if master {
			s.deck.Masters.Base.Slide.Elements = []Element{e}
		}
		originalPorts := shapePorts(e, 5, 4)
		changed := e
		q, _ := url.ParseQuery(e.Query)
		q.Set("shape-depth", "0.55")
		q.Set("shape-neck", "0.7")
		q.Set("fg", "#ff0000")
		changed.Query = q.Encode()
		if err := s.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &changed}); err != nil {
			t.Fatal(err)
		}
		get := func(d Deck) Element {
			if master {
				return d.Masters.Base.Slide.Elements[0]
			}
			return d.Slides[0].Elements[0]
		}
		actual := get(s.deck)
		aq, _ := url.ParseQuery(actual.Query)
		if shapeSpecFromValues(aq).Adjustments["depth"] != .55 || aq.Get("fg") != "#ff0000" {
			t.Fatal("geometry or paint not applied")
		}
		if ports := shapePorts(actual, 5, 4); ports[0].Y == originalPorts[0].Y {
			t.Fatal("adjustment did not move the silhouette port")
		}
		raw, err := serializeDeck("Shapes.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Shapes.md", raw)
		if err != nil {
			t.Fatal(err)
		}
		lq, _ := url.ParseQuery(get(loaded).Query)
		if shapeSpecFromValues(lq).Adjustments["neck"] != .7 {
			t.Fatal("adjustments did not survive Markdown")
		}
		if err = s.apply(nativeEditorAction{Action: "undo"}); err != nil {
			t.Fatal(err)
		}
		uq, _ := url.ParseQuery(get(s.deck).Query)
		if shapeSpecFromValues(uq).Adjustments["depth"] != .2 {
			t.Fatal("undo lost original geometry")
		}
		if err = s.apply(nativeEditorAction{Action: "redo"}); err != nil {
			t.Fatal(err)
		}
		invalid := get(s.deck)
		invalid.Query = setQueryValue(invalid.Query, "shape-depth", "NaN")
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "update-element", Element: 0, ElementData: &invalid})
	}
}

func BenchmarkShapeRasterScanlines(b *testing.B) {
	for _, kind := range []string{"circle", "speech-bubble", "cloud"} {
		b.Run(kind, func(b *testing.B) {
			e := Element{Kind: "shape", Query: "shape=" + kind + "&width=400&height=100"}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				renderShapeRows(e, 500)
			}
		})
	}
}

func TestCuratedShapeGeometryFixture(t *testing.T) {
	output := os.Getenv("KEYNOPE_SHAPE_EVIDENCE")
	if output == "" {
		t.Skip("explicit local geometry evidence only")
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	d := Deck{Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{BG: "48;2;238;243;248"}}}
	for i, definition := range shapeDefinitions {
		x, y := float64(5+(i%5)*47), 3+float64(i/5)*8.5
		q := url.Values{"shape": {definition.ID}, "left": {decimal(x)}, "top": {decimal(math.Floor(y))}, "object-offset-y": {decimal(y - math.Floor(y))}, "width": {"34"}, "height": {"5.5"}, "fg": {"#0e7490"}, "outline": {"dark"}}
		captionY := y + 5.8
		d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "shape-" + definition.ID, Kind: "shape", Query: q.Encode()}, Element{ID: "caption-" + definition.ID, Kind: "text", Text: definition.Title, Query: fmt.Sprintf("left=%g&top=%g&object-offset-y=%g&width=44&height=1.5&render=truetype&modern-font=sans&modern-size=22&ttf-size=22&fg=%%23263445", x, math.Floor(captionY), captionY-math.Floor(captionY))})
	}
	label := func(text string) string {
		raw, _ := encodeModernRuns(text, "", "text", []sceneRun{{Text: text}})
		q := url.Values{"modern-font": {"sans"}, "modern-size": {"36"}, "ttf-size": {"36"}, "fg": {"#ffffff"}, "modern-runs": {raw}}
		encoded, _ := json.Marshal(shapeLabelData{Text: text, Query: q.Encode()})
		return base64.StdEncoding.EncodeToString(encoded)
	}
	// A labelled, rotated diagram exercises inscribed regions and attachments.
	d.Slides = append(d.Slides, Slide{BG: "48;2;238;243;248", Elements: []Element{
		{ID: "diagram-source", Kind: "shape", Query: "shape=rounded&shape-radius=0.3&left=12&top=15&width=55&height=15&fg=%230e7490&shape-label=" + url.QueryEscape(label("Start here"))},
		{ID: "diagram-target", Kind: "shape", Query: "shape=hexagon&shape-depth=0.3&left=145&top=20&width=60&height=16&object-rotation=12&fg=%237e22ce&shape-label=" + url.QueryEscape(label("Next step"))},
		{ID: "diagram-obstacle", Kind: "shape", Query: "shape=cloud&left=90&top=14&width=38&height=20&fg=%23f59e0b"},
		{ID: "diagram-line", Kind: "connector", Query: "connector-from=diagram-source&connector-from-side=right&connector-to=diagram-target&connector-to-side=left&connector-mode=elbow&connector-arrows=end&connector-width=1&connector-arrow-width=6&fg=%23334155"},
	}})
	md, err := serializeDeck("Curated shapes.md", d)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(output, "curated-shapes.md"), md, 0644); err != nil {
		t.Fatal(err)
	}
	for _, style := range []string{"modern", "retro"} {
		proof := cloneDeck(d)
		proof.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
		if style == "retro" {
			proof.Theme = nil
		}
		doc, err := makePDFExportDocument("Curated shapes", proof, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(output, style+"-shapes.html"), []byte(doc.FallbackContent), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
