package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func wordStyleFixture(t *testing.T) (Deck, []sceneRun) {
	t.Helper()
	runs := []sceneRun{{Text: "Alpha ", Bold: true, Color: "#ff55aa", FontID: "mono", FontSize: 80}, {Text: "Beta", Italic: true}, {Text: " 👩🏽‍💻 tail", Underline: true}}
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light")}
	element := withConvertedRuns(Element{ID: "words", Kind: "text", Query: "render=truetype&modern-font=sans&modern-size=60&left=15&top=6&width=210&height=40&object-rotation=5&link=https%3A%2F%2Fexample.com"}, runs)
	deck.Slides = []Slide{{ID: "word-slide", Elements: []Element{element}}}
	return deck, runs
}

func TestTextRangeBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_WORD_STYLE_EVIDENCE")
	if root == "" {
		t.Skip("word style fixture output not requested")
	}
	deck, _ := wordStyleFixture(t)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "words.md"), deck); err != nil {
		t.Fatal(err)
	}
	painters := cloneDeck(deck)
	painters.Slides[0].Elements[0].Query = setQueryValue(painters.Slides[0].Elements[0].Query, "height", "8")
	painters.Slides[0].Elements = append(painters.Slides[0].Elements,
		Element{ID: "destination", Kind: "text", Text: "Bravo Gamma", Query: "render=truetype&modern-font=c64&modern-size=72&fg=%2355ffff&left=30&top=23&width=150&height=10&object-rotation=-2&link=https%3A%2F%2Fkeynope.sh"},
		Element{ID: "label-owner", Kind: "shape", Text: "[shape:square]", Query: "shape=square&fg=%23143458&left=30&top=40&width=160&height=12"},
		Element{ID: "locked", Kind: "text", Text: "Locked", Query: "render=truetype&modern-font=mono&modern-size=40&left=30&top=35&width=80&height=3&object-locked=1"},
	)
	label, err := json.Marshal(shapeLabelData{Text: "Delta Zebra", Query: "render=truetype&modern-font=c64&modern-size=72&fg=%2355ffff"})
	if err != nil {
		t.Fatal(err)
	}
	painters.Slides[0].Elements[2].Query = setQueryValue(painters.Slides[0].Elements[2].Query, "shape-label", base64.StdEncoding.EncodeToString(label))
	if err := saveDeck(filepath.Join(root, "painters.md"), painters); err != nil {
		t.Fatal(err)
	}
}

