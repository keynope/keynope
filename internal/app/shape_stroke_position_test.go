package app

import (
	"encoding/json"
	"net/url"
	"testing"

	"keynope/internal/effects"
)

func TestCraftStrokePositions(t *testing.T) {
	for _, position := range []string{"inside", "center", "outside"} {
		s := capabilityFixture()
		v := s.version
		if err := s.apply(nativeEditorAction{Action: "set-selection-property", Property: "paint.strokePosition", PropertyValue: json.RawMessage(`"` + position + `"`), SceneRevision: &v, ObjectIDs: []string{"shape-a"}}); err != nil {
			t.Fatal(err)
		}
		if state := s.transportState().SelectionCapabilities.Properties["paint.strokePosition"]; state.Value != position {
			t.Fatal(state)
		}
		query := "shape=square&width=40&height=20&shape-stroke=%23ffffff&shape-stroke-width=8&shape-stroke-position=" + position
		q, _ := url.ParseQuery(query)
		p := scenePaintFromValues(Element{Kind: "shape", Query: query}, Slide{}, 1, 1, q, nil)
		p = Deck{}.applyThemeObjectPaint(Element{Kind: "shape", Query: query}, Slide{}, q, p)
		o := sceneObject{Paint: p, Bounds: sceneRect{Width: 200, Height: 100}}
		compileSceneEffects(&o, q)
		in, out := effects.StrokeWidths(position, 8)
		if in+out != 8 || position != "center" && o.Paint.StrokeSpan != 16 {
			t.Fatal("wrong PhotoCraft stroke split", position, o.Paint)
		}
		if got := resolvedShapeStrokeWidth(Slide{}, Element{Kind: "shape", Query: query}, q, 245, 56); got != out*2 {
			t.Fatal("connector edge did not follow outside stroke", got, out)
		}
		if err := validateShapeAdjustments(Element{Kind: "shape", Query: query}); err != nil {
			t.Fatal(err)
		}
		serialized := placementCommentText(query)
		roundtrip, ok := textPlacementComment("<!-- " + serialized + " -->")
		if !ok {
			t.Fatal(serialized)
		}
		parsed, _ := url.ParseQuery(roundtrip)
		if parsed.Get("shape-stroke-position") != position {
			t.Fatal("stroke position lost")
		}
	}
}
