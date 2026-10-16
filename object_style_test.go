package main

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeStyleMenuFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_NATIVE_STYLE_MENU_EVIDENCE")
	if root == "" {
		t.Skip("native menu fixture output not requested")
	}
	deck := labelledSelectionFixture(t)
	labels := cloneSlide(deck.Slides[0])
	labels.ID = "native-labels"
	labels.Elements[0].Query += "&label-modern-size=80&label-fg=%23aa55ff"
	_, table := cellStyleTransferFixture(t)
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"native-cells": table}}
	deck.Slides = []Slide{
		{ID: "native-styles", Elements: []Element{
			{ID: "style-source", Kind: "text", Text: "Native source", Query: "left=10&top=4&width=100&height=10&render=truetype&modern-font=c64&modern-size=120&fg=%23ff55aa"},
			{ID: "style-target", Kind: "text", Text: "Keep this text", Query: "left=110&top=22&width=90&height=10&render=truetype&modern-font=sans&modern-size=72"},
		}}, labels,
		{ID: "native-table", Elements: []Element{{ID: "native-table-a", Kind: "table", ObjectID: "native-cells", Query: "left=16&top=8&width=205&height=38"}}},
	}
	deck.Appearance = &DeckAppearance{Version: 3}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "original.md"), deck); err != nil {
		t.Fatal(err)
	}
}

func styleFixture(t *testing.T) *nativeEditorSession {
	t.Helper()
	s := capabilityFixture()
	s.selection = map[int]bool{0: true}
	s.deck.Theme = builtinDeckThemes()[0].Clone()
	s.deck.Slides[0].Elements[0].Query += "&fg=%23ff0055&modern-opacity=0.6&gradient-start=%23ff0000&gradient-end=%230000ff&gradient-dir=horizontal&shadow=solid&shadow-color=%23ffffff&shadow-x=1&shadow-y=1&ttf-weight=bold"
	s.deck.Slides[0].Elements[1].Query += "&left=20&object-offset-x=0.125&object-rotation=23&link=https%3A%2F%2Fexample.com&group=style-group"
	s.deck.Slides[0].Elements = append(s.deck.Slides[0].Elements, Element{ID: "style-image", Kind: "image", AssetID: "image-id", Path: "keynope-asset:image-id", Query: "left=100&top=3&width=20&height=20&image-style=retro&glyph=braille&brightness=1.7&modern-crop=%7B%7D"})
	return s
}

