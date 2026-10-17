package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestStructuredReplacementRegistryContainerOwnership(t *testing.T) {
	fields := []string{"Version", "Tables", "Charts", "Builds", "Extra"}
	typ := reflect.TypeOf(DeckObjects{})
	if typ.NumField() != len(fields) {
		t.Fatal("new registry fields require an explicit replacement ownership decision")
	}
	for index, name := range fields {
		if typ.Field(index).Name != name {
			t.Fatal("registry field ownership guard changed", name)
		}
	}
	if (*DeckObjects)(nil).cloneForPayloadReplacement() != nil {
		t.Fatal("nil registry became present")
	}
	empty := (&DeckObjects{Version: 1}).cloneForPayloadReplacement()
	if empty.Tables != nil || empty.Charts != nil || empty.Builds != nil {
		t.Fatal("nil registry namespaces became empty maps")
	}
	source := structuredReplacementFixture(t, 2).Objects
	source.Extra = &jsonExtensions{"future": json.RawMessage(`{"keep":true}`)}
	source.Builds["page"] = SlideBuild{Version: 1}
	before := source.Clone()
	copy := source.cloneForPayloadReplacement()
	delete(copy.Tables, "table-0")
	delete(copy.Charts, "chart-0")
	delete(copy.Builds, "page")
	(*copy.Extra)["future"][0] = '['
	if !reflect.DeepEqual(source, before) {
		t.Fatal("replacement container or root extensions alias source")
	}
	// The ordinary clone API is still fully deep, not the restricted command
	// helper: callers can independently mutate any cloned nested payload.
	deep := source.Clone()
	deep.Tables["table-1"].Cells[0][0].Runs[0].Text = "Deep clone edit"
	deep.Charts["chart-1"].Series[0].Values[0] = -999
	if !reflect.DeepEqual(source, before) {
		t.Fatal("general Clone contract changed")
	}
}

func structuredReplacementFixture(t testing.TB, count int) Deck {
	t.Helper()
	table, err := newTableData(20, 20)
	if err != nil {
		t.Fatal(err)
	}
	for row := range table.Cells {
		for col := range table.Cells[row] {
			table.Cells[row][col].Text = fmt.Sprintf("Cell %d:%d", row, col)
			table.Cells[row][col].Runs = []ObjectTextRun{{Text: "Rich cell", Bold: true}}
		}
	}
	objects := &DeckObjects{Version: 1, Tables: map[string]TableData{}, Charts: map[string]ChartData{}, Builds: map[string]SlideBuild{}}
	for i := 0; i < count; i++ {
		objects.Tables[fmt.Sprintf("table-%d", i)] = cloneTable(table)
		objects.Charts[fmt.Sprintf("chart-%d", i)] = newChartData("column")
	}
	return Deck{Objects: objects, Slides: []Slide{{ID: "page", Elements: []Element{{ID: "table", Kind: "table", ObjectID: "table-0"}, {ID: "chart", Kind: "chart", ObjectID: "chart-0"}}}}}
}

func BenchmarkStructuredSinglePayloadReplacement(b *testing.B) {
	for _, kind := range []string{"table", "chart"} {
		b.Run(kind, func(b *testing.B) {
			source := structuredReplacementFixture(b, 24)
			table := cloneTable(source.Objects.Tables["table-0"])
			table.Cells[0][0].Text = "Changed"
			chart := cloneChart(source.Objects.Charts["chart-0"])
			chart.Series[0].Values[0]++
			action := nativeEditorAction{Slide: 0, ObjectID: kind, TableData: &table, ChartData: &chart}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				candidate := source
				candidate.Slides = []Slide{cloneSlide(source.Slides[0])}
				var changed bool
				var err error
				if kind == "table" {
					action.Action = "set-table"
					changed, err = applyTableCommand(&candidate, action, "")
				} else {
					action.Action = "set-chart-data"
					changed, err = applyChartCommand(&candidate, action, "")
				}
				if err != nil || !changed {
					b.Fatal("replacement failed", err)
				}
			}
		})
	}
}

