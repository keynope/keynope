package app

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func TestPageSizeConnectorBrowserFixture(t *testing.T) {
	output := os.Getenv("KEYNOPE_PAGE_SIZE_PPTX_PROOF")
	if output == "" {
		t.Skip("optional browser fixture")
	}
	useTestAuthoredSize(t, 245, 56)
	d := Deck{PageSize: &DeckPageSize{1, 1080, 1920}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{Elements: []Element{
		{ID: "box", Kind: "shape", Query: "shape=square&left=80&top=20&width=30&height=10&object-rotation=37.5&fg=%230e7490&shape-stroke=%23ff55aa&shape-stroke-width=12"},
		{ID: "end", Kind: "shape", Query: "shape=rounded&left=155&top=36&width=50&height=10&fg=%230e7490&shape-stroke=%23ff55aa&shape-stroke-width=12"},
		{ID: "wire", Kind: "connector", Query: "connector-from=box&connector-from-side=right&connector-to=end&connector-to-side=left&connector-mode=elbow&connector-arrows=end&connector-width=2&connector-arrow-width=6&fg=%23ff0055"},
	}}}}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(output, "diagram.md"), d); err != nil {
		t.Fatal(err)
	}
	right, _ := encodeShapeLabel(shapeLabelData{Text: "Wide label", Query: "render=truetype&modern-font=mono&modern-size=64&fg=%23ffffff"})
	down, _ := encodeShapeLabel(shapeLabelData{Text: "One\nTwo\nThree", Query: "render=truetype&modern-font=mono&modern-size=64&fg=%23ffffff"})
	d.Slides[0].Elements = []Element{
		{ID: "label-right", Kind: "shape", Query: setQueryValue("shape=rounded&shape-radius=0.4&left=30&top=7&width=3&height=9&fg=%230e7490", "shape-label", right)},
		{ID: "label-down", Kind: "shape", Query: setQueryValue("shape=square&left=30&top=30&width=100&height=2&fg=%237c3aed", "shape-label", down)},
	}
	if err := saveDeck(filepath.Join(output, "labels.md"), d); err != nil {
		t.Fatal(err)
	}
	d.Appearance = &DeckAppearance{Version: 3}
	d.Slides[0].Elements = nil
	for i, font := range []string{"c64", "sans", "mono", "mono", "sans"} {
		kind := "text"
		if i == 3 {
			kind = "code"
		}
		if i == 4 {
			kind = "bullet"
		}
		d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "natural-" + strconv.Itoa(i), Kind: kind, Text: []string{"C64 body\nSecond line", "Sans body\nSecond line", "Mono body\nSecond line", "print(42)\nreturn true", "First item\nSecond item"}[i], Query: fmt.Sprintf("render=truetype&modern-font=%s&modern-size=64&modern-width=80&align=center&top=%d", font, 3+i*10)})
	}
	if err := saveDeck(filepath.Join(output, "natural.md"), d); err != nil {
		t.Fatal(err)
	}
	d.Slides[0].Elements = []Element{
		{ID: "wrap-c64", Kind: "text", Text: "Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda.", Query: "render=truetype&modern-font=c64&modern-size=48&width=90&left=15&top=2"},
		{ID: "wrap-sans", Kind: "text", Text: "Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda.", Query: "render=truetype&modern-font=sans&modern-size=48&modern-width=80&width=90&left=135&top=2"},
		{ID: "wrap-code", Kind: "code", Text: "abcdefghijklmno0123456789_abcdefghijklmno\nprint(42)", Query: "render=truetype&modern-font=mono&modern-size=48&modern-width=80&width=90&left=15&top=19"},
		{ID: "wrap-bullets", Kind: "bullet", Text: "First item with several words to wrap around\nSecond item with several words to wrap around", Query: "render=truetype&modern-font=mono&modern-size=48&modern-width=80&width=90&left=135&top=19"},
		{ID: "wrap-emoji", Kind: "text", Text: "Hello 👨‍👩‍👧‍👦 and hello 😍 from all of us together again.", Query: "render=truetype&modern-font=sans&modern-size=48&modern-width=80&width=90&left=15&top=39"},
	}
	if err := saveDeck(filepath.Join(output, "wrap.md"), d); err != nil {
		t.Fatal(err)
	}
}

