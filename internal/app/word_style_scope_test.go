package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

func TestWordStyleScopeParityOwnershipAndOmissions(t *testing.T) {
	d, _ := wordStyleFixture(t)
	d.Slides = append([]Slide{{ID: "unused", Notes: "Private notes", EngagementResult: &EngagementResult{Version: 1, State: map[string]json.RawMessage{"private": json.RawMessage(`"answer"`)}}}}, d.Slides...)
	d.Slides[1].LayoutID = "bound"
	d.Masters.Base.Slide = Slide{TTFSize: 90, FG: "38;2;3;4;5", FGSet: true, Elements: []Element{{ID: "base-extra", Kind: "text", Text: "Inherited", Query: "fg=%23abcdef"}}}
	d.Masters.Layouts = []MasterLayout{{ID: "unrelated", Slide: Slide{Notes: "Unused", Elements: []Element{{ID: "unused-master", Kind: "text", Text: "Heavy"}}}}, {ID: "bound", Slide: Slide{TTFWidth: 75, Elements: []Element{{ID: "slot", Kind: "text", PlaceholderRole: "body", SlotID: "slot", Query: "modern-size=90&modern-font=mono"}}}}}
	d.Slides[1].Elements[0].MasterSlotID = "slot"
	d.Slides[1].Elements[0].Extra = &jsonExtensions{"owned": json.RawMessage(`{"value":1}`)}
	d.Assets = map[string]DeckAsset{"huge": {Data: make([]byte, 1<<20)}}
	table, _ := newTableData(20, 20)
	d.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"unused": table}}
	for _, mode := range []int{0, 1, 2} {
		a := nativeEditorAction{ObjectID: "words", Slide: 1}
		if mode > 0 {
			a.SceneMaster = true
			a.Slide = mode
			source := *masterSlideAt(&d, mode)
			source.Elements = append(source.Elements, Element{ID: "words", Kind: "text", Text: "Master", Query: "modern-size=90"})
			*masterSlideAt(&d, mode) = source
		}
		want, expectedErr := textRangeStyleTarget(d, a, "")
		scope, err := captureWordStyleScope(d, a)
		if err != nil {
			t.Fatal(err)
		}
		got, err := scope.owner(a, "")
		if (err != nil) != (expectedErr != nil) || err == nil && !reflect.DeepEqual(got, want) {
			t.Fatal("scoped owner differs", mode, got, want, err, expectedErr)
		}
		if objectStyleThemeSignature(scope.deck) != objectStyleThemeSignature(d) {
			t.Fatal("compatibility/theme association changed")
		}
		if scope.deck.Assets != nil || scope.deck.Objects != nil || scope.deck.Slides != nil || scope.source.Notes != "" || scope.source.EngagementResult != nil {
			t.Fatal("unrelated/private/heavy data captured")
		}
		if len(scope.deck.Masters.Layouts) > 1 {
			t.Fatal("unrelated layouts captured")
		}
		before := cloneDeck(d)
		scope.deck.Theme.Colors.Accent[0] = "#abcdef"
		scope.source.Elements[0].Text = "Changed"
		if scope.source.Elements[0].Extra != nil {
			(*scope.source.Elements[0].Extra)["owned"] = json.RawMessage(`false`)
		}
		scope.deck.Masters.Base.Slide.Elements[0].Text = "Changed base"
		if !reflect.DeepEqual(d, before) {
			t.Fatal("scoped mutable metadata aliases document")
		}
	}
}

func TestWordStyleScopeOriginalIndexAndFailures(t *testing.T) {
	d, _ := wordStyleFixture(t)
	d.Slides = append([]Slide{{}}, d.Slides...)
	d.Slides[1].Elements[0].ID = "keynope-page-number-0"
	d.Slides[1].LayoutID = "attached"
	d.Slides[1].PageNumber = pageNumberShow
	d.Masters.Layouts = []MasterLayout{{ID: "attached"}}
	a := nativeEditorAction{Slide: 1, ObjectID: "keynope-page-number-0"}
	scope, err := captureWordStyleScope(d, a)
	if err != nil {
		t.Fatal(err)
	}
	got, err := scope.owner(a, "")
	want, originalErr := textRangeStyleTarget(d, a, "")
	if err != nil || originalErr != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("rebased page-number identity collides with author", got, err, originalErr)
	}
	for _, change := range []func(*Deck){
		func(d *Deck) { d.Slides[1].Elements = append(d.Slides[1].Elements, d.Slides[1].Elements[0]) },
		func(d *Deck) {
			d.Slides[1].Elements[0].Query = setQueryValue(d.Slides[1].Elements[0].Query, "object-locked", "1")
		},
		func(d *Deck) { d.Slides[1].Elements[0].Inherited = true },
	} {
		copy := cloneDeck(d)
		change(&copy)
		scope, err := captureWordStyleScope(copy, a)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := scope.owner(a, ""); err == nil {
			t.Fatal("invalid scoped target accepted")
		}
	}
}

