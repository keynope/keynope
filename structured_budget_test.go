package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestStructuredBudgetCountsAndScopedUpdates(t *testing.T) {
	deck := structuredFixture()
	index := newStructuredBudgetIndex(deck.Objects)
	encoded, _ := json.Marshal(deck.Objects)
	if index.usage != (structuredBudgetUsage{Objects: 3, Cells: 4, Points: 2, TextBytes: 23, EncodedBytes: int64(len(encoded))}) {
		t.Fatal("wrong stored-data accounting", index.usage)
	}
	before := *index
	objects := deck.Objects.Clone()
	table := objects.Tables["table-1"]
	table.Cells[1][1].Text += " more"
	objects.Tables["table-1"] = table
	metrics := &structuredBudgetMetrics{}
	touched := map[workspacePayloadReference]bool{{"table", "table-1"}: true}
	next, err := updateStructuredBudget(index, objects, touched, defaultStructuredBudgetLimits, metrics)
	if err != nil || next.usage.TextBytes != 28 || !reflect.DeepEqual(metrics.Measured, []workspacePayloadReference{{"table", "table-1"}}) {
		t.Fatal("rescanned unrelated payload or missed text delta", next, metrics, err)
	}
	if !reflect.DeepEqual(*index, before) {
		t.Fatal("count index mutated in place")
	}
	size := 80.0
	table.Cells[1][1].Style = &TableCellStyle{FontSize: &size}
	objects.Tables["table-1"] = table
	reused, err := updateStructuredBudget(next, objects, touched, defaultStructuredBudgetLimits, nil)
	if err != nil || reused.usage.Cells != next.usage.Cells || reused.usage.TextBytes != next.usage.TextBytes || reused.usage.EncodedBytes <= next.usage.EncodedBytes {
		t.Fatal("style bytes were not accounted independently", err)
	}
	*table.Cells[1][1].Style.FontSize = 81
	objects.Tables["table-1"] = table
	sameSize, err := updateStructuredBudget(reused, objects, touched, defaultStructuredBudgetLimits, nil)
	if err != nil || sameSize != reused {
		t.Fatal("same-size style edit rebuilt count index", err)
	}
	next = sameSize
	objects.Tables["copy"] = cloneTable(table)
	metrics = &structuredBudgetMetrics{}
	added, err := updateStructuredBudget(next, objects, nil, defaultStructuredBudgetLimits, metrics)
	if err != nil || added.usage.Cells != 8 || !reflect.DeepEqual(metrics.Measured, []workspacePayloadReference{{"table", "copy"}}) {
		t.Fatal("new payload budget wrong", added, metrics, err)
	}
	delete(objects.Tables, "copy")
	removed, err := updateStructuredBudget(added, objects, nil, defaultStructuredBudgetLimits, nil)
	if err != nil || removed.usage != next.usage {
		t.Fatal("removed payload retained budget", err)
	}
	raw, err := json.Marshal(deck.Objects)
	if err != nil || strings.Contains(string(raw), "TextBytes") || strings.Contains(string(raw), "entries") {
		t.Fatal("runtime budget entered authored metadata")
	}
}

func TestStructuredBudgetActualChartPointBoundary(t *testing.T) {
	// Reuse immutable fixture slices: exercise the production count boundary,
	// not a rendering/load benchmark or millions of independently allocated nodes.
	chart := ChartData{Version: 1, Type: "column", Categories: make([]string, 2000)}
	values := make([]float64, 2000)
	for i := 0; i < 64; i++ {
		chart.Series = append(chart.Series, ChartSeries{Values: values})
	}
	objects := &DeckObjects{Version: 1, Charts: map[string]ChartData{}}
	for i := 0; i < 15; i++ {
		objects.Charts[fmt.Sprint("chart-", i)] = chart
	}
	if err := objects.Validate(); err != nil {
		t.Fatal("valid 1,920,000-point archive rejected", err)
	}
	objects.Charts["one-too-many"] = chart
	if err := objects.Validate(); err == nil || !strings.Contains(err.Error(), "combined chart points") {
		t.Fatal("individually valid charts bypass combined budget", err)
	}
}

func structuredBudgetSession(t *testing.T, master bool) *nativeEditorSession {
	t.Helper()
	table, _ := newTableData(2, 2)
	deck := Deck{Slides: []Slide{{ID: "budget-slide", Elements: []Element{
		{ID: "table-a", Kind: "table", ObjectID: "table-data", Query: "left=5&top=5&width=100&height=20"},
		{ID: "chart-a", Kind: "chart", ObjectID: "chart-data", Query: "left=110&top=5&width=100&height=30"},
	}}}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"table-data": table}, Charts: map[string]ChartData{"chart-data": newChartData("column")}}}
	if master {
		deck.EnsureDefaultMasters()
		deck.Masters.Base.Slide.Elements = cloneSlide(deck.Slides[0]).Elements
		deck.Slides[0].Elements = nil
	}
	session := newNativeEditorSession("budget.md", deck, false, false)
	session.masterMode = master
	session.structuredLimits = defaultStructuredBudgetLimits
	return session
}

