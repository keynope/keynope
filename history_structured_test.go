package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func BenchmarkEditorStructuredHistorySnapshot(b *testing.B) {
	source := structuredReplacementFixture(b, 24)
	previous := []Deck{cloneDeck(source)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snapshot := cloneEditorHistoryDeck(source, previous)
		if snapshot.Objects == nil {
			b.Fatal("history lost registry")
		}
	}
}

func TestEditorHistoryReusesOnlyImmutablePayloads(t *testing.T) {
	source := structuredReplacementFixture(t, 3)
	source.Objects.Builds["page"] = SlideBuild{Version: 1, Steps: []BuildStep{{TargetIDs: []string{"table"}, Effect: "appear", Advance: "click"}}}
	first := cloneEditorHistoryDeck(source, nil)
	second := cloneEditorHistoryDeck(source, []Deck{first})
	if !reflect.DeepEqual(first, source) || !reflect.DeepEqual(second, source) {
		t.Fatal("history snapshot changed document")
	}
	a, b := first.Objects.Tables["table-1"], second.Objects.Tables["table-1"]
	if &a.Cells[0][0] != &b.Cells[0][0] {
		t.Fatal("unchanged history payload was needlessly deep-copied")
	}
	// The live source must never be one of those immutable shared payloads.
	source.Objects.Tables["table-1"].Cells[0][0].Text = "Live edit"
	source.Objects.Charts["chart-1"].Series[0].Values[0] = 99
	source.Objects.Builds["page"].Steps[0].TargetIDs[0] = "chart"
	if !reflect.DeepEqual(first, second) || first.Objects.Tables["table-1"].Cells[0][0].Text == "Live edit" || first.Objects.Charts["chart-1"].Series[0].Values[0] == 99 {
		t.Fatal("live state aliases immutable history")
	}
	third := cloneEditorHistoryDeck(source, []Deck{second})
	if !reflect.DeepEqual(third, source) || reflect.DeepEqual(third, second) {
		t.Fatal("changed history payload was reused")
	}
	if &third.Objects.Tables["table-1"].Cells[0][0] == &b.Cells[0][0] {
		t.Fatal("changed table still aliases previous snapshot")
	}
	// Restoring and then mutating even unrelated payloads cannot modify any
	// surviving immutable history entry or the originally captured deck.
	restored := restoreEditorHistoryDeck(second)
	restored.Objects.Tables["table-1"].Cells[0][0].Runs[0].Text = "Restored edit"
	restored.Objects.Charts["chart-1"].Series[0].Values[0] = -99
	restored.Objects.Builds["page"].Steps[0].TargetIDs[0] = "changed"
	if !reflect.DeepEqual(first, second) || second.Objects.Tables["table-1"].Cells[0][0].Runs[0].Text == "Restored edit" || second.Objects.Charts["chart-1"].Series[0].Values[0] == -99 {
		t.Fatal("restored editor state aliases surviving history")
	}
	if first.Objects.Builds["page"].Steps[0].TargetIDs[0] != "table" {
		t.Fatal("timeline targets alias live/restored state")
	}
}

func TestEditorHistoryRetainsOnlyChangedTableGenerations(t *testing.T) {
	source := structuredReplacementFixture(t, 2)
	var history []Deck
	first, second := map[*TableCell]bool{}, map[*TableCell]bool{}
	for i := 0; i < 100; i++ {
		if i == 50 {
			source.Objects.Tables["table-0"].Cells[0][0].Text = "One payload change"
		}
		history = append(history, cloneEditorHistoryDeck(source, history))
		first[&history[i].Objects.Tables["table-0"].Cells[0][0]] = true
		second[&history[i].Objects.Tables["table-1"].Cells[0][0]] = true
	}
	if len(first) != 2 || len(second) != 1 {
		t.Fatal("history duplicated unchanged payload bodies", len(first), len(second))
	}
	if history[0].Objects.Tables["table-0"].Cells[0][0].Text == "One payload change" || history[99].Objects.Tables["table-0"].Cells[0][0].Text != "One payload change" {
		t.Fatal("history generations lost their values")
	}
}

func TestEditorHistoryRestoreAndRollbackAreMutableOwned(t *testing.T) {
	for _, master := range []bool{false, true} {
		deck := structuredReplacementFixture(t, 2)
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "marker", Kind: "text", Text: "History", Query: "render=truetype"})
		if master {
			deck.Masters.Base.Slide = cloneSlide(deck.Slides[0])
		}
		s := newNativeEditorSession("history.md", deck)
		s.masterMode, s.currentMaster = master, 0
		paint := func(color string) {
			t.Helper()
			revision := s.version
			value, _ := json.Marshal(color)
			if err := s.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, SceneMaster: master, SceneRevision: &revision, ObjectIDs: []string{"marker"}, Property: "paint.color", PropertyValue: value}); err != nil {
				t.Fatal(err)
			}
		}
		paint("#ff0055")
		one := cloneDeck(s.deck)
		paint("#55aaff")
		two := cloneDeck(s.deck)
		if len(s.undo) != 2 {
			t.Fatal("paint history changed")
		}
		if &s.undo[0].Objects.Tables["table-1"].Cells[0][0] != &s.undo[1].Objects.Tables["table-1"].Cells[0][0] {
			t.Fatal("session did not reuse immutable table data")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, one) {
			t.Fatal("undo changed snapshot", master, err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, two) {
			t.Fatal("redo changed snapshot", master, err)
		}
		undo := make([]Deck, len(s.undo))
		for i := range undo {
			undo[i] = cloneDeck(s.undo[i])
		}
		// A failure restores tx.beforeDeck, which may share immutable history
		// data. It must become an owned mutable registry before publication.
		if err := s.apply(nativeEditorAction{Action: "unsupported-history-probe"}); err == nil {
			t.Fatal("invalid action accepted")
		}
		if !reflect.DeepEqual(s.deck, two) {
			t.Fatal("failure failed to restore document")
		}
		s.deck.Objects.Tables["table-1"].Cells[0][0].Runs[0].Text = "Live after rollback"
		s.deck.Objects.Charts["chart-1"].Series[0].Values[0] = -999
		if !reflect.DeepEqual(s.undo, undo) {
			t.Fatal("rollback left active data aliased to history", master)
		}
	}
}

func TestEditorHistoryRollbackCopiesCachedRegistry(t *testing.T) {
	s := newNativeEditorSession("rollback.md", structuredReplacementFixture(t, 2))
	s.undo = []Deck{cloneEditorHistoryDeck(s.deck, nil)}
	before := cloneDeck(s.deck)
	history := cloneDeck(s.undo[0])
	s.mu.Lock()
	tx := beginEditorCommand(s, true)
	if &tx.beforeDeck.Objects.Tables["table-1"].Cells[0][0] != &s.undo[0].Objects.Tables["table-1"].Cells[0][0] {
		s.mu.Unlock()
		t.Fatal("rollback fixture did not reuse cached history data")
	}
	s.deck.Slides[0].Elements = nil
	failure := errors.New("late transaction rejection")
	tx.finish(&failure)
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("transaction did not restore before state")
	}
	s.deck.Objects.Tables["table-1"].Cells[0][0].Runs[0].Text = "Live after real rollback"
	s.deck.Objects.Charts["chart-1"].Series[0].Values[0] = -999
	if !reflect.DeepEqual(s.undo[0], history) {
		t.Fatal("real rollback published mutable cached payloads")
	}
}
