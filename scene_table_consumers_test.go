package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

func TestSceneTableCacheThemeConsumers(t *testing.T) {
	base := Deck{Theme: builtinDeckTheme("clean-light"), Appearance: &DeckAppearance{Version: 3}}
	data, _ := newTableData(3, 3)
	for r := range data.Cells {
		for c := range data.Cells[r] {
			data.Cells[r][c].Text = fmt.Sprintf("Cell %d/%d", r, c)
		}
	}
	data.Style.Header, data.Style.Banded = true, true
	box := sceneRect{Width: 800, Height: 400}
	tests := []struct {
		name   string
		setup  func(*TableData)
		mutate func(*Deck)
		hit    bool
	}{
		{name: "theme-name", mutate: func(d *Deck) { d.Theme.Name = "Unrelated name" }, hit: true},
		{name: "unused-accent", mutate: func(d *Deck) { d.Theme.Colors.Accent[5] = "#ffeedd" }, hit: true},
		{name: "unused-heading", mutate: func(d *Deck) { d.Theme.Fonts.Heading.FontID = "mono" }, hit: true},
		{name: "unused-mono", mutate: func(d *Deck) { d.Theme.Fonts.Mono.Size = 120 }, hit: true},
		{name: "unused-shape-defaults", mutate: func(d *Deck) { d.Theme.Defaults.LineWidth = 8 }, hit: true},
		{name: "private-extensions", mutate: func(d *Deck) {
			extra := &jsonExtensions{"private": json.RawMessage(`{"secret":"not a render input"}`)}
			d.Theme.Extra, d.Theme.Colors.Extra, d.Theme.Fonts.Extra = extra, extra, extra
			d.Theme.Fonts.Body.Extra, d.Theme.Fonts.Heading.Extra, d.Theme.Fonts.Mono.Extra = extra, extra, extra
			d.Theme.Defaults.Extra = extra
			d.Appearance.Extra = map[string]json.RawMessage{"private": json.RawMessage(`true`)}
		}, hit: true},
		{name: "appearance-presence-with-theme", mutate: func(d *Deck) { d.Appearance = nil }, hit: true},
		{name: "body-family", mutate: func(d *Deck) { d.Theme.Fonts.Body.FontID = "mono" }},
		{name: "body-size", mutate: func(d *Deck) { d.Theme.Fonts.Body.Size += 10 }},
		{name: "body-width", mutate: func(d *Deck) { d.Theme.Fonts.Body.Width -= 10 }},
		{name: "body-line-height", mutate: func(d *Deck) { d.Theme.Fonts.Body.LineHeight += .1 }},
		{name: "header-fill", mutate: func(d *Deck) { d.Theme.Colors.Accent[0] = "#ffeedd" }},
		{name: "band-fill", mutate: func(d *Deck) { d.Theme.Colors.Surface = "#ffeedd" }},
		{name: "body-fill", mutate: func(d *Deck) { d.Theme.Colors.Background = "#ffeedd" }},
		{name: "text", mutate: func(d *Deck) { d.Theme.Colors.Text = "#ffeedd" }},
		{name: "border", mutate: func(d *Deck) { d.Theme.Colors.Line = "#ffeedd" }},
		{name: "overridden-font", setup: func(data *TableData) {
			size, width, height := 36., 80., 1.2
			data.Style.Cell.FontID, data.Style.Cell.FontSize = "c64", &size
			data.Style.Cell.FontWidth, data.Style.Cell.LineHeight = &width, &height
		}, mutate: func(d *Deck) { d.Theme.Fonts.Body = ThemeFont{FontID: "mono", Size: 100, Width: 120, LineHeight: 2} }, hit: true},
		{name: "overridden-colors", setup: func(data *TableData) {
			data.Style.Cell.Fill, data.Style.Cell.Color, data.Style.Cell.BorderColor = "#112233", "#ddeeff", "#445566"
		}, mutate: func(d *Deck) {
			d.Theme.Colors.Background, d.Theme.Colors.Text, d.Theme.Colors.Line, d.Theme.Colors.Accent[0], d.Theme.Colors.Surface = "#121212", "#232323", "#343434", "#454545", "#565656"
		}, hit: true},
		{name: "late-rich-color-role", setup: func(data *TableData) {
			data.Cells[2][2].Runs = []ObjectTextRun{{Text: "Role", Color: "#112233", ColorToken: "accent-6"}}
		}, mutate: func(d *Deck) { d.Theme.Colors.Accent[5] = "#ffeedd" }},
		{name: "late-rich-font-role", setup: func(data *TableData) {
			data.Cells[2][2].Runs = []ObjectTextRun{{Text: "Role", FontID: "c64", FontRole: "heading"}}
		}, mutate: func(d *Deck) { d.Theme.Fonts.Heading.FontID = "mono" }},
		{name: "late-side-border-role", setup: func(data *TableData) {
			width := 3.
			data.Cells[2][2].Style = &TableCellStyle{Borders: map[string]TableBorderStyle{"left": {Color: "theme:accent-6", Width: &width}}}
		}, mutate: func(d *Deck) { d.Theme.Colors.Accent[5] = "#ffeedd" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			table := cloneTable(data)
			if test.setup != nil {
				test.setup(&table)
			}
			var cache sceneTableCache
			if _, err, hit := cache.project(base, table, box, ""); err != nil || hit {
				t.Fatal("cold", hit, err)
			}
			d := cloneDeck(base)
			test.mutate(&d)
			before := cloneDeck(d)
			got, err, hit := cache.project(d, table, box, "")
			if err != nil || hit != test.hit {
				t.Fatalf("hit=%v want=%v: %v", hit, test.hit, err)
			}
			cold, err := d.projectTable(table, box, "")
			if err != nil || !reflect.DeepEqual(got, cold) {
				t.Fatal("cached table differs from cold", err)
			}
			if !reflect.DeepEqual(d, before) {
				t.Fatal("authored theme mutated")
			}
			if _, err, hit := cache.project(d, table, box, ""); err != nil || !hit {
				t.Fatal("repeat", hit, err)
			}
			for _, node := range cache.entries {
				theme := node.Value.(sceneTableCacheItem).entry.theme
				if theme != nil && (theme.Name != "" || theme.Extra != nil || theme.Colors.Extra != nil || theme.Fonts.Extra != nil || theme.Fonts.Body.Extra != nil || theme.Fonts.Heading.Extra != nil || theme.Fonts.Mono.Extra != nil || theme.Defaults.Extra != nil) {
					t.Fatal("retained private theme inputs")
				}
			}
		})
	}
}

