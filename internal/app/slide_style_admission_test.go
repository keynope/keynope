package app

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestSlideStyleMetadataRejectsAmbiguityAndInvalidValues(t *testing.T) {
	for _, text := range []string{
		"<!-- effect=warp -->\n<!-- effect=none -->\nText",
		"<!-- background=aurora -->\n<!-- background=none -->\nText",
		"<!-- effect= -->\nText", "<!-- effect=warp --> junk\nText",
		"<!-- background= -->\nText", "<!-- background=aurora --><!-- background=stars -->\nText",
		"Text\n\n<!-- fg=#ff0055 fg=#0055ff -->", "Text\n\n<!-- fg=#ff0055 FG=#0055ff -->",
		"<!-- fg=#ff0055 -->\n\nText\n\n<!-- fg=#0055ff -->",
		"Text\n\n<!-- ttf-default-size=513 -->", "Text\n\n<!-- ttf-default-size=1.5 -->",
		"Text\n\n<!-- ttf-default-size=0 -->", "Text\n\n<!-- ttf-default-width=NaN -->",
		"Text\n\n<!-- ttf-default-width=+Inf -->", "Text\n\n<!-- ttf-default-width=201 -->",
		"Text\n\n<!-- fg=invalid -->", "Text\n\n<!-- bg=invalid -->",
		"Text\n\n<!-- header=invalid -->", "Text\n\n<!-- default-style=maybe -->",
		"Text\n\n<!-- ttf-default-size -->", "Text\n\n<!-- fg=" + strings.Repeat("f", 4096) + " -->",
	} {
		if _, err := parseSlideChecked(text, "."); err == nil {
			t.Fatal("ambiguous/malformed style accepted", text[:min(len(text), 120)])
		}
	}
	// Validate the entire comment before changing the receiver or claiming keys.
	slide := Slide{FG: "37", FGSet: true, TTFSize: 97}
	before := cloneSlide(slide)
	owners := map[string]bool{}
	if _, err := applySlideStyleChecked("<!-- fg=#ff0055 ttf-default-size=NaN -->", &slide, owners); err == nil || !reflect.DeepEqual(before, slide) || len(owners) != 0 {
		t.Fatal("rejected style partially mutated owner", err)
	}
}

func TestSlideStyleMetadataOwnersCodeAndRoundTrip(t *testing.T) {
	oldW, oldH, oldOrder := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = oldW, oldH, oldOrder
	}()
	code := "<!-- effect=warp -->\n<!-- effect=none -->\n<!-- bg=invalid -->\n<!-- ttf-default-size=NaN -->"
	text := "<!-- effect=warp -->\n<!-- background=aurora -->\n```\n" + code + "\n```\n\n<!-- fg=#ff0055 bg=none header=#55aaff ttf-default-size=512 ttf-default-width=200 -->\n---\n<!-- effect=none -->\n<!-- background=none -->\nText\n\n<!-- fg=#0055ff ttf-default-size=1 ttf-default-width=1 -->"
	d, err := parseDeckData("styles.md", []byte(text))
	if err != nil || len(d.Slides) != 2 {
		t.Fatal("valid scoped styles rejected", err)
	}
	if d.Slides[0].Effect != "warp" || d.Slides[0].Background != "aurora" || d.Slides[0].TTFSize != 512 || d.Slides[0].TTFWidth != 200 || d.Slides[0].Elements[0].Text != code || d.Slides[1].Effect != "" || !d.Slides[1].EffectSet || d.Slides[1].Background != "" || !d.Slides[1].BackgroundSet || d.Slides[1].TTFSize != 1 {
		t.Fatal("scope, code or explicit None changed")
	}
	encoded, err := serializeDeck("styles.md", d)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("styles.md", encoded)
	if err != nil || reopened.Slides[0].TTFSize != 512 || reopened.Slides[1].TTFWidth != 1 || reopened.Slides[0].Elements[0].Text != code {
		t.Fatal("style round trip changed", err)
	}
	// Colour/geometry comments still belong to the next element, not defaults.
	placed, err := parseSlideChecked("<!-- top=2 fg=#00ff55 -->\n# Positioned\n\n<!-- top=4 fg=#ff0055 -->\nText", ".")
	if err != nil || len(placed.Elements) != 2 || placed.FGSet || !strings.Contains(placed.Elements[0].Query, "00ff55") || !strings.Contains(placed.Elements[1].Query, "ff0055") {
		t.Fatal("element placement changed", err)
	}
}

func TestSlideStyleFailureRestoresActiveRuntime(t *testing.T) {
	oldW, oldH, oldOrder := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = oldW, oldH, oldOrder
	}()
	for _, metadata := range []string{"<!-- effect=warp -->\n<!-- effect=none -->\nText", "Text\n\n<!-- ttf-default-size=NaN -->"} {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = 132, 41, true
		if _, err := parseEditorDeckData("bad.md", []byte("<!-- keynope width=245 height=56 -->\n"+metadata)); err == nil || authoredTerminalWidth != 132 || authoredTerminalHeight != 41 || !parsedDeckElementOrderChanged {
			t.Fatal("failed style load changed runtime", err)
		}
	}
}

func TestSlideStyleTypedMasterSaveAndActionAdmission(t *testing.T) {
	for _, invalid := range []Slide{{TTFSize: -1}, {TTFSize: 513}, {TTFWidth: .5}, {TTFWidth: 201}, {TTFWidth: math.NaN()}, {DefaultStyle: "future"}, {Effect: "warp\n<!--"}, {Background: strings.Repeat("x", 129)}} {
		for _, master := range []bool{false, true} {
			d := Deck{Masters: defaultMasterDeck(), Slides: []Slide{{ID: "one", Elements: []Element{{ID: "text", Kind: "text", Text: "Keep"}}}}}
			if master {
				d.Masters.Base.Slide = invalid
			} else {
				d.Slides[0].TTFSize, d.Slides[0].TTFWidth, d.Slides[0].DefaultStyle, d.Slides[0].Effect, d.Slides[0].Background = invalid.TTFSize, invalid.TTFWidth, invalid.DefaultStyle, invalid.Effect, invalid.Background
			}
			if _, err := serializeDeck("bad-style.md", d); err == nil {
				t.Fatal("save accepted unloadable style", master, invalid)
			}
			session := newNativeEditorSession("Owned.md", Deck{Masters: defaultMasterDeck(), Slides: []Slide{{ID: "one", Elements: []Element{{ID: "text", Kind: "text", Text: "Keep"}}}}})
			session.masterMode = master
			before, version := cloneDeck(session.deck), session.version
			if err := session.apply(nativeEditorAction{Action: "update-slide", SlideData: &invalid}); err == nil || !reflect.DeepEqual(before, session.deck) || version != session.version || len(session.undo) != 0 {
				t.Fatal("invalid settings action modified state", master, err)
			}
		}
		// Master JSON bypasses the Markdown style-comment parser, but must use
		// the same bounds before publishing runtime state or writing a cache.
		masters := defaultMasterDeck()
		masters.Base.Slide = invalid
		payload, err := json.Marshal(masters)
		if err != nil {
			continue
		} // NaN is rejected by the JSON codec itself.
		metadata := "<!-- keynope-masters version=1 base64:" + base64.StdEncoding.EncodeToString(payload) + " -->\nText"
		if _, err := parseDeckData("bad-master.md", []byte(metadata)); err == nil {
			t.Fatal("typed master style bypassed admission", invalid)
		}
	}
}
