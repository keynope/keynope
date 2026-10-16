package pptx

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func connectedNativeFixture() Presentation {
	body := Object{ID: "body", Kind: "shape", X: 100, Y: 100, Width: 400, Height: 200, Fill: "#123456", Text: "Connected", CustomGeometry: customGeometryFixture(), ConnectionSites: []ConnectionSite{{X: 0, Y: 500000, Angle: 10800000}, {X: 1000000, Y: 500000}}}
	line := Object{ID: "line", Kind: "line", Width: 500, Height: 200, Stroke: "#123456", LineStart: &Connection{Target: "body", Site: 1}, LineEnd: &Connection{Target: "other", Site: 0}}
	other := body
	other.ID = "other"
	other.X = 600
	return Presentation{Slides: []Slide{{Objects: []Object{line, {ID: "skipped", Kind: "shape"}, body, other}}}}
}

func TestNativeConnectionParserBounds(t *testing.T) {
	for _, attrs := range []string{`id="0" idx="0"`, `id="2" idx="-1"`, `id="2" idx="1024"`, `id="4294967296" idx="0"`, `id="w" idx="0"`} {
		n, err := parseXML([]byte(`<stCxn `+attrs+`/>`), 8)
		if err != nil {
			t.Fatal(err)
		}
		if parseConnection(n) != nil {
			t.Fatal("invalid native reference accepted", attrs)
		}
	}
	g := customGeometryFixture()
	xml := customGeometryXML(g, nil, []ConnectionSite{{X: 500000, Y: 0, Angle: 16200000}})
	for _, bad := range []string{strings.Replace(xml, `ang="16200000"`, `ang="21600000"`, 1), strings.Replace(xml, `fmla="*/ w 500000 1000000"`, `fmla="val 500000"`, 1)} {
		n, err := parseXML([]byte(`<root xmlns:a="a">`+bad+`</root>`), 32)
		if err != nil {
			t.Fatal(err)
		}
		if parseConnectionSites(findFirst(n, "custGeom")) != nil {
			t.Fatal("unsupported connection formula accepted")
		}
	}
}

func TestNativeConnectionSitesAndForwardReferences(t *testing.T) {
	p := connectedNativeFixture()
	var data bytes.Buffer
	report, err := Write(context.Background(), &data, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range report.Warnings {
		if w.Code != "invalid-bounds" {
			t.Fatal(w)
		}
	}
	got, _, err := Read(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	objects := got.Slides[0].Objects
	if len(objects) != 3 || objects[0].LineStart == nil || objects[0].LineEnd == nil || objects[0].LineStart.Target != objects[1].ID || objects[0].LineEnd.Target != objects[2].ID || objects[0].LineStart.Site != 1 || !reflect.DeepEqual(objects[1].ConnectionSites, p.Slides[0].Objects[2].ConnectionSites) {
		t.Fatal("native endpoint/site mapping changed", objects)
	}
	// Emitting a package must not write transient numeric IDs into caller data.
	if p.Slides[0].Objects[0].LineStart.nativeID != 0 {
		t.Fatal("writer mutated source connection")
	}
}

func TestNativeConnectionsRejectAndDiagnoseBadTargets(t *testing.T) {
	for _, problem := range []string{"missing", "duplicate", "site", "zero-route"} {
		p := connectedNativeFixture()
		switch problem {
		case "missing":
			p.Slides[0].Objects[0].LineStart.Target = "absent"
		case "duplicate":
			p.Slides[0].Objects = append(p.Slides[0].Objects, p.Slides[0].Objects[2])
		case "site":
			p.Slides[0].Objects[0].LineStart.Site = 10
		case "zero-route":
			p.Slides[0].Objects = append([]Object{{ID: "degenerate", Kind: "line", LinePoints: []LinePoint{{X: 1, Y: 1}, {X: 1, Y: 1}}}}, p.Slides[0].Objects...)
			p.Slides[0].Objects[1].LineStart.Target = "degenerate"
		}
		var data bytes.Buffer
		report, err := Write(context.Background(), &data, p)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, w := range report.Warnings {
			found = found || w.Code == "connector-attachment-omitted"
		}
		if !found {
			t.Fatal("invalid reference not diagnosed", problem, report)
		}
	}
	for _, problem := range []string{"count", "coordinates", "angle", "negative-index"} {
		p := connectedNativeFixture()
		switch problem {
		case "count":
			p.Slides[0].Objects[2].ConnectionSites = make([]ConnectionSite, 17)
		case "coordinates":
			p.Slides[0].Objects[2].ConnectionSites[0].X = 4*GeometryUnits + 1
		case "angle":
			p.Slides[0].Objects[2].ConnectionSites[0].Angle = 21600000
		case "negative-index":
			p.Slides[0].Objects[0].LineStart.Site = -1
		}
		var data bytes.Buffer
		if _, err := Write(context.Background(), &data, p); err == nil || data.Len() != 0 {
			t.Fatal("bad attachment partially written", problem)
		}
	}
}
