package app

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
)

func previewTypefaceDeck(t *testing.T) Deck {
	t.Helper()
	deck := typefaceDeck(t)
	data := bytes.Clone(gomono.TTF)
	binary.BigEndian.PutUint16(sfntTable(data, "OS/2")[8:10], 4)
	face := testTypeface(t, data)
	deck.Typefaces = map[string]DeckTypeface{face.ID(): face}
	deck.Slides[0].Elements[0].Query = setQueryValue(deck.Slides[0].Elements[0].Query, "modern-font", face.ID())
	deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "editable", Kind: "text", Text: "Keep editing me", Query: "render=truetype&modern-font=sans&top=30"})
	return deck
}

func TestPreviewPrintLocksOnlyItsText(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(map[bool]string{false: "slide", true: "master"}[master], func(t *testing.T) {
			deck := previewTypefaceDeck(t)
			if master {
				deck.Masters.Base.Slide = cloneSlide(deck.Slides[0])
				deck.Masters.Base.Slide.ID = "font-master"
			}
			s := newNativeEditorSession("Preview.md", deck)
			if master {
				if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
					t.Fatal(err)
				}
			}
			index := func(id string) int {
				slide := s.deck.Slides[0]
				if master {
					slide = *masterSlideAt(&s.deck, 0)
				}
				for i, e := range slide.Elements {
					if e.ID == id {
						return i
					}
				}
				t.Fatal(id)
				return -1
			}
			element := func(id string) Element {
				if master {
					return masterSlideAt(&s.deck, 0).Elements[index(id)]
				}
				return s.deck.Slides[0].Elements[index(id)]
			}
			oldVersion, oldUndo := s.version, len(s.undo)
			revision := s.version
			if err := s.apply(nativeEditorAction{Action: "set-scene-text", ObjectID: "font-text", SceneMaster: master, SceneRevision: &revision, TextRuns: []sceneRun{{Text: "Should not change"}}}); err == nil || !strings.Contains(err.Error(), "Preview & Print") {
				t.Fatal("preview text edited", err)
			}
			if s.version != oldVersion || len(s.undo) != oldUndo {
				t.Fatal("rejected edit leaked history or revision")
			}
			updated := element("font-text")
			updated.Text = "Bypass using legacy update"
			if err := s.apply(nativeEditorAction{Action: "update-element", Element: index("font-text"), ElementData: &updated}); err == nil {
				t.Fatal("legacy mutation bypassed protection")
			}
			updated = element("editable")
			updated.Text = "Other text is editable"
			if err := s.apply(nativeEditorAction{Action: "update-element", Element: index("editable"), ElementData: &updated}); err != nil {
				t.Fatal("unrelated text locked", err)
			}
			updated = element("font-text")
			updated.Query = setQueryValue(updated.Query, "left", "20")
			if err := s.apply(nativeEditorAction{Action: "update-element", Element: index("font-text"), ElementData: &updated}); err != nil {
				t.Fatal("moving text blocked", err)
			}
			updated = element("font-text")
			updated.Query = setQueryValue(updated.Query, "modern-font", "sans")
			if err := s.apply(nativeEditorAction{Action: "update-element", Element: index("font-text"), ElementData: &updated}); err != nil {
				t.Fatal("font replacement blocked", err)
			}
			updated = element("font-text")
			updated.Text = "Now editable"
			if err := s.apply(nativeEditorAction{Action: "update-element", Element: index("font-text"), ElementData: &updated}); err != nil {
				t.Fatal("replacement did not unlock text", err)
			}
		})
	}
}

