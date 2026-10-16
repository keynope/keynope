package main

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"testing"
)

func TestConcreteFontStyleDoesNotRelinkToNewTheme(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	source := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "source", Elements: []Element{{ID: "source-text", Kind: "text", Text: "Frozen Mono", Query: "render=truetype&left=10&top=5&width=100&height=12"}}}}}
	source.Theme.Fonts.Body.FontID = "mono"
	source.Theme.Fonts.Body.Width = 75
	detached, err := concreteThemeDeck(source)
	if err != nil {
		t.Fatal(err)
	}
	// Adding another theme must not reinterpret the detached font fallback.
	detached.Theme = builtinDeckTheme("bold")
	resolved := detached.ResolveSlide(0, false)
	snapshot, err := captureObjectStyle(detached, detached.Slides[0], resolved, "source-text")
	if err != nil {
		t.Fatal(err)
	}
	font := snapshot.Fields["text.font"]
	if !font.Present || font.Value != "mono" || font.Fallback != "" {
		t.Fatal("frozen font was relinked to a new theme", font)
	}
	if snapshot.Fields["text.width"].Value != "75" {
		t.Fatal("frozen width was not copied", snapshot.Fields["text.width"])
	}
	target := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("c64"), Slides: []Slide{{ID: "target", Elements: []Element{{ID: "target-text", Kind: "text", Text: "Keep my content", Query: "render=truetype&left=120&top=8&width=100&height=12"}}}}}
	if _, err := applyObjectStyle(&target, nativeEditorAction{Action: "paste-object-style", Slide: 0, ObjectIDs: []string{"target-text"}, StyleData: snapshot}, ""); err != nil {
		t.Fatal(err)
	}
	scene, err := buildSlideScene(target, 0, 245, 56)
	if err != nil || len(scene.Objects) != 1 || scene.Objects[0].Text.FontID != "mono" || target.Slides[0].Elements[0].Text != "Keep my content" {
		t.Fatal("cross-theme paste changed frozen font/content", err)
	}
}

func TestFontBindingIsIndependentOfMixedResolvedValues(t *testing.T) {
	for _, kind := range []string{"text", "shape"} {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "slide"}}}
		d.Theme.Fonts.Body.FontID = "mono"
		for i, query := range []string{"render=truetype&default-font=mono", "render=truetype&modern-font=theme"} {
			element := Element{ID: []string{"fixed", "linked"}[i], Kind: kind, Text: "Same family", Query: query}
			if kind == "shape" {
				data, err := json.Marshal(shapeLabelData{Text: element.Text, Query: query})
				if err != nil {
					t.Fatal(err)
				}
				element.Query = setQueryValue("shape=square&width=50&height=20", "shape-label", base64.StdEncoding.EncodeToString(data))
			}
			d.Slides[0].Elements = append(d.Slides[0].Elements, element)
		}
		property := "text.font"
		if kind == "shape" {
			property = "label.font"
		}
		caps := buildSelectionCapabilities(d, d.Slides[0], d.ResolveSlide(0, false), []string{"fixed", "linked"}, "", 0)
		state := caps.Properties[property]
		if state.State != "value" || state.Value != "mono" || state.Provenance != "inherited" || state.FontBinding != "mixed" {
			t.Fatal("equal families erased different font sources", kind, state)
		}
	}
}

func TestConcreteFontDefaultsInspectorAndSwitch(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, test := range []struct{ name, query, font, snapshot string }{
		{"fixed", "", "mono", "mono"},
		{"theme", "&modern-font=theme", "sans", "theme"},
		{"explicit", "&modern-font=c64", "c64", "c64"},
		{"empty", "&modern-font=", "sans", "theme"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "slide", Elements: []Element{{ID: "text", Kind: "text", Text: "Concrete typography", Query: "render=truetype&left=10&top=5&width=200&height=15&default-font=mono&default-width=75&default-size=80&default-line-height=1.4" + test.query}}}}}
			resolved := d.ResolveSlide(0, false)
			caps := buildSelectionCapabilities(d, d.Slides[0], resolved, []string{"text"}, "", 0)
			for property, want := range map[string]any{"text.font": test.font, "text.width": float64(75), "text.size": float64(80), "text.lineHeight": 1.4} {
				state := caps.Properties[property]
				if state.State != "value" || state.Value != want {
					t.Fatal("inspector disagrees with concrete typography", property, state, want)
				}
			}
			binding := "concrete"
			if test.snapshot == "theme" {
				binding = "theme"
			}
			if caps.Properties["text.font"].FontBinding != binding {
				t.Fatal("font source confused with authored inheritance", caps.Properties["text.font"])
			}
			scene, err := buildSlideScene(d, 0, 245, 56)
			if err != nil || scene.Objects[0].Text.FontID != test.font || scene.Objects[0].Text.Size != 80 {
				t.Fatal("rendered font/size differs", err)
			}
			snapshot, err := captureObjectStyle(d, d.Slides[0], resolved, "text")
			if err != nil || snapshot.Fields["text.font"].Value != test.snapshot || snapshot.Fields["text.width"].Value != "75" {
				t.Fatal("font provenance or width lost in style capture", snapshot, err)
			}
			if test.name == "fixed" {
				font := "c64"
				changed, err := applyModernTextStyle(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"text"}, ModernFont: &font}, "")
				if err != nil || !changed {
					t.Fatal("font switch failed", err)
				}
				got := d.modernSceneText(d.Slides[0].Elements[0]).WidthScale / .4167 * 100
				want := 75 / normalizedFontWidthFactor("mono")
				if math.Abs(got-want) > 1e-9 {
					t.Fatal("font switch normalized from the wrong font/width", got, want)
				}
			}
		})
	}
}

