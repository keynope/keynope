package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPairedThemeDefaultsAndOverrides(t *testing.T) {
	d := Deck{Slides: []Slide{{Elements: []Element{{ID: "title", Kind: "heading", Level: 1, Text: "Title"}, {ID: "body", Kind: "text", Text: "Body", Query: "top=20"}, {ID: "explicit", Kind: "text", Text: "Red", Query: "top=30&fg=%23ff0000"}}}}}
	original := cloneDeck(d)
	var err error
	d.Appearance, err = d.withTheme("studio-v1")
	if err != nil {
		t.Fatal(err)
	}
	retro := d.ResolveSlide(0, false)
	if sceneColor(slideBG(retro)) != "#101828" {
		t.Fatal("Retro palette", slideBG(retro))
	}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if scene.Background != "#101828" {
		t.Fatal("unified preview palette", scene.Background)
	}
	for _, object := range scene.Objects {
		want := map[string]string{"title": "#55ffff", "body": "#e6f0ff", "explicit": "#ff0000"}[object.ID]
		if want != "" && object.Paint.Color != want {
			t.Fatalf("%s: %s != %s", object.ID, object.Paint.Color, want)
		}
	}
	if !reflect.DeepEqual(d.Slides, original.Slides) {
		t.Fatal("projection mutated source")
	}
	d.Slides[0].BG = "48;2;1;2;3"
	d.Slides[0].BGSet = true
	scene, err = buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if scene.Background != "#010203" {
		t.Fatal("explicit slide background lost", scene.Background)
	}
	d.EnsureDefaultMasters()
	d.Slides[0].LayoutID = "blank"
	d.Slides[0].BG = ""
	d.Slides[0].BGSet = false
	d.Masters.Base.Slide.BG = "48;2;4;5;6"
	d.Masters.Base.Slide.BGSet = true
	scene, err = buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if scene.Background != "#040506" {
		t.Fatal("master override lost", scene.Background)
	}
}

func TestThemeTransaction(t *testing.T) {
	fixture := sceneFixture(t)
	fixture.Slides[0].EngagementResult.ActivityID = "private"
	fixture.Slides[0].EngagementResult.Kind = "impostor"
	fixture.Slides[0].EngagementResult.State["phase"] = json.RawMessage(`3`)
	s := newNativeEditorSession("Themes.md", fixture)
	before := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "set-theme", Name: "warm-v1"}); err != nil {
		t.Fatal(err)
	}
	updated := cloneDeck(s.deck)
	content := cloneDeck(updated)
	content.Appearance = before.Appearance
	if !reflect.DeepEqual(content, before) {
		t.Fatal("theme changed content or workshop data")
	}
	if len(s.undo) != 1 {
		t.Fatal("theme must be one undo")
	}
	data, err := serializeDeck("Themes.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Themes.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.themeID() != "warm-v1" {
		t.Fatal("theme not persisted")
	}
	if err := s.apply(nativeEditorAction{Action: "set-theme", Name: "unknown"}); err == nil {
		t.Fatal("unknown theme accepted")
	}
	if !reflect.DeepEqual(s.deck, updated) {
		t.Fatal("invalid theme changed deck")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("theme undo was not exact")
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, updated) {
		t.Fatal("theme redo was not exact")
	}
}

func TestThemeTypography(t *testing.T) {
	d := Deck{}
	d.Appearance, _ = d.withTheme("studio-v1")
	for _, test := range []struct {
		kind         string
		level        int
		query        string
		size, height float64
	}{
		{"heading", 1, "", 386, 1.2}, {"heading", 2, "", 193, 1.2}, {"text", 0, "", 97, 1.2}, {"bullet", 0, "", 97, 1.2}, {"code", 0, "", 97, 1.2},
		{"text", 0, "ttf-size=120&modern-line-height=1.8", 120, 1.8},
	} {
		e := Element{Kind: test.kind, Level: test.level, Text: "Typography", Query: test.query}
		got := d.modernSceneText(e)
		if got.Size != test.size || got.LineHeight != test.height {
			t.Fatalf("%s: size=%v height=%v", test.kind, got.Size, got.LineHeight)
		}
	}
	scale := .75
	d.Appearance, _ = d.withAppearanceTextScale("modern", &scale)
	if got := d.modernSceneText(Element{Kind: "text", Text: "Scaled"}).Size; got != 97 {
		t.Fatal("retired appearance scale changed concrete typography", got)
	}
	d.Slides = []Slide{{TTFSize: 194, Elements: []Element{{ID: "text", Kind: "text", Text: "Master sized", Query: "render=truetype"}}}}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if scene.Objects[0].Text.Size != 194 {
		t.Fatal("inherited explicit size overridden", scene.Objects[0].Text.Size)
	}
}
