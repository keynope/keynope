package main

import (
	"fmt"
	"testing"
)

func TestSceneEditingIdentityLookup(t *testing.T) {
	authored := []Element{
		{ID: "text", Kind: "text"},
		{ID: "locked", Kind: "text", Query: "object-locked=1"},
		{ID: "protected", Kind: "text", PlaceholderRole: activityTitleRole},
		{ID: "bullet", Kind: "bullet", Text: "One\nTwo"},
		{ID: "shape", Kind: "shape"},
		{ID: "badshape", Kind: "shape", Query: "shape-label=invalid"},
		{ID: "image", Kind: "image"},
		{ID: "missing-media", Kind: "image"},
		{Kind: "text"},
		{ID: "duplicate", Kind: "text"},
		{ID: "duplicate", Kind: "shape", Query: "shape-label=invalid"},
	}
	scene := slideScene{}
	for _, id := range []string{"text", "locked", "protected", "bullet", "shape", "badshape", "image", "missing-media", "", "inherited", "duplicate"} {
		o := sceneObject{ID: id}
		if id == "image" {
			o.Media = &sceneMedia{}
		}
		scene.Objects = append(scene.Objects, o)
	}
	annotateSceneEditing(&scene, Deck{}, authored)
	for _, o := range scene.Objects {
		want := o.ID == "text" || o.ID == "bullet" || o.ID == "shape" || o.ID == "image"
		if o.Editable != want {
			t.Errorf("%q editable=%v want=%v", o.ID, o.Editable, want)
		}
		if o.ID == "bullet" && len(o.EditRuns) == 0 {
			t.Error("missing bullet editor runs")
		}
		if o.ID == "shape" && (o.Label == nil || o.LabelPaint == nil) {
			t.Error("missing draft shape label")
		}
	}
}

func BenchmarkSceneEditingLookup(b *testing.B) {
	for _, count := range []int{100, 500, 1000} {
		for _, indexed := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/indexed=%v", count, indexed), func(b *testing.B) {
				authored := make([]Element, count)
				scene := slideScene{Objects: make([]sceneObject, count)}
				for i := range authored {
					id := fmt.Sprint("text-", i)
					authored[i] = Element{ID: id, Kind: "text", Query: "render=truetype&modern-size=32&left=1&top=3&width=30&height=5"}
					scene.Objects[i] = sceneObject{ID: id}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					if indexed {
						annotateSceneEditing(&scene, Deck{}, authored)
						continue
					}
					// The previous endpoint loop parsed lock metadata before
					// comparing IDs, even for unrelated authored objects.
					for i := range scene.Objects {
						for _, e := range authored {
							if objectLocked(e) {
								continue
							}
							if e.ID == scene.Objects[i].ID && e.ID != "" && e.Kind == "text" && !protectedActivityElement(e) {
								scene.Objects[i].Editable = true
							}
						}
					}
				}
			})
		}
	}
}
