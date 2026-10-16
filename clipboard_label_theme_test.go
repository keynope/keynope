package main

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClipboardLabelThemeDefaultsPreservePlainAndAdoptThemed(t *testing.T) {
	source := labelledSelectionFixture(t)
	source.Theme.Fonts.Body = ThemeFont{FontID: "mono", Size: 64, Width: 72, LineHeight: 1.6}
	source.Theme.Colors.Accent[0] = "#ff66cc"
	data, _ := json.Marshal(shapeLabelData{Text: "Shared label", Query: "render=truetype&modern-font=theme&theme-fg=accent-1", Extra: extensionFixture("futureLabel", `{"keep":true}`)})
	source.Slides[0].Elements = source.Slides[0].Elements[:1]
	source.Slides[0].Elements[0].Query = setQueryValue(source.Slides[0].Elements[0].Query, "shape-label", base64.StdEncoding.EncodeToString(data))
	before := cloneDeck(source)
	if output := os.Getenv("KEYNOPE_CLIPBOARD_LABEL_EVIDENCE"); output != "" {
		if err := os.MkdirAll(output, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(output, "source.md"), source); err != nil {
			t.Fatal(err)
		}
	}
	want, err := buildSlideScene(source, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	for _, themed := range []bool{false, true} {
		destination := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "destination"}}}
		if themed {
			destination.Theme = builtinDeckTheme("clean-dark")
			destination.Theme.Fonts.Body = ThemeFont{FontID: "sans", Size: 52, Width: 90, LineHeight: 1.3}
			destination.Theme.Colors.Accent[0] = "#33ccaa"
		}
		if output := os.Getenv("KEYNOPE_CLIPBOARD_LABEL_EVIDENCE"); output != "" {
			name := "plain.md"
			if themed {
				name = "themed.md"
			}
			if err := saveDeck(filepath.Join(output, name), destination); err != nil {
				t.Fatal(err)
			}
		}
		elements, assets, objects, err := prepareObjectClipboardPaste(destination, bundle)
		if err != nil {
			t.Fatal(err)
		}
		destination.Slides[0].Elements, destination.Assets, destination.Objects = elements, assets, objects
		got, err := buildSlideScene(destination, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if !themed {
			if !reflect.DeepEqual(got.Objects[0].Label, want.Objects[0].Label) || !reflect.DeepEqual(got.Objects[0].LabelPaint, want.Objects[0].LabelPaint) {
				t.Fatal("plain paste lost label typography/colour", got.Objects[0].Label, got.Objects[0].LabelPaint)
			}
		} else if got.Objects[0].Label.FontID != "sans" || got.Objects[0].Label.Size != 52 || got.Objects[0].LabelPaint.Color != "#33ccaa" {
			t.Fatal("themed paste flattened label roles", got.Objects[0].Label, got.Objects[0].LabelPaint)
		}
		q, _ := url.ParseQuery(elements[0].Query)
		nested, err := decodeShapeLabelData(q.Get("shape-label"))
		if err != nil || !reflect.DeepEqual(nested.Extra, extensionFixture("futureLabel", `{"keep":true}`)) {
			t.Fatal("paste changed nested metadata", err)
		}
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("copy/paste mutated source")
	}
}

func TestClipboardLabelThemeFallbackRejectsUnrelatedMutations(t *testing.T) {
	source := labelledSelectionFixture(t)
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements[:1])
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"unknown-owner", "unlabelled", "content", "geometry", "shape-paint", "missing-theme"} {
		var copy ObjectClipboard
		if err := json.Unmarshal(wire, &copy); err != nil {
			t.Fatal(err)
		}
		if copy.LabelThemeFallbacks == nil {
			copy.LabelThemeFallbacks = map[string]string{}
		}
		switch scenario {
		case "unknown-owner":
			copy.LabelThemeFallbacks["absent"] = "modern-size=60"
		case "unlabelled":
			copy.Elements[0].Query = "shape=square"
			copy.LabelThemeFallbacks["label-a"] = "modern-size=60"
		case "content":
			copy.LabelThemeFallbacks["label-a"] = "shape-label=changed"
		case "geometry":
			copy.LabelThemeFallbacks["label-a"] = "left=40"
		case "shape-paint":
			copy.LabelThemeFallbacks["label-a"] = "shape-stroke=%23abcdef"
		case "missing-theme":
			copy.ThemeSignature = ""
		}
		s := newNativeEditorSession("Labels.md", Deck{Slides: []Slide{{ID: "destination"}}})
		assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "paste-objects", Clipboard: &copy})
	}
}

func TestClipboardLabelRichOverridesRetainSourceWords(t *testing.T) {
	for _, sparse := range []bool{false, true} {
		source := labelledSelectionFixture(t)
		source.Slides[0].Elements = source.Slides[0].Elements[:1]
		source.Theme.Colors.Accent[1] = "#cc3366"
		source.Theme.Fonts.Heading.FontID = "sans"
		runs := []sceneRun{{Text: "Shared", Color: "#112233", ColorToken: "accent-2", FontID: "mono", FontRole: "heading"}, {Text: " label"}}
		encoded, err := encodeModernRuns("Shared label", "", "text", runs)
		if err != nil {
			t.Fatal(err)
		}
		owner := &source.Slides[0].Elements[0]
		q := mustQuery(owner.Query)
		if sparse {
			q.Set("label-modern-runs", encoded)
		} else {
			data, err := decodeShapeLabelData(q.Get("shape-label"))
			if err != nil {
				t.Fatal(err)
			}
			style := mustQuery(data.Query)
			style.Set("modern-runs", encoded)
			data.Query = style.Encode()
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			q.Set("shape-label", base64.StdEncoding.EncodeToString(raw))
		}
		owner.Query = q.Encode()
		bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
		if err != nil {
			t.Fatal(err)
		}
		destination := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{}}}
		elements, assets, objects, err := prepareObjectClipboardPaste(destination, bundle)
		if err != nil {
			t.Fatal(err)
		}
		destination.Slides[0].Elements, destination.Assets, destination.Objects = elements, assets, objects
		scene, err := buildSlideScene(destination, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, run := range scene.Objects[0].Label.Runs {
			if run.Text == "Shared" {
				found = true
				if run.Color != "#cc3366" || run.FontID != "sans" || run.ColorToken != "" || run.FontRole != "" {
					t.Fatal("copied label word retained destination-dependent roles", sparse, run)
				}
			}
		}
		if !found {
			t.Fatal("copied label word disappeared", sparse)
		}
	}
}