func TestObjectStyleCopyTransferResetHistory(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "master"}[master], func(t *testing.T) {
			s := styleFixture(t)
			index := 0
			if master {
				s.deck.Masters.Layouts = append(s.deck.Masters.Layouts, MasterLayout{ID: "style-master", Name: "Style", Slide: cloneSlide(s.deck.Slides[0])})
				s.masterMode = true
				s.currentMaster = len(s.deck.Masters.Layouts)
				index = s.currentMaster
			}
			before := cloneDeck(s.deck)
			version := s.version
			copy := nativeEditorAction{Action: "copy-object-style", Slide: index, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"text-a"}}
			if err := s.apply(copy); err != nil {
				t.Fatal(err)
			}
			if s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("copy changed document/history")
			}
			snapshot := s.transportState().StyleClipboard
			bytes, _ := json.Marshal(snapshot)
			if strings.Contains(string(bytes), "First text") || snapshot.Fields["text.size"].Value != "120" || snapshot.Fields["text.bold"].Value != "true" {
				t.Fatal("style copy leaked content or lost typography", string(bytes))
			}
			if master {
				s.selected = 1
				s.selection = map[int]bool{1: true}
			} else {
				s.selected = 1
				s.selection = map[int]bool{1: true}
			}
			s.editingGroup = "style-group"
			action := nativeEditorAction{Action: "paste-object-style", Slide: index, SceneMaster: master, SceneRevision: &version, ObjectIDs: []string{"text-b", "style-image"}, StyleData: snapshot}
			if err := s.apply(action); err != nil {
				t.Fatal(err)
			}
			if s.version != version+1 || len(s.undo) != 1 {
				t.Fatal("style transfer wasn't one transaction")
			}
			var slide Slide
			if master {
				slide = *masterSlideAt(&s.deck, index)
			} else {
				slide = s.deck.Slides[0]
			}
			q, _ := url.ParseQuery(slide.Elements[1].Query)
			if q.Get("modern-size") != "120" || q.Get("modern-font") != "c64" || q.Get("ttf-weight") != "bold" || q.Get("fg") != "#ff0055" || slide.Elements[1].Text != "Second text" {
				t.Fatal("text style transfer failed", q)
			}
			for _, key := range []string{"left", "top", "width", "height", "object-offset-x", "object-rotation", "link", "group"} {
				old, _ := url.ParseQuery(before.Slides[0].Elements[1].Query)
				if q.Get(key) != old.Get(key) {
					t.Fatal("style transfer modified nonstyle", key)
				}
			}
			image, _ := url.ParseQuery(slide.Elements[3].Query)
			if image.Get("modern-opacity") != "0.6" || image.Get("glyph") != "braille" || image.Get("brightness") != "1.7" || image.Get("modern-crop") != "{}" || image.Has("modern-size") || slide.Elements[3].AssetID != "image-id" {
				t.Fatal("image transfer copied typography or changed source/crop", image)
			}
			after := cloneDeck(s.deck)
			version = s.version
			action.SceneRevision = &version
			if err := s.apply(action); err != nil || s.version != version || len(s.undo) != 1 {
				t.Fatal("identical paste was not a no-op", err)
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("style undo lost authored state", err)
			}
			if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
				t.Fatal("style redo failed", err)
			}
			version = s.version
			action.Action = "reset-object-style"
			action.SceneRevision = &version
			if err := s.apply(action); err != nil {
				t.Fatal(err)
			}
			if master {
				slide = *masterSlideAt(&s.deck, index)
			} else {
				slide = s.deck.Slides[0]
			}
			reset, _ := url.ParseQuery(slide.Elements[1].Query)
			if reset.Has("modern-font") || reset.Has("modern-size") || reset.Has("ttf-weight") || reset.Has("fg") || reset.Has("shadow") || reset.Get("left") != "20" || reset.Get("object-rotation") != "23" || slide.Elements[1].Text != "Second text" {
				t.Fatal("reset removed content/geometry or retained overrides", reset)
			}
		})
	}
}

func TestObjectStyleInvalidAndLockedAtomic(t *testing.T) {
	for _, scenario := range []string{"nil", "unknown-property", "version", "colour", "token", "absent-value", "nan", "stale", "lock", "group-lock", "missing", "unsupported"} {
		t.Run(scenario, func(t *testing.T) {
			s := styleFixture(t)
			version := s.version
			snapshot, err := captureObjectStyle(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), "text-a")
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{"text-b", "shape-a"}
			switch scenario {
			case "nil":
				snapshot = nil
			case "unknown-property":
				snapshot.Fields["object-rotation"] = ObjectStyleField{Present: true, Value: "90"}
			case "version":
				snapshot.Version = 5
			case "colour":
				snapshot.Fields["paint.color"] = ObjectStyleField{Present: true, Value: "url(evil)"}
			case "token":
				snapshot.Fields["paint.color"] = ObjectStyleField{Present: true, Value: "#ffffff", Token: "unknown"}
			case "absent-value":
				snapshot.Fields["paint.shadow"] = ObjectStyleField{Value: "solid"}
			case "nan":
				snapshot.Fields["text.size"] = ObjectStyleField{Present: true, Value: "NaN"}
			case "stale":
				version--
			case "lock":
				s.deck.Slides[0].Elements[2].Query += "&object-locked=1"
			case "group-lock":
				s.deck.Slides[0].Elements[2].Query += "&group=style-group&object-locked=1"
				ids = []string{"text-b"}
			case "missing":
				ids = append(ids, "missing")
			case "unsupported":
				s.deck.Slides[0].Elements[2].Kind = "future-widget"
			}
			before := s.state()
			if err := s.apply(nativeEditorAction{Action: "paste-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: ids, StyleData: snapshot}); err == nil || !reflect.DeepEqual(before, s.state()) || len(s.undo) != 0 {
				t.Fatal("invalid style changed state/history", err)
			}
		})
	}
}

