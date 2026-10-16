package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"keynope/internal/pptx"
)

func TestShapeStrokePortsMatchOuterPhysicalBoundary(t *testing.T) {
	const cols, rows = 245, 56
	sx, sy := 1920.0/cols, 1080.0/rows
	for _, angle := range []float64{0, 37.5, 90, 180} {
		e := Element{ID: "box", Kind: "shape", Query: "shape=square&left=50&top=15&width=30&height=10&shape-stroke=%23ffffff&shape-stroke-width=32"}
		q, _ := url.ParseQuery(e.Query)
		q.Set("object-rotation", strconv.FormatFloat(angle, 'f', -1, 64))
		e.Query = q.Encode()
		slide := Slide{Elements: []Element{e}}
		ports := slideShapePorts(slide, layout(slide, cols, rows), cols, rows)
		want := rotateShapePorts([]shapePort{{ID: "box", Side: "top", X: 65, Y: 15 - 16/sy}, {ID: "box", Side: "right", X: 80 + 16/sx, Y: 20}, {ID: "box", Side: "bottom", X: 65, Y: 25 + 16/sy}, {ID: "box", Side: "left", X: 50 - 16/sx, Y: 20}}, e, 50, 15, cols, rows)
		for i, p := range ports {
			if math.Abs(p.X-want[i].X) > 1e-7 || math.Abs(p.Y-want[i].Y) > 1e-7 || p.Direction != want[i].Direction {
				t.Fatalf("rotation %g: %+v != %+v", angle, p, want[i])
			}
		}
		if len(ports) != 4 {
			t.Fatal("missing stroke ports", ports)
		}
	}
}

func TestShapeStrokeZeroWidthSurvivesSceneJSON(t *testing.T) {
	paint := explicitShapeStroke(url.Values{"shape-stroke": {"#ffffff"}, "shape-stroke-width": {"0"}}, scenePaint{})
	data, err := json.Marshal(paint)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if width, ok := value["strokeWidth"]; !ok || width != float64(0) {
		t.Fatal("Off serialized as an inherited width", value)
	}
}

func TestCuratedStrokePortsTouchSameRoutingContour(t *testing.T) {
	for _, definition := range shapeDefinitions {
		for _, stroke := range []float64{.25, 8.25, 64} {
			q := url.Values{"shape": {definition.ID}, "width": {"37.125"}, "height": {"8.875"}, "shape-stroke": {"#ffffff"}, "shape-stroke-width": {strconv.FormatFloat(stroke, 'f', -1, 64)}}
			ports := shapePortsAtStrokeBoundary(shapePortsFromValues("shape", q, 40, 12), q, 40, 12, stroke, 245, 56)
			spec := shapeSpecFromValues(q)
			obstacle := projectConnectorObstacle(connectorObstacle{X: 40, Y: 12, Width: 37.125, Height: 8.875, W: 75, H: 18, Shape: definition.ID, Spec: &spec, StrokeWidth: stroke}, 245, 56)
			for _, port := range ports {
				x, y := port.X, port.Y
				dx, dy := 0.0, 0.0
				switch port.Side {
				case "top":
					dy = -.001 / obstacle.sy
				case "bottom":
					dy = .001 / obstacle.sy
				case "left":
					dx = -.001 / obstacle.sx
				case "right":
					dx = .001 / obstacle.sx
				}
				if !obstacle.contains(x-dx, y-dy) || obstacle.contains(x+dx, y+dy) {
					t.Fatalf("%s width %g %s port does not touch routed silhouette: %+v", definition.ID, stroke, port.Side, port)
				}
			}
		}
	}
}