func TestSceneTableCacheThemeColdCorpus(t *testing.T) {
	deck := powerupProofCorpus(t)["tables.md"]
	mutations := []func(*Deck){
		func(d *Deck) { d.Theme = nil },
		func(d *Deck) { d.Theme.Fonts.Body = ThemeFont{FontID: "mono", Size: 83, Width: 79, LineHeight: 1.7} },
		func(d *Deck) { d.Theme.Fonts.Heading.FontID = "c64"; d.Theme.Fonts.Mono.FontID = "sans" },
		func(d *Deck) {
			d.Appearance = &DeckAppearance{Version: 1, Mode: "modern", Extra: map[string]json.RawMessage{"theme": json.RawMessage(`"midnight-v1"`), "profiles": json.RawMessage(`{"modern":{"textScale":0.8}}`)}}
		},
	}
	for index := 0; index < 12; index++ {
		mutations = append(mutations, func(d *Deck) {
			colors := &d.Theme.Colors
			fields := []*string{&colors.Background, &colors.Surface, &colors.Text, &colors.Heading, &colors.Muted, &colors.Line}
			if index < len(fields) {
				*fields[index] = "#123456"
			} else {
				colors.Accent[index-len(fields)] = "#123456"
			}
		})
	}
	for id, table := range deck.Objects.Tables {
		t.Run(id, func(t *testing.T) {
			for index, mutate := range mutations {
				var cache sceneTableCache
				box := sceneRect{Width: 740, Height: 390}
				if _, err, _ := cache.project(deck, table, box, ""); err != nil {
					t.Fatal(err)
				}
				changed := cloneDeck(deck)
				mutate(&changed)
				for repeat := 0; repeat < 2; repeat++ {
					got, err, _ := cache.project(changed, table, box, "")
					cold, coldErr := changed.projectTable(table, box, "")
					if err != nil || coldErr != nil || !reflect.DeepEqual(got, cold) {
						t.Fatalf("mutation %d repeat %d differs: %v / %v", index, repeat, err, coldErr)
					}
				}
			}
		})
	}
}