func TestStructuredBudgetNativeAdmissionAtomicAcrossScopes(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, kind := range []string{"cells", "points", "text", "bytes"} {
			t.Run(fmt.Sprintf("master=%v/%s", master, kind), func(t *testing.T) {
				session := structuredBudgetSession(t, master)
				revision := session.version
				action := nativeEditorAction{Slide: 0, SceneMaster: master, SceneRevision: &revision}
				switch kind {
				case "cells":
					session.structuredLimits.Cells = 4
					data, _ := newTableData(3, 2)
					action.Action, action.ObjectID, action.TableData = "set-table", "table-a", &data
				case "points":
					session.structuredLimits.Points = 4
					data := newChartData("column")
					data.Series = append(data.Series, ChartSeries{Values: []float64{1, 2, 3, 4}})
					action.Action, action.ObjectID, action.ChartData = "set-chart-data", "chart-a", &data
				case "text":
					session.structuredLimits.TextBytes = session.structuredBudget.usage.TextBytes + 2
					options := session.deck.Objects.Charts["chart-data"].Options
					options.XTitle = "New title"
					action.Action, action.ObjectID, action.ChartOptions = "set-chart-options", "chart-a", &options
				case "bytes":
					session.structuredLimits.EncodedBytes = session.structuredBudget.usage.EncodedBytes + 2
					options := session.deck.Objects.Charts["chart-data"].Options
					options.Extra = extensionFixture("future", `{"contents":"preserved source metadata"}`)
					action.Action, action.ObjectID, action.ChartOptions = "set-chart-options", "chart-a", &options
				}
				before := session.structuredBudget
				assertEditorFailureUnchanged(t, session, action)
				want := map[string]string{"cells": "table cells", "points": "chart points", "text": "text bytes", "bytes": "metadata bytes"}[kind]
				if err := session.apply(action); err == nil || !strings.Contains(err.Error(), "combined "+want+" limit") {
					t.Fatal("edit did not reach aggregate admission", err)
				}
				if session.structuredBudget != before {
					t.Fatal("rejected edit changed runtime budget")
				}
			})
		}
	}
}

func TestStructuredBudgetHistoryClipboardAndNonPayloadReuse(t *testing.T) {
	for _, master := range []bool{false, true} {
		session := structuredBudgetSession(t, master)
		before := session.structuredBudget
		revision := session.version
		data := cloneTable(session.deck.Objects.Tables["table-data"])
		data.Cells[0][0].Text = "Accepted text"
		if err := session.apply(nativeEditorAction{Action: "set-table", ObjectID: "table-a", TableData: &data, Slide: 0, SceneMaster: master, SceneRevision: &revision}); err != nil {
			t.Fatal(err)
		}
		if session.structuredBudget.usage != newStructuredBudgetIndex(session.deck.Objects).usage {
			t.Fatal("accepted edit drifted from full budget")
		}
		if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil || session.structuredBudget.usage != before.usage {
			t.Fatal("undo budget", err)
		}
		if err := session.apply(nativeEditorAction{Action: "redo"}); err != nil || session.structuredBudget.usage != newStructuredBudgetIndex(session.deck.Objects).usage {
			t.Fatal("redo budget", err)
		}
		index := session.structuredBudget
		revision = session.version
		if err := session.apply(nativeEditorAction{Action: "set-selection-property", ObjectIDs: []string{"table-a"}, Property: "paint.opacity", PropertyValue: json.RawMessage(`0.5`), Slide: 0, SceneMaster: master, SceneRevision: &revision}); err != nil || session.structuredBudget != index {
			t.Fatal("non-payload edit rebuilt count summaries", err)
		}
		var slide Slide
		if master {
			slide = session.deck.Masters.Base.Slide
		} else {
			slide = session.deck.Slides[0]
		}
		bundle, err := makeObjectClipboard(session.deck, slide.Elements)
		if err != nil {
			t.Fatal(err)
		}
		session.structuredLimits.Cells = 4
		beforeDeck := cloneDeck(session.deck)
		undo, redo := len(session.undo), len(session.redo)
		if err := session.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err == nil || !strings.Contains(err.Error(), "combined table cells limit") || !reflect.DeepEqual(session.deck, beforeDeck) || len(session.undo) != undo || len(session.redo) != redo || session.structuredBudget != index {
			t.Fatal("combined clipboard budget was not atomic", err)
		}
	}
}