func TestMasterStrokeRoutingUsesInheritedDefaultsWithoutAuthoringKeys(t *testing.T) {
	d := strokeRouteFixture(t)
	d.Masters = defaultMasterDeck()
	d.Masters.Base.Slide = cloneSlide(d.Slides[0])
	before := cloneDeck(d)
	master := d.masterRenderPreview(0, 245, 56)
	pages := exportSlidePages(master, 0, 1, 245, 56)
	if len(pages) != 1 || len(pages[0].Connectors) != 1 {
		t.Fatal("master route unavailable")
	}
	var wall sceneObject
	for _, o := range pages[0].Scene.Objects {
		if o.ID == "wall" {
			wall = o
		}
	}
	if wall.Paint.StrokeWidth != 20 || wall.Paint.Stroke != "#ff55aa" {
		t.Fatal("master outline default differs from route", wall.Paint)
	}
	obstacles := connectorObstacles(master, layout(master, 245, 56), 245, 56)
	matched := false
	for _, o := range obstacles {
		if o.ID == "wall" {
			matched = true
			if o.StrokeWidth != wall.Paint.StrokeWidth {
				t.Fatal("master obstacle differs from paint")
			}
		}
	}
	if !matched || !reflect.DeepEqual(before, d) {
		t.Fatal("master projection changed source or omitted obstacle")
	}
	query, _ := url.ParseQuery(d.Masters.Base.Slide.Elements[2].Query)
	if query.Has("shape-stroke") || query.Has("shape-stroke-width") {
		t.Fatal("inherited stroke materialized as explicit metadata")
	}
}

func BenchmarkStrokeObstacleCoverage(b *testing.B) {
	for _, stroke := range []float64{0, 20, 64} {
		b.Run(strconv.FormatFloat(stroke, 'f', -1, 64), func(b *testing.B) {
			p := projectConnectorObstacle(connectorObstacle{X: 80, Y: 10, Width: 30, Height: 20, W: 60, H: 40, Shape: "circle", StrokeWidth: stroke}, 245, 56)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = p.contains(70+float64(i%500)/10, 8+float64(i%240)/10)
			}
		})
	}
}

func strokeRouteFixture(t *testing.T) Deck {
	t.Helper()
	label := func(text string) string {
		value, err := encodeShapeLabel(shapeLabelData{Text: text, Query: "render=truetype&modern-font=sans&modern-size=40&ttf-size=40&fg=%23ffffff"})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	d := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "modern"}, Theme: builtinDeckTheme("clean-light")}
	d.Theme.Defaults.ShapeLine = "#ff55aa"
	d.Theme.Defaults.LineWidth = 20
	slide := Slide{BG: "48;2;16;24;34", BGSet: true, Elements: []Element{
		{ID: "title", Kind: "text", Text: "Stroke-aware connections", Query: "top=2&left=14&width=215&render=truetype&modern-font=sans&modern-size=74&ttf-size=74&fg=%23ffffff"},
		{ID: "source", Kind: "shape", Query: "shape=rounded&left=15&top=20&width=45&height=12&fg=%23234359&shape-stroke=%23ffdf66&shape-stroke-width=48&shape-label=" + url.QueryEscape(label("Source"))},
		{ID: "wall", Kind: "shape", Query: "shape=circle&left=103&top=18&width=30&height=16&fg=%23234359&shape-label=" + url.QueryEscape(label("Avoid"))},
		{ID: "target", Kind: "shape", Query: "shape=diamond&left=183&top=20&width=45&height=12&fg=%23234359&shape-stroke=%2355ffff&shape-stroke-width=48&shape-label=" + url.QueryEscape(label("Target"))},
		{ID: "line", Kind: "connector", Query: "connector-from=source&connector-from-side=right&connector-to=target&connector-to-side=left&connector-mode=elbow&connector-width=1&connector-arrows=end&connector-arrow-width=6&fg=%23ffffff"},
		{ID: "caption", Kind: "text", Text: "Dots meet the outer outline. Routes avoid the inherited border.", Query: "left=15&top=44&width=215&render=truetype&modern-font=sans&modern-size=40&ttf-size=40&fg=%23ffffff"},
	}}
	d.Slides = []Slide{slide, cloneSlide(slide)}
	d.Slides[1].DefaultStyle = "retro"
	return d
}

