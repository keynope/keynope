package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectorForegroundPreviewAndInheritedColor(t *testing.T) {
	slide := connectorFixture()
	slide.FG = "38;2;85;170;255"
	e := &slide.Elements[2]
	e.Query = setQueryValue(e.Query, "fg", "")
	e.Query = setQueryValue(e.Query, "connector-arrows", "both")
	lines := displayLines(slide, 245, 56, 0)
	seenConnector := false
	for _, line := range lines {
		if line.Role == "connector" {
			seenConnector = true
		} else if seenConnector {
			t.Fatal("shape content painted over connector")
		}
	}
	if !seenConnector {
		t.Fatal("connector missing")
	}
	ports := slideShapePorts(slide, lines, 245, 56)
	a, b, ok := connectorEndpoints(*e, ports)
	if !ok {
		t.Fatal("missing endpoints")
	}
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{slide}})
	before, _ := json.Marshal(session.state())
	body, _ := json.Marshal(map[string]any{"element": e, "from": a, "to": b, "cols": 245, "rows": 56})
	w := httptest.NewRecorder()
	session.handleConnectorPreview(w, httptest.NewRequest(http.MethodPost, "/api/editor/connector-preview", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var preview struct {
		Lines  []exportLine     `json:"lines"`
		Points []connectorPoint `json:"points"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Lines) == 0 || len(preview.Points) < 2 {
		t.Fatal("missing preview")
	}
	for _, line := range preview.Lines {
		for _, part := range line.Parts {
			if part.Color != "rgb(85,170,255)" {
				t.Fatal("did not inherit slide color", part.Color)
			}
		}
	}
	expected := exportLines(connectorPathLines(*e, -1, connectorPath(*e, a, b), connectorRasterBounds{a, b, connectorObstacles(slide, lines)}), slide, 245, 56, 1)
	want, _ := json.Marshal(expected)
	got, _ := json.Marshal(preview.Lines)
	if !bytes.Equal(want, got) {
		t.Fatal("preview differs from saved connector raster")
	}
	after, _ := json.Marshal(session.state())
	if !bytes.Equal(before, after) {
		t.Fatal("preview mutated the document")
	}
}

func TestConnectorMinimumWidthAndContinuousSemiBlocks(t *testing.T) {
	if got := connectorNumber(url.Values{"connector-width": {"0.5"}}, "connector-width", 1, 8); got != 1 {
		t.Fatal(got)
	}
	// A shallow or steep diagonal must join edge-to-edge, not isolated corners.
	for _, points := range [][]connectorPoint{{{5, 5}, {40, 12}}, {{5, 5}, {12, 40}}, {{40, 5}, {5, 40}}, {{5, 5}, {25, 5}, {25, 20}, {40, 20}}} {
		pixels := map[[2]int]bool{}
		for _, line := range connectorPathLines(Element{Kind: "connector"}, 0, points) {
			for x, r := range []rune(line.Text) {
				for mask := 0; mask < 16; mask++ {
					if quadrantRune(mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0) != r {
						continue
					}
					for bit := 0; bit < 4; bit++ {
						if mask&(1<<bit) != 0 {
							pixels[[2]int{(line.Col+x)*2 + bit%2, line.Row*2 + bit/2}] = true
						}
					}
					break
				}
			}
		}
		if len(pixels) == 0 {
			t.Fatal("empty line")
		}
		var seed [2]int
		for p := range pixels {
			seed = p
			break
		}
		queue := [][2]int{seed}
		delete(pixels, seed)
		for len(queue) > 0 {
			p := queue[0]
			queue = queue[1:]
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				next := [2]int{p[0] + d[0], p[1] + d[1]}
				if pixels[next] {
					delete(pixels, next)
					queue = append(queue, next)
				}
			}
		}
		if len(pixels) > 0 {
			t.Fatalf("disconnected semi-block line: %v", points)
		}
	}
}

func TestConnectorHorizontalHalfVerticalThickness(t *testing.T) {
	for _, width := range []string{"1", "2", "3", "4", "6", "8"} {
		e := Element{Kind: "connector", Query: "connector-width=" + width}
		var thickness [2]int
		for axis, points := range [][]connectorPoint{{{10, 10}, {40, 10}}, {{10, 10}, {10, 40}}} {
			min, max := 100000, -100000
			for _, line := range connectorPathLines(e, 0, points) {
				for x, r := range []rune(line.Text) {
					for mask := 1; mask < 16; mask++ {
						if quadrantRune(mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0) != r {
							continue
						}
						for bit := 0; bit < 4; bit++ {
							if mask&(1<<bit) == 0 {
								continue
							}
							p := line.Row*2 + bit/2
							if axis == 1 {
								p = (line.Col+x)*2 + bit%2
							}
							if p < min {
								min = p
							}
							if p > max {
								max = p
							}
						}
					}
				}
			}
			thickness[axis] = max - min + 1
		}
		if thickness[0]*2 != thickness[1] {
			t.Fatalf("width %s: horizontal %d, vertical %d half-cells", width, thickness[0], thickness[1])
		}
	}
}

func TestConnectorCapsMeetShapeWithoutOverlap(t *testing.T) {
	for _, kind := range []string{"square", "circle", "triangle", "diamond"} {
		shape := Element{Kind: "shape", ID: "shape", Query: "shape=" + kind + "&width=20&height=12&shape-offset-x=0.5&shape-offset-y=0.5"}
		obstacle := connectorObstacle{X: 40.5, Y: 20.5, W: 40, H: 24, Shape: kind, ID: "shape"}
		for _, port := range shapePorts(shape, 40, 20) {
			outside := port
			outside.ID = "outside"
			switch port.Side {
			case "left":
				outside.X -= 20
				outside.Side = "right"
			case "right":
				outside.X += 20
				outside.Side = "left"
			case "top":
				outside.Y -= 15
				outside.Side = "bottom"
			case "bottom":
				outside.Y += 15
				outside.Side = "top"
			}
			for _, width := range []string{"1", "4", "8"} {
				for _, arrows := range []string{"none", "both"} {
					for _, reverse := range []bool{false, true} {
						a, b := port, outside
						if reverse {
							a, b = b, a
						}
						points := []connectorPoint{{a.X, a.Y}, {b.X, b.Y}}
						e := Element{Kind: "connector", Query: "connector-width=" + width + "&connector-arrows=" + arrows + "&connector-arrow-width=6"}
						lines := connectorPathLines(e, 0, points, connectorRasterBounds{a, b, []connectorObstacle{obstacle}})
						touch := false
						for _, line := range lines {
							for col, r := range []rune(line.Text) {
								for mask := 1; mask < 16; mask++ {
									if quadrantRune(mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0) != r {
										continue
									}
									for bit := 0; bit < 4; bit++ {
										if mask&(1<<bit) == 0 {
											continue
										}
										x, y := float64(line.Col+col)+float64(bit%2)/2, float64(line.Row)+float64(bit/2)/2
										sx, sy := int(math.Floor((x+.25-obstacle.X)*2)), int(math.Floor((y+.25-obstacle.Y)*2))
										if sx >= 0 && sy >= 0 && sx < obstacle.W && sy < obstacle.H && shapeCellFilled(kind, sx, sy, obstacle.W, obstacle.H) {
											t.Fatalf("%s %s width %s arrows %s paints inside shape", kind, port.Side, width, arrows)
										}
										switch port.Side {
										case "left":
											touch = touch || x+.5 == port.X && math.Abs(y+.25-port.Y) <= .5
										case "right":
											touch = touch || x == port.X && math.Abs(y+.25-port.Y) <= .5
										case "top":
											touch = touch || y+.5 == port.Y && math.Abs(x+.25-port.X) <= .5
										case "bottom":
											touch = touch || y == port.Y && math.Abs(x+.25-port.X) <= .5
										}
									}
								}
							}
						}
						if !touch {
							t.Fatalf("%s %s width %s arrows %s has gap at endpoint (reversed=%v)", kind, port.Side, width, arrows, reverse)
						}
					}
				}
			}
		}
	}
}

func connectorFixture() Slide {
	return Slide{Elements: []Element{
		{Kind: "shape", ID: "a", Query: "shape=circle&left=20&top=5&width=30&height=12"},
		{Kind: "shape", ID: "b", Query: "shape=triangle&left=100&top=20&width=40&height=20"},
		{Kind: "connector", ID: "line", Query: "connector-from=a&connector-from-side=right&connector-to=b&connector-to-side=left&fg=%23ffffff"},
	}}
}

func TestShapeConnectorSilhouettePorts(t *testing.T) {
	for _, kind := range []string{"circle", "square", "triangle", "diamond"} {
		e := Element{Kind: "shape", ID: "a", Query: "shape=" + kind + "&width=20&height=10&shape-offset-x=0.5"}
		ports := shapePorts(e, 10, 5)
		if len(ports) != 4 {
			t.Fatalf("%s ports: %v", kind, ports)
		}
		for _, p := range ports {
			if p.X < 10.5 || p.X > 30.5 || p.Y < 5 || p.Y > 15 {
				t.Fatalf("out of bounds: %+v", p)
			}
			if (p.Side == "top" || p.Side == "bottom") && p.X != 20.5 {
				t.Fatal("not centred")
			}
		}
		if kind == "triangle" && ports[3].X <= 10.5 {
			t.Fatal("triangle side must hit silhouette, not box")
		}
	}
}

func TestShapeConnectorFollowsAndRoundTrips(t *testing.T) {
	oldW, oldH := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	defer func() { authoredTerminalWidth, authoredTerminalHeight = oldW, oldH }()
	slide := connectorFixture()
	lines := layout(slide, 245, 56)
	a, b, ok := connectorEndpoints(slide.Elements[2], slideShapePorts(slide, lines, 245, 56))
	if !ok {
		t.Fatal("missing endpoints")
	}
	if a.X != 50 || a.Y != 11 || b.X <= 100 {
		t.Fatalf("unexpected ports %+v %+v", a, b)
	}
	count := 0
	for _, line := range lines {
		if line.Role == "connector" {
			count++
		}
	}
	if count == 0 {
		t.Fatal("no connector rendered")
	}
	slide.Elements[0].Query = setQueryValue(slide.Elements[0].Query, "left", "40")
	slide.Elements[0].Query = setQueryValue(slide.Elements[0].Query, "width", "50")
	moved, _, _ := connectorEndpoints(slide.Elements[2], slideShapePorts(slide, layout(slide, 245, 56), 245, 56))
	if moved.X != 90 {
		t.Fatalf("connector stayed behind: %+v", moved)
	}
	path := filepath.Join(t.TempDir(), "connections.md")
	data, err := serializeDeck(path, Deck{Slides: []Slide{slide}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[connector]") || !strings.Contains(string(data), "element-id=a") {
		t.Fatal(string(data))
	}
	deck, err := parseDeckData(path, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range deck.Slides[0].Elements {
		if e.Kind == "connector" && !validShapeConnector(e, deck.Slides[0]) {
			t.Fatal("lost saved identity")
		}
	}
	copy := append([]Element(nil), slide.Elements...)
	freshPastedConnectorIDs(copy)
	q, _ := url.ParseQuery(copy[2].Query)
	if q.Get("connector-from") != copy[0].ID || q.Get("connector-to") != copy[1].ID || copy[0].ID == "a" {
		t.Fatal("paste references original")
	}
	slide.Elements = slide.Elements[1:]
	for _, line := range layout(slide, 245, 56) {
		if line.Role == "connector" {
			t.Fatal("dangling line should be hidden")
		}
	}
}

func TestShapeConnectorAtBottomDoesNotPaginate(t *testing.T) {
	oldW, oldH := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	defer func() { authoredTerminalWidth, authoredTerminalHeight = oldW, oldH }()
	slide := connectorFixture()
	slide.Elements[0].Query = "shape=square&left=10&top=40&width=30&height=16"
	slide.Elements[2].Query = setQueryValue(slide.Elements[2].Query, "connector-from-side", "bottom")
	if got := len(displayPages(slide, 245, 56)); got != 1 {
		t.Fatalf("connector at canvas edge created %d pages", got)
	}
}

func TestNativeShapeConnectorUndo(t *testing.T) {
	slide := connectorFixture()
	connector := slide.Elements[2]
	slide.Elements = slide.Elements[:2]
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "deck.md"), Deck{Slides: []Slide{slide}})
	if err := session.apply(nativeEditorAction{Action: "add-element", Kind: "connector", ElementData: &connector}); err != nil {
		t.Fatal(err)
	}
	if len(session.state().Slides[0].Elements) != 3 {
		t.Fatal("not inserted")
	}
	if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if len(session.state().Slides[0].Elements) != 2 {
		t.Fatal("not undone")
	}
	if err := session.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if len(session.state().Slides[0].Elements) != 3 {
		t.Fatal("not redone")
	}
}

func TestElbowConnectorAvoidsSilhouette(t *testing.T) {
	slide := connectorFixture()
	slide.Elements[0].Query = "shape=square&left=10&top=20&width=20&height=10"
	slide.Elements[1].Query = "shape=square&left=160&top=20&width=20&height=10"
	slide.Elements[2].Query = "connector-mode=elbow&connector-from=a&connector-from-side=right&connector-to=b&connector-to-side=left"
	slide.Elements = append(slide.Elements, Element{Kind: "shape", ID: "obstacle", Query: "shape=circle&left=80&top=12&width=30&height=26"})
	lines := layout(slide, 245, 56)
	ports := slideShapePorts(slide, lines, 245, 56)
	a, b, _ := connectorEndpoints(slide.Elements[2], ports)
	points := routedConnectorPath(slide.Elements[2], a, b, connectorObstacles(slide, lines), 245, 56)
	if len(points) < 4 {
		t.Fatal(points)
	}
	for i := 1; i < len(points); i++ {
		p, n := points[i-1], points[i]
		if p.X != n.X && p.Y != n.Y {
			t.Fatal("diagonal segment", points)
		}
		for j := 0; j <= 1000; j++ {
			f := float64(j) / 1000
			x, y := p.X+(n.X-p.X)*f, p.Y+(n.Y-p.Y)*f
			px, py := int(math.Floor((x-80)*2)), int(math.Floor((y-12)*2))
			if px >= 0 && py >= 0 && px < 60 && py < 52 && shapeCellFilled("circle", px, py, 60, 52) {
				t.Fatal("crosses obstacle", points)
			}
		}
	}
	// A wall spanning the viewport is traversable as a last resort.
	slide.Elements[3].Query = "shape=square&left=80&top=0&width=30&height=56"
	if paths := slideShapeConnectors(slide, layout(slide, 245, 56), 245, 56); len(paths) != 1 || len(paths[0].Points) < 2 {
		t.Fatal("blocked route disappeared")
	}
}

func TestInsertedShapesHaveStableConnectorAnchors(t *testing.T) {
	session := newNativeEditorSession(filepath.Join(t.TempDir(), "new.md"), Deck{Slides: []Slide{{}}})
	for _, name := range []string{"circle", "square"} {
		if err := session.apply(nativeEditorAction{Action: "add-element", Kind: "shape", Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	state := session.state()
	shapes := state.Slides[0].Elements
	if shapes[0].ID == "" || shapes[1].ID == "" || shapes[0].ID == shapes[1].ID {
		t.Fatal("inserted shapes lack stable identities", shapes)
	}
	e := Element{Kind: "connector", Query: url.Values{"connector-from": {shapes[0].ID}, "connector-to": {shapes[1].ID}, "connector-from-side": {"right"}, "connector-to-side": {"left"}, "connector-mode": {"elbow"}}.Encode()}
	if err := session.apply(nativeEditorAction{Action: "add-element", Kind: "connector", ElementData: &e}); err != nil {
		t.Fatal(err)
	}
	for _, left := range []string{"80", "140", "20"} {
		state = session.state()
		index := nativeEditorElementIndex(state.Slides[0].Elements, -1, shapes[0].ID)
		moved := state.Slides[0].Elements[index]
		moved.Query = setQueryValue(moved.Query, "left", left)
		moved.Query = setQueryValue(moved.Query, "top", "20")
		if err := session.apply(nativeEditorAction{Action: "update-elements", ElementIndices: []int{index}, ElementsData: []Element{moved}}); err != nil {
			t.Fatal(err)
		}
		state = session.state()
		slide := state.Resolved[0]
		if paths := slideShapeConnectors(slide, displayLines(slide, 245, 56, 0), 245, 56); len(paths) != 1 {
			t.Fatal("connector lost after move", left, paths)
		}
	}
}

func TestElbowManualRouteAndWidths(t *testing.T) {
	slide := connectorFixture()
	e := &slide.Elements[2]
	e.Query = setQueryValue(e.Query, "connector-mode", "elbow")
	ports := slideShapePorts(slide, layout(slide, 245, 56), 245, 56)
	a, b, _ := connectorEndpoints(*e, ports)
	points := []connectorPoint{{a.X, a.Y}, {70, a.Y}, {70, b.Y}, {b.X, b.Y}}
	encoded, _ := json.Marshal(points)
	e.Query = setQueryValue(e.Query, "connector-route", string(encoded))
	e.Query = setQueryValue(e.Query, "connector-arrows", "both")
	e.Query = setQueryValue(e.Query, "connector-width", "2")
	e.Query = setQueryValue(e.Query, "connector-arrow-width", "8")
	data, err := serializeDeck("test.md", Deck{Slides: []Slide{slide}})
	if err != nil {
		t.Fatal(err)
	}
	deck, err := parseDeckData("test.md", data)
	if err != nil {
		t.Fatal(err)
	}
	var restored Element
	for _, item := range deck.Slides[0].Elements {
		if item.Kind == "connector" {
			restored = item
		}
	}
	q, _ := url.ParseQuery(restored.Query)
	if len(decodeConnectorRoute(q.Get("connector-route"))) != 4 || q.Get("connector-arrow-width") != "8" {
		t.Fatal("lost controls", string(data))
	}
	a.X += 20
	a.Y += 3
	b.Y -= 4
	route := connectorPath(restored, a, b)
	if route[1].X != 70 || route[1].Y != a.Y || route[2].Y != b.Y {
		t.Fatal("route did not re-anchor", route)
	}
	count := func(lines []Line) int {
		n := 0
		for _, l := range lines {
			if l.Role == "connector" {
				for _, r := range l.Text {
					if r != ' ' {
						n++
					}
				}
			}
		}
		return n
	}
	wide := count(layout(slide, 245, 56))
	e.Query = setQueryValue(e.Query, "connector-arrow-width", "4")
	if smallArrows := count(layout(slide, 245, 56)); smallArrows >= wide {
		t.Fatalf("arrow width did not add ink %d >= %d", smallArrows, wide)
	}
	e.Query = setQueryValue(e.Query, "connector-arrows", "none")
	wide = count(layout(slide, 245, 56))
	e.Query = setQueryValue(e.Query, "connector-width", ".5")
	if thin := count(layout(slide, 245, 56)); wide <= thin {
		t.Fatalf("line width did not add ink %d <= %d", wide, thin)
	}
}
