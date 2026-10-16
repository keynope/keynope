package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"testing"
)

func comparableThemeScene(scene slideScene) slideScene {
	data, _ := json.Marshal(scene)
	var result slideScene
	_ = json.Unmarshal(data, &result)
	for i, object := range result.Objects {
		object = comparableDetachedObject(object)
		concreteThemeTestText(object.Text)
		concreteThemeTestText(object.Label)
		object.EditRuns = nil // Deliberately unlinks authoring roles/fallbacks.
		result.Objects[i] = object
	}
	return result
}

func TestConcreteThemeDeckCorpusAppearance(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for name, original := range powerupProofCorpus(t) {
		t.Run(name, func(t *testing.T) {
			source := cloneDeck(original)
			if source.Theme == nil {
				source.Theme = builtinDeckTheme("clean-light")
			}
			before := cloneDeck(source)
			candidate, err := concreteThemeDeck(source)
			if err != nil {
				t.Fatal(err)
			}
			if candidate.Theme != nil || !reflect.DeepEqual(source, before) {
				t.Fatal("source mutated or live theme retained")
			}
			rethemed := cloneDeck(candidate)
			rethemed.Theme = builtinDeckTheme("bold")
			if reflect.DeepEqual(source.Theme, rethemed.Theme) {
				rethemed.Theme = builtinDeckTheme("clean-light")
			}
			for i := range source.Slides {
				want, we := buildSlideScene(source, i, 245, 56)
				got, ge := buildSlideScene(candidate, i, 245, 56)
				if we != nil || ge != nil {
					t.Fatal(we, ge)
				}
				w, g := comparableThemeScene(want), comparableThemeScene(got)
				if !reflect.DeepEqual(w, g) {
					wb, _ := json.Marshal(w)
					gb, _ := json.Marshal(g)
					t.Fatalf("appearance changed on %d\nwant %s\ngot %s", i, wb, gb)
				}
				replaced, err := buildSlideScene(rethemed, i, 245, 56)
				if err != nil || !reflect.DeepEqual(g.Objects, comparableThemeScene(replaced).Objects) {
					t.Fatal("detached objects changed when assigning another theme", i, err)
				}
			}
		})
	}
}

func TestConcreteDefaultsOpeningAndMasterFontInheritance(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Theme: builtinDeckTheme("clean-light"), Appearance: &DeckAppearance{Version: 3}, Masters: defaultMasterDeck(), Slides: []Slide{{ID: "slide", LayoutID: "title", Elements: []Element{{ID: "child", Kind: "heading", Text: "Inherited font", MasterSlotID: "title-title"}}}}}
	for i := range d.Masters.Layouts {
		if d.Masters.Layouts[i].ID == "title" {
			d.Masters.Layouts[i].Slide.Elements[0].Query = "render=truetype&modern-font=mono"
		}
	}
	s := newNativeEditorSession("inherit.md", d)
	scene, err := buildSlideScene(s.deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range scene.Objects {
		if object.ID == "child" {
			found = true
			if object.Text.FontID != "mono" {
				t.Fatal("opening replaced inherited explicit master font", object.Text.FontID)
			}
		}
	}
	if !found {
		t.Fatal("missing bound child")
	}
	plain := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "plain", Elements: []Element{{ID: "text", Kind: "text", Text: "Fixed defaults", Query: "render=truetype&default-font=mono&default-size=80&default-width=75&default-line-height=1.4"}}}}}
	opened := newNativeEditorSession("defaults.md", plain)
	if mustQuery(opened.deck.Slides[0].Elements[0].Query).Has("modern-font") {
		t.Fatal("opening promoted a default font to a strong override")
	}
	scene, err = buildSlideScene(opened.deck, 0, 245, 56)
	if err != nil || scene.Objects[0].Text.FontID != "mono" || scene.Objects[0].Text.Size != 80 {
		t.Fatal("opening lost concrete font defaults", err)
	}
}