func TestStrokeRouteSceneExportAndHistory(t *testing.T) {
	d := strokeRouteFixture(t)
	for _, index := range []int{0, 1} {
		resolved := d.slideRenderPreview(index, 245, 56)
		pages := exportSlidePages(resolved, index, 2, 245, 56)
		if len(pages) != 1 || len(pages[0].Connectors) != 1 {
			t.Fatal("missing route")
		}
		points := pages[0].Connectors[0].Points
		var scenePoints []connectorPoint
		for _, o := range pages[0].Scene.Objects {
			if o.ID == "line" {
				scenePoints = o.Points
			}
		}
		if len(points) != len(scenePoints) {
			t.Fatal("HTML and scene routing disagree")
		}
		for i, p := range points {
			if math.Abs(p.X*1920/245-scenePoints[i].X) > 1e-7 || math.Abs(p.Y*1080/56-scenePoints[i].Y) > 1e-7 {
				t.Fatal("scaled route differs")
			}
		}
	}
	s := newNativeEditorSession("StrokeRoutes.md", d)
	before := cloneDeck(s.deck)
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"wall"}, Property: "paint.strokeWidth", PropertyValue: json.RawMessage(`0`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("stroke-route edit did not undo atomically", err)
	}
	data, _, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil || len(parsed.Slides) != 2 {
		t.Fatal("invalid routing PPTX", err)
	}
	if root := os.Getenv("KEYNOPE_ROUTE_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(root, "routes.md")
		if err := saveDeck(name, d); err != nil {
			t.Fatal(err)
		}
		html, err := exportHTMLDocument(name, d.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "routes.html"), []byte(html), 0644); err != nil {
			t.Fatal(err)
		}
		legacy := cloneDeck(d)
		legacy.Theme = nil
		legacy.Slides = legacy.Slides[:1]
		legacy.Slides[0].DefaultStyle = "retro"
		for i := range legacy.Slides[0].Elements {
			if legacy.Slides[0].Elements[i].ID == "wall" {
				legacy.Slides[0].Elements[i].Query += "&shape-stroke=%23ff55aa&shape-stroke-width=20"
			}
		}
		blockHTML, err := exportHTMLDocument(name, legacy.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "routes-blocks.html"), []byte(blockHTML), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "routes.pptx"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShapeStrokeObstacleCoverageRotatesWithPhysicalWidth(t *testing.T) {
	for _, angle := range []float64{0, 37.5, 90} {
		o := connectorObstacle{X: 50, Y: 15, Width: 30, Height: 10, W: 60, H: 20, Shape: "square", Rotation: angle, StrokeWidth: 32}
		p := projectConnectorObstacle(o, 245, 56)
		world := func(x, y float64) (float64, float64) {
			dx, dy := (x-p.cx)*p.sx, (y-p.cy)*p.sy
			return p.cx + (dx*p.c-dy*p.s)/p.sx, p.cy + (dx*p.s+dy*p.c)/p.sy
		}
		for _, tc := range []struct {
			x, y   float64
			inside bool
		}{{50 - 15/p.sx, 20, true}, {50 - 17/p.sx, 20, false}, {65, 15 - 15/p.sy, true}, {65, 15 - 17/p.sy, false}, {50 - 10/p.sx, 15 - 10/p.sy, true}, {50 - 14/p.sx, 15 - 14/p.sy, false}} {
			x, y := world(tc.x, tc.y)
			if got := p.contains(x, y); got != tc.inside {
				t.Fatalf("rotation %g point %g,%g coverage=%v", angle, x, y, got)
			}
		}
	}
	// A stroked concave shape must not become a filled rectangular obstacle.
	spec := ShapeSpec{Kind: "chevron"}
	o := connectorObstacle{X: 40, Y: 10, Width: 40, Height: 20, W: 80, H: 40, Shape: "chevron", Spec: &spec, StrokeWidth: 8}
	p := projectConnectorObstacle(o, 245, 56)
	if p.contains(41, 20) {
		t.Fatal("stroke filled the chevron's negative space")
	}
}

