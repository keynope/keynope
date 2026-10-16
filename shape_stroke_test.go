package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func encodeShapeLabel(label shapeLabelData) (string, error) {
	data, err := json.Marshal(label)
	return base64.StdEncoding.EncodeToString(data), err
}

func TestShapeStrokeSelectionHistoryAndSubset(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := capabilityFixture()
		if master {
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
			s.masterMode = true
			s.currentMaster = 0
		}
		before := cloneDeck(s.deck)
		for _, edit := range []struct {
			property, value string
			accepted        interface{}
		}{
			{"paint.strokeColor", `"#AA55FF"`, "#aa55ff"},
			{"paint.strokeWidth", `8.25`, 8.25},
			{"paint.strokeWidth", `0`, 0.0},
			{"paint.strokeColor", `"none"`, "none"},
		} {
			version := s.version
			a := nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"text-a", "text-b", "shape-a"}, Property: edit.property, PropertyValue: json.RawMessage(edit.value)}
			if err := s.apply(a); err != nil {
				t.Fatal(err)
			}
			p := s.actionTransportState(a.Action).SelectionCapabilities.Properties[edit.property]
			if p.State != "value" || p.Value != edit.accepted || p.Provenance != "explicit" || p.TotalCount != 3 || !reflect.DeepEqual(p.EligibleIDs, []string{"shape-a"}) {
				t.Fatalf("accepted subset: %+v", p)
			}
			if s.version != version+1 {
				t.Fatal("not one revision")
			}
			version = s.version
			depth := len(s.undo)
			if err := s.apply(a); err != nil || s.version != version || len(s.undo) != depth {
				t.Fatal("no-op dirtied history", err)
			}
		}
		after := cloneDeck(s.deck)
		for range 4 {
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(before, s.deck) {
			t.Fatal("undo failed")
		}
		for range 4 {
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(after, s.deck) {
			t.Fatal("redo failed")
		}
	}
}

func TestShapeStrokeRejectsInvalidAndLockedAtomically(t *testing.T) {
	for _, edit := range []struct{ property, value string }{{"paint.strokeColor", `"url(example)"`}, {"paint.strokeColor", `8`}, {"paint.strokeWidth", `-1`}, {"paint.strokeWidth", `64.25`}, {"paint.strokeWidth", `"2"`}} {
		s := capabilityFixture()
		version := s.version
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"text-a", "shape-a"}, Property: edit.property, PropertyValue: json.RawMessage(edit.value)})
	}
	s := capabilityFixture()
	s.deck.Slides[0].Elements[0].Query += "&object-locked=1"
	version := s.version
	assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"text-a", "shape-a"}, Property: "paint.strokeWidth", PropertyValue: json.RawMessage(`3`)})
	for _, query := range []string{"shape-stroke=red", "shape-stroke-width=NaN", "shape-stroke-width=Inf", "shape-stroke-width=-1", "shape-stroke-width=65"} {
		if validateShapeAdjustments(Element{Kind: "shape", Query: query}) == nil {
			t.Fatal("invalid stroke accepted", query)
		}
	}
}

