package main

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func TestImageSelectionCommands(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := sceneFixture(t)
		var image Element
		for _, element := range d.Slides[0].Elements {
			if element.Kind == "image" {
				image = element
				break
			}
		}
		if image.Kind != "image" {
			t.Fatal("fixture needs image")
		}
		image.ID = "image-a"
		image.Query += "&brightness=1.4"
		second := image
		second.ID = "image-b"
		second.Query += "&contrast=1.2"
		d.Slides[0].Elements = []Element{image, second, {ID: "text", Kind: "text", Text: "Untouched"}}
		s := newNativeEditorSession("Images.md", d)
		if master {
			s.deck.EnsureDefaultMasters()
			s.deck.Masters.Base.Slide.Elements = cloneSlide(d.Slides[0]).Elements
			s.masterMode = true
		}
		for _, tc := range []struct {
			property string
			value    json.RawMessage
		}{{"image.style", json.RawMessage(`"retro"`)}, {"image.style", json.RawMessage(`"modern"`)}, {"image.brightness", json.RawMessage("1")}, {"image.saturation", json.RawMessage("0")}, {"image.alpha", json.RawMessage("0")}, {"image.glyph", json.RawMessage(`"braille"`)}, {"image.sampling", json.RawMessage(`"contrast"`)}} {
			before := cloneDeck(s.deck)
			revision := s.version
			command := nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneMaster: master, SceneRevision: &revision, ObjectIDs: []string{"image-a", "image-b", "text"}, Property: tc.property, PropertyValue: tc.value}
			if err := s.apply(command); err != nil {
				t.Fatal(err)
			}
			target := s.deck.Slides[0]
			if master {
				target = s.deck.Masters.Base.Slide
			}
			original := before.Slides[0]
			if master {
				original = before.Masters.Base.Slide
			}
			for _, element := range target.Elements {
				if element.ID == "text" {
					for _, old := range original.Elements {
						if old.ID == "text" && !reflect.DeepEqual(old, element) {
							t.Fatal("image edit touched text")
						}
					}
				}
			}
			control := imageSelectionControls[tc.property]
			for _, element := range target.Elements {
				if element.Kind != "image" {
					continue
				}
				q, _ := url.ParseQuery(element.Query)
				if !q.Has(control.key) {
					t.Fatal("explicit neutral value was lost")
				}
			}
			after := cloneDeck(s.deck)
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("undo", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("redo", err)
			}
			revision = s.version
			command.SceneRevision = &revision
			command.PropertyValue = json.RawMessage(`""`)
			if err := s.apply(command); err != nil {
				t.Fatal("inherit", err)
			}
		}
		for _, value := range []json.RawMessage{json.RawMessage("3"), json.RawMessage("-1"), json.RawMessage(`"1.2"`), json.RawMessage("true")} {
			revision := s.version
			assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneMaster: master, SceneRevision: &revision, ObjectIDs: []string{"image-a", "image-b"}, Property: "image.brightness", PropertyValue: value})
		}
	}
}

func TestImageSelectionValuesMatchRenderer(t *testing.T) {
	for _, query := range []string{"", "brightness=1.4&contrast=.8&saturation=0&sharpness=2&alpha=0&glyph=braille&shape=contrast", "brightness=-1&saturation=-1&alpha=-1&glyph=block", "sharpness=8&alpha=1.2"} {
		q, _ := url.ParseQuery(query)
		options := parseImageASCIIOptions(query)
		want := map[string]interface{}{"image.brightness": options.brightness, "image.contrast": options.contrast, "image.saturation": options.saturation, "image.sharpness": options.sharpness, "image.alpha": float64(options.alphaGate), "image.glyph": options.glyph, "image.sampling": options.shape}
		want["image.mask"] = "none"
		want["image.fit"] = "legacy"
		want["image.style"] = "modern"
		for property, control := range imageSelectionControls {
			if control.colour {
				continue
			}
			value, _, eligible := imageSelectionValue(Element{Kind: "image"}, q, q, control)
			if !eligible || value != want[property] {
				t.Fatalf("%s: %s got %v want %v", query, property, value, want[property])
			}
		}
	}
}