func TestTextRangeUnicodeBoundariesAndMixedCopy(t *testing.T) {
	for _, item := range []struct {
		text string
		a, b int
		ok   bool
	}{{"a😀b", 1, 3, true}, {"a😀b", 1, 2, false}, {"a\u0308b", 0, 2, true}, {"a\u0308b", 0, 1, false}, {"👩🏽‍💻", 0, 7, true}, {"👩🏽‍💻", 0, 5, false}, {"🇳🇱", 0, 4, true}, {"🇳🇱", 0, 2, false}, {"abc", -1, 2, false}, {"abc", 2, 1, false}, {"abc", 0, 4, false}, {"abc", 1, 1, false}} {
		_, _, err := textRangeBytes([]sceneRun{{Text: item.text}}, &textStyleRange{item.a, item.b})
		if (err == nil) != item.ok {
			t.Fatalf("%q [%d,%d]: %v", item.text, item.a, item.b, err)
		}
	}
	runs := []sceneRun{{Text: "A", Bold: true, Color: "#ff0000", FontID: "mono", FontSize: 80}, {Text: "B", Italic: true, Color: "#00ff00", FontID: "sans", FontSize: 40}}
	snapshot, err := captureTextRangeStyle(runs, &textStyleRange{0, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Fields) != 1 || snapshot.Fields["text.underline"].Value != "false" {
		t.Fatal("mixed values flattened", snapshot.Fields)
	}
	target := []sceneRun{{Text: "Target", Bold: true, Italic: true, Underline: true, FontID: "c64", FontSize: 120, Color: "#123456"}}
	got, _, err := applyTextRangeStyle(target, &textStyleRange{0, 6}, snapshot, false, false)
	if err != nil || len(got) != 1 || got[0].Underline || !got[0].Bold || !got[0].Italic || got[0].FontID != "c64" || got[0].FontSize != 120 || got[0].Color != "#123456" {
		t.Fatal("mixed style copy overwrote omitted destination fields", got, err)
	}
	owned := append([]sceneRun(nil), target...)
	reset, changed, err := applyTextRangeStyle(target, &textStyleRange{0, 6}, nil, true, true)
	if err != nil || !changed || !reset[0].Bold || reset[0].FontSize != 0 || reset[0].FontID != "" || reset[0].Color != "" || reset[0].Italic || reset[0].Underline || !reflect.DeepEqual(target, owned) {
		t.Fatal("reset failed to inherit defaults or mutated source", reset, err)
	}
}

func TestTextRangeCrossObjectDraftIsolation(t *testing.T) {
	for _, master := range []bool{false, true} {
		deck, sourceRuns := wordStyleFixture(t)
		labelData, err := json.Marshal(shapeLabelData{Text: "Delta Zebra", Query: "render=truetype&modern-font=c64&modern-size=72&fg=%2355ffff"})
		if err != nil {
			t.Fatal(err)
		}
		shape := Element{ID: "label-owner", Kind: "shape", Text: "[shape:square]", Query: "shape=square&fg=%23143458&left=30&top=40&width=160&height=12"}
		shape.Query = setQueryValue(shape.Query, "shape-label", base64.StdEncoding.EncodeToString(labelData))
		deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "destination", Kind: "text", Text: "Bravo Gamma", Query: "render=truetype&modern-font=c64&modern-size=72&left=30&top=23&width=150&height=10&link=https%3A%2F%2Fkeynope.sh"}, shape)
		s := newNativeEditorSession("Painters.md", deck)
		if master {
			s.masterMode, s.currentMaster = true, 0
			s.deck.Masters.Base.Slide = cloneSlide(deck.Slides[0])
			s.deck.Masters.Base.Slide.ID = "painter-master"
		}
		before, version := cloneDeck(s.deck), s.version
		call := func(action nativeEditorAction) []byte {
			data, err := json.Marshal(action)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			s.handleTextStyleDraft(w, httptest.NewRequest(http.MethodPost, "/api/editor/text-style-draft", bytes.NewReader(data)))
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			return w.Body.Bytes()
		}
		request := nativeEditorAction{Action: "copy-object-style", StyleContext: "text-range", SceneMaster: master, SceneRevision: &version, ObjectID: "words", TextRuns: sourceRuns, TextRange: &textStyleRange{0, 5}}
		var copyResult struct {
			Style *ObjectStyleSnapshot `json:"styleClipboard"`
		}
		if err := json.Unmarshal(call(request), &copyResult); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"destination", "label-owner"} {
			request.Action, request.ObjectID, request.StyleData = "paste-object-style", id, copyResult.Style
			request.TextRuns = []sceneRun{{Text: "Bravo Gamma", Underline: true}}
			if id == "label-owner" {
				request.TextRuns[0].Text = "Delta Zebra"
			}
			var response struct {
				Runs []sceneRun `json:"textRuns"`
			}
			if err := json.Unmarshal(call(request), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Runs) != 2 || response.Runs[0].FontID != "mono" || response.Runs[0].FontSize != 80 || response.Runs[0].Color != "#ff55aa" || !response.Runs[0].Bold || response.Runs[0].Underline || response.Runs[1].Text != " Gamma" && response.Runs[1].Text != " Zebra" || !response.Runs[1].Underline || response.Runs[1].FontSize != 0 {
				t.Fatal("cross-object style lost identity or restyled unselected words", id, response.Runs)
			}
			if s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(s.deck, before) {
				t.Fatal("cross-object draft modified the deck before Apply")
			}
		}
		// Returned/caller-owned data cannot mutate the session clipboard.
		copyResult.Style.Fields["text.font"] = ObjectStyleField{Present: true, Value: "c64"}
		if s.styleClipboard.Fields["text.font"].Value != "mono" {
			t.Fatal("word snapshot aliases the ephemeral source buffer")
		}
	}
}

