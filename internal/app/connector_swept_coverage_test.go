package app

import (
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSweptConnectorCoverageFindsThinSilhouettesBetweenRails(t *testing.T) {
	obstacles := []projectedConnectorObstacle{projectConnectorObstacle(connectorObstacle{X: 50.1, Y: 20.5, Width: .05, Height: .05, Shape: "square"}, 245, 56)}
	index := indexConnectorObstacles(obstacles)
	// Old centre and +/- width/2 rails all miss this actual painted obstacle.
	for _, y := range []float64{20, 21, 22} {
		if index.contains(50.12, y) {
			t.Fatal("negative control does not miss obstacle")
		}
	}
	if !index.intersectsRect(49.9, 20, 50.4, 22) {
		t.Fatal("swept line stepped over narrow shape")
	}
	if index.intersectsRect(49.9, 22.01, 50.4, 23) {
		t.Fatal("clear stroke reported blocked")
	}
}

func TestSweptConnectorCoverageMatchesSampledSilhouettes(t *testing.T) {
	for _, kind := range []string{"square", "circle", "star", "brace", "speech-bubble"} {
		for _, rotation := range []float64{0, 37.5, 90} {
			for _, stroke := range []float64{0, 12} {
				p := projectConnectorObstacle(connectorObstacle{X: 40, Y: 12, Width: 30, Height: 16, Shape: kind, Rotation: rotation, StrokeWidth: stroke}, 245, 56)
				for y := 8.; y < 34; y += .7 {
					for x := 32.; x < 78; x += .7 {
						if p.contains(x, y) && !p.intersectsRect(x-.03, y-.03, x+.03, y+.03) {
							t.Fatal("coverage hole", kind, rotation, stroke, x, y)
						}
					}
				}
				if p.intersectsRect(0, 0, 1, 1) {
					t.Fatal("distant sweep intersects", kind)
				}
			}
		}
	}
}

func TestWideConnectorRoutesReserveCompleteStrokeAndArrowLegs(t *testing.T) {
	for _, canvas := range [][2]int{{245, 56}, {160, 90}} {
		cols, rows := canvas[0], canvas[1]
		a, b := shapePort{ID: "a", X: 10, Y: float64(rows) / 2, Side: "right"}, shapePort{ID: "b", X: float64(cols) - 10, Y: float64(rows) / 2, Side: "left"}
		obstacle := connectorObstacle{ID: "thin", X: float64(cols) / 2, Y: a.Y + 1, Width: .08, Height: .08, Shape: "square"}
		line := Element{Query: "connector-mode=elbow&connector-width=8&connector-arrows=both&connector-arrow-width=20"}
		path := routedConnectorPath(line, a, b, []connectorObstacle{obstacle}, cols, rows)
		if len(path) < 4 {
			t.Fatal("route disappeared", path)
		}
		if path[1].X-a.X < 30-1e-9 || b.X-path[len(path)-2].X < 30-1e-9 {
			t.Fatal("head extends past endpoint elbow", path)
		}
		index := indexConnectorObstacles([]projectedConnectorObstacle{projectConnectorObstacle(obstacle, cols, rows)})
		rx, ry := 4., math.Max(2., 4*(1920/float64(cols))/(1080/float64(rows)))
		for i := 1; i < len(path); i++ {
			p, n := path[i-1], path[i]
			if p.X != n.X && p.Y != n.Y {
				t.Fatal("non-orthogonal", path)
			}
			if index.intersectsRect(math.Min(p.X, n.X)-rx, math.Min(p.Y, n.Y)-ry, math.Max(p.X, n.X)+rx, math.Max(p.Y, n.Y)+ry) {
				t.Fatal("clear alternative exists but full stroke crosses obstacle", canvas, path)
			}
		}
		// Arrow footprint changes are route-cache dependencies, even when only
		// terminal lengths (not shape positions) change.
		small := line
		small.Query = setQueryValue(small.Query, "connector-arrow-width", "6")
		other := routedConnectorPath(small, a, b, []connectorObstacle{obstacle}, cols, rows)
		if reflect.DeepEqual(path, other) {
			t.Fatal("arrow width reused stale route")
		}
		q, _ := url.ParseQuery(small.Query)
		q.Set("connector-arrows", "none")
		small.Query = q.Encode()
		none := routedConnectorPath(small, a, b, []connectorObstacle{obstacle}, cols, rows)
		if none[1].X-a.X >= 9 {
			t.Fatal("disabled heads still reserve head length", none)
		}
	}
}

func BenchmarkSweptConnectorRoute(b *testing.B) {
	line := Element{Query: "connector-mode=elbow&connector-width=8&connector-arrows=end&connector-arrow-width=20"}
	a, z := shapePort{ID: "a", X: 20, Y: 28, Side: "right"}, shapePort{ID: "b", X: 220, Y: 28, Side: "left"}
	obstacle := connectorObstacle{X: 100, Y: 15, Width: 30, Height: 26, Shape: "circle", StrokeWidth: 16}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Every iteration has a distinct endpoint key: measure a real search,
		// not repeated hits in the global bounded route cache.
		a.X = 20 + float64(i)*1e-8
		_ = routedConnectorPath(line, a, z, []connectorObstacle{obstacle}, 245, 56)
	}
}

