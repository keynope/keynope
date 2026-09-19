package main

import "testing"

func TestSceneHeadingSemantics(t *testing.T) {
	for _, level := range []int{1, 2, 3, 4, 5, 6, 0, 7} {
		text := sceneTextFor(Element{Kind: "heading", Level: level, Text: "Heading"})
		want := level
		if want < 1 || want > 6 {
			want = 1
		}
		if text.Role != "heading" || text.Level != want {
			t.Fatalf("heading level %d: %#v", level, text)
		}
	}
	if text := sceneTextFor(Element{Kind: "text", Level: 2, Text: "Body"}); text.Level != 0 {
		t.Fatal("body text acquired heading semantics")
	}
}
