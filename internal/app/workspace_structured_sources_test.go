package app

import (
	"reflect"
	"testing"
)

func BenchmarkWorkspaceStructuredSources(b *testing.B) {
	before := structuredReplacementFixture(b, 24)
	after := cloneDeck(before)
	index := newWorkspaceConsumerIndex()
	index.payloads[workspacePayloadReference{"table", "table-0"}] = workspaceConsumers{0: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _, _ := index.changedSources(before, after)
		if len(data) != 0 {
			b.Fatal("equal independent registry invalidated pages")
		}
	}
}

func TestWorkspaceStructuredSourceInvalidation(t *testing.T) {
	before := structuredReplacementFixture(t, 2)
	before.Objects.Builds["page"] = SlideBuild{Version: 1, Steps: []BuildStep{{TargetIDs: []string{"table"}, Effect: "appear", Advance: "click"}}}
	index := newWorkspaceConsumerIndex()
	index.payloads[workspacePayloadReference{"table", "table-0"}] = workspaceConsumers{0: true, 2: true}
	index.payloads[workspacePayloadReference{"chart", "chart-0"}] = workspaceConsumers{1: true}
	index.builds["page"] = workspaceConsumers{3: true}
	index.unknown[4] = true
	for _, tc := range []struct {
		name string
		edit func(*Deck)
		want workspaceConsumers
	}{
		{"unchanged independent storage", func(*Deck) {}, workspaceConsumers{}},
		{"table rich run", func(d *Deck) { d.Objects.Tables["table-0"].Cells[0][0].Runs[0].Text = "Changed" }, workspaceConsumers{0: true, 2: true, 4: true}},
		{"chart value", func(d *Deck) { d.Objects.Charts["chart-0"].Series[0].Values[0]++ }, workspaceConsumers{1: true, 4: true}},
		{"timeline target", func(d *Deck) { d.Objects.Builds["page"].Steps[0].TargetIDs[0] = "other" }, workspaceConsumers{3: true, 4: true}},
		{"table removal", func(d *Deck) { delete(d.Objects.Tables, "table-0") }, workspaceConsumers{0: true, 2: true, 4: true}},
		{"chart removal", func(d *Deck) { delete(d.Objects.Charts, "chart-0") }, workspaceConsumers{1: true, 4: true}},
		{"timeline removal", func(d *Deck) { delete(d.Objects.Builds, "page") }, workspaceConsumers{3: true, 4: true}},
		{"unused payload", func(d *Deck) { d.Objects.Tables["table-1"].Cells[0][0].Text = "Unused" }, workspaceConsumers{4: true}},
		{"registry removal", func(d *Deck) { d.Objects = nil }, workspaceConsumers{0: true, 1: true, 2: true, 3: true, 4: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := cloneDeck(before)
			tc.edit(&after)
			owned := cloneDeck(after)
			got, masters, unknown := index.changedSources(before, after)
			if !reflect.DeepEqual(got, tc.want) || len(masters) != 0 || unknown {
				t.Fatalf("invalid consumers: data=%v masters=%v unknown=%v; want %v", got, masters, unknown, tc.want)
			}
			if !reflect.DeepEqual(after, owned) {
				t.Fatal("source comparison mutated input")
			}
		})
	}
}