func TestStructuredBudgetCanonicalBytesAndBuildReconciliation(t *testing.T) {
	for _, objects := range []*DeckObjects{nil, {Version: 1}, structuredFixture().Objects} {
		if objects != nil {
			objects.Extra = extensionFixture("future", `{"opaque":"<>&\\\""}`)
			if objects.Tables == nil {
				objects.Tables = map[string]TableData{}
			}
		}
		usage := newStructuredBudgetIndex(objects).usage
		want := int64(0)
		if objects != nil {
			encoded, err := json.Marshal(objects)
			if err != nil {
				t.Fatal(err)
			}
			want = int64(len(encoded))
		}
		if usage.EncodedBytes != want {
			t.Fatalf("canonical bytes: got %d want %d", usage.EncodedBytes, want)
		}
	}
	for _, master := range []bool{false, true} {
		s := buildEditorFixture(master)
		if err := s.apply(buildAction(s, ownedBuild())); err != nil {
			t.Fatal(err)
		}
		before := s.structuredBudget.usage
		revision := s.version
		if err := s.apply(nativeEditorAction{Action: "delete-objects", ObjectIDs: []string{"first"}, SceneRevision: &revision, SceneMaster: master, Slide: 0}); err != nil {
			t.Fatal(err)
		}
		if s.structuredBudget.usage != newStructuredBudgetIndex(s.deck.Objects).usage || s.structuredBudget.usage.EncodedBytes >= before.EncodedBytes {
			t.Fatal("reconciled timeline retained stale metadata size", master)
		}
	}
}

func TestStructuredBudgetCopyOnWriteChartAdmission(t *testing.T) {
	for _, master := range []bool{false, true} {
		s := structuredBudgetSession(t, master)
		var slide *Slide
		if master {
			slide = &s.deck.Masters.Base.Slide
		} else {
			slide = &s.deck.Slides[0]
		}
		copy := slide.Elements[1]
		copy.ID = "chart-copy"
		slide.Elements = append(slide.Elements, copy)
		s.structuredLimits.Points = 4
		revision, series, color := s.version, 0, "#ff0000"
		action := nativeEditorAction{Action: "set-chart-series-style", ObjectID: "chart-a", ChartSeriesIndex: &series, ChartSeriesColor: &color, Slide: 0, SceneMaster: master, SceneRevision: &revision}
		assertEditorFailureUnchanged(t, s, action)
		if err := s.apply(action); err == nil || !strings.Contains(err.Error(), "combined chart points limit") {
			t.Fatal("copy-on-write styling bypassed point budget", err)
		}
		s.structuredLimits = defaultStructuredBudgetLimits
		if err := s.apply(action); err != nil {
			t.Fatal(err)
		}
		if s.structuredBudget.usage != newStructuredBudgetIndex(s.deck.Objects).usage || s.structuredBudget.usage.Points != 8 {
			t.Fatal("copy-on-write accounting drift")
		}
	}
}

func TestStructuredBudgetFormattingAndTimelineBytes(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, context := range []string{"table-cells", "table-defaults"} {
			s := structuredBudgetSession(t, master)
			s.structuredLimits.EncodedBytes = s.structuredBudget.usage.EncodedBytes
			revision := s.version
			action := nativeEditorAction{Action: "set-selection-property", ObjectID: "table-a", StyleContext: "table-cells", PropertyValue: json.RawMessage(`120`), SceneRevision: &revision, SceneMaster: master}
			if context == "table-cells" {
				action.Property, action.CellRange = "cell.fontSize", &tableCellRange{}
			} else {
				action.Property = "table.fontSize"
			}
			assertEditorFailureUnchanged(t, s, action)
			if err := s.apply(action); err == nil || !strings.Contains(err.Error(), "combined metadata bytes limit") {
				t.Fatal("formatting bypassed metadata budget", master, context, err)
			}
		}
		s := buildEditorFixture(master)
		if err := s.apply(buildAction(s, ownedBuild())); err != nil {
			t.Fatal(err)
		}
		s.structuredLimits = defaultStructuredBudgetLimits
		s.structuredLimits.EncodedBytes = s.structuredBudget.usage.EncodedBytes
		build := ownedBuild()
		build.Extra = extensionFixture("future", `{"description":"additional authored bytes"}`)
		action := buildAction(s, build)
		assertEditorFailureUnchanged(t, s, action)
		if err := s.apply(action); err == nil || !strings.Contains(err.Error(), "combined metadata bytes limit") {
			t.Fatal("timeline extension bypassed metadata budget", err)
		}
	}
}
