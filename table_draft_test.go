package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestTableDraftProductionProjectionDoesNotMutateDocument(t *testing.T) {
	for _, master := range []bool{false, true} {
		deck := structuredFixture()
		pixels := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		pixels.SetNRGBA(1, 1, color.NRGBA{R: 255, A: 255})
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, pixels); err != nil {
			t.Fatal(err)
		}
		deck.Assets = map[string]DeckAsset{"unrelated": {MIME: "image/png", Width: 4, Height: 4, Data: encoded.Bytes()}}
		deck.Slides[0].BackgroundMedia = &slideBackgroundMedia{AssetID: "unrelated", Query: "image-style=retro"}
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "unrelated-image", Kind: "image", AssetID: "unrelated", Path: "keynope-asset:unrelated", Query: "top=2&left=120&width=20&height=20&image-style=retro"})
		s := newNativeEditorSession("draft.md", deck, false, false)
		s.deck.Masters.Base.Slide = cloneSlide(s.deck.Slides[0])
		s.masterMode, s.currentMaster = master, 0
		before, revision := cloneDeck(s.deck), s.version
		table := cloneTable(s.deck.Objects.Tables["table-1"])
		size := 76.0
		table.Cells[1][0].Style = &TableCellStyle{FontID: "mono", FontSize: &size, Align: "right", Vertical: "bottom", Color: "#55aaff", Fill: "#334455"}
		table.Cells[1][0].Runs = []ObjectTextRun{{Text: "Preview ", Bold: true}, {Text: "😀", Color: "#ffff00"}}
		action := nativeEditorAction{Slide: 0, SceneMaster: master, SceneRevision: &revision, ObjectID: "table-element", TableData: &table}
		body, _ := json.Marshal(action)
		w := httptest.NewRecorder()
		s.handleTableDraft(w, httptest.NewRequest(http.MethodPost, "/api/editor/table-draft", bytes.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatal(master, w.Code, w.Body.String())
		}
		var scene slideScene
		if err := json.Unmarshal(w.Body.Bytes(), &scene); err != nil {
			t.Fatal(err)
		}
		if len(scene.Objects) != 1 || scene.Objects[0].ID != action.ObjectID || scene.Objects[0].Table == nil || scene.Revision == nil || *scene.Revision != revision || scene.Master != master {
			t.Fatal("draft did not isolate the production table projection", master, scene)
		}
		// Compare against a full production projection, not a second isolated
		// code path: dropping siblings must not change the target's appearance.
		fullDeck := cloneDeck(s.deck)
		fullDeck.Objects.Tables["table-1"] = cloneTable(table)
		resolved := fullDeck.ResolveSlide(0, false)
		if master {
			resolved = masterViewPreview(fullDeck.Masters, 0)
		}
		cols, rows := authoredRenderSize(245, 56)
		full, err := buildStyledSlideScene(fullDeck, resolved, 0, cols, rows, true)
		if err != nil {
			t.Fatal(err)
		}
		matched := false
		for _, object := range full.Objects {
			if object.ID == action.ObjectID {
				// Compare wire values: optional empty capability arrays are
				// omitted by JSON, without changing any rendering property.
				matched = bytes.Equal(jsonForTest(t, object), jsonForTest(t, scene.Objects[0]))
			}
		}
		if !matched || scene.BackgroundMedia != nil {
			t.Fatal("isolated preview changed the production table or included background media")
		}
		cell := scene.Objects[0].Table.Cells[1]
		if cell.Text.Size != 76 || cell.Text.Align != "end" || cell.Text.Vertical != "bottom" || cell.Paint.Color != "#55aaff" || cell.Fill != "#334455" || cell.Text.EmojiFonts["😀"] == "" || !cell.Text.Runs[0].Bold {
			t.Fatal("draft lost resolved typography, paint or rich runs", cell)
		}
		if !reflect.DeepEqual(s.deck, before) || s.version != revision || len(s.undo) != 0 || len(s.redo) != 0 {
			t.Fatal("draft modified document/history", master)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("draft responses must not cache across document revisions")
		}
	}
}

func TestTableDraftRejectsInvalidStaleLockedAndForeignScope(t *testing.T) {
	for _, kind := range []string{"method", "missing", "stale", "master", "slide", "locked", "group-locked", "invalid", "trailing", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			s := newNativeEditorSession("draft.md", structuredFixture(), false, false)
			revision := s.version
			table := cloneTable(s.deck.Objects.Tables["table-1"])
			action := nativeEditorAction{Slide: 0, SceneRevision: &revision, ObjectID: "table-element", TableData: &table}
			method, want := http.MethodPost, http.StatusBadRequest
			switch kind {
			case "method":
				method, want = http.MethodGet, http.StatusMethodNotAllowed
			case "missing":
				action.ObjectID, want = "missing", http.StatusNotFound
			case "stale":
				revision--
				want = http.StatusConflict
			case "master":
				action.SceneMaster, want = true, http.StatusConflict
			case "slide":
				action.Slide, want = 1, http.StatusConflict
			case "locked":
				s.deck.Slides[0].Elements[0].Query += "&object-locked=1"
			case "group-locked":
				s.deck.Slides[0].Elements[0].Query += "&group=shared"
				s.deck.Slides[0].Elements[1].Query += "&group=shared&object-locked=1"
			case "invalid":
				table.Cells[0][1].Text = "covered cells cannot contain text"
			}
			body, _ := json.Marshal(action)
			if kind == "trailing" {
				body = append(body, []byte(" {}")...)
			}
			if kind == "oversized" {
				body = []byte(strings.Repeat(" ", maxStructuredObjectBytes+1025))
			}
			before, version := cloneDeck(s.deck), s.version
			w := httptest.NewRecorder()
			s.handleTableDraft(w, httptest.NewRequest(method, "/api/editor/table-draft", bytes.NewReader(body)))
			if w.Code != want {
				t.Fatalf("got %d, want %d: %s", w.Code, want, w.Body.String())
			}
			if !reflect.DeepEqual(before, s.deck) || s.version != version || len(s.undo) != 0 {
				t.Fatal("rejected draft modified the document")
			}
		})
	}
}
