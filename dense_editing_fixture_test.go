package main

import (
	"fmt"
	"os"
	"testing"
)

func powerupDenseEditingDeck(t testing.TB) Deck {
	corpus := powerupProofCorpus(t)
	deck := cloneDeck(corpus["tables.md"])
	deck.Slides = []Slide{{ID: "dense-editing"}}
	for id := range deck.Objects.Tables {
		if id != "table-dense" {
			delete(deck.Objects.Tables, id)
		}
	}
	deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "dense-table", Kind: "table", ObjectID: "table-dense", Query: "left=135&top=8&width=105&height=46"})
	chart := corpus["charts.md"].Objects.Charts["chart-column"]
	deck.Objects.Charts["dense-chart"] = chart
	deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "dense-chart", Kind: "chart", ObjectID: "dense-chart", Query: "left=4&top=37&width=120&height=17"})
	for i := 0; i < 40; i++ {
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: fmt.Sprintf("dense-text-%d", i), Kind: "text", Text: fmt.Sprintf("Text %d", i), Query: fmt.Sprintf("render=truetype&modern-font=sans&modern-size=18&left=%d&top=%d&width=11&height=4", 4+i%10*12, 2+i/10*5)})
	}
	media := corpus["media.md"]
	deck.Assets = media.Assets
	for i, e := range media.Slides[0].Elements {
		e.Query = fmt.Sprintf("image-style=modern&image-fit=contain&left=%d&top=24&width=30&height=10", 4+i*40)
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, e)
	}
	deck.Slides[0].Elements = append(deck.Slides[0].Elements, corpus["diagrams.md"].Slides[0].Elements...)
	canonicalizeConcreteAppearance(&deck)
	standardizeDeckText(&deck)
	scene, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil || len(scene.Objects) != 49 {
		t.Fatal("dense fixture lost mixed primitives", len(scene.Objects), err)
	}
	return deck
}

func TestPowerupDenseEditingFixture(t *testing.T) {
	deck := powerupDenseEditingDeck(t)
	if file := os.Getenv("KEYNOPE_DENSE_EDITING_FIXTURE"); file != "" {
		oldW, oldH := authoredTerminalWidth, authoredTerminalHeight
		authoredTerminalWidth, authoredTerminalHeight = 245, 56
		defer func() { authoredTerminalWidth, authoredTerminalHeight = oldW, oldH }()
		data, err := serializeDeck(file, deck)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// Profile actual owned workspace projections, not a synthetic serialization
// microbenchmark. No desktop app or document files are opened by this benchmark.
func BenchmarkWorkspaceDensePaint(b *testing.B) {
	deck := powerupDenseEditingDeck(b)
	var cache workspacePageCache
	if _, err := cache.render(deck, false, 0, 245, 56); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		color := "&fg=%23ff0055"
		if i%2 == 0 {
			color = "&fg=%2355aaff"
		}
		deck.Slides[0].Elements[2].Query = "render=truetype&modern-font=sans&modern-size=18&left=4&top=2&width=11&height=4" + color
		if _, err := cache.render(deck, false, 0, 245, 56); err != nil {
			b.Fatal(err)
		}
	}
}