func TestWordStyleScopeTableDependencyMetadataAndDuplicateLayouts(t *testing.T) {
	d := tableWordThemeFixture(t)
	a := nativeEditorAction{ObjectID: "table-word", TableWord: &tableWordContext{Row: 0, Column: 1, Rows: 2, Columns: 2}}
	scope, err := captureWordStyleScope(d, a)
	if err != nil {
		t.Fatal(err)
	}
	if scope.tables["table-word-data"] != (wordTableInfo{1, 2, 2}) || scope.deck.Objects != nil {
		t.Fatal("table content was captured instead of dependency metadata")
	}
	delete(d.Objects.Tables, "table-word-data")
	if _, err := scope.owner(a, ""); err != nil {
		t.Fatal("captured presence aliases registry", err)
	}
	// Preserve existing first-ID lookup in master previews without mistaking
	// the selected authored master for its resolved layout winner.
	d.Masters.Layouts = []MasterLayout{{ID: "duplicate", Slide: Slide{Elements: []Element{{ID: "first", Kind: "text", Text: "First"}}}}, {ID: "duplicate", Slide: Slide{Elements: []Element{{ID: "second", Kind: "text", Text: "Second", Query: "modern-size=90"}}}}}
	a = nativeEditorAction{SceneMaster: true, Slide: 2, ObjectID: "second"}
	want, werr := textRangeStyleTarget(d, a, "")
	scope, err = captureWordStyleScope(d, a)
	if err != nil {
		t.Fatal(err)
	}
	got, gerr := scope.owner(a, "")
	if (werr != nil) != (gerr != nil) || gerr == nil && !reflect.DeepEqual(got, want) {
		t.Fatal("duplicate layout lookup semantics changed", got, want, gerr, werr)
	}
}

func wordStyleHeavyFixture(t testing.TB) Deck {
	t.Helper()
	d := Deck{Theme: builtinDeckTheme("clean-light"), Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "selected", Elements: []Element{{ID: "target", Kind: "text", Text: "Selected word", Query: "modern-font=sans&modern-size=64"}}}}}
	d.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{}}
	for i := 0; i < 50; i++ {
		table, _ := newTableData(20, 20)
		for r := range table.Cells {
			for c := range table.Cells[r] {
				table.Cells[r][c].Text = "Unrelated cell"
				table.Cells[r][c].Extra = &jsonExtensions{"future": json.RawMessage(`{"value":42}`)}
			}
		}
		d.Objects.Tables["unused-"+strconv.Itoa(i)] = table
		d.Slides = append(d.Slides, Slide{ID: "slide-" + strconv.Itoa(i), Elements: []Element{{ID: "table-" + strconv.Itoa(i), Kind: "table", ObjectID: "unused-" + strconv.Itoa(i)}}})
	}
	return d
}

func BenchmarkWordStylePreparationScope(b *testing.B) {
	d := wordStyleHeavyFixture(b)
	a := nativeEditorAction{ObjectID: "target"}
	b.Run("whole-deck", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			snapshot := cloneDeckForScene(d)
			if len(snapshot.Slides) == 0 {
				b.Fatal("missing snapshot")
			}
		}
	})
	b.Run("scoped", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			snapshot, err := captureWordStyleScope(d, a)
			if err != nil || len(snapshot.source.Elements) != 1 {
				b.Fatal("missing scope", err)
			}
		}
	})
}

func TestWordStyleScopeAllocationIndependentOfUnrelatedPayloads(t *testing.T) {
	heavy := wordStyleHeavyFixture(t)
	light := Deck{Theme: heavy.Theme, Appearance: heavy.Appearance, Slides: heavy.Slides[:1]}
	a := nativeEditorAction{ObjectID: "target"}
	allocs := func(d Deck) float64 {
		return testing.AllocsPerRun(20, func() {
			scope, err := captureWordStyleScope(d, a)
			if err != nil || len(scope.source.Elements) != 1 || len(scope.tables) != 0 {
				t.Fatal("bad scope", err)
			}
		})
	}
	x, y := allocs(light), allocs(heavy)
	if y > x+2 {
		t.Fatalf("unrelated deck size grows snapshot allocations: light %.0f heavy %.0f", x, y)
	}
}

func TestWordStyleHTTPPreparationIndependentOfUnrelatedPayloads(t *testing.T) {
	heavy := wordStyleHeavyFixture(t)
	light := Deck{Theme: heavy.Theme, Appearance: heavy.Appearance, Slides: heavy.Slides[:1]}
	allocs := func(d Deck) float64 {
		s := newNativeEditorSession("Owned.md", d)
		version := s.version
		raw, err := json.Marshal(nativeEditorAction{Action: "text-style-capabilities", StyleContext: "text-range", SceneRevision: &version, ObjectID: "target", TextRuns: []sceneRun{{Text: "Word"}}, TextRange: &textStyleRange{0, 4}})
		if err != nil {
			t.Fatal(err)
		}
		return testing.AllocsPerRun(10, func() {
			w := httptest.NewRecorder()
			s.handleTextStyleDraft(w, httptest.NewRequest(http.MethodPost, "/api/editor/text-style-draft", bytes.NewReader(raw)))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	x, y := allocs(light), allocs(heavy)
	if y > x+10 {
		t.Fatalf("HTTP preparation clones unrelated payloads: light %.0f heavy %.0f", x, y)
	}
}