func TestObjectStyleTokensAndOwnership(t *testing.T) {
	s := styleFixture(t)
	source := &s.deck.Slides[0].Elements[0]
	q, _ := url.ParseQuery(source.Query)
	q.Del("fg")
	q.Set("theme-fg", "accent-2")
	source.Query = q.Encode()
	snapshot, err := captureObjectStyle(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), source.ID)
	if err != nil || snapshot.Fields["paint.color"].Token != "accent-2" || snapshot.Fields["paint.color"].Value != s.deck.Theme.Colors.Accent[1] {
		t.Fatal("theme token/fallback lost", err, snapshot)
	}
	other := capabilityFixture()
	other.deck.Theme = builtinDeckThemes()[3].Clone()
	version := other.version
	if err := other.apply(nativeEditorAction{Action: "paste-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"text-b"}, StyleData: snapshot}); err != nil {
		t.Fatal(err)
	}
	q, _ = url.ParseQuery(other.deck.Slides[0].Elements[1].Query)
	if q.Get("theme-fg") != "accent-2" || q.Has("fg") {
		t.Fatal("available target token flattened", q)
	}
	before := cloneDeck(other.deck)
	snapshot.Fields["text.size"] = ObjectStyleField{Present: true, Value: "199"}
	if !reflect.DeepEqual(before, other.deck) {
		t.Fatal("snapshot aliases document")
	}
	source.Query += "&object-locked=1"
	version = s.version
	if err := s.apply(nativeEditorAction{Action: "copy-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{source.ID}}); err != nil {
		t.Fatal("locked source could not be read", err)
	}
	state := s.transportState()
	state.StyleClipboard.Fields["text.size"] = ObjectStyleField{Present: true, Value: "900"}
	if s.styleClipboard.Fields["text.size"].Value == "900" {
		t.Fatal("transport aliases clipboard")
	}
	// A deck without an authored theme cannot resolve this source palette/font.
	q, _ = url.ParseQuery(source.Query)
	q.Del("modern-font")
	source.Query = q.Encode()
	snapshot, err = captureObjectStyle(s.deck, s.deck.Slides[0], s.deck.ResolveSlide(0, false), source.ID)
	if err != nil || snapshot.Fields["text.font"].Value != "theme" {
		t.Fatal("missing theme font capture", err)
	}
	withoutTheme := capabilityFixture()
	version = withoutTheme.version
	if err := withoutTheme.apply(nativeEditorAction{Action: "paste-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{"text-b"}, StyleData: snapshot}); err != nil {
		t.Fatal(err)
	}
	q, _ = url.ParseQuery(withoutTheme.deck.Slides[0].Elements[1].Query)
	if q.Has("theme-fg") || q.Get("fg") != snapshot.Fields["paint.color"].Value || q.Get("modern-font") != snapshot.Fields["text.font"].Fallback {
		t.Fatal("unavailable theme token/font did not use captured concrete values", q)
	}
}

func TestObjectStyleResetRichEmphasisKeepsContent(t *testing.T) {
	s := capabilityFixture()
	s.selection = map[int]bool{0: true}
	e := &s.deck.Slides[0].Elements[0]
	encoded, err := encodeModernRuns(e.Text, "", "text", []sceneRun{{Text: "First", Italic: true, Underline: true}, {Text: " text", Bold: true}})
	if err != nil {
		t.Fatal(err)
	}
	e.Query = setQueryValue(e.Query, "modern-runs", encoded)
	before := e.Text
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "reset-object-style", Slide: 0, SceneRevision: &version, ObjectIDs: []string{e.ID}}); err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	if q.Has("modern-runs") || s.deck.Slides[0].Elements[0].Text != before {
		t.Fatal("reset retained emphasis or changed content", q)
	}
}
