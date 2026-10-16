package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func BenchmarkSceneColdDenseGeometry(b *testing.B) {
	deck := powerupDenseEditingDeck(b)
	label, err := json.Marshal(shapeLabelData{Text: "Label", Query: "render=truetype&modern-font=sans&modern-size=24"})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 24; i++ {
		shape := Element{ID: fmt.Sprintf("fit-shape-%d", i), Kind: "shape", Query: fmt.Sprintf("shape=circle&left=%d&top=%d&width=12&height=6", i%8*28, i/8*16)}
		shape.Query = setQueryValue(shape.Query, "shape-label", base64.StdEncoding.EncodeToString(label))
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, shape)
	}
	resolved := deck.ResolveSlide(0, false)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics := &sceneRenderMetrics{DisableLayoutCache: true}
		if _, err := buildStyledSceneTargetsMeasured(deck, resolved, 0, 245, 56, false, nil, metrics); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTrueTypeLayoutRowsRetainFallbackAndOwnership(t *testing.T) {
	e := Element{Kind: "text", Text: "Short\nSecond line\n🎉 Unicode", Query: "render=truetype&width=20&height=12"}
	first := trueTypeLayoutRows(e, 245, 56)
	second := trueTypeLayoutRows(e, 245, 56)
	if !reflect.DeepEqual(first, second) || len(first) != 12 {
		t.Fatal("fallback changed")
	}
	for _, row := range first {
		if displayWidth(row) != 20 {
			t.Fatal("row width changed", row)
		}
	}
	first[0], first[5] = "Caller changed", "Another change"
	if second[0] == first[0] || second[5] == first[5] || e.Text != "Short\nSecond line\n🎉 Unicode" {
		t.Fatal("layout slices or input are aliased")
	}
}

func TestPreparedSceneLayoutMatchesLegacyAndOwnsInput(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	// Both layout paths must see the same GIF frame. Comparing successive
	// wall-clock samples can legitimately cross a frame boundary.
	previous := exportImageAnimationPosition
	t.Cleanup(func() { exportImageAnimationPosition = previous })
	for name, deck := range powerupProofCorpus(t) {
		t.Run(name, func(t *testing.T) {
			for i := range deck.Slides {
				prepared := withTrueTypeDefaults(deck.ResolveSlide(i, false))
				prepared = deck.withThemeTypography(prepared)
				prepared = deck.applyShapeStrokeDefaults(prepared)
				fitSlideShapeLabels(&prepared, 245, 56)
				before := cloneSlide(prepared)
				for _, position := range []time.Duration{0, 119 * time.Millisecond, 120 * time.Millisecond, 359 * time.Millisecond} {
					exportImageAnimationPosition = &position
					got := layoutPreparedSlide(prepared, 245, 56)
					want := layout(prepared, 245, 56)
					if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(before, prepared) {
						t.Fatal("prepared layout changed output or source", i, position)
					}
				}
			}
		})
	}
}