func TestStrokeRoutingInheritanceOffAndCacheInvalidation(t *testing.T) {
	d := Deck{Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{Elements: []Element{{ID: "wall", Kind: "shape", Query: "shape=square&left=80&top=15&width=30&height=20&shape-stroke-width=64"}}}}}
	d.Theme.Defaults.ShapeLine = "#ffffff"
	d.Theme.Defaults.LineWidth = 20
	before := cloneDeck(d)
	slide := d.ResolveSlide(0, false)
	lines := layout(slide, 245, 56)
	obstacles := connectorObstacles(slide, lines, 245, 56)
	if len(obstacles) != 1 || obstacles[0].StrokeWidth != 64 {
		t.Fatal("inherited stroke missing", obstacles)
	}
	themed := cloneSlide(slide)
	themed.Elements[0].Query = setQueryValue(themed.Elements[0].Query, "shape-stroke-width", "")
	if got := connectorObstacles(themed, layout(themed, 245, 56), 245, 56)[0].StrokeWidth; got != 20 {
		t.Fatal("theme width not inherited", got)
	}
	e := Element{ID: "route", Kind: "connector", Query: "connector-mode=elbow&connector-width=1"}
	a, b := shapePort{ID: "source", Side: "right", X: 20, Y: 13.75}, shapePort{ID: "target", Side: "left", X: 160, Y: 13.75}
	clear := append([]connectorObstacle(nil), obstacles...)
	clear[0].StrokeWidth = 0
	unoutlined := routedConnectorPath(e, a, b, clear, 245, 56)
	outlined := routedConnectorPath(e, a, b, obstacles, 245, 56)
	if reflect.DeepEqual(unoutlined, outlined) {
		t.Fatal("route cache ignored stroke change", outlined)
	}
	p := projectConnectorObstacle(obstacles[0], 245, 56)
	for i := 1; i < len(outlined); i++ {
		u, v := outlined[i-1], outlined[i]
		steps := max(1, int(math.Ceil((math.Abs(v.X-u.X)+2*math.Abs(v.Y-u.Y))*8)))
		for j := 0; j <= steps; j++ {
			f := float64(j) / float64(steps)
			x, y := u.X+(v.X-u.X)*f, u.Y+(v.Y-u.Y)*f
			if p.contains(x, y) {
				t.Fatal("route crosses thick outline", outlined)
			}
		}
	}
	for _, off := range []string{"shape-stroke=none", "shape-stroke-width=0", "outline=none"} {
		q, _ := url.ParseQuery(before.Slides[0].Elements[0].Query)
		values, _ := url.ParseQuery(off)
		for key, value := range values {
			q[key] = value
		}
		slide.Elements[0].Query = q.Encode()
		if got := connectorObstacles(slide, layout(slide, 245, 56), 245, 56)[0].StrokeWidth; got != 0 {
			t.Fatal("Off did not disable routing stroke", off, got)
		}
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("projection mutated authored deck")
	}
	payload, _ := json.Marshal(d.ResolveSlide(0, false))
	var roundtrip Slide
	if err := json.Unmarshal(payload, &roundtrip); err != nil || roundtrip.ShapeStrokeDefaults != nil {
		t.Fatal("derived defaults became portable slide overrides", err)
	}
	copy := cloneSlide(d.ResolveSlide(0, false))
	copy.ShapeStrokeDefaults.Width = 1
	if d.Theme.Defaults.LineWidth != 20 {
		t.Fatal("derived defaults aliased theme")
	}
	d.StoreResolvedSlide(0, d.ResolveSlide(0, true))
	if d.Slides[0].ShapeStrokeDefaults != nil {
		t.Fatal("render defaults entered the authored deck")
	}
}
