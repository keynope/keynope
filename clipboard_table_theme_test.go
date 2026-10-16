package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func clipboardTableThemeFixture(t *testing.T) Deck {
	deck := portableMergedTableFixture(t)
	deck.Theme.Fonts.Body = ThemeFont{FontID: "mono", Size: 64, Width: 72, LineHeight: 1.6}
	data := deck.Objects.Tables["portable-data"]
	data.Style.Cell = TableCellStyle{}
	data.Style.Cell.Extra = extensionFixture("future-style", `{"keep":true}`)
	data.Style.HeaderFill, data.Style.BandFill = "", ""
	data.Cells[1][1].Style.Borders["bottom"] = testEdge("theme:accent-3", 3)
	data.Cells[1][0].BorderFragments = map[string][]TableBorderStyle{"left": {testEdge("theme:accent-2", 1), testEdge("none", 0)}}
	deck.Objects.Tables["portable-data"] = data
	return deck
}

func TestClipboardTableThemePlainAppearanceAndThemedAdoption(t *testing.T) {
	source := clipboardTableThemeFixture(t)
	before := cloneDeck(source)
	want, err := buildSlideScene(source, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(bundle)
	var wire ObjectClipboard
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	for _, themed := range []bool{false, true} {
		destination := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "destination"}}}
		if themed {
			destination.Theme = builtinDeckTheme("clean-light")
			destination.Theme.Colors.Accent[0] = "#cc5500"
		}
		elements, assets, objects, err := prepareObjectClipboardPaste(destination, wire)
		if err != nil {
			t.Fatal(err)
		}
		destination.Slides[0].Elements, destination.Assets, destination.Objects = elements, assets, objects
		actual, err := buildSlideScene(destination, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if themed {
			if !reflect.DeepEqual(destination.Objects.Tables[elements[0].ObjectID], wire.Objects.Tables["portable-data"]) {
				t.Fatal("themed paste baked source defaults")
			}
			if actual.Objects[0].Table.Cells[0].Text.FontID != destination.Theme.Fonts.Body.FontID {
				t.Fatal("destination font not adopted")
			}
		} else if !reflect.DeepEqual(actual.Objects[0].Table, want.Objects[0].Table) {
			for i, cell := range want.Objects[0].Table.Cells {
				if !reflect.DeepEqual(cell, actual.Objects[0].Table.Cells[i]) {
					t.Errorf("cell %d source=%+v pasted=%+v source text=%+v pasted text=%+v", i, cell, actual.Objects[0].Table.Cells[i], cell.Text, actual.Objects[0].Table.Cells[i].Text)
				}
			}
			t.Fatalf("plain paste changed source appearance\nborders want=%+v got=%+v", want.Objects[0].Table.Borders, actual.Objects[0].Table.Borders)
		}
		md, err := serializeDeck("pasted.md", destination)
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := parseDeckData("pasted.md", md)
		if err != nil || reopened.Objects == nil || len(reopened.Objects.Tables) != 1 || len(reopened.Objects.Charts) != 0 || !reflect.DeepEqual(reopened.Objects.Tables[elements[0].ObjectID], destination.Objects.Tables[elements[0].ObjectID]) {
			t.Fatal("pasted table style save/reopen", err)
		}
		if output := os.Getenv("KEYNOPE_CLIPBOARD_TABLE_EVIDENCE"); output != "" {
			if err := os.MkdirAll(output, 0755); err != nil {
				t.Fatal(err)
			}
			name := "plain"
			if themed {
				name = "themed"
			}
			if err := saveDeck(filepath.Join(output, name+".md"), destination); err != nil {
				t.Fatal(err)
			}
			empty := cloneDeck(destination)
			empty.Objects = nil
			empty.Slides[0].Elements = nil
			if err := saveDeck(filepath.Join(output, name+"-target.md"), empty); err != nil {
				t.Fatal(err)
			}
			if err := saveDeck(filepath.Join(output, "source.md"), source); err != nil {
				t.Fatal(err)
			}
			fixture, err := json.Marshal(map[string]any{"source": want, "destination": actual})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(output, name+".json"), fixture, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("clipboard mutated source")
	}
}

func TestClipboardTableThemeFallbackValidation(t *testing.T) {
	source := clipboardTableThemeFixture(t)
	for _, mutate := range []func(*ObjectClipboard){
		func(b *ObjectClipboard) { b.ThemeSignature = "" },
		func(b *ObjectClipboard) { b.TableThemeFallbacks["unrelated"] = b.TableThemeFallbacks["portable-table"] },
		func(b *ObjectClipboard) {
			f := b.TableThemeFallbacks["portable-table"]
			f.Cells = append(f.Cells, f.Cells[0])
			b.TableThemeFallbacks["portable-table"] = f
		},
		func(b *ObjectClipboard) {
			f := b.TableThemeFallbacks["portable-table"]
			f.Cells[0].Row = 99
			b.TableThemeFallbacks["portable-table"] = f
		},
		func(b *ObjectClipboard) {
			f := b.TableThemeFallbacks["portable-table"]
			f.Cells[0].Color = "theme:heading"
			b.TableThemeFallbacks["portable-table"] = f
		},
		func(b *ObjectClipboard) {
			f := b.TableThemeFallbacks["portable-table"]
			f.FontWidth = 201
			b.TableThemeFallbacks["portable-table"] = f
		},
		func(b *ObjectClipboard) {
			f := b.TableThemeFallbacks["portable-table"]
			f.Colors["theme:heading"] = "#123456"
			b.TableThemeFallbacks["portable-table"] = f
		},
	} {
		bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
		if err != nil {
			t.Fatal(err)
		}
		mutate(&bundle)
		if bundle.Validate() == nil {
			t.Fatal("invalid table formatting fallback accepted")
		}
		session := newNativeEditorSession("destination.md", Deck{Slides: []Slide{{ID: "destination"}}}, false, false)
		before := cloneDeck(session.deck)
		if err := session.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err == nil || !reflect.DeepEqual(session.deck, before) || len(session.undo) != 0 {
			t.Fatal("invalid fallback was not rejected atomically", err)
		}
	}
}