func TestPageSizeRotatedStrokePortsAndObstaclesUsePhysicalAxes(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, size := range []DeckPageSize{{1, 1920, 1080}, {1, 1440, 1080}, {1, 1080, 1920}, {1, 2560, 1080}} {
		for _, angle := range []float64{0, 37.5, 90} {
			e := Element{ID: "box", Kind: "shape", Query: "shape=square&left=80&top=20&width=30&height=10&shape-stroke=%23ffffff&shape-stroke-width=12"}
			e.Query = setQueryValue(e.Query, "object-rotation", strconv.FormatFloat(angle, 'f', -1, 64))
			d := Deck{PageSize: size.Clone(), Slides: []Slide{{Elements: []Element{e}}}}
			before := cloneDeck(d)
			slide := d.ResolveSlide(0, false)
			lines := layout(slide, 245, 56)
			ports := slideShapePorts(slide, lines, 245, 56)
			if len(ports) != 4 {
				t.Fatal("missing ports", size, angle, ports)
			}
			sx, sy := float64(size.Width)/245, float64(size.Height)/56
			cx, cy := 95*sx, 25*sy
			local := [][2]float64{{0, -5*sy - 6}, {15*sx + 6, 0}, {0, 5*sy + 6}, {-15*sx - 6, 0}}
			c, s := math.Cos(angle*math.Pi/180), math.Sin(angle*math.Pi/180)
			for i, p := range ports {
				wantX, wantY := cx+local[i][0]*c-local[i][1]*s, cy+local[i][0]*s+local[i][1]*c
				if math.Abs(p.X*sx-wantX) > 1e-7 || math.Abs(p.Y*sy-wantY) > 1e-7 {
					t.Fatalf("%+v angle %g port %s: got %g,%g want %g,%g", size, angle, p.Side, p.X*sx, p.Y*sy, wantX, wantY)
				}
			}
			obstacles := connectorObstacles(slide, lines, 245, 56)
			if len(obstacles) != 1 {
				t.Fatal("missing obstacle")
			}
			projected := projectConnectorObstacle(obstacles[0], 245, 56)
			rx, ry := math.Abs(c)*15*sx+math.Abs(s)*5*sy+6, math.Abs(s)*15*sx+math.Abs(c)*5*sy+6
			if math.Abs(projected.left*sx-(cx-rx)) > 1e-7 || math.Abs(projected.bottom*sy-(cy+ry)) > 1e-7 {
				t.Fatal("obstacle uses wrong physical bounds", size, angle, projected)
			}
			// Independently rotate an interior point and an exterior point back into
			// grid coordinates. Both collision directions must use the same canvas.
			for _, sample := range []struct {
				x, y   float64
				inside bool
			}{{14 * sx, 4 * sy, true}, {15*sx + 20, 0, false}} {
				x, y := (cx+sample.x*c-sample.y*s)/sx, (cy+sample.x*s+sample.y*c)/sy
				if projected.contains(x, y) != sample.inside {
					t.Fatal("rotated collision mismatch", size, angle, sample)
				}
			}
			key, ok := sceneLayoutKey(slide, 245, 56)
			legacy := slide
			legacy.canvasWidth, legacy.canvasHeight = 0, 0
			oldKey, oldOK := sceneLayoutKey(legacy, 245, 56)
			if !ok || !oldOK || (size.Width != 1920 || size.Height != 1080) && key == oldKey {
				t.Fatal("layout cache ignores physical axes")
			}
			if !reflect.DeepEqual(d, before) {
				t.Fatal("geometry mutated source")
			}
		}
	}
}