func TestSceneTableCacheThemeOwnershipAndHotRetention(t *testing.T) {
	table, _ := newTableData(2, 2)
	d := Deck{Theme: builtinDeckTheme("clean-light")}
	box := sceneRect{Width: 400, Height: 200}
	var cache sceneTableCache
	want, err, _ := cache.project(d, table, box, "")
	if err != nil {
		t.Fatal(err)
	}
	// The immutable dependency snapshot must not alias caller palette slices.
	d.Theme.Colors.Accent[0] = "#123456"
	got, err, hit := cache.project(d, table, box, "")
	if err != nil || hit || reflect.DeepEqual(got, want) {
		t.Fatal("palette alias hid a real change", err, hit)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				changed := cloneDeck(d)
				changed.Theme.Colors.Accent[5] = fmt.Sprintf("#%06x", i+worker*100)
				got, err, _ := cache.project(changed, table, box, "")
				cold, coldErr := changed.projectTable(table, box, "")
				if err != nil || coldErr != nil || !reflect.DeepEqual(got, cold) {
					t.Error("concurrent consumer mismatch", err, coldErr)
				}
			}
		}(worker)
	}
	wg.Wait()
	accounted := 0
	for _, node := range cache.entries {
		accounted += node.Value.(sceneTableCacheItem).entry.cost
	}
	if cache.bytes != accounted {
		t.Fatal("dependency refresh broke byte accounting")
	}
	for i := 0; i < 80; i++ {
		if _, err, _ := cache.project(d, table, box, fmt.Sprintf("#%06x", i)); err != nil {
			t.Fatal(err)
		}
		if _, err, hit := cache.project(d, table, box, ""); err != nil || !hit {
			t.Fatal("hot table evicted", i, err)
		}
		if cache.bytes > 8<<20 || len(cache.entries) > 64 || cache.order.Len() != len(cache.entries) {
			t.Fatal("LRU budget/index mismatch")
		}
	}
	count, bytes := len(cache.entries), cache.bytes
	cache.store([32]byte{}, sceneTableCacheEntry{table: want, theme: sceneTableThemeInputs(d.Theme), cost: 8 << 20})
	if len(cache.entries) != count || cache.bytes != bytes {
		t.Fatal("oversized entry evicted useful tables")
	}
}

func TestSceneTableCacheSurvivesOtherObjectThemeEdit(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	table, _ := newTableData(3, 3)
	table.Cells[2][2].Text = newStableID("consumer-proof")
	d := Deck{Theme: builtinDeckTheme("clean-light"), Appearance: &DeckAppearance{Version: 3}, Objects: &DeckObjects{Version: 1, Tables: map[string]TableData{"table": table}}, Slides: []Slide{{Elements: []Element{
		{ID: "table-owner", Kind: "table", ObjectID: "table", Query: "top=2&left=5&width=150&height=20"},
		{ID: "text-owner", Kind: "text", Text: "Another theme consumer", Query: "top=30&left=10&render=truetype&theme-fg=accent-6"},
	}}}}
	s := newNativeEditorSession("Table consumers.md", d)
	read := func() (*httptest.ResponseRecorder, slideScene) {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0&timing=1", nil))
		var scene slideScene
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &scene) != nil {
			t.Fatal("scene failed", w.Code)
		}
		return w, scene
	}
	_, baseline := read()
	theme := s.deck.Theme.Clone()
	theme.Colors.Accent[5] = "#ff44aa"
	if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
		t.Fatal(err)
	}
	w, got := read()
	if w.Header().Get("X-Keynope-Table-Builds") != "0" || w.Header().Get("X-Keynope-Table-Cache-Hits") != "1" {
		t.Fatal("unrelated table rebuilt", w.Header())
	}
	if !reflect.DeepEqual(baseline.Objects[0].Table, got.Objects[0].Table) || reflect.DeepEqual(baseline.Objects[1].Paint, got.Objects[1].Paint) {
		t.Fatal("did not isolate unrelated table from real text colour edit")
	}
	theme = s.deck.Theme.Clone()
	theme.Colors.Line = "#994422"
	if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
		t.Fatal(err)
	}
	w, got = read()
	if w.Header().Get("X-Keynope-Table-Builds") != "1" || w.Header().Get("X-Keynope-Table-Cache-Hits") != "0" {
		t.Fatal("table border consumer did not rebuild", w.Header())
	}
	want, err := s.deck.projectTable(table, got.Objects[0].Bounds, "")
	if err != nil || !reflect.DeepEqual(got.Objects[0].Table, want) {
		t.Fatal("real table edit differs from cold", err)
	}
}

func BenchmarkSceneTableUnusedThemeEdit(b *testing.B) {
	table, _ := newTableData(20, 20)
	for r := range table.Cells {
		for c := range table.Cells[r] {
			table.Cells[r][c].Text = fmt.Sprintf("%d/%d", r, c)
		}
	}
	d := Deck{Theme: builtinDeckTheme("clean-light"), Appearance: &DeckAppearance{Version: 3}}
	box := sceneRect{Width: 1700, Height: 900}
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprintf("cached=%v", cached), func(b *testing.B) {
			var cache sceneTableCache
			if _, err, _ := cache.project(d, table, box, ""); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if i%2 == 0 {
					d.Theme.Colors.Accent[5] = "#123456"
				} else {
					d.Theme.Colors.Accent[5] = "#abcdef"
				}
				if cached {
					if _, err, hit := cache.project(d, table, box, ""); err != nil || !hit {
						b.Fatal(err, hit)
					}
				} else if _, err := d.projectTable(table, box, ""); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
