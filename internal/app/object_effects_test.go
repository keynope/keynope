package app

import (
	"encoding/json"
	"keynope/internal/effects"
	"net/url"
	"reflect"
	"testing"
)

func TestCraftEffectsSelectionHistoryAndInherit(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := capabilityFixture()
		if master {
			s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
			s.masterMode = true
			s.currentMaster = 0
		}
		for _, edit := range []struct{ property, value string }{{"paint.blur", "4"}, {"paint.blurAxes", `"horizontal"`}, {"paint.posterize", "6"}, {"paint.colourEffect", `"tritone"`}, {"paint.effectMidtones", `"#dd00ff"`}, {"paint.glowRadius", "10"}, {"paint.glowThreshold", "60"}, {"paint.glowIntensity", "75"}, {"paint.glowColour", `"#55aaff"`}, {"paint.gradientDirection", `"radial"`}} {
			before := cloneDeck(s.deck)
			version := s.version
			action := nativeEditorAction{Action: "set-selection-property", Property: edit.property, PropertyValue: json.RawMessage(edit.value), SceneRevision: &version, SceneMaster: master, ObjectIDs: []string{"text-a", "text-b", "shape-a"}}
			if err := s.apply(action); err != nil {
				t.Fatalf("%s: %v", edit.property, err)
			}
			after := cloneDeck(s.deck)
			if s.version != version+1 {
				t.Fatal("not one transaction")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("undo", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("redo", err)
			}
		}
		version := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Property: "paint.blur", PropertyValue: json.RawMessage(`""`), SceneRevision: &version, SceneMaster: master, ObjectIDs: []string{"text-a"}}); err != nil {
			t.Fatal("inherit", err)
		}
	}
}
func TestCraftEffectsMetadataAndStyle(t *testing.T) {
	q := url.Values{}
	for _, c := range effectControls {
		addPlacementValue(q, c.key, c.fallback)
	}
	q.Set("fx-blur", "4")
	q.Set("fx-colour", "tritone")
	q.Set("gradient-dir", "diamond")
	q.Set("gradient-start", "#ff0055")
	q.Set("gradient-end", "#55ffff")
	comment := "<!-- " + placementCommentText(q.Encode()) + " -->"
	query, ok := textPlacementComment(comment)
	if !ok {
		t.Fatal(comment)
	}
	decoded, _ := url.ParseQuery(query)
	for key := range q {
		if decoded.Get(key) != q.Get(key) {
			t.Fatal("lost metadata", key, decoded)
		}
	}
	snapshot := &ObjectStyleSnapshot{Version: 1, Fields: map[string]ObjectStyleField{}}
	for property := range effectControls {
		snapshot.Fields[property] = ObjectStyleField{Present: true, Value: effectValue(q, property)}
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	p := scenePaintFor(Element{Kind: "text", Query: q.Encode()}, Slide{}, 1, 1)
	o := sceneObject{Kind: "text", Bounds: sceneRect{Width: 200, Height: 100}, Paint: p}
	compileSceneEffects(&o, q)
	if o.Paint.Effects == nil || o.Paint.GradientImage == "" {
		t.Fatal("effects missing from audience-safe scene")
	}
}
func TestCraftEffectsInvalidInputsAreAtomic(t *testing.T) {
	for _, edit := range []struct{ property, value string }{{"paint.blur", "49"}, {"paint.blur", "-1"}, {"paint.blur", `"oops"`}, {"paint.posterize", "1"}, {"paint.posterize", "3.5"}, {"paint.glowRadius", "10000"}, {"paint.glowThreshold", "101"}, {"paint.colourEffect", `"script"`}, {"paint.glowColour", `"url(x)"`}} {
		s := capabilityFixture()
		v := s.version
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-selection-property", Property: edit.property, PropertyValue: json.RawMessage(edit.value), SceneRevision: &v, ObjectIDs: []string{"text-a", "shape-a"}})
	}
}

func TestCraftEffectsPPTXUncapturedFallbackIsReported(t *testing.T) {
	object := retroAnimationFixture()
	object.RetroLines.Frames = nil
	object.Paint.Effects = effects.Compile(effects.Settings{Blur: 8}, object.Bounds.Width, object.Bounds.Height)
	picture, warnings := sceneObjectToPPTX(object)
	if picture == nil || picture.X != object.Bounds.X-64 || picture.Y != object.Bounds.Y-64 {
		t.Fatal("uncaptured effect changed fallback padding", picture)
	}
	found := false
	for _, warning := range warnings {
		if warning.Code == "object-effects-omitted" {
			found = true
		}
	}
	if !found {
		t.Fatal("uncaptured effect was silently omitted")
	}
	if object.Paint.Effects == nil {
		t.Fatal("export modified source scene")
	}
}

func TestCraftEffectsPreserveExistingPaintOverflow(t *testing.T) {
	p := scenePaint{StrokeWidth: 16, ShadowBlur: 8, ShadowX: 7, ShadowY: 19}
	compilePaintEffects(&p, url.Values{"fx-glow-radius": {"12"}}, sceneRect{Width: 200, Height: 100})
	if p.Effects == nil || p.Effects.Padding < 81 {
		t.Fatal("existing outline/shadow would be cropped", p.Effects)
	}
}
