package app

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const legacyDanglingDiagram = `<!-- keynope width=245 height=56 -->
<!-- element-id=existing shape=square width=20 height=10 -->
[shape:square]

<!-- element-id=legacy-line connector-from=existing connector-to=missing connector-from-side=right connector-to-side=left -->
[connector]

<!-- element-id=caption -->
Old deck
`

func TestLoadedDanglingConnectorOpensEditsAndSaves(t *testing.T) {
	d, err := parseDeckData("legacy.md", []byte(legacyDanglingDiagram))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Diagnostics) != 1 || d.Diagnostics[0].Code != "dangling-connectors-preserved" {
		t.Fatal("expected a compatibility notice", d.Diagnostics)
	}
	s := newNativeEditorSession("legacy.md", d)
	before := cloneDeck(s.deck)
	index := nativeEditorElementIndex(s.deck.Slides[0].Elements, -1, "caption")
	updated := s.deck.Slides[0].Elements[index]
	updated.Text = "Edited deck"
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: index, ElementData: &updated}); err != nil {
		t.Fatal("unrelated edit was blocked", err)
	}
	lineIndex := nativeEditorElementIndex(s.deck.Slides[0].Elements, -1, "legacy-line")
	line := s.deck.Slides[0].Elements[lineIndex]
	// Real frontend replacements have no parser-owned compatibility fields.
	encoded, _ := json.Marshal(line)
	var replacement Element
	if err := json.Unmarshal(encoded, &replacement); err != nil {
		t.Fatal(err)
	}
	replacement.Query = setQueryValue(replacement.Query, "fg", "#ff0055")
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: lineIndex, ElementData: &replacement}); err != nil {
		t.Fatal("existing connector paint edit was blocked", err)
	}
	data, err := serializeDeck("legacy.md", s.deck)
	if err != nil {
		t.Fatal("save was blocked", err)
	}
	reloaded, err := parseDeckData("legacy.md", data)
	if err != nil {
		t.Fatal("saved deck cannot reopen", err)
	}
	if !bytes.Contains(data, []byte("connector-to=missing")) || len(reloaded.Slides[0].Elements) != 3 {
		t.Fatal("recovery discarded authored data")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("undo lost original references", err)
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := serializeDeck("legacy.md", s.deck); err != nil {
		t.Fatal("save after redo was blocked", err)
	}
}

func TestLoadedConnectorAllowanceDoesNotPermitNewInvalidReferences(t *testing.T) {
	d, err := parseDeckData("legacy.md", []byte(legacyDanglingDiagram))
	if err != nil {
		t.Fatal(err)
	}
	s := newNativeEditorSession("legacy.md", d)
	index := nativeEditorElementIndex(s.deck.Slides[0].Elements, -1, "legacy-line")
	line := s.deck.Slides[0].Elements[index]
	line.Query = setQueryValue(line.Query, "connector-to", "different-missing")
	assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "update-element", Element: index, ElementData: &line})
	line = s.deck.Slides[0].Elements[index]
	line.ID = "new-invalid-line"
	line.unresolvedConnectorReference = ""
	d.Slides[0].Elements = append(d.Slides[0].Elements, line)
	if err := validateDeckObjectReferences(d); err == nil {
		t.Fatal("new dangling connector accepted")
	}
}

func TestLoadedDanglingConnectorCloneAndMissingIdentity(t *testing.T) {
	for _, noID := range []bool{false, true} {
		input := legacyDanglingDiagram
		if noID {
			input = strings.Replace(input, "element-id=legacy-line ", "", 1)
		}
		d, err := parseDeckData("legacy.md", []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		s := newNativeEditorSession("legacy.md", d)
		if err := validateDeckObjectReferences(s.deck); err != nil {
			t.Fatal("assigning editor IDs invalidated the loaded connector", err)
		}
		if err := s.apply(nativeEditorAction{Action: "clone-slide"}); err != nil {
			t.Fatal("cannot duplicate recovered slide", err)
		}
		if _, err := serializeDeck("legacy.md", s.deck); err != nil {
			t.Fatal("cannot save duplicated recovered slide", err)
		}
	}
}

func TestLoadedDanglingMasterConnectorPreservesData(t *testing.T) {
	d := referenceDeck()
	d.Objects = nil
	d.Masters = defaultMasterDeck()
	d.Masters.Base.Slide = referenceDiagram()
	d.Masters.Base.Slide.ID = "base-diagram"
	d.Masters.Base.Slide.Elements = d.Masters.Base.Slide.Elements[1:]
	d.Slides[0].LayoutID = "blank"
	master, err := encodeMasterDeckMetadata(d.Masters)
	if err != nil {
		t.Fatal(err)
	}
	d, err = parseDeckData("master.md", []byte(master+"\n\n<!-- layout=blank -->\n# Title\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Masters.Base.Slide.Elements) != 2 {
		t.Fatal("removed master connector")
	}
	if _, err := serializeDeck("master.md", d); err != nil {
		t.Fatal(err)
	}
}

// Optional private fixture: never copy a customer's deck into public testdata,
// and never write the input file. Exercise the exact reported document in memory.
func TestLegacyConnectorPrivateDeck(t *testing.T) {
	path := os.Getenv("KEYNOPE_LEGACY_DECK")
	if path == "" {
		t.Skip("private regression fixture not configured")
	}
	input, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := parseDeckData(path, input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := serializeDeck(path, d)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := parseDeckData(path, data)
	if err != nil || len(reloaded.Slides) != len(d.Slides) {
		t.Fatal("round trip failed", err)
	}
	lines := 0
	for i, slide := range d.Slides {
		for _, element := range slide.Elements {
			if element.Kind == "connector" {
				lines++
				j := nativeEditorElementIndex(reloaded.Slides[i].Elements, -1, element.ID)
				if j < 0 || reloaded.Slides[i].Elements[j].Query != element.Query {
					t.Fatal("connector changed on round trip", element.ID)
				}
			}
		}
		if _, err := buildDocumentSlideScene(d, i, 245, 56); err != nil {
			t.Fatal("scene failed", i, err)
		}
	}
	s := newNativeEditorSession(path, d)
	if _, err := serializeDeck(path, s.deck); err != nil {
		t.Fatal("editor normalization blocked saving", err)
	}
	for i, slide := range s.deck.Slides {
		// Exercise one real page with orphan lines, not just its parser model.
		recovered := false
		for _, e := range slide.Elements {
			recovered = recovered || e.unresolvedConnectorReference != ""
		}
		if !recovered {
			continue
		}
		for j, e := range slide.Elements {
			if (e.Kind == "text" || e.Kind == "heading") && !objectLocked(e) && !protectedActivityElement(e) {
				s.current = i
				e.Text += " (regression check)"
				if err := s.apply(nativeEditorAction{Action: "update-element", Element: j, ElementData: &e}); err != nil {
					t.Fatal("cannot edit page with existing orphan lines", err)
				}
				if _, err := serializeDeck(path, s.deck); err != nil {
					t.Fatal("cannot save edited page", err)
				}
				break
			}
		}
		break
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(input, after) {
		t.Fatal("source file changed", err)
	}
	t.Logf("Opened, rendered and round-tripped %d slides, preserving %d connectors; source unchanged", len(d.Slides), lines)
}