func TestImageMaskSelectionPreservesCrop(t *testing.T) {
	d := strokeInheritanceFixture()
	d.Masters.Layouts[0].Slide.Elements[0].Kind = "image"
	d.Masters.Layouts[0].Slide.Elements[0].Query = "modern-mask=ellipse&modern-crop=0.1,0,0.1,0&brightness=1.4"
	d.Slides[0].Elements[0].Kind = "image"
	for _, tc := range []struct{ value, want string }{{`"none"`, ""}, {`"rounded"`, "rounded"}, {`""`, "ellipse"}} {
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"instance"}, Property: "image.mask", PropertyValue: json.RawMessage(tc.value)}, ""); err != nil {
			t.Fatal(err)
		}
		for _, e := range d.ResolveSlide(0, false).Elements {
			if e.ID == "instance" {
				q, _ := url.ParseQuery(e.Query)
				if q.Get("modern-mask") != tc.want || q.Get("modern-crop") != "0.1,0,0.1,0" || q.Get("brightness") != "1.4" {
					t.Fatal(tc.value, e.Query)
				}
			}
		}
	}
}

func TestImageTintInheritance(t *testing.T) {
	d := strokeInheritanceFixture()
	d.Masters.Layouts[0].Slide.Elements[0].Kind = "image"
	d.Masters.Layouts[0].Slide.Elements[0].Query = "tint=%23ff0000"
	d.Slides[0].Elements[0].Kind = "image"
	for _, tc := range []struct{ value, want string }{{`"none"`, ""}, {`"#00ff00"`, "#00ff00"}, {`""`, "#ff0000"}} {
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"instance"}, Property: "image.tint", PropertyValue: json.RawMessage(tc.value)}, ""); err != nil {
			t.Fatal(err)
		}
		for _, e := range d.ResolveSlide(0, false).Elements {
			if e.ID == "instance" {
				q, _ := url.ParseQuery(e.Query)
				if q.Get("tint") != tc.want {
					t.Fatalf("%s: %s", tc.value, e.Query)
				}
			}
		}
	}
	for property, control := range imageSelectionControls {
		before := cloneSlide(d.Slides[0])
		if _, err := applyImageSelection(&d.Slides[0], []string{"instance"}, control, json.RawMessage("null")); err == nil {
			t.Fatalf("%s accepted null", property)
		}
		if !reflect.DeepEqual(before, d.Slides[0]) {
			t.Fatal("invalid edit mutated slide")
		}
	}
}

func TestImageAdjustmentNeutralAndInherit(t *testing.T) {
	d := strokeInheritanceFixture()
	d.Masters.Layouts[0].Slide.Elements[0].Kind = "image"
	d.Masters.Layouts[0].Slide.Elements[0].Query = "brightness=1.6&contrast=1.3"
	d.Slides[0].Elements[0].Kind = "image"
	d.Slides[0].Elements[0].Query = "top=10&left=20"
	for _, value := range []json.RawMessage{json.RawMessage("1"), json.RawMessage(`""`)} {
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"instance"}, Property: "image.brightness", PropertyValue: value}, ""); err != nil {
			t.Fatal(err)
		}
		resolved := d.ResolveSlide(0, false)
		for _, element := range resolved.Elements {
			if element.ID == "instance" {
				q, _ := url.ParseQuery(element.Query)
				want := 1.0
				if string(value) == `""` {
					want = 1.6
				}
				if options := parseImageASCIIOptions(element.Query); options.brightness != want || options.contrast != 1.3 {
					t.Fatal("lost independent inheritance", q)
				}
			}
		}
	}
}

func TestImageStyleIndependentInheritanceAndPersistence(t *testing.T) {
	d := strokeInheritanceFixture()
	d.Masters.Layouts[0].Slide.Elements[0].Kind = "image"
	d.Masters.Layouts[0].Slide.Elements[0].Query = "image-style=retro&glyph=braille&brightness=1.4&modern-mask=ellipse&stretch=1&width=40&height=20"
	d.Slides[0].Elements[0].Kind = "image"
	d.Slides[0].Elements[0].Query = "left=20&top=10"
	for _, tc := range []struct{ value, want string }{{`"modern"`, "modern"}, {`""`, "retro"}} {
		if _, err := applySelectionProperty(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"instance"}, Property: "image.style", PropertyValue: json.RawMessage(tc.value)}, ""); err != nil {
			t.Fatal(err)
		}
		encoded, err := serializeDeck("Style.md", d)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Style.md", encoded)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range loaded.ResolveSlide(0, false).Elements {
			if e.Kind != "image" {
				continue
			}
			found = true
			q, _ := url.ParseQuery(e.Query)
			if q.Get("image-style") != tc.want || q.Get("glyph") != "braille" || q.Get("brightness") != "1.4" || q.Get("modern-mask") != "ellipse" {
				t.Fatal("style changed independent inheritance", e.Query)
			}
		}
		if !found {
			t.Fatal("image missing")
		}
	}
}
