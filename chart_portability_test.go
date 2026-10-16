package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestChartsSharedSurfaceParityAndPublicArchive(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		t.Run(kind, func(t *testing.T) {
			data := newChartData(kind)
			data.Title = "Workshop outcomes"
			data.Options.DataLabels = true
			extra := &jsonExtensions{"private": json.RawMessage(`"PRIVATE EXTENSION"`)}
			data.Extra, data.Options.Extra, data.Series[0].Extra = extra, extra, extra
			data.Series[0].Color, data.Series[0].ColorToken = "#112233", "accent-2"
			if kind == "pie" || kind == "doughnut" {
				data.CategoryColors = []ChartColorStyle{{Color: "#112233", ColorToken: "accent-2", Extra: extra}, {Color: "#ff55aa"}}
			}
			deck := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1, Charts: map[string]ChartData{"data": data, "unused": newChartData("bar")}}, Slides: []Slide{{ID: "slide", Notes: "PRIVATE NOTES", Engagement: &EngagementDefinition{ID: "private-activity", Kind: "storm", Prompt: "PRIVATE PROMPT"}, Elements: []Element{{ID: "chart", Kind: "chart", ObjectID: "data", Query: "left=24&top=8&width=170&height=36"}}}}}
			before, _ := json.Marshal(deck)
			local, err := buildDocumentSlideScene(deck, 0, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			participant, err := participantRenderedDeck(deck, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*participant.Pages[0].Scene, local) {
				t.Fatal("participant geometry differs")
			}
			_, presenter, err := projectPresenterDocument(filepath.Join(t.TempDir(), "chart.md"), deck.ResolvedSlides(), 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			exported := projectExportDeck("chart.md", deck.ResolvedSlides(), 245, 56, false)
			if len(presenter) != 1 || len(presenter[0]) != 1 || len(exported.Pages) != 1 || !reflect.DeepEqual(presenter[0][0].Scene, &local) || !reflect.DeepEqual(exported.Pages[0].Scene, &local) {
				t.Fatal("presenter/export geometry differs")
			}
			md, err := participantSlideMarkdown(deck, 0)
			if err != nil {
				t.Fatal(err)
			}
			public, err := parseDeckData("Presentation.md", md)
			if err != nil {
				t.Fatal(err)
			}
			if public.Objects == nil || len(public.Objects.Charts) != 1 || len(public.Objects.Tables) != 0 || len(public.Objects.Builds) != 0 {
				t.Fatal("public archive not dependency-scoped")
			}
			for _, value := range []any{public, participant, exported} {
				raw, err := json.Marshal(value)
				if err != nil || bytes.Contains(raw, []byte("PRIVATE")) {
					t.Fatal("authoring metadata disclosed", err)
				}
			}
			rebuilt, err := buildDocumentSlideScene(public, 0, 245, 56)
			if err != nil || len(rebuilt.Objects) != 1 || !reflect.DeepEqual(rebuilt.Objects[0].Chart, local.Objects[0].Chart) {
				t.Fatal("public Markdown lost geometry", err)
			}
			public.Objects.Charts["data"].Series[0].Values[0] = 999
			after, _ := json.Marshal(deck)
			if !bytes.Equal(before, after) {
				t.Fatal("projection aliases/mutates authored payload")
			}
		})
	}
}

func TestChartClipboardCollisionAndUndo(t *testing.T) {
	data := newChartData("column")
	source := Deck{Objects: &DeckObjects{Version: 1, Charts: map[string]ChartData{"data": data}}, Slides: []Slide{{Elements: []Element{{ID: "chart", Kind: "chart", ObjectID: "data", Query: "width=170&height=36"}}}}}
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	s := newNativeEditorSession("copy.md", cloneDeck(source), false, false)
	before := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
		t.Fatal(err)
	}
	pasted := s.deck.Slides[0].Elements[1]
	if pasted.ID == "chart" || !reflect.DeepEqual(s.deck.Objects.Charts[pasted.ObjectID], data) || len(s.undo) != 1 {
		t.Fatal("chart paste lost data/identity/history")
	}
	after := cloneDeck(s.deck)
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("chart paste undo", err)
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
		t.Fatal("chart paste redo", err)
	}
}