func TestShapeStrokeInheritanceAndStyleIsolation(t *testing.T) {
	s := capabilityFixture()
	s.deck.Theme = builtinDeckTheme("clean-light")
	s.deck.Theme.Defaults.ShapeLine = "#00aa55"
	s.deck.Theme.Defaults.LineWidth = 5.5
	s.selection = map[int]bool{2: true}
	s.selected = 2
	set := func(property, value string) {
		t.Helper()
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"shape-a"}, Property: property, PropertyValue: json.RawMessage(value)}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(color string, width float64) {
		t.Helper()
		p := s.transportState().SelectionCapabilities.Properties
		if p["paint.strokeColor"].Value != color || p["paint.strokeWidth"].Value != width {
			t.Fatalf("wrong stroke: %+v %+v", p["paint.strokeColor"], p["paint.strokeWidth"])
		}
	}
	check("#00aa55", 5.5)
	set("paint.strokeColor", `"#ff55aa"`)
	check("#ff55aa", 5.5)
	set("paint.strokeWidth", `3.25`)
	check("#ff55aa", 3.25)
	set("paint.strokeColor", `""`)
	check("#00aa55", 3.25)
	set("paint.strokeWidth", `""`)
	check("#00aa55", 5.5)
	set("paint.strokeColor", `"none"`)
	check("none", 5.5)
	set("paint.strokeColor", `"#ff55aa"`)
	set("paint.strokeWidth", `8.25`)
	label, err := encodeShapeLabel(shapeLabelData{Text: "Label unchanged", Query: "fg=%23ffffff&modern-size=80"})
	if err != nil {
		t.Fatal(err)
	}
	target := Element{ID: "stroke-target", Kind: "shape", Query: "shape=circle&left=50&top=20&width=60&height=10&fg=%230055aa&shape-label=" + url.QueryEscape(label)}
	s.deck.Slides[0].Elements = append(s.deck.Slides[0].Elements, target)
	snapshot, err := captureObjectStyle(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), "shape-a")
	if err != nil {
		t.Fatal(err)
	}
	version := s.version
	if err = s.apply(nativeEditorAction{Action: "paste-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{target.ID}, StyleData: snapshot}); err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[3].Query)
	old, _ := url.ParseQuery(target.Query)
	if q.Get("shape-stroke") != "#ff55aa" || q.Get("shape-stroke-width") != "8.25" {
		t.Fatal("style lost stroke", q)
	}
	for _, key := range []string{"shape", "shape-label", "left", "top", "width", "height"} {
		if q.Get(key) != old.Get(key) {
			t.Fatal("style changed content/geometry", key)
		}
	}
	version = s.version
	if err = s.apply(nativeEditorAction{Action: "reset-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{target.ID}}); err != nil {
		t.Fatal(err)
	}
	q, _ = url.ParseQuery(s.deck.Slides[0].Elements[3].Query)
	if q.Has("shape-stroke") || q.Has("shape-stroke-width") || q.Get("shape-label") != old.Get("shape-label") {
		t.Fatal("reset lost content or retained stroke", q)
	}
}

func TestShapeStrokeMasterOverridesAndSave(t *testing.T) {
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Masters: MasterDeck{Version: 1, Layouts: []MasterLayout{{ID: "stroke-layout", Name: "Stroke", Slide: Slide{Elements: []Element{{ID: "master-shape", SlotID: "shape-slot", Kind: "shape", Query: "shape=square&width=40&height=15&shape-stroke=%2300aa55&shape-stroke-width=7.5"}}}}}}, Slides: []Slide{{ID: "stroke-slide", LayoutID: "stroke-layout", Elements: []Element{{ID: "authored-shape", MasterSlotID: "shape-slot", Kind: "shape", Query: "shape=square&shape-stroke=none&shape-stroke-width=0"}}}}}
	deck.Masters.Layouts[0].Slide.Elements[0].PlaceholderRole = placeholderBody
	resolved := deck.ResolveSlide(0, false)
	if len(resolved.Elements) != 1 {
		t.Fatalf("unexpected resolution: %+v", resolved.Elements)
	}
	q, _ := url.ParseQuery(resolved.Elements[0].Query)
	if q.Get("shape-stroke") != "none" || q.Get("shape-stroke-width") != "0" {
		t.Fatal("master overwrote Off", q)
	}
	file := filepath.Join(t.TempDir(), "strokes.md")
	if err := saveDeck(file, deck); err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeck(file)
	if err != nil {
		t.Fatal(err)
	}
	q, _ = url.ParseQuery(loaded.ResolveSlide(0, false).Elements[0].Query)
	if q.Get("shape-stroke") != "none" || q.Get("shape-stroke-width") != "0" {
		t.Fatal("save lost Off", q)
	}
	master, _ := url.ParseQuery(loaded.Masters.Layouts[0].Slide.Elements[0].Query)
	if master.Get("shape-stroke") != "#00aa55" || master.Get("shape-stroke-width") != "7.5" {
		t.Fatal("master stroke lost", master)
	}
}

func TestRetroShapeStrokeContinuousAndBounded(t *testing.T) {
	for _, definition := range shapeDefinitions {
		g := shapeGeometry(ShapeSpec{Kind: definition.ID}, 2)
		for _, width := range []float64{.25, 1, 8.25, 64} {
			cells, reduced := retroShapeStroke(g, sceneRect{Width: 400, Height: 200}, width, 1920.0/245, 1080.0/56)
			if reduced || len(cells) == 0 {
				t.Fatal("missing normal-sized border", definition.ID, width)
			}
			for _, c := range cells {
				if c.Width <= 0 || c.Height <= 0 || math.IsNaN(c.X) || c.X < -width/2-4 || c.Y < -width/2-10 || c.X+c.Width > 400+width/2+4 || c.Y+c.Height > 200+width/2+10 {
					t.Fatal("invalid block coverage", c)
				}
			}
		}
	}
	g := shapeGeometry(ShapeSpec{Kind: "square"}, 2)
	for _, width := range []float64{.25, 1} {
		cells, _ := retroShapeStroke(g, sceneRect{Width: 400, Height: 200}, width, 8, 20)
		for y := 5.0; y < 200; y += 10 {
			for _, x := range []float64{0, 399.9} {
				hit := false
				for _, c := range cells {
					hit = hit || x >= c.X && x <= c.X+c.Width && y >= c.Y && y <= c.Y+c.Height
				}
				if !hit {
					t.Fatal("discontinuous thin border", width, x, y)
				}
			}
		}
	}
	if cells, _ := retroShapeStroke(g, sceneRect{Width: 400, Height: 200}, 0, 8, 20); len(cells) > 0 {
		t.Fatal("Off painted")
	}
	if _, reduced := retroShapeStroke(g, sceneRect{Width: 10000, Height: 10000}, 64, 8, 20); !reduced {
		t.Fatal("unbounded large border")
	}
}

func shapeStrokeFixture(t *testing.T) Deck {
	t.Helper()
	label, err := encodeShapeLabel(shapeLabelData{Text: "Stroke", Query: "render=truetype&modern-font=c64&modern-size=20&ttf-size=20&fg=%23ffffff"})
	if err != nil {
		t.Fatal(err)
	}
	d := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "modern"}, Slides: []Slide{{ID: "stroke-proof", BG: "48;2;16;24;32", BGSet: true, Elements: []Element{}}}}
	for _, e := range []struct{ id, shape, style, color, width, left, top, w, h, rotation string }{
		{"vector-rounded", "rounded", "modern", "#55ffff", "8.25", "18", "7", "85", "13", "0"},
		{"retro-rounded", "rounded", "retro", "#55ffff", "8.25", "138", "7", "85", "13", "0"},
		{"vector-arrow", "arrow-right", "modern", "#ffbf55", "2.5", "18", "27", "85", "13", "0"},
		{"retro-arrow", "arrow-right", "retro", "#ffbf55", "2.5", "138", "27", "85", "13", "0"},
		{"vector-diamond", "diamond", "modern", "#ff55aa", "12", "25", "44", "38", "9", "12"},
		{"retro-diamond", "diamond", "retro", "#ff55aa", "12", "145", "44", "38", "9", "12"},
	} {
		q := url.Values{"shape": {e.shape}, "element-style": {e.style}, "fg": {"#234359"}, "shape-stroke": {e.color}, "shape-stroke-width": {e.width}, "left": {e.left}, "top": {e.top}, "width": {e.w}, "height": {e.h}, "object-rotation": {e.rotation}, "shape-label": {label}}
		d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: e.id, Kind: "shape", Query: q.Encode()})
	}
	return d
}

