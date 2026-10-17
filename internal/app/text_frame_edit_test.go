package app

import (
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestEmojiFrameContainsArtworkWithoutChangingAuthoredBox(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, align := range []string{"top", "middle", "bottom"} {
		d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{{ID: "flags", Kind: "text", Text: "🇳🇱 🇬🇧 🇪🇸 🇩🇪", Query: "render=truetype&modern-font=c64&modern-size=100&width=114&height=3.75&left=126&top=45&text-valign=" + align}}}}}
		before := cloneDeck(d)
		scene, err := buildMixedSlideScene(d, 0, 245, 56)
		if err != nil || len(scene.Objects) != 1 {
			t.Fatal(err)
		}
		o := scene.Objects[0]
		height := sceneTextWrappedHeight(o.Text, o.Bounds.Width)
		y := 45. * 1080 / 56
		switch align {
		case "middle":
			y += (3.75*1080/56 - height) / 2
		case "bottom":
			y += 3.75*1080/56 - height
		}
		if o.Bounds.Height < height || math.Abs(o.Bounds.Y-y) > .001 || !reflect.DeepEqual(before, d) {
			t.Fatal("emoji frame lost artwork/anchor or mutated document", align, o.Bounds)
		}
	}
}

func TestEditedTextFrameGrowth(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, kind := range []string{"text", "heading", "code", "bullet"} {
		for _, anchor := range []string{"left=20&top=7&object-offset-y=.3", "align=center&bottom=8", "align=center&valign=middle"} {
			t.Run(kind+"/"+anchor, func(t *testing.T) {
				d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{{ID: "text", Kind: kind, Level: 1, Text: "Original", Query: "render=truetype&modern-font=sans&modern-size=80&width=80&height=6&" + anchor}}}}}
				s := newNativeEditorSession("Growth.md", d)
				before, err := buildMixedSlideScene(s.deck, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				original := before.Objects[0]
				revision := s.version
				if err := s.apply(nativeEditorAction{Action: "set-scene-text", Slide: 0, SceneRevision: &revision, ObjectID: "text", TextRuns: []sceneRun{{Text: strings.Repeat("New line\n", 5) + "Last line"}}}); err != nil {
					t.Fatal(err)
				}
				after, err := buildMixedSlideScene(s.deck, 0, 245, 56)
				if err != nil {
					t.Fatal(err)
				}
				o := after.Objects[0]
				if math.Abs(o.Bounds.Y-original.Bounds.Y) > .001 || math.Abs(o.Bounds.X-original.Bounds.X) > .001 || o.Bounds.Width != original.Bounds.Width || o.Text.Size != original.Text.Size || o.Bounds.Height < sceneTextWrappedHeight(o.Text, o.Bounds.Width) {
					t.Fatalf("wrong growth: %+v -> %+v", original.Bounds, o.Bounds)
				}
				if len(s.undo) != 1 {
					t.Fatal("text and growth must be one undo")
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
					t.Fatal(err)
				}
				undone, err := buildMixedSlideScene(s.deck, 0, 245, 56)
				if err != nil || undone.Objects[0].Bounds != original.Bounds || sceneTextPlain(undone.Objects[0].Text) != "Original" {
					t.Fatal("undo lost original frame", err)
				}
			})
		}
	}
}

func TestTextFrameBrowser(t *testing.T) {
	if os.Getenv("KEYNOPE_TEXT_FRAME_BROWSER") != "1" {
		t.Skip("set KEYNOPE_TEXT_FRAME_BROWSER=1 for text frame browser regression")
	}
	useTestAuthoredSize(t, 245, 56)
	d, err := loadPresentationTemplate("clean-light")
	if err != nil {
		t.Fatal(err)
	}
	d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "flags", Kind: "text", Text: "🇳🇱 🇬🇧 🇪🇸 🇩🇪", Query: "top=45&left=126&width=114.0881931435&height=3.7496533674&render=truetype&ttf-size=512&text-box=1&text-align=center&text-valign=middle&modern-font=c64&modern-size=100&modern-width=179.98560115190784"})
	for _, engine := range []string{"chromium", "webkit"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("KEYNOPE_BROWSER_ENGINE", engine)
			s := newNativeEditorSession("Text frames.md", cloneDeck(d))
			server := newGeometryBrowserServer(t, s)
			runGeometryBrowser(t, "tools/test_text_frame_edit.cjs", server.URL)
		})
	}
}
