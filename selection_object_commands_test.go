package main

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func objectCommandFixture(t *testing.T, mode string) (*nativeEditorSession, *Slide, int) {
	t.Helper()
	deck := powerupDenseEditingDeck(t)
	source := cloneSlide(deck.Slides[0])
	source.Elements = append(source.Elements, Element{ID: "adjustable", Kind: "shape", Query: "shape=rounded&left=5&top=4&width=30&height=12"})
	deck.Masters = MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base"}}
	deck.Slides = []Slide{source}
	if mode == "base" {
		deck.Masters.Base.Slide = source
		deck.Slides = []Slide{{ID: "empty"}}
	}
	if mode == "layout" {
		deck.Masters.Layouts = []MasterLayout{{ID: "layout", Name: "Layout", Slide: source}}
		deck.Slides = []Slide{{ID: "empty"}}
	}
	if mode == "tab" {
		deck.Slides[0].TabID = "private-tab"
		deck.Tabs = []DeckTab{{ID: "private-tab", Name: "Reference", SlideTab: true, Page: 1}}
	}
	s := newNativeEditorSession("commands.md", deck)
	index := 0
	if mode == "base" || mode == "layout" {
		s.masterMode = true
		if mode == "layout" {
			index = 1
		}
		s.currentMaster = index
		return s, masterSlideAt(&s.deck, index), index
	}
	return s, &s.deck.Slides[0], 0
}

func TestSelectionObjectCommandContextsAndOwnership(t *testing.T) {
	for _, mode := range []string{"normal", "tab", "base", "layout"} {
		t.Run(mode, func(t *testing.T) {
			s, slide, _ := objectCommandFixture(t, mode)
			before := cloneDeck(s.deck)
			for id, names := range map[string][]string{
				"dense-table": {"table.edit", "table.fit", "build.add"}, "dense-chart": {"chart.edit", "build.add"},
				"alpha": {"image.adjust", "image.crop", "image.background", "build.add"}, "adjustable": {"shape.adjust", "build.add"},
			} {
				caps := buildSelectionCapabilities(s.deck, *slide, *slide, []string{id}, "", s.version)
				for _, name := range names {
					c := caps.Commands[name]
					if !c.Enabled || c.TotalCount != 1 || !reflect.DeepEqual(c.EligibleIDs, []string{id}) {
						t.Fatal(id, name, c)
					}
				}
				for i := range slide.Elements {
					if slide.Elements[i].ID == id {
						slide.Elements[i].Query = setQueryValue(slide.Elements[i].Query, "object-locked", "1")
					}
				}
				caps = buildSelectionCapabilities(s.deck, *slide, *slide, []string{id}, "", s.version)
				for _, name := range names {
					c := caps.Commands[name]
					if c.Enabled || c.Reason == "" {
						t.Fatal("locked typed command enabled", id, name, c)
					}
				}
				for i := range slide.Elements {
					if slide.Elements[i].ID == id {
						q, _ := url.ParseQuery(slide.Elements[i].Query)
						q.Del("object-locked")
						slide.Elements[i].Query = q.Encode()
					}
				}
			}
			if !reflect.DeepEqual(s.deck, before) || len(s.undo) != 0 {
				t.Fatal("capability reads changed source/history")
			}
		})
	}
}

