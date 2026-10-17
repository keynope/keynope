package app

import (
	"bytes"
	"context"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"keynope/internal/pptx"
)

func connectedPPTXDeck(t *testing.T) Deck {
	t.Helper()
	d := Deck{Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck()}
	for i, kind := range []string{"square", "database", "cloud", "manual-input"} {
		label, err := encodeShapeLabel(shapeLabelData{Text: kind, Query: "render=truetype&modern-font=sans&modern-size=30&fg=%23ffffff"})
		if err != nil {
			t.Fatal(err)
		}
		q := url.Values{"shape": {kind}, "left": {"24"}, "top": {"12"}, "width": {"45"}, "height": {"14"}, "fg": {"#0e7490"}, "shape-stroke": {"#55ffff"}, "shape-stroke-width": {"2"}, "shape-label": {label}, "object-rotation": {[]string{"0", "90", "45", "-30"}[i]}}
		from, to := "from-"+strconv.Itoa(i), "to-"+strconv.Itoa(i)
		connector := Element{ID: "wire-" + strconv.Itoa(i), Kind: "connector", Query: "connector-from=" + from + "&connector-from-side=right&connector-to=" + to + "&connector-to-side=left&connector-mode=elbow&connector-width=2&connector-arrows=both&connector-arrow-width=6&fg=%23ff0055&modern-opacity=0.6"}
		d.Slides = append(d.Slides, Slide{ID: "connected-" + strconv.Itoa(i), LayoutID: "blank", Elements: []Element{connector, {ID: from, Kind: "shape", Query: q.Encode()}, {ID: to, Kind: "shape", Query: "shape=rounded&shape-radius=0.3&left=160&top=34&width=48&height=14&fg=%230e7490&shape-stroke=%2355ffff&shape-stroke-width=2&shape-label=" + url.QueryEscape(label)}}})
	}
	return d
}

