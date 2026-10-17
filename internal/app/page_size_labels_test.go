package app

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestPageSizeLabelPhysicalMetricsAndEffectPadding(t *testing.T) {
	for _, canvas := range []sceneRect{{Width: 1920, Height: 1080}, {Width: 1440, Height: 1080}, {Width: 1080, Height: 1920}, {Width: 3840, Height: 1080}} {
		for _, grid := range [][2]int{{245, 56}, {160, 90}} {
			for _, font := range []string{"c64", "sans", "mono"} {
				label := Element{Kind: "text", Text: "Wide label WWW iii\nSecond line", Query: "render=truetype&modern-font=" + font + "&modern-size=80&modern-width=85&outline=dark"}
				q, _ := url.ParseQuery(label.Query)
				style := (Deck{Appearance: &DeckAppearance{Version: 3}}).modernSceneTextFromValues(label, q)
				longest := math.Max(chartTextWidth("Wide label WWW iii", style), chartTextWidth("Second line", style))
				wantW := int(math.Ceil(math.Ceil(longest+1)*float64(grid[0])/canvas.Width)) + 2*(1+int(math.Ceil(style.Size*.035*float64(grid[0])/canvas.Width)))
				wantH := int(math.Ceil(math.Ceil(2*style.Size*style.LineHeight)*float64(grid[1])/canvas.Height)) + 2*(1+int(math.Ceil(style.Size*.035*float64(grid[1])/canvas.Height)))
				w, h := shapeLabelSize(label, grid[0], grid[1], canvas)
				if w != wantW || h != wantH {
					t.Fatal("label uses wrong physical axes", canvas, grid, font, w, h, wantW, wantH)
				}
			}
		}
	}
}

