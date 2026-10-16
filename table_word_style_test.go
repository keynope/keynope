package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func tableWordStyleCall(t *testing.T, s *nativeEditorSession, action nativeEditorAction, cell map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if cell != nil {
		fields["tableWord"], err = json.Marshal(cell)
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleTextStyleDraft(w, httptest.NewRequest(http.MethodPost, "/api/editor/text-style-draft", bytes.NewReader(raw)))
	return w
}

func TestTableWordStyleBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_TABLE_WORD_STYLE_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	d := tableWordThemeFixture(t)
	d.Slides[0].Elements = append(d.Slides[0].Elements, withConvertedRuns(Element{ID: "word-source", Kind: "text", Query: "left=20&top=3&width=200&height=6&render=truetype&modern-font=sans&modern-size=40&fg=%23ffffff"}, []sceneRun{{Text: "Source", FontID: "mono", FontSize: 80, Color: "#112233", ColorToken: "accent-1", Bold: true}, {Text: " words"}}))
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "word-transfer.md"), d); err != nil {
		t.Fatal(err)
	}
}

func TestTableWordStyleSharedDraftAndOwnership(t *testing.T) {
	for _, master := range []bool{false, true} {
		d := tableWordThemeFixture(t)
		s := newNativeEditorSession("Table word styles.md", d)
		if master {
			s.masterMode = true
			s.deck.Masters.Base.Slide = cloneSlide(d.Slides[0])
			s.deck.Slides[0].Elements = nil
		}
		before, version := cloneDeck(s.deck), s.version
		a := nativeEditorAction{Action: "copy-object-style", StyleContext: "text-range", SceneMaster: master, SceneRevision: &version, ObjectID: "table-word", TextRuns: []sceneRun{{Text: "Role", FontID: "mono", FontSize: 80, Color: "#112233", ColorToken: "accent-1", Bold: true}, {Text: " untouched"}}, TextRange: &textStyleRange{0, 4}}
		cell := map[string]any{"row": 0, "column": 1, "rows": 2, "columns": 2, "header": false}
		w := tableWordStyleCall(t, s, a, cell)
		if w.Code != 200 {
			t.Fatal("table caret cannot use shared word command", master, w.Code, w.Body.String())
		}
		var response struct {
			Clipboard *ObjectStyleSnapshot      `json:"styleClipboard"`
			Caps      *selectionCapabilityState `json:"selectionCapabilities"`
			Runs      []sceneRun                `json:"textRuns"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Clipboard.Context != "text-range" || response.Clipboard.Fields["text.font"].Value != "mono" || response.Caps.Properties["paint.colorToken"].Value != "accent-1" {
			t.Fatal("table words got a cell/object snapshot", response)
		}
		snapshot := response.Clipboard.Clone()
		a.Action = "paste-object-style"
		a.StyleData = snapshot
		a.TextRuns = []sceneRun{{Text: "Target ", Color: "#ffaa55"}, {Text: "Tail", Italic: true}}
		a.TextRange = &textStyleRange{0, 6}
		w = tableWordStyleCall(t, s, a, map[string]any{"row": 1, "column": 0, "rows": 3, "columns": 2, "header": false})
		// Local insertion/deletion may change draft topology. A bounded cell
		// descriptor is validated, without uploading the entire table per word.
		if w.Code != 200 {
			t.Fatal("cross-cell word paste failed", w.Code, w.Body.String())
		}
		response.Runs = nil
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Runs) != 3 || response.Runs[0].FontID != "mono" || response.Runs[0].FontSize != 80 || response.Runs[0].ColorToken != "accent-1" || response.Runs[1].Text != " " || response.Runs[2].Text != "Tail" || !response.Runs[2].Italic {
			t.Fatal("word paste changed unselected text/style", response.Runs)
		}
		if s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(s.deck, before) || !reflect.DeepEqual(s.styleClipboard, snapshot) {
			t.Fatal("word draft changed table/history/source clipboard")
		}
		a.Action = "reset-object-style"
		a.TextRuns = []sceneRun{{Text: "Header", Color: "#ffaa55", Bold: false, FontID: "mono", FontSize: 90}}
		a.TextRange = &textStyleRange{0, 6}
		w = tableWordStyleCall(t, s, a, map[string]any{"row": 0, "column": 0, "rows": 2, "columns": 2, "header": true})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		response.Runs = nil
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Runs[0].Bold || response.Runs[0].Color != "" || response.Runs[0].FontID != "" || response.Runs[0].FontSize != 0 {
			t.Fatal("Reset did not retain header inheritance", response.Runs)
		}
	}
}

func TestTableWordStyleScopeValidation(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*nativeEditorSession, *nativeEditorAction, map[string]any)
	}{
		{"missing-cell", func(_ *nativeEditorSession, _ *nativeEditorAction, c map[string]any) { clear(c) }},
		{"negative-row", func(_ *nativeEditorSession, _ *nativeEditorAction, c map[string]any) { c["row"] = -1 }},
		{"outside-column", func(_ *nativeEditorSession, _ *nativeEditorAction, c map[string]any) { c["column"] = 2 }},
		{"oversized-grid", func(_ *nativeEditorSession, _ *nativeEditorAction, c map[string]any) { c["rows"] = 101 }},
		{"non-header-row", func(_ *nativeEditorSession, _ *nativeEditorAction, c map[string]any) {
			c["row"] = 1
			c["header"] = true
		}},
		{"fractional-row", func(_ *nativeEditorSession, _ *nativeEditorAction, c map[string]any) { c["row"] = .5 }},
		{"foreign-owner", func(s *nativeEditorSession, _ *nativeEditorAction, _ map[string]any) {
			s.deck.Slides[0].Elements[0].Kind = "text"
			s.deck.Slides[0].Elements[0].ObjectID = ""
		}},
		{"missing-payload", func(s *nativeEditorSession, _ *nativeEditorAction, _ map[string]any) { s.deck.Objects = nil }},
		{"locked", func(s *nativeEditorSession, _ *nativeEditorAction, _ map[string]any) {
			s.deck.Slides[0].Elements[0].Query = setQueryValue(s.deck.Slides[0].Elements[0].Query, "object-locked", "1")
		}},
		{"group", func(s *nativeEditorSession, _ *nativeEditorAction, _ map[string]any) {
			s.deck.Slides[0].Elements[0].Query = setQueryValue(s.deck.Slides[0].Elements[0].Query, "group", "outside")
		}},
		{"stale", func(_ *nativeEditorSession, a *nativeEditorAction, _ map[string]any) {
			version := *a.SceneRevision - 1
			a.SceneRevision = &version
		}},
		{"ambiguous-range", func(_ *nativeEditorSession, a *nativeEditorAction, _ map[string]any) { a.CellRange = &tableCellRange{} }},
		{"whole-table-upload", func(s *nativeEditorSession, a *nativeEditorAction, _ map[string]any) {
			d := cloneTable(s.deck.Objects.Tables["table-word-data"])
			a.TableData = &d
		}},
		{"cell-snapshot", func(_ *nativeEditorSession, a *nativeEditorAction, _ map[string]any) {
			a.Action = "paste-object-style"
			a.StyleData = &ObjectStyleSnapshot{Version: 1, Context: "table-cells", Fields: map[string]ObjectStyleField{}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newNativeEditorSession("Scope.md", tableWordThemeFixture(t))
			version := s.version
			a := nativeEditorAction{Action: "copy-object-style", StyleContext: "text-range", SceneRevision: &version, ObjectID: "table-word", TextRuns: []sceneRun{{Text: "Test"}}, TextRange: &textStyleRange{0, 4}}
			cell := map[string]any{"row": 0, "column": 1, "rows": 2, "columns": 2, "header": false}
			test.edit(s, &a, cell)
			before := cloneDeck(s.deck)
			w := tableWordStyleCall(t, s, a, cell)
			if w.Code < 400 || s.version != version || len(s.undo) != 0 || s.styleClipboard != nil || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("invalid table word scope accepted or changed document", w.Code, w.Body.String())
			}
		})
	}
}

func TestTableWordStyleCancellationAndEnteredGroup(t *testing.T) {
	s := newNativeEditorSession("Grouped table.md", tableWordThemeFixture(t))
	s.deck.Slides[0].Elements[0].Query = setQueryValue(s.deck.Slides[0].Elements[0].Query, "group", "editing")
	s.editingGroup = "editing"
	version := s.version
	a := nativeEditorAction{Action: "copy-object-style", StyleContext: "text-range", SceneRevision: &version, ObjectID: "table-word", TextRuns: []sceneRun{{Text: "😀 Word", FontID: "mono", FontSize: 80}}, TextRange: &textStyleRange{3, 7}}
	cell := map[string]any{"row": 1, "column": 0, "rows": 2, "columns": 2, "header": false}
	w := tableWordStyleCall(t, s, a, cell)
	if w.Code != 200 {
		t.Fatal("entered group rejected", w.Code, w.Body.String())
	}
	s.styleClipboard = nil
	bad := a
	bad.TextRange = &textStyleRange{0, 1}
	w = tableWordStyleCall(t, s, bad, cell)
	if w.Code != 400 || s.styleClipboard != nil {
		t.Fatal("table word styling split a surrogate", w.Code)
	}
	a.TableWord = &tableWordContext{Row: 1, Column: 0, Rows: 2, Columns: 2}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodPost, "/api/editor/text-style-draft", bytes.NewReader(raw)).WithContext(ctx)
	w = httptest.NewRecorder()
	s.handleTextStyleDraft(w, r)
	if w.Body.Len() != 0 || s.styleClipboard != nil || s.version != version || len(s.undo) != 0 {
		t.Fatal("cancelled table word copy changed state", w.Code, w.Body.String())
	}
}
