package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"keynope/internal/pptx"
)

// Exercise production export entry points, not only scene-to-object adapters.
// Editable package readback compares native runs/cells/chart data/geometry and
// fonts; it is intentionally not a claim about external Office rendering.
func TestThemeDetachExportCorpus(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	corpus := powerupProofCorpus(t)
	names := make([]string, 0, len(corpus))
	for name := range corpus {
		names = append(names, name)
	}
	sort.Strings(names)
	wantCombined := exportDeck{Cols: 245, Rows: 56, Source: "Theme detachment proof.md"}
	gotCombined := wantCombined
	var labels []string
	for _, name := range names {
		source := corpus[name]
		t.Run(name, func(t *testing.T) {
			before := cloneDeck(source)
			candidate, err := concreteThemeDeck(source)
			if err != nil {
				t.Fatal(err)
			}
			wantPDF, wantHTML, err := projectPDFExport("proof.md", source, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			gotPDF, gotHTML, err := projectPDFExport("proof.md", candidate, 245, 56)
			if err != nil || len(wantPDF.Pages) != len(gotPDF.Pages) || len(wantHTML.Pages) != len(gotHTML.Pages) {
				t.Fatal("export page count changed", err)
			}
			for i, want := range wantHTML.Pages {
				got := gotHTML.Pages[i]
				if want.Scene == nil || got.Scene == nil {
					t.Fatal("export omitted semantic scene", i)
				}
				if !reflect.DeepEqual(comparableThemeScene(*want.Scene), comparableThemeScene(*got.Scene)) {
					t.Fatal("HTML/PDF resolved appearance changed", i)
				}
				wp, gp := wantPDF.Pages[i], gotPDF.Pages[i]
				if wp.Width != gp.Width || wp.Height != gp.Height || wp.Quality != gp.Quality || !reflect.DeepEqual(wp.Reasons, gp.Reasons) {
					t.Fatal("PDF quality/fallback eligibility changed", i)
				}
			}
			if _, err := exportProjectedHTML(gotHTML, preservedExportHead{}, false); err != nil {
				t.Fatal("detached HTML cannot be encoded", err)
			}
			if os.Getenv("KEYNOPE_DETACH_EXPORT_EVIDENCE") != "" {
				wantCombined.Pages = append(wantCombined.Pages, wantHTML.Pages...)
				gotCombined.Pages = append(gotCombined.Pages, gotHTML.Pages...)
				for i := range wantHTML.Pages {
					labels = append(labels, name+":"+fmt.Sprint(i+1))
				}
			}
			read := func(deck Deck) pptx.Presentation {
				t.Helper()
				data, _, err := exportPPTXDocument(deck)
				if err != nil {
					t.Fatal("PPTX export failed", err)
				}
				presentation, _, err := pptx.Read(context.Background(), bytes.NewReader(data), int64(len(data)), pptx.Options{})
				if err != nil {
					t.Fatal("exported PPTX could not be read", err)
				}
				return presentation
			}
			want, got := read(source), read(candidate)
			// Native scheme metadata now exposes the authored theme before
			// Detach, and the default scheme after it. Detach promises unchanged
			// visible/editable objects, not retained live theme identity.
			if source.Theme != nil && (want.Theme == nil || want.Theme.Name != source.Theme.Name) {
				t.Fatal("authored native theme missing before Detach")
			}
			if candidate.Theme == nil && (got.Theme == nil || got.Theme.Name != "Keynope") {
				t.Fatal("detached export retained live authored theme")
			}
			want.Theme, got.Theme = nil, nil
			if !reflect.DeepEqual(want, got) {
				for i := range want.Slides {
					if i >= len(got.Slides) || !reflect.DeepEqual(want.Slides[i], got.Slides[i]) {
						t.Fatalf("editable PPTX slide changed: %d", i)
					}
				}
				t.Fatal("PPTX canvas or embedded fonts changed")
			}
			if !reflect.DeepEqual(source, before) {
				t.Fatal("export mutated authored source")
			}
		})
	}
	if directory := os.Getenv("KEYNOPE_DETACH_EXPORT_EVIDENCE"); directory != "" {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for name, document := range map[string]exportDeck{"source": wantCombined, "detached": gotCombined} {
			html, err := exportProjectedHTML(document, preservedExportHead{}, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, name+".html"), []byte(html), 0600); err != nil {
				t.Fatal(err)
			}
		}
		data, err := json.Marshal(labels)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "pages.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
