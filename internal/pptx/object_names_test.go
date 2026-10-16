package pptx

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestNonvisualNamesDoNotBecomeReferenceIDs(t *testing.T) {
	p := connectedNativeFixture()
	name := "Same <node> & 中文 🚀"
	for i := range p.Slides[0].Objects {
		p.Slides[0].Objects[i].Name = name
	}
	p.Slides[0].Timeline = &Timeline{Steps: []BuildStep{{TargetIDs: []string{"body", "other"}, Effect: "appear", DurationMS: 300, Advance: "click"}}}
	before := p.Slides[0].Objects[0]
	var out bytes.Buffer
	if _, err := Write(context.Background(), &out, p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Slides[0].Objects[0], before) {
		t.Fatal("display names mutated caller identity")
	}
	parsed, report, err := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()), Options{})
	if err != nil || len(report.Warnings) > 0 {
		t.Fatal(err, report)
	}
	objects := parsed.Slides[0].Objects
	if len(objects) != 3 || objects[0].Name != name || objects[1].Name != name || objects[2].Name != name {
		t.Fatal("duplicate display names were omitted or deduplicated", objects)
	}
	if objects[0].ID == objects[1].ID || objects[1].ID == objects[2].ID || objects[0].ID == name {
		t.Fatal("display names became ambiguous reference identities")
	}
	if objects[0].LineStart.Target != objects[1].ID || objects[0].LineEnd.Target != objects[2].ID {
		t.Fatal("display names broke connection targets")
	}
	steps := parsed.Slides[0].Timeline.Steps
	if len(steps) != 1 || !reflect.DeepEqual(steps[0].TargetIDs, []string{objects[1].ID, objects[2].ID}) {
		t.Fatal("display names broke timing targets", steps)
	}
	if !bytes.Contains(packageParts(t, out.Bytes())["ppt/slides/slide1.xml"], []byte("Same &lt;node&gt; &amp;")) {
		t.Fatal("nonvisual name not XML escaped")
	}
}

func TestInvalidNonvisualNameFailsBeforeZIP(t *testing.T) {
	for _, name := range []string{strings.Repeat("x", 129), "two\nlines", "bad\x00", "separator\u2028", "invalid\xff"} {
		var out bytes.Buffer
		_, err := Write(context.Background(), &out, Presentation{Slides: []Slide{{Objects: []Object{{ID: "body", Name: name, Kind: "shape", Width: 100, Height: 100}}}}})
		if err == nil || out.Len() != 0 {
			t.Fatal("invalid name wrote a partial package")
		}
	}
}
