package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSlideThumbnailInvalidationUsesRetainedPageIdentity(t *testing.T) {
	for _, marker := range []string{
		"const key=index=>masterMode?masterKey+':'+index:(pages.get(index)||'missing:'+index)",
		"WASM workspace deltas retain the exact page objects",
	} {
		if !strings.Contains(exportHTMLSuffix(), marker) {
			t.Fatalf("thumbnail invalidation is missing retained-page contract %q", marker)
		}
	}
	if strings.Contains(exportHTMLSuffix(), "JSON.stringify([cols,rows,masterMode?editorState.masters:pages.get(index)") {
		t.Fatal("thumbnail invalidation still serialises every full page")
	}
}

func TestSlideThumbnailRefreshIsTurnCoalesced(t *testing.T) {
	source := exportHTMLSuffix()
	for _, marker := range []string{
		"window.keynopeScheduleThumbnailRefresh?.();",
		"let slideThumbnailRefreshPending=false;",
		"if(slideThumbnailRefreshPending)return;",
		"slideThumbnailRefreshPending=true;",
		"queueMicrotask(()=>",
		"slideThumbnailRefreshPending=false;",
		"window.keynopeRefreshThumbnails=refreshSlideThumbnails;",
		"window.keynopeScheduleThumbnailRefresh=scheduleSlideThumbnailRefresh;",
	} {
		if !strings.Contains(source, marker) {
			t.Fatalf("thumbnail refresh turn coalescing omitted %q", marker)
		}
	}
	if strings.Count(source, "refreshSlideThumbnails();") != 1 {
		t.Fatalf("internal thumbnail refresh should have one scheduled execution, got %d direct calls", strings.Count(source, "refreshSlideThumbnails();"))
	}
}

func TestEditorCanvasOverlayRefreshIsFrameCoalesced(t *testing.T) {
	source := exportHTMLSuffix()
	for _, marker := range []string{
		"let editorCanvasOverlayFrame = 0;",
		"function scheduleEditorCanvasOverlay()",
		"if (editorCanvasOverlayFrame) return;",
		"editorCanvasOverlayFrame = requestAnimationFrame(() =>",
		"cancelAnimationFrame(editorCanvasOverlayFrame);",
		"if (keynopeAppSurface) scheduleEditorCanvasOverlay();",
	} {
		if !strings.Contains(source, marker) {
			t.Fatalf("editor canvas-overlay coalescing omitted %q", marker)
		}
	}
	if strings.Contains(source, "requestAnimationFrame(renderEditorCanvasOverlay)") {
		t.Fatal("editor canvas overlay still has an uncoalesced frame request")
	}
}

func TestSingleSlideRenderPreview(t *testing.T) {
	for _, style := range []string{"retro", "modern"} {
		deck := sceneFixture(t)
		deck.Appearance = &DeckAppearance{Version: 2, DefaultStyle: style}
		deck.Slides = append(deck.Slides, cloneSlide(deck.Slides[0]))
		for index := range deck.Slides {
			want := deck.ResolveSlide(index, false)
			scene, err := buildDocumentSlideScene(deck, index, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			want.ModernScene = &scene
			got := deck.slideRenderPreview(index, 245, 56)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s slide %d changed projection", style, index)
			}
			if deck.Slides[index].ModernScene != nil {
				t.Fatal("preview mutated authored slide")
			}
		}
	}
}

func BenchmarkSingleSlidePreview(b *testing.B) {
	deck := Deck{Appearance: &DeckAppearance{Version: 2, DefaultStyle: "modern"}}
	for s := 0; s < 30; s++ {
		slide := Slide{PageNumber: "hide"}
		for i := 0; i < 40; i++ {
			slide.Elements = append(slide.Elements, Element{ID: fmt.Sprintf("text-%d-%d", s, i), Kind: "text", Text: "Workshop content", Query: "render=truetype&top=3&left=4&width=30&height=5"})
		}
		deck.Slides = append(deck.Slides, slide)
	}
	for _, all := range []bool{true, false} {
		b.Run(fmt.Sprintf("resolveAll=%t", all), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if all {
					_ = deck.ResolvedSlides()[15]
				} else {
					_ = deck.slideRenderPreview(15, 245, 56)
				}
			}
		})
	}
}
