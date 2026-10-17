package app

import (
	"reflect"
	"testing"
)

func TestStructuredPayloadLifecycleAcrossEditorContexts(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, mode := range []string{"normal", "tab", "base", "layout"} {
		t.Run(mode, func(t *testing.T) {
			table, err := newTableData(2, 2)
			if err != nil {
				t.Fatal(err)
			}
			table.Cells[0][0].Text = "Original table"
			chart := newChartData("column")
			owner := Slide{ID: "registry-owner", Elements: []Element{
				{ID: "table-a", Kind: "table", ObjectID: "shared-table", Query: "left=5&top=5&width=60&height=20"},
				{ID: "table-b", Kind: "table", ObjectID: "shared-table", Query: "left=80&top=5&width=60&height=20"},
				{ID: "chart-a", Kind: "chart", ObjectID: "shared-chart", Query: "left=5&top=30&width=60&height=20"},
				{ID: "chart-b", Kind: "chart", ObjectID: "shared-chart", Query: "left=80&top=30&width=60&height=20"},
			}}
			d := Deck{Appearance: &DeckAppearance{Version: 3}, Masters: defaultMasterDeck(), Slides: []Slide{{ID: "ordinary-slide"}}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"shared-table": table}, Charts: map[string]ChartData{"shared-chart": chart}}}
			switch mode {
			case "normal", "tab":
				d.Slides[0] = owner
				if mode == "tab" {
					d.Slides[0].TabID = "registry-tab"
					syncSlideTabs(&d)
				}
			case "base":
				d.Masters.Base.Slide = owner
			case "layout":
				d.Masters.Layouts[0].Slide = owner
			}
			s := newNativeEditorSession("Lifecycle.md", d)
			if mode == "base" || mode == "layout" {
				s.masterMode = true
				if mode == "layout" {
					s.currentMaster = 1
				}
			}
			current := func() *Slide {
				if s.masterMode {
					return masterSlideAt(&s.deck, s.currentMaster)
				}
				return &s.deck.Slides[0]
			}
			command := func(a nativeEditorAction) {
				t.Helper()
				revision := s.version
				a.SceneRevision = &revision
				a.SceneMaster = s.masterMode
				if s.masterMode {
					a.Slide = s.currentMaster
				}
				if err := s.apply(a); err != nil {
					t.Fatalf("%s: %v", a.Action, err)
				}
				if err := validateDeckObjectReferences(s.deck); err != nil {
					t.Fatal("accepted command left invalid graph", err)
				}
			}
			var copies []Element
			for _, id := range []string{"table-a", "chart-a"} {
				index := -1
				for i, e := range current().Elements {
					if e.ID == id {
						index = i
					}
				}
				command(nativeEditorAction{Action: "duplicate-element", Element: index})
				copy := current().Elements[s.selected]
				if copy.ID == id || copy.ObjectID == "shared-table" || copy.ObjectID == "shared-chart" {
					t.Fatal("duplicate retained mutable identity", copy)
				}
				copies = append(copies, copy)
				if copy.Kind == "table" {
					edited := cloneTable(s.deck.Objects.Tables[copy.ObjectID])
					edited.Cells[0][0].Text = "Edited copy"
					command(nativeEditorAction{Action: "set-table", ObjectID: copy.ID, TableData: &edited})
				} else {
					edited := cloneChart(s.deck.Objects.Charts[copy.ObjectID])
					edited.Series[0].Values[0] = 99
					command(nativeEditorAction{Action: "set-chart-data", ObjectID: copy.ID, ChartData: &edited})
				}
			}
			if !reflect.DeepEqual(s.deck.Objects.Tables["shared-table"], table) || !reflect.DeepEqual(s.deck.Objects.Charts["shared-chart"], chart) {
				t.Fatal("copy edit changed shared source payload")
			}
			beforeDelete := cloneDeck(s.deck)
			command(nativeEditorAction{Action: "delete-objects", ObjectIDs: []string{"table-a", "table-b", "chart-a", "chart-b"}})
			deleted := cloneDeck(s.deck)
			if len(s.deck.Objects.Tables) != 2 || len(s.deck.Objects.Charts) != 2 {
				t.Fatal("delete eagerly collected undo-owned payloads")
			}
			version, history := s.version, len(s.undo)
			data, err := serializeDeck("Lifecycle.md", s.deck)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeckData("Elsewhere.md", data)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Objects == nil || len(loaded.Objects.Tables) != 1 || len(loaded.Objects.Charts) != 1 {
				t.Fatal("save did not collect exactly the unreferenced payloads")
			}
			for _, copy := range copies {
				if copy.Kind == "table" {
					if _, ok := loaded.Objects.Tables[copy.ObjectID]; !ok {
						t.Fatal("saved duplicate table missing")
					}
				} else {
					if _, ok := loaded.Objects.Charts[copy.ObjectID]; !ok {
						t.Fatal("saved duplicate chart missing")
					}
				}
			}
			if !reflect.DeepEqual(s.deck, deleted) || s.version != version || len(s.undo) != history {
				t.Fatal("save changed live registry/history")
			}
			command(nativeEditorAction{Action: "undo"})
			if !reflect.DeepEqual(s.deck, beforeDelete) {
				t.Fatal("post-save Undo failed to restore registry owners")
			}
			command(nativeEditorAction{Action: "redo"})
			if !reflect.DeepEqual(s.deck, deleted) {
				t.Fatal("post-save Redo lost payload lifetime")
			}
		})
	}
}

func TestHistoryClonePreservesEmptyContainerIdentity(t *testing.T) {
	d := Deck{Slides: []Slide{{Elements: []Element{}}}, Masters: defaultMasterDeck(), Tabs: []DeckTab{}, Assets: map[string]DeckAsset{}}
	d.Masters.Base.Slide.Elements = []Element{}
	d.Masters.Layouts[0].Slide.Elements = []Element{}
	if !reflect.DeepEqual(d, cloneDeck(d)) {
		t.Fatal("history clone changed nil/empty authored containers")
	}
	nilDeck := Deck{Slides: []Slide{{}}}
	if !reflect.DeepEqual(nilDeck, cloneDeck(nilDeck)) {
		t.Fatal("history clone materialized absent containers")
	}
	s := newNativeEditorSession("Empty.md", d, false)
	s.deck.Masters.Layouts[0].Slide.Elements = []Element{}
	s.deck.Tabs = []DeckTab{}
	s.deck.Assets = map[string]DeckAsset{}
	if !s.confirmSaved("Empty.md", s.version) || s.dirtyLocked() || s.transportState().Dirty {
		t.Fatal("confirmed save of empty containers stayed dirty")
	}
}
