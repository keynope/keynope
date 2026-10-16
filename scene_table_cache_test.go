package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func TestSceneTableCacheColdParityAndDependencies(t *testing.T) {
	corpus := powerupProofCorpus(t)
	deck := corpus["tables.md"]
	var cache sceneTableCache
	bounds := sceneRect{Width: 800, Height: 500}
	check := func(d Deck, table TableData, box sceneRect, color string, wantHit bool) {
		t.Helper()
		cold, err := d.projectTable(table, box, color)
		if err != nil {
			t.Fatal(err)
		}
		warm, err, hit := cache.project(d, table, box, color)
		if err != nil || hit != wantHit {
			t.Fatalf("hit=%v want=%v: %v", hit, wantHit, err)
		}
		a, _ := json.Marshal(cold)
		b, _ := json.Marshal(warm)
		if string(a) != string(b) {
			t.Fatal("cached table differs from cold projection")
		}
	}
	for _, table := range deck.Objects.Tables {
		cache = sceneTableCache{}
		check(deck, table, bounds, "", false)
		check(deck, table, bounds, "", true)
		moved := bounds
		moved.X, moved.Y = 500, 900
		check(deck, table, moved, "", true)
		check(deck, table, sceneRect{Width: 900, Height: 500}, "", false)
	}
	cache = sceneTableCache{}
	table := cloneTable(deck.Objects.Tables["table-basic"])
	for _, mutate := range []func(*Deck, *TableData){
		func(d *Deck, t *TableData) { d.Theme.Colors.Accent[0] = "#ff0055" },
		func(d *Deck, t *TableData) { d.Theme.Fonts.Body.FontID = "mono" },
		func(d *Deck, t *TableData) { d.Theme.Fonts.Body.Width = 71 },
		func(d *Deck, t *TableData) { d.Appearance = nil },
		func(d *Deck, t *TableData) { t.Cells[0][0].Text += "updated" },
		func(d *Deck, t *TableData) {
			t.Cells[0][0].Runs = []ObjectTextRun{{Text: "Role", Color: "#55aaff", ColorToken: "accent-1", FontID: "c64", FontRole: "heading"}}
		},
		func(d *Deck, t *TableData) { t.Style.Header = !t.Style.Header },
		func(d *Deck, t *TableData) { t.Columns[0].Width = 2 },
	} {
		d, changed := cloneDeck(deck), cloneTable(table)
		mutate(&d, &changed)
		check(d, changed, bounds, "", false)
		check(d, changed, bounds, "", true)
	}
	check(deck, table, bounds, "#55aaff", false)
}

func TestSceneTableCacheOwnsProjectionAndEvicts(t *testing.T) {
	table, _ := newTableData(2, 2)
	table.Cells[0][0].Text = "owned"
	deck := Deck{Theme: builtinDeckTheme("clean-light")}
	var cache sceneTableCache
	bounds := sceneRect{Width: 400, Height: 200}
	first, err, _ := cache.project(deck, table, bounds, "")
	if err != nil {
		t.Fatal(err)
	}
	first.Cells[0].Text.Runs[0].Text = "corrupt"
	first.Rows[0] = -1
	table.Cells[0][0].Text = "input changed"
	// Restore authored content; cached source must not follow caller edits.
	table.Cells[0][0].Text = "owned"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			projection, err, hit := cache.project(deck, table, bounds, "")
			if err != nil || !hit || projection.Rows[0] < 0 || projection.Cells[0].Text.Runs[0].Text != "owned" {
				t.Error("caller modified cached projection")
			}
			projection.Cells[0].Text.Runs[0].Text = "other caller"
		}()
	}
	wg.Wait()
	for i := 0; i < 70; i++ {
		if _, err, _ := cache.project(deck, table, bounds, fmt.Sprintf("#%06x", i)); err != nil {
			t.Fatal(err)
		}
		if cache.bytes > 8<<20 || len(cache.entries) > 64 {
			t.Fatal("cache exceeded budget")
		}
	}
	invalid := cloneTable(table)
	invalid.Cells[0][0].ColSpan = 10
	if _, err, _ := cache.project(deck, invalid, bounds, ""); err == nil {
		t.Fatal("cached projection bypassed validation")
	}
}