func TestSelectionObjectCommandSubsetGroupAndMissingData(t *testing.T) {
	s, slide, _ := objectCommandFixture(t, "normal")
	ids := []string{"alpha", "vector", "dense-text-0"}
	caps := buildSelectionCapabilities(s.deck, *slide, *slide, ids, "", s.version)
	c := caps.Commands["image.adjust"]
	if !c.Enabled || c.TotalCount != 3 || !reflect.DeepEqual(c.EligibleIDs, []string{"alpha", "vector"}) || caps.Commands["image.crop"].Enabled {
		t.Fatal("wrong mixed image subset", caps.Commands)
	}
	for i := range slide.Elements {
		if slide.Elements[i].ID == "dense-text-0" {
			slide.Elements[i].Query = setQueryValue(slide.Elements[i].Query, "object-locked", "1")
		}
	}
	if buildSelectionCapabilities(s.deck, *slide, *slide, ids, "", s.version).Commands["image.adjust"].Enabled {
		t.Fatal("locked non-image member bypassed atomic selection lock")
	}
	for i := range slide.Elements {
		if slide.Elements[i].ID == "dense-table" || slide.Elements[i].ID == "dense-chart" {
			slide.Elements[i].Query = setQueryValue(slide.Elements[i].Query, "group", "typed-group")
		}
	}
	for _, pair := range []struct{ id, name string }{{"dense-table", "table.edit"}, {"dense-chart", "chart.edit"}} {
		c := buildSelectionCapabilities(s.deck, *slide, *slide, []string{pair.id}, "", s.version).Commands[pair.name]
		if c.Enabled || c.Reason == "" {
			t.Fatal("collapsed group exposed independent edit", c)
		}
		if !buildSelectionCapabilities(s.deck, *slide, *slide, []string{pair.id}, "typed-group", s.version).Commands[pair.name].Enabled {
			t.Fatal("entered child lost edit", pair)
		}
	}
	// The target list of a build is all-or-nothing, not a silent visible subset.
	for i := range slide.Elements {
		if slide.Elements[i].ID == "alpha" {
			slide.Elements[i].Query = setQueryValue(slide.Elements[i].Query, "object-hidden", "1")
		}
	}
	if buildSelectionCapabilities(s.deck, *slide, *slide, []string{"alpha", "vector"}, "", s.version).Commands["build.add"].Enabled {
		t.Fatal("hidden build target silently omitted")
	}
	delete(s.deck.Objects.Tables, "table-dense")
	if buildSelectionCapabilities(s.deck, *slide, *slide, []string{"dense-table"}, "typed-group", s.version).Commands["table.edit"].Enabled {
		t.Fatal("missing payload advertised as editable")
	}
	missing := cloneSlide(*slide)
	missing.Elements = append(missing.Elements, Element{ID: "reference", Kind: "image", Path: "/not/imported.png"})
	for _, name := range []string{"image.background", "image.crop"} {
		if buildSelectionCapabilities(s.deck, missing, missing, []string{"reference"}, "", s.version).Commands[name].Enabled {
			t.Fatal("nonembedded image advertised for source-pixel operation", name)
		}
	}
	resolved := cloneSlide(*slide)
	for i := range resolved.Elements {
		if resolved.Elements[i].ID == "adjustable" {
			resolved.Elements[i].Query = setQueryValue(resolved.Elements[i].Query, "shape", "circle")
		}
	}
	if buildSelectionCapabilities(s.deck, *slide, resolved, []string{"adjustable"}, "", s.version).Commands["shape.adjust"].Enabled {
		t.Fatal("shape handles ignored resolved master type")
	}
}

func TestSelectionObjectCommandMutationRechecksGroupAndLocks(t *testing.T) {
	for _, mode := range []string{"normal", "tab", "base", "layout"} {
		for _, kind := range []string{"table", "chart"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				s, slide, index := objectCommandFixture(t, mode)
				id := "dense-" + kind
				for i := range slide.Elements {
					if slide.Elements[i].ID == id || slide.Elements[i].ID == "adjustable" {
						slide.Elements[i].Query = setQueryValue(slide.Elements[i].Query, "group", "typed-group")
					}
				}
				data := nativeEditorAction{Action: "set-" + kind, Slide: index, SceneMaster: s.masterMode, ObjectID: id}
				if kind == "table" {
					table := cloneTable(s.deck.Objects.Tables["table-dense"])
					table.Cells[0][0].Text = "Edited"
					data.TableData = &table
				} else {
					data.Action = "set-chart-data"
					chart := cloneChart(s.deck.Objects.Charts["dense-chart"])
					chart.Title = "Edited"
					data.ChartData = &chart
				}
				before := cloneDeck(s.deck)
				revision := s.version
				data.SceneRevision = &revision
				if err := s.apply(data); err == nil {
					t.Fatal("forged independent child edit accepted without entering group")
				}
				if !reflect.DeepEqual(s.deck, before) || s.version != revision || len(s.undo) != 0 {
					t.Fatal("rejected child edit mutated source/history")
				}
				s.editingGroup = "typed-group"
				if err := s.apply(data); err != nil {
					t.Fatal("entered child edit rejected", err)
				}
				if len(s.undo) != 1 {
					t.Fatal("child edit not one transaction")
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
					t.Fatal("child undo", err)
				}
				// A stale dialog cannot evade a newly acquired object lock.
				slide = &s.deck.Slides[0]
				if s.masterMode {
					slide = masterSlideAt(&s.deck, index)
				}
				for i := range slide.Elements {
					if slide.Elements[i].ID == id {
						slide.Elements[i].Query = setQueryValue(slide.Elements[i].Query, "object-locked", "1")
					}
				}
				revision = s.version
				data.SceneRevision = &revision
				before = cloneDeck(s.deck)
				if err := s.apply(data); err == nil || !reflect.DeepEqual(s.deck, before) {
					t.Fatal("new lock bypassed typed dispatch", err)
				}
				if _, err := json.Marshal(s.transportState().SelectionCapabilities); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