func TestTextRangeDraftTransactionsAndMaster(t *testing.T) {
	for _, master := range []bool{false, true} {
		deck, runs := wordStyleFixture(t)
		s := newNativeEditorSession("Words.md", deck)
		if master {
			s.masterMode, s.currentMaster = true, 0
			s.deck.Masters.Base.Slide = cloneSlide(deck.Slides[0])
			s.deck.Masters.Base.Slide.ID = "word-master"
		}
		before, version := cloneDeck(s.deck), s.version
		request := nativeEditorAction{Action: "copy-object-style", StyleContext: "text-range", SceneMaster: master, SceneRevision: &version, ObjectID: "words", TextRuns: runs, TextRange: &textStyleRange{0, 5}}
		call := func(action nativeEditorAction) (int, []byte) {
			body, _ := json.Marshal(action)
			w := httptest.NewRecorder()
			s.handleTextStyleDraft(w, httptest.NewRequest(http.MethodPost, "/api/editor/text-style-draft", bytes.NewReader(body)))
			return w.Code, w.Body.Bytes()
		}
		status, body := call(request)
		if status != 200 {
			t.Fatal(string(body))
		}
		var copied struct {
			Clipboard *ObjectStyleSnapshot `json:"styleClipboard"`
		}
		if err := json.Unmarshal(body, &copied); err != nil || copied.Clipboard.Context != "text-range" {
			t.Fatal("invalid clipboard", err)
		}
		if bytes.Contains(body, []byte("Alpha")) || s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("copy leaked content or mutated document")
		}
		request.Action, request.TextRange, request.StyleData = "paste-object-style", &textStyleRange{6, 10}, copied.Clipboard
		status, body = call(request)
		if status != 200 {
			t.Fatal(string(body))
		}
		var pasted struct {
			Runs         []sceneRun               `json:"textRuns"`
			Capabilities selectionCapabilityState `json:"selectionCapabilities"`
		}
		if err := json.Unmarshal(body, &pasted); err != nil {
			t.Fatal(err)
		}
		if s.version != version || len(s.undo) != 0 || !reflect.DeepEqual(s.deck, before) || pasted.Capabilities.Properties["text.size"].Value != float64(80) {
			t.Fatal("draft paste committed or returned stale accepted values")
		}
		if err := s.apply(nativeEditorAction{Action: "set-scene-text", Slide: 0, SceneMaster: master, SceneRevision: &version, ObjectID: "words", TextRuns: pasted.Runs}); err != nil {
			t.Fatal(err)
		}
		after := cloneDeck(s.deck)
		var changed Element
		if master {
			changed = s.deck.Masters.Base.Slide.Elements[0]
		} else {
			changed = s.deck.Slides[0].Elements[0]
		}
		q, old := mustQuery(changed.Query), mustQuery(deck.Slides[0].Elements[0].Query)
		for _, key := range []string{"left", "top", "width", "height", "link", "object-rotation", "modern-size", "modern-font"} {
			if q.Get(key) != old.Get(key) {
				t.Fatal("word formatting changed object property", key)
			}
		}
		visible := sceneTextFor(changed).Runs
		if len(visible) != 2 || visible[0].Text != "Alpha Beta" || visible[0].FontID != "mono" || visible[0].FontSize != 80 || visible[0].Color != "#ff55aa" || visible[1].Text != " 👩🏽‍💻 tail" || !visible[1].Underline {
			t.Fatal("range style/content did not survive commit", visible)
		}
		if s.version != version+1 || len(s.undo) != 1 {
			t.Fatal("range edit wasn't one transaction")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
			t.Fatal("word undo mismatch", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
			t.Fatal("word redo mismatch", err)
		}
		data, err := serializeDeck("Words.md", s.deck)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Words.md", data)
		if err != nil {
			t.Fatal(err)
		}
		if master {
			changed = loaded.Masters.Base.Slide.Elements[0]
		} else {
			changed = loaded.Slides[0].Elements[0]
		}
		if !reflect.DeepEqual(sceneTextFor(changed).Runs, visible) {
			t.Fatal("Markdown reload lost per-word fonts")
		}
	}
}