func TestWideConnectorVerticalHeadLegs(t *testing.T) {
	for _, canvas := range [][2]int{{245, 56}, {160, 90}} {
		for _, side := range []string{"top", "bottom"} {
			a, b := shapePort{X: 20, Y: 35, Side: side}, shapePort{X: 100, Y: 35, Side: side}
			path := routedConnectorPath(Element{Query: "connector-mode=elbow&connector-arrows=both&connector-arrow-width=20"}, a, b, nil, canvas[0], canvas[1])
			minimum := math.Max(15, 20*(1920/float64(canvas[0]))/(1080/float64(canvas[1])))
			if len(path) < 4 || math.Abs(path[1].Y-a.Y) < minimum-1e-9 || math.Abs(path[len(path)-2].Y-b.Y) < minimum-1e-9 {
				t.Fatal("vertical arrow leg too short", canvas, side, path)
			}
		}
	}
}

func TestSampledConnectorArrowTipIsNotCoveredByWideShaft(t *testing.T) {
	columnCount := func(arrows string) int {
		count := 0
		for _, line := range connectorPathLines(Element{Query: "connector-width=8&connector-arrow-width=20&connector-arrows=" + arrows}, 0, []connectorPoint{{10, 20}, {100, 20}}) {
			for col, r := range []rune(line.Text) {
				for mask := 1; mask < 16; mask++ {
					if quadrantRune(mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0) != r {
						continue
					}
					for bit := 0; bit < 4; bit++ {
						if mask&(1<<bit) != 0 && (line.Col+col)*2+bit%2 == 199 {
							count++
						}
					}
				}
			}
		}
		return count
	}
	plain, head := columnCount("none"), columnCount("end")
	if plain < 8 || head < 1 || head >= plain/2 {
		t.Fatal("wide shaft flattened arrow tip", plain, head)
	}
}

func TestSweptConnectorVisualFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_SWEEP_EVIDENCE")
	if root == "" {
		t.Skip("visual fixture output not requested")
	}
	d := strokeRouteFixture(t)
	for i := range d.Slides {
		for j := range d.Slides[i].Elements {
			e := &d.Slides[i].Elements[j]
			if e.Kind == "connector" {
				e.Query = setQueryValue(setQueryValue(setQueryValue(e.Query, "connector-width", "8"), "connector-arrow-width", "20"), "connector-arrows", "both")
			}
		}
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := serializeDeck("Swept routes.md", d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "routes.md"), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, sampled := range []bool{false, true} {
		deck := cloneDeck(d)
		file := "routes.html"
		if sampled {
			file = "routes-blocks.html"
			deck.Theme = nil
			deck.Slides = deck.Slides[:1]
			deck.Slides[0].DefaultStyle = "retro"
		}
		html, err := exportHTMLDocument("Swept routes.md", deck.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, file), []byte(html), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
