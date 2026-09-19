package main

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRetiredShapeLabelMigration(t *testing.T) {
	shape := labelledShape("circle")
	q, _ := url.ParseQuery(shape.Query)
	raw := []byte(`{"text":"Hello ** literal","query":"font=old&glyph=blocks&render=text-image&ttf-size=123&fg=%23ffff00","extension":{"keep":true}}`)
	q.Set("shape-label", base64.StdEncoding.EncodeToString(raw))
	shape.Query = q.Encode()
	deck := Deck{Slides: []Slide{{Elements: []Element{shape}}}}
	deck.Masters.Base.Slide.Elements = []Element{shape}
	deck.Masters.Layouts = []MasterLayout{{ID: "test", Slide: Slide{Elements: []Element{shape}}}}
	standardizeDeckText(&deck)
	encoded, err := serializeDeck("labels.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("labels.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := shapeLabel(loaded.Slides[0].Elements[0]); !ok || !sharedRetroText(got) || got.Text != "Hello ** literal" {
		t.Fatalf("migrated label did not survive save/reopen: %+v", got)
	}
	for _, migrated := range []Element{deck.Slides[0].Elements[0], deck.Masters.Base.Slide.Elements[0], deck.Masters.Layouts[0].Slide.Elements[0]} {
		label, ok := shapeLabel(migrated)
		if !ok || label.Text != "Hello ** literal" || !sharedRetroText(label) || trueTypeSize(label) != 123 {
			t.Fatalf("label not migrated: %+v", label)
		}
		mq, _ := url.ParseQuery(migrated.Query)
		payload, _ := base64.StdEncoding.DecodeString(mq.Get("shape-label"))
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(payload, &fields); err != nil || string(fields["extension"]) != `{"keep":true}` {
			t.Fatalf("label extension lost: %s", payload)
		}
		mq.Del("shape-label")
		original, _ := url.ParseQuery(shape.Query)
		original.Del("shape-label")
		if !reflect.DeepEqual(mq, original) || migrated.ID != shape.ID || migrated.Kind != shape.Kind {
			t.Fatal("shape geometry, identity or paint changed")
		}
		if standardTextElement(migrated) != migrated {
			t.Fatal("label migration not idempotent")
		}
	}
	for _, invalid := range []string{"", "not-base64", base64.StdEncoding.EncodeToString([]byte("null")), base64.StdEncoding.EncodeToString([]byte("{bad"))} {
		q.Set("shape-label", invalid)
		shape.Query = q.Encode()
		if standardTextElement(shape) != shape {
			t.Fatal("malformed label changed")
		}
	}
}

func labelledShape(kind string) Element {
	data, _ := json.Marshal(shapeLabelData{Text: "Hello shape", Query: "ttf-size=97&fg=%23ffff00&gradient-start=%23ff0000&gradient-end=%23ffffff"})
	q := url.Values{"shape": {kind}, "left": {"80"}, "top": {"20"}, "width": {"20"}, "height": {"12"}, "fg": {"#00ff00"}, "shape-label": {base64.StdEncoding.EncodeToString(data)}}
	return Element{Kind: "shape", ID: "shape", Query: q.Encode()}
}

func TestShapeLabelFit(t *testing.T) {
	for _, kind := range []string{"square", "circle", "diamond", "triangle"} {
		t.Run(kind, func(t *testing.T) {
			e := labelledShape(kind)
			fitShapeLabel(&e, 245, 56)
			q, _ := url.ParseQuery(e.Query)
			label, ok := shapeLabel(e)
			if !ok {
				t.Fatal("label lost")
			}
			w, h := shapeHalfCells(q, "width", 12), shapeHalfCells(q, "height", 6)
			tw, th := shapeLabelSize(label, 245, 56)
			if !shapeLabelFits(kind, w, h, tw*2, th*2) {
				t.Fatal("text exceeds silhouette")
			}
			if h != 24 {
				t.Fatal("horizontal overflow changed shape height")
			}
			left := float64(intQueryDefault(q, "left", 0)) + shapeSubcellOffset(q, "shape-offset-x")
			top := float64(intQueryDefault(q, "top", 0)) + shapeSubcellOffset(q, "shape-offset-y")
			if left != 80 || top != 20 {
				t.Fatalf("top-left moved: %v,%v", left, top)
			}
			line, _ := shapeLabelLine(e, 0, 20, 80, 245, 56)
			if math.Abs(float64(line.Col)+float64(tw)/2-(left+float64(w)/4)) > .5 || math.Abs(float64(line.Row)+float64(th)/2-(top+float64(h)/4)) > .5 {
				t.Fatal("text was not re-centred inside expanded shape")
			}
			before := e.Query
			fitShapeLabel(&e, 245, 56)
			if before != e.Query {
				t.Fatal("fit is not stable")
			}
			if q.Get("fg") != "#00ff00" {
				t.Fatal("text styling modified shape fill")
			}
			pages := exportSlidePages(Slide{Elements: []Element{e}}, 0, 1, 245, 56)
			found := false
			for _, page := range pages {
				for _, line := range page.Lines {
					if line.Role == "shape-label" {
						found = true
						if line.TrueType == nil || line.TrueType.Text != "Hello shape" {
							t.Fatal("label not TrueType")
						}
					}
				}
			}
			if !found {
				t.Fatal("label missing from export")
			}
		})
	}
}

func TestShapeLabelAxisGrowth(t *testing.T) {
	for _, shape := range []string{"square", "circle", "diamond", "triangle"} {
		for _, tc := range []struct {
			name         string
			w, h, tw, th int
			x, y         bool
		}{
			{"horizontal", 40, 40, 60, 8, true, false},
			{"vertical", 100, 40, 8, 60, false, true},
			{"both", 40, 40, 60, 60, true, true},
			{"fits", 100, 100, 8, 8, false, false},
		} {
			t.Run(shape+"/"+tc.name, func(t *testing.T) {
				w, h := fitShapeLabelDimensions(shape, tc.w, tc.h, tc.tw, tc.th)
				if (w > tc.w) != tc.x || (h > tc.h) != tc.y {
					t.Fatalf("wrong axes grew: %dx%d -> %dx%d", tc.w, tc.h, w, h)
				}
				if !shapeLabelFits(shape, w, h, tc.tw, tc.th) {
					t.Fatal("text does not fit silhouette")
				}
			})
		}
	}
}

func TestShapeLabelTypingAtCurvedBoundary(t *testing.T) {
	for _, shape := range []string{"circle", "diamond", "triangle"} {
		// Neither rectangular dimension overflows, but the corners cross the
		// silhouette. The old fullness-ratio heuristic incorrectly grew height.
		w, h := fitShapeLabelDimensions(shape, 100, 20, 60, 18)
		if w <= 100 || h != 20 || !shapeLabelFits(shape, w, h, 60, 18) {
			t.Fatalf("%s: typing should widen, not heighten: %dx%d", shape, w, h)
		}
	}
}

func TestShapeLabelRoundTrip(t *testing.T) {
	e := labelledShape("circle")
	path := filepath.Join(t.TempDir(), "label.md")
	if err := saveDeck(path, Deck{Slides: []Slide{{Elements: []Element{e}}}}); err != nil {
		t.Fatal(err)
	}
	d, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	label, ok := shapeLabel(d.Slides[0].Elements[0])
	if !ok || label.Text != "Hello shape" {
		t.Fatal("shape label did not round trip")
	}
}

func TestMultilineShapeLabelGrowsDown(t *testing.T) {
	e := labelledShape("square")
	q, _ := url.ParseQuery(e.Query)
	q.Set("width", "100")
	q.Set("shape-offset-x", "0.5")
	q.Set("shape-offset-y", "0.5")
	data, _ := json.Marshal(shapeLabelData{Text: "One\nTwo\nThree", Query: "ttf-size=97"})
	q.Set("shape-label", base64.StdEncoding.EncodeToString(data))
	e.Query = q.Encode()
	fitShapeLabel(&e, 245, 56)
	q, _ = url.ParseQuery(e.Query)
	if shapeHalfCells(q, "width", 12) != 200 || shapeHalfCells(q, "height", 6) <= 24 {
		t.Fatal("new lines should grow only the height")
	}
	if q.Get("left") != "80" || q.Get("top") != "20" || shapeSubcellOffset(q, "shape-offset-x") != .5 || shapeSubcellOffset(q, "shape-offset-y") != .5 {
		t.Fatal("growing downward must preserve the fractional top-left origin")
	}
}

func TestShapeLabelGrowthPreservesContinuousOtherAxis(t *testing.T) {
	for _, tc := range []struct{ width, height, text, preserved string }{
		{"100.125", "2.375", "One\nTwo\nThree", "width"},
		{"2.125", "30.375", "A long line to grow the shape horizontally", "height"},
	} {
		e := labelledShape("square")
		q, _ := url.ParseQuery(e.Query)
		q.Set("width", tc.width)
		q.Set("height", tc.height)
		q.Set("object-offset-x", "0.125")
		q.Set("object-offset-y", "0.375")
		data, _ := json.Marshal(shapeLabelData{Text: tc.text, Query: "ttf-size=97"})
		q.Set("shape-label", base64.StdEncoding.EncodeToString(data))
		e.Query = q.Encode()
		fitShapeLabel(&e, 245, 56)
		got, _ := url.ParseQuery(e.Query)
		if got.Get(tc.preserved) != q.Get(tc.preserved) {
			t.Fatalf("%s rounded from %s to %s", tc.preserved, q.Get(tc.preserved), got.Get(tc.preserved))
		}
		grown := "width"
		if tc.preserved == "width" {
			grown = "height"
		}
		if shapeDimension(got, grown, 1) <= shapeDimension(q, grown, 1) {
			t.Fatal("fixture did not grow")
		}
		for _, key := range []string{"left", "top", "object-offset-x", "object-offset-y"} {
			if got.Get(key) != q.Get(key) {
				t.Fatalf("growth moved %s", key)
			}
		}
		once := e.Query
		fitShapeLabel(&e, 245, 56)
		if e.Query != once {
			t.Fatal("repeated fitting changed settled geometry")
		}
	}
}

func TestAnchoredShapeLabelGrowthKeepsPreciseOrigin(t *testing.T) {
	e := labelledShape("square")
	q, _ := url.ParseQuery(e.Query)
	q.Del("left")
	q.Del("top")
	q.Set("align", "center")
	q.Set("valign", "middle")
	q.Set("width", "100.125")
	q.Set("height", "2.375")
	e.Query = q.Encode()
	fitShapeLabel(&e, 245, 56)
	got, _ := url.ParseQuery(e.Query)
	p := parseImagePlacement(e.Query)
	x := float64(*p.left) + shapeSubcellOffset(got, "shape-offset-x") + objectPlacementOffset(got, "x")
	y := float64(*p.top) + shapeSubcellOffset(got, "shape-offset-y") + objectPlacementOffset(got, "y")
	if math.Abs(x-(245-100.125)/2) > 1e-9 || math.Abs(y-(56-2.375)/2) > 1e-9 {
		t.Fatalf("growth moved anchored origin: %g,%g", x, y)
	}
	if got.Get("align") != "" || got.Get("valign") != "" {
		t.Fatal("growth retained competing anchors")
	}
}

func TestShapeLabelMeasurements(t *testing.T) {
	label := Element{Kind: "text", Text: "Text", Query: "render=truetype&ttf-size=97"}
	w, h := shapeLabelSize(label, 245, 56)
	label.Text = "[color=#ff0055]Text[/color]"
	if cw, ch := shapeLabelSize(label, 245, 56); cw != w || ch != h {
		t.Fatal("colour markup changed text dimensions")
	}
	label.Text = "****Text****"
	if sw, _ := shapeLabelSize(label, 245, 56); sw <= w {
		t.Fatal("literal asterisks were omitted from measurements")
	}
	label.Text = "Text\nText"
	if mw, mh := shapeLabelSize(label, 245, 56); mw != w || mh <= h {
		t.Fatal("multiline text dimensions are incorrect")
	}
	label.Text = "Text"
	label.Query += "&outline=light&shadow=soft"
	if ew, eh := shapeLabelSize(label, 245, 56); ew <= w || eh <= h {
		t.Fatal("effects need room inside the silhouette")
	}
}