func TestConcreteDefaultCommentRoundTripAndAdmission(t *testing.T) {
	q := url.Values{"render": {"truetype"}, "default-font": {"mono"}, "default-size": {"80"}, "default-width": {"75"}, "default-line-height": {"1.4"}, "default-color": {"#ff0055"}, "default-fill": {"inherit"}, "default-stroke": {"none"}, "default-stroke-width": {"0"}, "label-default-font": {"sans"}}
	got, ok, err := textPlacementCommentChecked("<!-- " + placementCommentText(q.Encode()) + " -->")
	if err != nil || !ok || got != q.Encode() {
		t.Fatal("concrete default codec changed", got, err)
	}
	for _, body := range []string{"default-font=theme", "default-font=file.ttf", "default-size=NaN", "default-width=201", "default-line-height=0", "default-color=none", "default-stroke-width=-1", "label-default-font=unknown", "default-color=%23ff0055 default-color=%230055ff"} {
		if _, _, err := textPlacementCommentChecked("<!-- render=truetype " + body + " -->"); err == nil {
			t.Fatal("invalid default accepted", body)
		}
	}
}

func TestConcreteThemeDeckBoundPlaceholderRole(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	deck := Deck{Theme: builtinDeckTheme("clean-light"), Appearance: &DeckAppearance{Version: 3}, Masters: defaultMasterDeck(), Slides: []Slide{{ID: "slide", LayoutID: "title", Elements: []Element{{ID: "child", MasterSlotID: "title-title", Kind: "text", Text: "Theme heading", Query: "render=truetype&modern-font=theme"}}}}}
	deck.Theme.Fonts.Heading.FontID = "mono"
	deck.Theme.Fonts.Body.FontID = "sans"
	before := cloneDeck(deck)
	candidate, err := concreteThemeDeck(deck)
	if err != nil {
		t.Fatal(err)
	}
	child := candidate.Slides[0].Elements[0]
	if child.Kind != "text" || child.MasterSlotID != "title-title" || mustQuery(child.Query).Get("modern-font") != "mono" || !reflect.DeepEqual(deck, before) {
		t.Fatal("detachment lost authored identity, effective heading role or source ownership", child)
	}
	want, we := buildSlideScene(deck, 0, 245, 56)
	got, ge := buildSlideScene(candidate, 0, 245, 56)
	if we != nil || ge != nil || !reflect.DeepEqual(comparableThemeScene(want), comparableThemeScene(got)) {
		t.Fatal("bound placeholder appearance changed", we, ge)
	}
}

func TestConcreteThemeDeckOpaqueExtensions(t *testing.T) {
	deck := Deck{Theme: builtinDeckTheme("clean-light"), Extra: &jsonExtensions{
		"detached-theme-extensions": json.RawMessage(`{"prior":true}`),
		"future-deck":               json.RawMessage(`{"asset":"future-image"}`),
	}, Slides: []Slide{{ID: "slide", Elements: []Element{{ID: "text", Kind: "text", Text: "Keep me"}}}}}
	deck.Theme.Extra = &jsonExtensions{"future-theme": json.RawMessage(`{"object":"future-chart"}`)}
	deck.Theme.Fonts.Body.Extra = &jsonExtensions{"future-font": json.RawMessage(`{"weight":650}`)}
	before := cloneDeck(deck)
	candidate, err := concreteThemeDeck(deck)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Theme != nil || !reflect.DeepEqual(deck, before) {
		t.Fatal("live theme retained or source changed")
	}
	for key, value := range *deck.Extra {
		if !reflect.DeepEqual((*candidate.Extra)[key], value) {
			t.Fatal("overwrote existing deck extension", key)
		}
	}
	var archived map[string]json.RawMessage
	if err := json.Unmarshal((*candidate.Extra)["detached-theme-extensions-2"], &archived); err != nil {
		t.Fatal(err)
	}
	if len(archived) != 2 || string(archived["theme"]) != `{"future-theme":{"object":"future-chart"}}` || string(archived["body"]) != `{"future-font":{"weight":650}}` {
		t.Fatal("opaque extensions dropped or live theme settings archived", archived)
	}
	data, err := serializeDeck("detached.md", candidate)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("detached.md", data)
	if err != nil || loaded.Theme != nil || !reflect.DeepEqual(candidate.Extra, loaded.Extra) {
		t.Fatal("extension archive failed save/reopen", err)
	}
	// The archive must be owned, including its raw JSON byte slices.
	(*candidate.Extra)["future-deck"][0] = '['
	if !reflect.DeepEqual(deck, before) {
		t.Fatal("candidate aliases source metadata")
	}
}

