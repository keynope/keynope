package main

import (
	"net/url"
	"reflect"
	"testing"
)

func TestSceneSharedQueriesPreserveLayerVisibilityAndNames(t *testing.T) {
	slide := Slide{Elements: []Element{
		{ID: "back", Kind: "shape", Query: "group=g&object-name=%20Frame%20&z-index=8"},
		{ID: "front", Kind: "image", Query: "layer=front&object-hidden=1&z-index=2"},
		{ID: "text", Kind: "text", Query: "layer=back&object-name=First&object-name=Second&z-index=bad"},
		{ID: "route", Kind: "connector", Query: "connector-from=front&connector-to=back"},
		{ID: "inherited", Kind: "shape", Inherited: true, Query: "z-index=100"},
		{ID: "malformed", Kind: "shape", Query: "bad=%zz&object-hidden=1&group=kept&object-name=%00"},
	}}
	before := cloneSlide(slide)
	queries := make([]url.Values, len(slide.Elements))
	for i, e := range slide.Elements {
		queries[i], _ = url.ParseQuery(e.Query)
	}
	owned := make([]url.Values, len(queries))
	for i, q := range queries {
		owned[i] = cloneQueryValues(q)
	}
	for pass := 0; pass < 2; pass++ {
		for _, supplied := range [][]url.Values{nil, queries} {
			if got := slidePaintOrderWithQueries(slide, supplied); !reflect.DeepEqual(got, []int{4, 1, 5, 0, 2, 3}) {
				t.Fatal("stack bands, authored ranks or inherited order changed", got)
			}
			if got := hiddenSlideObjectsWithQueries(slide, supplied); !reflect.DeepEqual(got, map[int]bool{1: true, 3: true, 5: true}) {
				t.Fatal("hidden endpoint or partial malformed query changed", got)
			}
		}
		if objectNameFromValues(queries[0]) != "Frame" || objectNameFromValues(queries[2]) != "First" || objectNameFromValues(queries[5]) != "" || queries[5].Get("group") != "kept" {
			t.Fatal("metadata normalization changed")
		}
	}
	if !reflect.DeepEqual(slide, before) || !reflect.DeepEqual(queries, owned) {
		t.Fatal("read-only projection mutated authored data or query slices")
	}
}