func TestShapeStrokeSceneAndPPTXRoundTrip(t *testing.T) {
	d := shapeStrokeFixture(t)
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	sampled := 0
	for _, o := range scene.Objects {
		if o.Paint.Stroke == "" || o.Paint.StrokeWidth <= 0 {
			t.Fatal("missing projected stroke", o.ID)
		}
		if o.Style == "retro" {
			sampled++
			if len(o.RetroStroke) == 0 {
				t.Fatal("missing sampled stroke", o.ID)
			}
		}
	}
	if sampled != 3 {
		t.Fatal("legacy sampling not exercised", sampled)
	}
	concrete := cloneDeck(d)
	canonicalizeConcreteAppearance(&concrete)
	current, err := buildSlideScene(concrete, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range current.Objects {
		if o.RetroLines != nil || len(o.RetroStroke) > 0 || o.Paint.Stroke == "" {
			t.Fatal("concrete shapes lost vector stroke", o.ID)
		}
	}
	// Editable PPTX uses vector objects; sampled Retro artwork has its existing
	// exact-appearance warning and is not claimed as editable vector parity.
	for i := range d.Slides[0].Elements {
		d.Slides[0].Elements[i].Query = setQueryValue(d.Slides[0].Elements[i].Query, "element-style", "modern")
	}
	data, _, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Slides) != 1 {
		t.Fatal("wrong slide count")
	}
	want := map[string]float64{"#55ffff": 4.125, "#ffbf55": 1.25, "#ff55aa": 6}
	counts := map[string]int{}
	for _, o := range p.Slides[0].Objects {
		if w, ok := want[strings.ToLower(o.Stroke)]; ok {
			if math.Abs(o.StrokeWidth-w) > .0001 {
				t.Fatalf("PPTX stroke physical units: %s %g != %g", o.ID, o.StrokeWidth, w)
			}
			counts[strings.ToLower(o.Stroke)]++
		}
	}
	for color := range want {
		if counts[color] != 2 {
			t.Fatal("missing editable strokes", color, counts)
		}
	}
	loaded, report, err := importPPTXDocument(data, "Stroke.pptx")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range report.Warnings {
		if w.Code == "shape-stroke-approximated" {
			t.Fatal("stroke approximated")
		}
	}
	found := 0
	for _, e := range loaded.Slides[0].Elements {
		q, _ := url.ParseQuery(e.Query)
		if q.Get("shape-stroke") == "#55ffff" || q.Get("shape-stroke") == "#55FFFF" {
			found++
			width, err := strconv.ParseFloat(q.Get("shape-stroke-width"), 64)
			if err != nil || math.Abs(width-8.25) > 2.0/12700 {
				t.Fatal("stroke width changed on import", q)
			}
		}
	}
	if found != 2 {
		t.Fatal("coloured strokes lost", found)
	}
}