func TestConcreteThemeDeckMasterStyleContexts(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	label, _ := json.Marshal(shapeLabelData{Text: "Shared label", Query: "render=truetype"})
	for _, palette := range []string{"clean-light", "clean-dark", "editorial", "bold", "workshop", "c64"} {
		for _, fill := range []string{"color", "none", "inherit"} {
			t.Run(palette+"/"+fill, func(t *testing.T) {
				theme := builtinDeckTheme(palette)
				if fill == "none" {
					theme.Defaults.ShapeFill = "none"
					theme.Defaults.LineWidth = 0
				}
				if fill == "inherit" {
					theme.Defaults.ShapeFill = ""
					theme.Defaults.ShapeLine = ""
				}
				deck := Deck{Theme: theme, Appearance: &DeckAppearance{Version: 3}, Masters: MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base", Slide: Slide{TTFSize: 110, TTFWidth: 80, Elements: []Element{
					{ID: "master-text", Kind: "text", Text: "Inherited body", Query: "render=truetype&left=4&top=2&width=110&height=8"},
					{ID: "slot", SlotID: "slot", PlaceholderRole: "title", Kind: "heading", Level: 1, Text: "Title", Query: "render=truetype&modern-font=mono&left=4&top=12&width=150&height=12"},
					{ID: "box-a", Kind: "shape", Query: url.Values{"shape": {"square"}, "left": {"10"}, "top": {"30"}, "width": {"75"}, "height": {"20"}, "shape-label": {base64.StdEncoding.EncodeToString(label)}}.Encode()},
					{ID: "box-b", Kind: "shape", Query: "shape=circle&left=145&top=30&width=75&height=20&shape-stroke=theme%3Aaccent-2"},
					{ID: "line", Kind: "connector", Query: "connector-from=box-a&connector-to=box-b&connector-from-side=right&connector-to-side=left&connector-mode=elbow"},
				}}}, Layouts: []MasterLayout{{ID: "layout", Name: "Layout"}}}}
				for i, color := range []string{"", "#ff5500", "#0055ff"} {
					s := Slide{ID: fmt.Sprintf("slide-%d", i), LayoutID: "layout", Elements: []Element{{ID: fmt.Sprintf("child-%d", i), MasterSlotID: "slot", Kind: "heading", Level: 1, Text: "Bound title"}}}
					if color != "" {
						s.FG, _ = ansiFG(color)
						s.HeaderFG, _ = ansiFG(color)
						s.FGSet, s.HeaderFGSet = true, true
					}
					deck.Slides = append(deck.Slides, s)
				}
				candidate, err := concreteThemeDeck(deck)
				if err != nil {
					t.Fatal(err)
				}
				for i := range deck.Slides {
					w, we := buildSlideScene(deck, i, 245, 56)
					g, ge := buildSlideScene(candidate, i, 245, 56)
					if we != nil || ge != nil {
						t.Fatal(we, ge)
					}
					if !reflect.DeepEqual(comparableThemeScene(w), comparableThemeScene(g)) {
						ww, gg := comparableThemeScene(w), comparableThemeScene(g)
						for j := range ww.Objects {
							if !reflect.DeepEqual(ww.Objects[j], gg.Objects[j]) {
								a, _ := json.Marshal(ww.Objects[j])
								b, _ := json.Marshal(gg.Objects[j])
								t.Fatalf("master object changed on %d: %s\nwant %s\ngot %s", i, ww.Objects[j].ID, a, b)
							}
						}
						t.Fatalf("master foreground/font/shape/label/port changed on %d", i)
					}
				}
				data, err := serializeDeck("detached.md", candidate)
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := parseDeckData("detached.md", data)
				if err != nil {
					t.Fatal(err)
				}
				opened := newNativeEditorSession("detached.md", loaded)
				for i := range deck.Slides {
					w, _ := buildSlideScene(deck, i, 245, 56)
					g, ge := buildSlideScene(opened.deck, i, 245, 56)
					if ge != nil || !reflect.DeepEqual(comparableThemeScene(w), comparableThemeScene(g)) {
						t.Fatal("reopening changed detached master appearance", i, ge)
					}
				}
			})
		}
	}
}