func TestAttachedConnectorUnsafeReferencesKeepFallback(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, problem := range []string{"missing", "ambiguous", "self", "one-ended", "far-site", "non-shape", "diagonal-route"} {
		objects := []pptx.Object{
			{ID: "a", Kind: "shape", Shape: "rect", X: 100, Y: 100, Width: 100, Height: 100, Fill: "#123456"},
			{ID: "b", Kind: "shape", Shape: "rect", X: 400, Y: 100, Width: 100, Height: 100, Fill: "#123456"},
			{ID: "wire", Kind: "line", X: 200, Y: 150, Width: 200, Stroke: "#ff0055", StrokeWidth: 4, LineStart: &pptx.Connection{Target: "a"}, LineEnd: &pptx.Connection{Target: "b"}},
		}
		switch problem {
		case "missing":
			objects[2].LineStart.Target = "missing"
		case "ambiguous":
			objects = append(objects, objects[0])
		case "self":
			objects[2].LineEnd.Target = "a"
		case "one-ended":
			objects[2].LineEnd = nil
		case "far-site":
			objects[2].X = 900
		case "non-shape":
			objects[0].Kind = "text"
			objects[0].Text = "Text"
		case "diagonal-route":
			objects[2].LinePoints = []pptx.LinePoint{{X: 200, Y: 150}, {X: 300, Y: 175}, {X: 400, Y: 150}}
		}
		target := Slide{}
		indices := map[int]int{}
		for i, o := range objects {
			e, _, _, err := pptxObjectToElement(o, 1, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			e.ID = "test-" + strconv.Itoa(i)
			indices[i] = len(target.Elements)
			target.Elements = append(target.Elements, e)
		}
		before := cloneSlide(target)
		restored, _ := importPPTXConnections(pptx.Slide{Objects: objects}, &target, indices, 1, 0, 0)
		if len(restored) != 0 || !reflect.DeepEqual(before, target) {
			t.Fatal("unsafe reference replaced fallback", problem)
		}
	}
}

func TestAttachedConnectorScopeAndThreePointElbow(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	objects := []pptx.Object{
		{ID: "a", Inherited: true, Kind: "shape", Shape: "rect", X: 800, Y: 500, Width: 100, Height: 100, Fill: "#123456"},
		{ID: "a", Kind: "shape", Shape: "rect", X: 100, Y: 100, Width: 100, Height: 100, Fill: "#123456"},
		{ID: "b", Kind: "shape", Shape: "rect", X: 300, Y: 200, Width: 100, Height: 100, Fill: "#123456"},
		{ID: "wire", Kind: "line", X: 200, Y: 150, Width: 100, Height: 100, Stroke: "#ff0055", StrokeWidth: 4, LineStart: &pptx.Connection{Target: "a"}, LineEnd: &pptx.Connection{Target: "b"}, LinePoints: []pptx.LinePoint{{X: 200, Y: 150}, {X: 300, Y: 150}, {X: 300, Y: 250}}},
	}
	target := Slide{}
	indices := map[int]int{}
	for i, o := range objects {
		e, _, _, err := pptxObjectToElement(o, 1, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		e.ID = "test-" + strconv.Itoa(i)
		indices[i] = i
		target.Elements = append(target.Elements, e)
	}
	restored, report := importPPTXConnections(pptx.Slide{Objects: objects}, &target, indices, 1, 0, 0)
	if !restored["test-3"] || target.Elements[3].Kind != "connector" {
		t.Fatal("inherited ID shadowed slide-local endpoint", report)
	}
	q, _ := url.ParseQuery(target.Elements[3].Query)
	if q.Get("connector-from") != "test-1" {
		t.Fatal("attached inherited shape instead of local shape", q)
	}
	route := decodeConnectorRoute(q.Get("connector-route"))
	if len(route) < 4 {
		t.Fatal("three-point elbow was not decoupled into editable terminal tracks", route)
	}
	ports := slideShapePorts(target, layout(withTrueTypeDefaults(target), 245, 56), 245, 56)
	a, b, ok := connectorEndpoints(target.Elements[3], ports)
	if !ok {
		t.Fatal("missing restored ports")
	}
	got := connectorPath(target.Elements[3], a, b)
	for i := 1; i < len(got); i++ {
		if got[i].X != got[i-1].X && got[i].Y != got[i-1].Y {
			t.Fatal("endpoint adaptation made a diagonal", got)
		}
	}
	corner := connectorPoint{X: 300 * 245.0 / pptx.SceneWidth, Y: 150 * 56.0 / pptx.SceneHeight}
	found := false
	for _, p := range got {
		found = found || math.Hypot(p.X-corner.X, p.Y-corner.Y) < .0001
	}
	if !found {
		t.Fatal("import dropped the native elbow corner", got)
	}
	filtered := removeRestoredLineWarnings([]pptx.Warning{{Slide: 0, Object: "test-3", Code: "line-route-approximated"}, {Slide: 0, Object: "inherited-wire", Code: "line-route-approximated"}}, restored, 0)
	if len(filtered) != 1 || filtered[0].Object != "inherited-wire" {
		t.Fatal("restoration removed another object's warning", filtered)
	}
}

func TestAttachedConnectorAllCataloguePorts(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck()}
	for _, definition := range shapeDefinitions {
		slide := Slide{LayoutID: "blank", Elements: []Element{
			{ID: "from", Kind: "shape", Query: "shape=" + definition.ID + "&left=30&top=18&width=45&height=16&fg=%230e7490"},
			{ID: "to", Kind: "shape", Query: "shape=square&left=165&top=18&width=45&height=16&fg=%230e7490"},
		}}
		for i, side := range pptxPortSides {
			slide.Elements = append(slide.Elements, Element{ID: "wire-" + side, Kind: "connector", Query: "connector-from=from&connector-from-side=" + side + "&connector-to=to&connector-to-side=" + pptxPortSides[(i+2)%4] + "&connector-mode=straight&connector-width=2&fg=%23ff0055"})
		}
		d.Slides = append(d.Slides, slide)
	}
	data, report, err := exportPPTXDocument(d)
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	loaded, report, err := importPPTXDocument(data, "All ports.pptx")
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	for i, definition := range shapeDefinitions {
		before, err := buildSlideScene(d, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		after, err := buildSlideScene(loaded, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		var a, b []sceneObject
		for _, o := range before.Objects {
			if o.Kind == "connector" {
				a = append(a, o)
			}
		}
		for _, o := range after.Objects {
			if o.Kind == "connector" {
				b = append(b, o)
			}
		}
		if len(a) != 4 || len(b) != 4 {
			t.Fatal("catalogue port connection omitted", definition.ID, len(a), len(b))
		}
		for j := range a {
			for k, p := range a[j].Points {
				if math.Hypot(p.X-b[j].Points[k].X, p.Y-b[j].Points[k].Y) > .01 {
					t.Fatal("catalogue port drift", definition.ID, pptxPortSides[j], p, b[j].Points[k])
				}
			}
		}
	}
}

func TestAttachedConnectorPPTXSemanticRoundTrip(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := connectedPPTXDeck(t)
	data, report, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range report.Warnings {
		if w.Code != "connector-arrow-size-approximated" {
			t.Fatal("unexpected export loss", w)
		}
	}
	native, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range native.Slides {
		found := false
		for _, o := range s.Objects {
			if o.Kind == "line" {
				found = true
				if o.LineStart == nil || o.LineEnd == nil {
					t.Fatal("attachments not native", o)
				}
			}
		}
		if !found {
			t.Fatal("connector disappeared")
		}
	}
	loaded, report, err := importPPTXDocument(data, "Connected.pptx")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range report.Warnings {
		if w.Code != "connector-arrow-size-approximated" {
			t.Fatal("unexpected import loss", w)
		}
	}
	for i := range d.Slides {
		before, err := buildSlideScene(d, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		after, err := buildSlideScene(loaded, i, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		var a, b *sceneObject
		for j := range before.Objects {
			if before.Objects[j].Kind == "connector" {
				a = &before.Objects[j]
			}
		}
		for j := range after.Objects {
			if after.Objects[j].Kind == "connector" {
				b = &after.Objects[j]
			}
		}
		if a == nil || b == nil || len(a.Points) != len(b.Points) || a.Arrows != b.Arrows {
			t.Fatal("attached route missing", i, a, b)
		}
		for j, p := range a.Points {
			if math.Hypot(p.X-b.Points[j].X, p.Y-b.Points[j].Y) > .01 {
				t.Fatal("route drift", i, j, p, b.Points[j])
			}
		}
		if math.Abs(a.Paint.StrokeWidth-b.Paint.StrokeWidth) > .001 || b.Paint.Opacity == nil || math.Abs(*b.Paint.Opacity-.6) > .0001 {
			t.Fatal("line paint lost", b.Paint)
		}
		for j, e := range loaded.Slides[i].Elements {
			if e.Kind == "connector" {
				q, _ := url.ParseQuery(e.Query)
				id := q.Get("connector-from")
				for k, body := range loaded.Slides[i].Elements {
					if body.ID == id {
						loaded.Slides[i].Elements[k].Query = setQueryValue(body.Query, "object-offset-x", "2")
					}
				}
				moved, err := buildSlideScene(loaded, i, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				changed := false
				for _, o := range moved.Objects {
					if o.ID == loaded.Slides[i].Elements[j].ID {
						changed = math.Abs(o.Points[0].X-b.Points[0].X) > 1
					}
				}
				if !changed {
					t.Fatal("imported connector does not follow moved body", i)
				}
			}
		}
	}
	if root := os.Getenv("KEYNOPE_CONNECTION_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "connected.md"), d); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "connected.pptx"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