func TestPreviewPrintPreservesPermissionsAndSceneFlag(t *testing.T) {
	deck := previewTypefaceDeck(t)
	data, err := serializeDeck("Preview.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	deck, err = parseDeckData("Preview.md", data)
	if err != nil {
		t.Fatal(err)
	}
	scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range scene.Objects {
		if o.Text != nil && o.Text.ReadOnly != (o.ID == "font-text") {
			t.Fatal("wrong text locked", o.ID)
		}
	}
	presentation, _, err := deckToPPTX(deck)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range presentation.EmbeddedFonts {
		if typefacePermission(typefaceEmbeddingFlags(f.Data)) == 4 {
			found = true
			if f.ForceEditable {
				t.Fatal("permission override")
			}
		}
	}
	if !found {
		t.Fatal("preview font not embedded")
	}
	if fonts, err := restrictedExportFonts(deck, "save"); err != nil || len(fonts) != 0 {
		t.Fatal("preview fonts should not receive restricted warning", fonts, err)
	}
}

func TestRestrictedSaveApprovalAndRecovery(t *testing.T) {
	deck, id := restrictedTypefaceDeck(t)
	s := newNativeEditorSession("Restricted.md", deck)
	save := func(draft bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(nativeEditorDocumentRequest{Path: "Restricted.md", Draft: draft})
		w := httptest.NewRecorder()
		s.handleDocument(w, httptest.NewRequest("POST", "/api/editor/document", bytes.NewReader(body)))
		return w
	}
	if w := save(false); w.Code != 428 {
		t.Fatal("save skipped warning", w.Code, w.Body.String())
	}
	if w := save(true); w.Code != 200 {
		t.Fatal("background recovery should not prompt", w.Code, w.Body.String())
	}
	decision, _ := json.Marshal(fontExportApproval{Owner: s.notesSessionID, Version: s.version, Format: "save", Confirmed: true})
	w := httptest.NewRecorder()
	s.handleFontExport(w, httptest.NewRequest("POST", "/api/editor/font-export", bytes.NewReader(decision)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = save(false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var saved nativeEditorDocument
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Restricted.md", []byte(saved.Content))
	if err != nil {
		t.Fatal(err)
	}
	if face := reopened.Typefaces[id]; !face.LocalOnly || len(face.Data) != 0 {
		t.Fatal("restricted data saved")
	}
	if w := save(false); w.Code != 428 {
		t.Fatal("approval reused")
	}
	scene, err := buildDocumentSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(pdfPageFallbackReasons(exportPage{Scene: &scene})) == 0 {
		t.Fatal("restricted fonts must not be embedded by the PDF print engine")
	}
}

func TestSaveFontReplacementIsUndoable(t *testing.T) {
	deck, id := restrictedTypefaceDeck(t)
	s := newNativeEditorSession("Restricted.md", deck)
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "replace-typefaces", SceneRevision: &revision, TypefaceReplacements: map[string]string{id: "sans"}}); err != nil {
		t.Fatal(err)
	}
	if q := mustQuery(s.deck.Slides[0].Elements[0].Query); q.Get("modern-font") != "sans" {
		t.Fatal(q)
	}
	if _, _, err := s.deckForFontExport("save"); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.deck.Slides[0].Elements[0].Query, id) {
		t.Fatal("replacement not undoable")
	}
}

func TestLegacyRestrictedEmbeddingBecomesReference(t *testing.T) {
	data := bytes.Clone(gomono.TTF)
	binary.BigEndian.PutUint16(sfntTable(data, "OS/2")[8:10], 2)
	old := DeckTypeface{Name: "Go Mono", PostScriptName: "GoMono", Data: data}
	oldID := old.ID()
	// Reproduce the previous format, before restricted data was stripped during
	// marshaling. The old hash must be validated before references are migrated.
	raw, _ := json.Marshal(map[string]any{oldID: map[string]any{"name": old.Name, "postScriptName": old.PostScriptName, "data": base64.StdEncoding.EncodeToString(data)}})
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write(raw)
	_ = writer.Close()
	markdown := "<!-- keynope width=245 height=56 -->\n<!-- keynope-typefaces version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->\n\n<!-- render=truetype modern-font=" + oldID + " -->\nLegacy text\n"
	deck, err := parseDeckData("Legacy.md", []byte(markdown))
	if err != nil {
		t.Fatal(err)
	}
	local := testTypeface(t, data)
	id := local.ID()
	if id == oldID || !deck.Typefaces[id].LocalOnly || len(deck.Typefaces[id].Data) != 0 {
		t.Fatal("legacy outlines retained")
	}
	if !strings.Contains(deck.Slides[0].Elements[0].Query, id) {
		t.Fatal("reference was not migrated")
	}
	encoded, err := serializeDeck("Legacy.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Legacy.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Typefaces[id].Data) != 0 {
		t.Fatal("outlines came back on reopen")
	}
}

func TestPreviewPrintTableCellDoesNotLockOtherCells(t *testing.T) {
	deck := previewTypefaceDeck(t)
	var font string
	for id := range deck.Typefaces {
		font = id
	}
	table, _ := newTableData(2, 2)
	table.Cells[0][0].Text = "Protected cell"
	table.Cells[0][0].Style = &TableCellStyle{FontID: font}
	table.Cells[0][1].Text = "Editable cell"
	deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{"payload": table}}
	deck.Slides[0].Elements = []Element{{ID: "table", Kind: "table", ObjectID: "payload", Query: "left=10&top=5&width=120&height=24"}}
	s := newNativeEditorSession("Cells.md", deck)
	change := func(row, column int, value string) error {
		table := cloneDeck(s.deck).Objects.Tables["payload"]
		table.Cells[row][column].Text = value
		revision := s.version
		return s.apply(nativeEditorAction{Action: "set-table", ObjectID: "table", SceneRevision: &revision, TableData: &table})
	}
	if err := change(0, 0, "Not allowed"); err == nil {
		t.Fatal("preview cell edited")
	}
	if err := change(0, 1, "Still editable"); err != nil {
		t.Fatal("unrelated cell locked", err)
	}
}