func TestShapeStrokeLabelFitUsesOneFrame(t *testing.T) {
	d := shapeStrokeFixture(t)
	label, err := encodeShapeLabel(shapeLabelData{Text: "A label taller than the original frame", Query: "render=truetype&modern-size=78&ttf-size=78&modern-font=c64"})
	if err != nil {
		t.Fatal(err)
	}
	d.Slides[0].Elements = d.Slides[0].Elements[3:4]
	e := &d.Slides[0].Elements[0]
	e.Query = setQueryValue(e.Query, "shape-label", label)
	before := cloneDeck(d)
	fitted := *e
	fitShapeLabel(&fitted, 245, 56)
	q, _ := url.ParseQuery(fitted.Query)
	if shapeDimension(q, "height", 6) <= 13 {
		t.Fatal("fixture did not exercise growth")
	}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	o := scene.Objects[0]
	if math.Abs(o.Bounds.Height-shapeDimension(q, "height", 6)*1080/56) > 1e-6 {
		t.Fatal("stroke used the unfitted frame", o.Bounds)
	}
	for _, line := range o.RetroLines.Lines {
		if line.Role == "shape" && (float64(line.Row)*1080/56 < o.Bounds.Y-20 || float64(line.Row)*1080/56 > o.Bounds.Y+o.Bounds.Height+20) {
			t.Fatal("fill escaped stroke frame", line.Row, o.Bounds)
		}
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("projection mutated authored data")
	}
}

func TestShapeStrokeHairlineAndOffPPTX(t *testing.T) {
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{Elements: []Element{
		{ID: "hairline", Kind: "shape", Query: "shape=square&top=8&left=10&width=30&height=10&fg=%23aa0055&shape-stroke=%23ff0055&shape-stroke-width=0.25"},
		{ID: "zero", Kind: "shape", Query: "shape=square&top=8&left=60&width=30&height=10&fg=%2300aa55&shape-stroke=%23ffffff&shape-stroke-width=0"},
		{ID: "off", Kind: "shape", Query: "shape=square&top=8&left=110&width=30&height=10&fg=%230055aa&shape-stroke=none"},
	}}}}
	data, _, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, o := range p.Slides[0].Objects {
		switch strings.ToLower(o.Fill) {
		case "#aa0055":
			count++
			if strings.ToLower(o.Stroke) != "#ff0055" || math.Abs(o.StrokeWidth-.125) > 1.0/12700 {
				t.Fatal("fractional hairline clamped", o.Stroke, o.StrokeWidth)
			}
		case "#00aa55", "#0055aa":
			count++
			if o.Stroke != "" {
				t.Fatal("Off became Office hairline", o.Stroke)
			}
		}
	}
	if count != 3 {
		t.Fatal("fixture shapes lost", count)
	}
	loaded, _, err := importPPTXDocument(data, "Off.pptx")
	if err != nil {
		t.Fatal(err)
	}
	loaded.Theme = d.Theme.Clone()
	scene, err := buildSlideScene(loaded, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range scene.Objects {
		if o.Paint.Color == "#00AA55" || o.Paint.Color == "#0055AA" {
			if o.Paint.Stroke != "" {
				t.Fatal("theme re-enabled imported Off", o.Paint)
			}
		}
	}
}

func TestShapeStrokeVisualFixture(t *testing.T) {
	output := os.Getenv("KEYNOPE_STROKE_EVIDENCE")
	if output == "" {
		t.Skip("fixture output not requested")
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	d := shapeStrokeFixture(t)
	if err := saveDeck(filepath.Join(output, "strokes.md"), d); err != nil {
		t.Fatal(err)
	}
	content, err := exportHTMLDocument(filepath.Join(output, "strokes.md"), d.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "strokes.html"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	canonicalizeConcreteAppearance(&d)
	data, _, err := exportPPTXDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(output, "strokes.pptx"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