func TestStructuredReplacementChainOwnsChangedPayloads(t *testing.T) {
	source := structuredReplacementFixture(t, 4)
	original := cloneDeck(source)
	candidate := source
	candidate.Slides = []Slide{cloneSlide(source.Slides[0])}
	// Every subsequent edit must own its replacement, even though unchanged
	// payloads may be shared read-only by earlier registry containers.
	for index := 0; index < 4; index++ {
		for _, kind := range []string{"table", "chart"} {
			id := fmt.Sprintf("%s-%d", kind, index)
			candidate.Slides[0].Elements = append(candidate.Slides[0].Elements, Element{ID: "instance", Kind: kind, ObjectID: id})
			before := cloneDeck(candidate)
			action := nativeEditorAction{Slide: 0, ObjectID: "instance"}
			var changed bool
			var err error
			if kind == "table" {
				data := cloneTable(candidate.Objects.Tables[id])
				data.Cells[0][0].Runs[0].Text = "Edited rich text"
				action.Action, action.TableData = "set-table", &data
				changed, err = applyTableCommand(&candidate, action, "")
				data.Cells[0][0].Runs[0].Text = "Caller corruption"
			} else {
				data := cloneChart(candidate.Objects.Charts[id])
				data.Series[0].Values[0] = 99
				action.Action, action.ChartData = "set-chart-data", &data
				changed, err = applyChartCommand(&candidate, action, "")
				data.Series[0].Values[0] = -999
			}
			if err != nil || !changed {
				t.Fatal("edit failed", kind, index, err)
			}
			if !reflect.DeepEqual(source, original) {
				t.Fatal("replacement or a later edit mutated original registry", kind, index)
			}
			for key, table := range before.Objects.Tables {
				if key != id && !reflect.DeepEqual(table, candidate.Objects.Tables[key]) {
					t.Fatal("table replacement changed unrelated payload", key)
				}
			}
			for key, chart := range before.Objects.Charts {
				if key != id && !reflect.DeepEqual(chart, candidate.Objects.Charts[key]) {
					t.Fatal("chart replacement changed unrelated payload", key)
				}
			}
			last := candidate.Slides[0].Elements[len(candidate.Slides[0].Elements)-1]
			if kind == "table" && candidate.Objects.Tables[last.ObjectID].Cells[0][0].Runs[0].Text != "Edited rich text" || kind == "chart" && candidate.Objects.Charts[last.ObjectID].Series[0].Values[0] != 99 {
				t.Fatal("replacement retained caller-owned data", kind)
			}
			candidate.Slides[0].Elements = candidate.Slides[0].Elements[:len(candidate.Slides[0].Elements)-1]
		}
	}
}

func TestStructuredReplacementConcurrentReaders(t *testing.T) {
	source := structuredReplacementFixture(t, 4)
	before := cloneDeck(source)
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 8; iteration++ {
				candidate := source
				candidate.Slides = []Slide{cloneSlide(source.Slides[0])}
				table := cloneTable(source.Objects.Tables["table-0"])
				table.Cells[0][0].Text = "Concurrent candidate"
				if changed, err := applyTableCommand(&candidate, nativeEditorAction{Action: "set-table", ObjectID: "table", TableData: &table}, ""); err != nil || !changed {
					t.Error("concurrent table candidate failed", err)
				}
				chart := cloneChart(source.Objects.Charts["chart-0"])
				chart.Series[0].Values[0]++
				if changed, err := applyChartCommand(&candidate, nativeEditorAction{Action: "set-chart-data", ObjectID: "chart", ChartData: &chart}, ""); err != nil || !changed {
					t.Error("concurrent chart candidate failed", err)
				}
			}
		}()
	}
	wait.Wait()
	if !reflect.DeepEqual(source, before) {
		t.Fatal("candidate writer changed immutable shared source")
	}
}