func TestTextRangeInvalidStaleAndCancelled(t *testing.T) {
	for _, scenario := range []string{"stale", "scope", "missing", "ambiguous", "locked", "group", "protected", "invalid-font", "invalid-size", "invalid-colour", "split-emoji", "wrong-context", "unknown-field", "oversized", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			deck, runs := wordStyleFixture(t)
			s := newNativeEditorSession("Words.md", deck)
			version := s.version
			snapshot, _ := captureTextRangeStyle(runs, &textStyleRange{0, 5})
			a := nativeEditorAction{Action: "paste-object-style", StyleContext: "text-range", SceneRevision: &version, ObjectID: "words", TextRuns: runs, TextRange: &textStyleRange{6, 10}, StyleData: snapshot}
			ctx := context.Background()
			switch scenario {
			case "stale":
				version--
			case "scope":
				a.SceneMaster = true
			case "missing":
				a.ObjectID = "gone"
			case "ambiguous":
				s.deck.Slides[0].Elements = append(s.deck.Slides[0].Elements, s.deck.Slides[0].Elements[0])
			case "locked":
				s.deck.Slides[0].Elements[0].Query += "&object-locked=1"
			case "group":
				s.deck.Slides[0].Elements[0].Query += "&group=closed"
			case "protected":
				s.deck.Slides[0].Elements[0].PlaceholderRole = activityTitleRole
			case "invalid-font":
				a.TextRuns = append([]sceneRun(nil), runs...)
				a.TextRuns[0].FontID = "external-font"
			case "invalid-size":
				a.TextRuns = append([]sceneRun(nil), runs...)
				a.TextRuns[0].FontSize = 2048
			case "invalid-colour":
				snapshot.Fields["paint.color"] = ObjectStyleField{Present: true, Value: "javascript:bad"}
			case "split-emoji":
				a.TextRange = &textStyleRange{11, 12}
			case "wrong-context":
				snapshot.Context = ""
			case "unknown-field":
				snapshot.Fields["paint.opacity"] = ObjectStyleField{Present: true, Value: "0.5"}
			case "oversized":
				a.TextRuns = []sceneRun{{Text: strings.Repeat("A", 100001)}}
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			before := cloneDeck(s.deck)
			body, _ := json.Marshal(a)
			w := httptest.NewRecorder()
			s.handleTextStyleDraft(w, httptest.NewRequest(http.MethodPost, "/api/editor/text-style-draft", bytes.NewReader(body)).WithContext(ctx))
			if scenario != "cancel" && w.Code == 200 {
				t.Fatal("accepted invalid draft", w.Body.String())
			}
			if s.version != 1 || len(s.undo) != 0 || s.styleClipboard != nil || !reflect.DeepEqual(before, s.deck) {
				t.Fatal("rejected draft changed document/clipboard/history")
			}
		})
	}
}

func TestModernRunFontsCodecAndLegacyEdit(t *testing.T) {
	runs := []sceneRun{{Text: "AB", FontID: "mono", FontSize: 80}, {Text: "CD", FontID: "sans", FontSize: 40}}
	e := withConvertedRuns(Element{ID: "font-runs", Kind: "text", Query: "render=truetype"}, runs)
	if !reflect.DeepEqual(sceneTextFor(e).Runs, runs) {
		t.Fatal("codec merged or dropped per-word fonts")
	}
	after := e
	after.Text = "ABxCD"
	after = preserveRunStyles(e, after)
	got := sceneTextFor(after).Runs
	if len(got) != 2 || got[0].Text != "ABx" || got[0].FontID != "mono" || got[0].FontSize != 80 || got[1].FontID != "sans" || got[1].FontSize != 40 {
		t.Fatal("legacy insertion lost neighbouring word fonts", got)
	}
	for _, run := range []sceneRun{{Text: "x", FontID: "unknown"}, {Text: "x", FontSize: math.NaN()}, {Text: "x", FontSize: math.Inf(1)}, {Text: "x", FontSize: -1}, {Text: string([]byte{0xff})}} {
		if _, err := encodeModernRuns(run.Text, "", "text", []sceneRun{run}); err == nil {
			t.Fatal("accepted invalid run", run)
		}
	}
}