func TestThemeFontSwitchUsesInheritedWidth(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "slide", Elements: []Element{{ID: "text", Kind: "text", Text: "Inherited width", Query: "render=truetype&modern-font=theme&left=10&top=5&width=200&height=15"}}}}}
	d.Theme.Fonts.Body.FontID, d.Theme.Fonts.Body.Width = "mono", 75
	caps := buildSelectionCapabilities(d, d.Slides[0], d.ResolveSlide(0, false), []string{"text"}, "", 0)
	if caps.Properties["text.width"].Value != float64(75) {
		t.Fatal("inspector lost inherited theme width", caps.Properties["text.width"])
	}
	font := "c64"
	if _, err := applyModernTextStyle(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"text"}, ModernFont: &font}, ""); err != nil {
		t.Fatal(err)
	}
	got := d.modernSceneText(d.Slides[0].Elements[0]).WidthScale / .4167 * 100
	if want := 75 / normalizedFontWidthFactor("mono"); math.Abs(got-want) > 1e-9 {
		t.Fatal("font switch lost inherited width", got, want)
	}
}

func TestFontSwitchUsesMasterInstanceTypography(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Masters: defaultMasterDeck(), Slides: []Slide{{ID: "slide", LayoutID: "title", Elements: []Element{{ID: "child", MasterSlotID: "title-title", Kind: "text", Text: "Master Mono"}}}}}
	for i := range d.Masters.Layouts {
		if d.Masters.Layouts[i].ID == "title" {
			d.Masters.Layouts[i].Slide.Elements[0].Query = "render=truetype&modern-font=mono&modern-width=75"
		}
	}
	font := "c64"
	if _, err := applyModernTextStyle(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"child"}, ModernFont: &font}, ""); err != nil {
		t.Fatal(err)
	}
	child := d.ResolveSlide(0, false).Elements
	for _, element := range child {
		if element.ID != "child" {
			continue
		}
		got := d.modernSceneText(element).WidthScale / .4167 * 100
		if want := 75 / normalizedFontWidthFactor("mono"); math.Abs(got-want) > 1e-9 {
			t.Fatal("font switch ignored inherited family/width", got, want)
		}
		return
	}
	t.Fatal("bound child disappeared")
}

func TestTypographyInspectorUsesSlideDefaults(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "slide", TTFSize: 194, TTFWidth: 60, Elements: []Element{{ID: "text", Kind: "text", Text: "Slide defaults", Query: "render=truetype&default-font=mono&default-width=75&default-size=80&left=10&top=5&width=200&height=15"}}}}}
	resolved := d.ResolveSlide(0, false)
	caps := buildSelectionCapabilities(d, d.Slides[0], resolved, []string{"text"}, "", 0)
	if caps.Properties["text.width"].Value != float64(60) || caps.Properties["text.size"].Value != float64(194) {
		t.Fatal("inspector ignored slide defaults", caps.Properties["text.width"], caps.Properties["text.size"])
	}
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil || scene.Objects[0].Text.Size != 194 || math.Abs(scene.Objects[0].Text.WidthScale-.6) > 1e-9 {
		t.Fatal("inspector and rendered defaults disagree", err)
	}
	font := "c64"
	if _, err := applyModernTextStyle(&d, nativeEditorAction{Slide: 0, ObjectIDs: []string{"text"}, ModernFont: &font}, ""); err != nil {
		t.Fatal(err)
	}
	q := mustQuery(d.Slides[0].Elements[0].Query)
	want := 60 / normalizedFontWidthFactor("mono")
	if got, err := strconv.ParseFloat(q.Get("modern-width"), 64); err != nil || math.Abs(got-want) > 1e-9 {
		t.Fatal("font switch ignored slide width", got, want)
	}
}

func TestShapeLabelDoesNotAcquireSlideTypographyDefaults(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	label, err := json.Marshal(shapeLabelData{Text: "Independent Mono", Query: "render=truetype&modern-font=mono"})
	if err != nil {
		t.Fatal(err)
	}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "slide", TTFSize: 194, TTFWidth: 60, Elements: []Element{{ID: "shape", Kind: "shape", Query: setQueryValue("shape=square&left=10&top=5&width=180&height=20", "shape-label", base64.StdEncoding.EncodeToString(label))}}}}}
	resolved := d.ResolveSlide(0, false)
	caps := buildSelectionCapabilities(d, d.Slides[0], resolved, []string{"shape"}, "", 0)
	scene, err := buildSlideScene(d, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	text := scene.Objects[0].Label
	if caps.Properties["label.size"].Value != text.Size || caps.Properties["label.width"].Value != text.WidthScale*100 {
		t.Fatal("nested label inherited unrelated slide typography", caps.Properties["label.size"], caps.Properties["label.width"], text)
	}
	snapshot, err := captureShapeLabelStyle(d, d.Slides[0], resolved, "shape", "")
	if err != nil || snapshot.Fields["text.size"].Value != "97" || snapshot.Fields["text.width"].Value != "100" {
		t.Fatal("label style capture inherited slide defaults", snapshot, err)
	}
}