func TestPageSizeShapeSceneAndSampledSilhouetteShareAspect(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, size := range []DeckPageSize{{1, 1080, 1920}, {1, 3840, 1080}} {
		e := Element{ID: "round", Kind: "shape", Query: "shape=rounded&shape-radius=0.4&left=80&top=12&width=40&height=12"}
		d := Deck{PageSize: size.Clone(), Slides: []Slide{{Elements: []Element{e}}}}
		scene, err := buildDocumentSlideScene(d, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		q, _ := url.ParseQuery(e.Query)
		geometry := shapeGeometry(shapeSpecFromValues(q), 40*float64(size.Width)/245/(12*float64(size.Height)/56))
		if len(scene.Objects) != 1 {
			t.Fatal("unexpected scene objects", scene.Objects)
		}
		actual := scene.Objects[0].ShapeGeometry
		if actual == nil || actual.Path != geometry.Path || len(actual.Contours) != len(geometry.Contours) {
			t.Fatal("scene uses legacy shape aspect", size)
		}
		for i, contour := range geometry.Contours {
			if len(actual.Contours[i]) != len(contour) {
				t.Fatal("shape contour changed")
			}
			for j, p := range contour {
				a := actual.Contours[i][j]
				if math.Abs(a.X-p.X) > 1e-10 || math.Abs(a.Y-p.Y) > 1e-10 {
					t.Fatal("physical shape contour differs", size, i, j)
				}
			}
		}
		for _, pair := range [][2]float64{{actual.TextRegion.X, geometry.TextRegion.X}, {actual.TextRegion.Y, geometry.TextRegion.Y}, {actual.TextRegion.Width, geometry.TextRegion.Width}, {actual.TextRegion.Height, geometry.TextRegion.Height}} {
			if math.Abs(pair[0]-pair[1]) > 1e-10 {
				t.Fatal("physical text region differs", size)
			}
		}
		rows := renderShapeSilhouette(geometry, 80, 24, 0, 0)
		var got []string
		for _, line := range layout(d.ResolveSlide(0, false), 245, 56) {
			if line.Role == "shape" {
				got = append(got, line.Text)
			}
		}
		if !reflect.DeepEqual(got, rows) {
			t.Fatal("sampled silhouette differs from physical scene", size)
		}
		if geometry.Path == shapeGeometryForValues(q).Path {
			t.Fatal("fixture does not distinguish legacy geometry")
		}
	}
}

func TestPageSizeLabelFitCacheAndCatalogueStability(t *testing.T) {
	var cache shapeLabelFitCache
	for _, size := range []DeckPageSize{{1, 1920, 1080}, {1, 1080, 1920}, {1, 3840, 1080}} {
		canvas := (Deck{PageSize: size.Clone()}).physicalCanvas()
		for _, grid := range [][2]int{{245, 56}, {160, 90}} {
			for _, font := range []string{"c64", "sans", "mono"} {
				for _, def := range shapeDefinitions {
					data, _ := encodeShapeLabel(shapeLabelData{Text: "Wide label\nSecond line", Query: "render=truetype&modern-font=" + font + "&modern-size=64"})
					original := Element{ID: "shape", Kind: "shape", Query: setQueryValue(fmt.Sprintf("shape=%s&left=10&top=5&width=3&height=2", def.ID), "shape-label", data)}
					cold, warm := original, original
					fitShapeLabelUncached(&cold, grid[0], grid[1], canvas)
					cache.fit(&warm, grid[0], grid[1], canvas)
					if !reflect.DeepEqual(cold, warm) {
						t.Fatal("cache reused another page size", size, grid, font, def.ID)
					}
					again := original
					if !cache.fit(&again, grid[0], grid[1], canvas) || !reflect.DeepEqual(again, cold) {
						t.Fatal("missing owned cache result")
					}
					fitShapeLabelUncached(&warm, grid[0], grid[1], canvas)
					if !reflect.DeepEqual(cold, warm) {
						t.Fatal("label grows repeatedly", size, grid, font, def.ID)
					}
					label, _ := shapeLabel(cold)
					tw, th := shapeLabelSize(label, grid[0], grid[1], canvas)
					q, _ := url.ParseQuery(cold.Query)
					w, h := shapeHalfCells(q, "width", 12), shapeHalfCells(q, "height", 6)
					if def.ID == "square" || def.ID == "circle" || def.ID == "triangle" || def.ID == "diamond" {
						if !shapeLabelFits(def.ID, w, h, tw*2, th*2) {
							t.Fatal("label does not fit convex shape", size, grid, font, def.ID)
						}
					} else {
						region := shapeGeometry(shapeSpecFromValues(q), float64(w)*canvas.Width/float64(grid[0])/(float64(h)*canvas.Height/float64(grid[1]))).TextRegion
						if float64(w)*region.Width+1e-7 < float64(tw*2) || float64(h)*region.Height+1e-7 < float64(th*2) {
							t.Fatal("label does not fit content region", size, grid, font, def.ID)
						}
					}
				}
			}
		}
	}
}

func TestPageSizeLabelInlinePreviewAndCommittedHistory(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, master := range []bool{false, true} {
		e := labelledShape("square")
		data, _ := encodeShapeLabel(shapeLabelData{Text: "Long label grows right", Query: "render=truetype&modern-font=mono&modern-size=80"})
		e.Query = setQueryValue(e.Query, "shape-label", data)
		e.Query = setQueryValue(e.Query, "height", "15.375")
		d := Deck{PageSize: &DeckPageSize{1, 1080, 1920}, Slides: []Slide{{Elements: []Element{e}}}}
		s := newNativeEditorSession("Labels.md", d)
		if master {
			s.masterMode = true
			s.currentMaster = 0
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
		}
		before := cloneDeck(s.deck)
		target := s.deck.Slides[0].Elements[0]
		if master {
			target = s.deck.Masters.Base.Slide.Elements[0]
		}
		// A real content edit must fit using deck dimensions, not an authored slide
		// copy's absent derived rendering context.
		target.Query = setQueryValue(target.Query, "width", "2.125")
		if err := s.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &target}); err != nil {
			t.Fatal(master, err)
		}
		slide := s.deck.Slides[0]
		if master {
			slide = s.deck.Masters.Base.Slide
		}
		actual := slide.Elements[0]
		expected := target
		fitShapeLabel(&expected, 245, 56, s.deck.physicalCanvas())
		aq, eq := mustQuery(actual.Query), mustQuery(expected.Query)
		if aq.Get("width") != eq.Get("width") || aq.Get("height") != "15.375" || aq.Get("left") != eq.Get("left") || aq.Get("top") != eq.Get("top") {
			t.Fatal("committed fit disagrees with physical fit", master, aq, eq)
		}
		preview := s.deck.applyShapeStrokeDefaults(slide)
		rr := httptest.NewRecorder()
		writeNativeEditorPreview(rr, nativeEditorAction{Name: "inline-edit", Element: 0, ElementData: &target}, preview, nil, 245, 56)
		var response nativeEditorInlinePreview
		if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil || response.ElementData == nil {
			t.Fatal("missing inline fit", err)
		}
		pq := mustQuery(response.ElementData.Query)
		if pq.Get("width") != eq.Get("width") || pq.Get("height") != eq.Get("height") {
			t.Fatal("inline preview uses wrong canvas", pq, eq)
		}
		after := cloneDeck(s.deck)
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("label fit undo not exact", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
			t.Fatal("label fit redo not exact", err)
		}
		encoded, err := serializeDeck("Labels.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Labels.md", encoded)
		if err != nil {
			t.Fatal(err)
		}
		restored := loaded.Slides[0]
		if master {
			restored = loaded.Masters.Base.Slide
		}
		rq := mustQuery(restored.Elements[0].Query)
		if shapeDimension(rq, "width", 0) != shapeDimension(aq, "width", 0) || shapeDimension(rq, "height", 0) != shapeDimension(aq, "height", 0) || strings.Contains(string(encoded), "canvasWidth") {
			t.Fatal("portable shape geometry or derived context leaked", master, rq, aq)
		}
	}
}