func TestPageSizeRoutingArrowStubsAndCacheHaveCanvasOwnership(t *testing.T) {
	a, b := shapePort{ID: "a", X: 60, Y: 35, Side: "top"}, shapePort{ID: "b", X: 160, Y: 15, Side: "bottom"}
	e := Element{Kind: "connector", Query: "connector-mode=elbow&connector-arrows=both&connector-arrow-width=20&connector-width=2"}
	var previousKey [32]byte
	for i, canvas := range []sceneRect{{Width: 1920, Height: 1080}, {Width: 1080, Height: 1920}, {Width: 3840, Height: 1080}, {Width: 1920, Height: 1080}} {
		path := routedConnectorPath(e, a, b, nil, 245, 56, canvas)
		want := math.Max(15, 20*(canvas.Width/245)/(canvas.Height/56))
		if len(path) < 4 || math.Abs(a.Y-path[1].Y-want) > 1e-9 || math.Abs(path[len(path)-2].Y-b.Y-want) > 1e-9 {
			t.Fatal("wrong physical arrow reserve", canvas, path, want)
		}
		pass := connectorRoutingPass{canvas: canvas}
		key := pass.key(a, b, 245, 56, 2, "both", 20.)
		if i > 0 && key == previousKey {
			t.Fatal("route cache omits canvas")
		}
		previousKey = key
	}
}

func TestPageSizeCurvedPortSilhouetteUsesActiveGrid(t *testing.T) {
	for _, dims := range [][2]int{{245, 56}, {160, 90}} {
		for _, size := range []DeckPageSize{{1, 1080, 1920}, {1, 2560, 1080}} {
			e := Element{ID: "rounded", Kind: "shape", Query: "shape=rounded&shape-radius=0.4&left=50&top=15&width=30&height=10&shape-stroke=%23ffffff&shape-stroke-width=12"}
			d := Deck{PageSize: size.Clone(), Slides: []Slide{{Elements: []Element{e}}}}
			slide := d.ResolveSlide(0, false)
			lines := layout(slide, dims[0], dims[1])
			ports := slideShapePorts(slide, lines, dims[0], dims[1])
			obstacles := connectorObstacles(slide, lines, dims[0], dims[1])
			if len(obstacles) != 1 || len(ports) != 4 {
				t.Fatal("missing curved geometry")
			}
			p := projectConnectorObstacle(obstacles[0], dims[0], dims[1])
			q, _ := url.ParseQuery(e.Query)
			if !reflect.DeepEqual(p.geometry, shapeGeometry(shapeSpecFromValues(q), 30*p.sx/(10*p.sy))) {
				t.Fatal("wrong curved aspect")
			}
			for _, port := range ports {
				dx, dy := 0., 0.
				switch port.Side {
				case "top":
					dy = -.001 / p.sy
				case "bottom":
					dy = .001 / p.sy
				case "left":
					dx = -.001 / p.sx
				case "right":
					dx = .001 / p.sx
				}
				if !p.contains(port.X-dx, port.Y-dy) || p.contains(port.X+dx, port.Y+dy) {
					t.Fatal("port does not touch curved contour", size, dims, port)
				}
			}
		}
	}
}

func TestPageSizeWideConnectorClearance(t *testing.T) {
	for _, canvas := range []sceneRect{{Width: 1080, Height: 1920}, {Width: 3840, Height: 1080}} {
		a, b := shapePort{ID: "a", X: 10, Y: 24, Side: "right"}, shapePort{ID: "b", X: 230, Y: 24, Side: "left"}
		obstacle := connectorObstacle{ID: "thin", X: 120, Y: 26.5, Width: .08, Height: .08, Shape: "square", CanvasWidth: canvas.Width, CanvasHeight: canvas.Height}
		line := Element{Kind: "connector", Query: "connector-mode=elbow&connector-width=8&connector-arrows=both&connector-arrow-width=20"}
		route := routedConnectorPath(line, a, b, []connectorObstacle{obstacle}, 245, 56, canvas)
		if len(route) < 4 {
			t.Fatal("missing route")
		}
		index := indexConnectorObstacles([]projectedConnectorObstacle{projectConnectorObstacle(obstacle, 245, 56)})
		rx, ry := 4., math.Max(2, 4*(canvas.Width/245)/(canvas.Height/56))
		for i := 1; i < len(route); i++ {
			p, n := route[i-1], route[i]
			if p.X != n.X && p.Y != n.Y {
				t.Fatal("non-orthogonal route")
			}
			if index.intersectsRect(math.Min(p.X, n.X)-rx, math.Min(p.Y, n.Y)-ry, math.Max(p.X, n.X)+rx, math.Max(p.Y, n.Y)+ry) {
				t.Fatal("wide connector crosses avoidable obstacle", canvas, route)
			}
		}
	}
}
