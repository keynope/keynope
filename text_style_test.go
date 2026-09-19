package main

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"reflect"
	"strconv"
	"testing"
)

func TestShapeLabelTypographyProfiles(t *testing.T) {
	for _, styles := range [][2]string{{"retro", "modern"}, {"modern", "retro"}, {"modern", ""}} {
		deck := sceneFixture(t)
		data, _ := json.Marshal(shapeLabelData{Text: "Keep [color=#55ff55]label[/color]", Query: "ttf-size=65&modern-size=42&element-style=" + styles[1]})
		owner := Element{ID: "shape", Kind: "shape", Query: "shape=square&width=80&height=20&element-style=" + styles[0]}
		owner.Query = setQueryValue(owner.Query, "shape-label", base64.StdEncoding.EncodeToString(data))
		deck.Slides[0].Elements = []Element{owner}
		before := cloneDeck(deck)
		size := 73.5
		action := nativeEditorAction{Kind: "label", ObjectIDs: []string{"shape"}, TextSize: &size}
		changed, err := applyTextSizeCommand(&deck, action, "")
		if err != nil || !changed {
			t.Fatalf("size: %v %v", changed, err)
		}
		label, _, err := sceneEditableShapeLabel(deck.Slides[0].Elements[0])
		if err != nil {
			t.Fatal(err)
		}
		q, _ := url.ParseQuery(label.Query)
		modern := styles[1] == "modern" || (styles[1] == "" && styles[0] == "modern")
		if modern && (q.Get("modern-size") != "73.5" || q.Get("ttf-size") != "65") {
			t.Fatal(q)
		}
		if !modern && (q.Get("modern-size") != "42" || q.Get("ttf-size") != "74") {
			t.Fatal(q)
		}
		if label.Text != "Keep [color=#55ff55]label[/color]" {
			t.Fatal("content changed")
		}
		body, _ := url.ParseQuery(deck.Slides[0].Elements[0].Query)
		old, _ := url.ParseQuery(owner.Query)
		body.Del("shape-label")
		old.Del("shape-label")
		if body.Encode() != old.Encode() {
			t.Fatal("body changed")
		}
		font := "mono"
		action.TextSize = nil
		action.ModernFont = &font
		changed, err = applyModernTextStyle(&deck, action, "")
		if err != nil || changed != modern {
			t.Fatalf("font: %v %v", changed, err)
		}
		deck = cloneDeck(before)
		deck.Slides[0].Elements[0].Query += "&group=g"
		locked := cloneDeck(deck)
		if _, err := applyModernTextStyle(&deck, action, ""); err == nil {
			t.Fatal("edited collapsed group")
		}
		if !reflect.DeepEqual(deck, locked) {
			t.Fatal("rejected edit mutated deck")
		}
		if _, err := applyModernTextStyle(&deck, action, "g"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestModernTextStyleBatchAtomicity(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Elements = []Element{
		{ID: "first", Kind: "text", Text: "[color=#00ff00]Keep[/color]", Query: "element-style=modern&group=g&ttf-size=97"},
		{ID: "second", Kind: "code", Text: "literal ** code", Query: "element-style=modern&group=g&ttf-size=60"},
		{ID: "shape", Kind: "shape", Query: "shape=square&group=g"},
		{ID: "retro", Kind: "text", Text: "Keep retro", Query: "element-style=retro&ttf-size=42"},
	}
	before := cloneDeck(deck)
	font, size := "mono", 72.0
	action := nativeEditorAction{ObjectIDs: []string{"first", "retro"}, ModernFont: &font, ModernSize: &size}
	changed, err := applyModernTextStyle(&deck, action, "")
	if err != nil || !changed {
		t.Fatalf("batch: %v %v", changed, err)
	}
	for i := 0; i < 2; i++ {
		q, _ := url.ParseQuery(deck.Slides[0].Elements[i].Query)
		if q.Get("modern-font") != "mono" || q.Get("modern-size") != "72" {
			t.Fatalf("group member not formatted: %v", q)
		}
		if deck.Slides[0].Elements[i].Text != before.Slides[0].Elements[i].Text {
			t.Fatal("typography rewrote content")
		}
	}
	if !reflect.DeepEqual(deck.Slides[0].Elements[2:], before.Slides[0].Elements[2:]) {
		t.Fatal("changed non-Modern-text members")
	}
	deck = cloneDeck(before)
	deck.Slides[0].Elements[1].Query += "&object-locked=1"
	locked := cloneDeck(deck)
	if _, err := applyModernTextStyle(&deck, action, ""); err == nil {
		t.Fatal("accepted locked group member")
	}
	if !reflect.DeepEqual(deck, locked) {
		t.Fatal("partial write on rejected group")
	}
	deck = cloneDeck(before)
	if _, err := applyModernTextStyle(&deck, action, "g"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deck.Slides[0].Elements[1], before.Slides[0].Elements[1]) {
		t.Fatal("entered group changed sibling")
	}
}

func TestFontSwitchNormalizesOpticalWidth(t *testing.T) {
	deck := Deck{
		Appearance: &DeckAppearance{Version: 3},
		Slides: []Slide{{Elements: []Element{{
			ID: "text", Kind: "text", Text: "The quick brown fox", Query: "render=truetype&modern-font=c64&modern-size=270&modern-width=140",
		}}}},
	}
	font := "mono"
	action := nativeEditorAction{Slide: 0, ObjectIDs: []string{"text"}, ModernFont: &font}
	if changed, err := applyModernTextStyle(&deck, action, ""); err != nil || !changed {
		t.Fatalf("C64 to mono: changed=%v err=%v", changed, err)
	}
	q, _ := url.ParseQuery(deck.Slides[0].Elements[0].Query)
	if got, _ := strconv.ParseFloat(q.Get("modern-width"), 64); math.Abs(got-77.784) > .001 || q.Get("modern-size") != "270" {
		t.Fatalf("C64 140 did not normalize to mono 77.784 at the same size: %v", q)
	}
	font = "sans"
	if changed, err := applyModernTextStyle(&deck, action, ""); err != nil || !changed {
		t.Fatalf("mono to sans: changed=%v err=%v", changed, err)
	}
	q, _ = url.ParseQuery(deck.Slides[0].Elements[0].Query)
	if got, _ := strconv.ParseFloat(q.Get("modern-width"), 64); math.Abs(got-68.6) > .001 {
		t.Fatalf("C64 140 did not normalize to proportional 68.6: %v", q)
	}
	font = "c64"
	if changed, err := applyModernTextStyle(&deck, action, ""); err != nil || !changed {
		t.Fatalf("sans to C64: changed=%v err=%v", changed, err)
	}
	q, _ = url.ParseQuery(deck.Slides[0].Elements[0].Query)
	if got, _ := strconv.ParseFloat(q.Get("modern-width"), 64); math.Abs(got-140) > .001 {
		t.Fatalf("round trip did not restore C64 width: %v", q)
	}
}

func TestModernTextStyleRevisionAndUndo(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Elements[0].Query += "&element-style=modern"
	s := newNativeEditorSession("Typography.md", deck)
	before := cloneDeck(s.deck)
	font, revision := "mono", s.version
	action := nativeEditorAction{Action: "set-modern-text-style", ObjectIDs: []string{"title"}, ModernFont: &font, SceneRevision: &revision}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	after := cloneDeck(s.deck)
	if len(s.undo) != 1 {
		t.Fatal("not one transaction")
	}
	if err := s.apply(action); err == nil {
		t.Fatal("accepted stale revision")
	}
	if !reflect.DeepEqual(after, s.deck) {
		t.Fatal("stale command changed document")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("undo did not exactly restore document")
	}
}

func TestMasterTypographyMultiSelectionInheritsAndRoundTrips(t *testing.T) {
	deck := Deck{Slides: []Slide{{Elements: []Element{{ID: "slide-text", Kind: "text", Text: "Unaffected"}}}}, Masters: defaultMasterDeck()}
	deck.Masters.Base.Slide.DefaultStyle = "modern"
	deck.Masters.Layouts[0].Slide.DefaultStyle = ""
	deck.Masters.Layouts[0].Slide.Elements = []Element{
		{ID: "master-title", Kind: "heading", Level: 1, Text: "Title", Query: "top=3&left=4"},
		{ID: "master-body", Kind: "bullet", Text: "One\nTwo", Query: "top=15&left=4"},
		{ID: "master-shape", Kind: "shape", Query: "shape=square&top=28&left=4&width=20&height=8"},
	}
	s := newNativeEditorSession("Masters.md", deck)
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	s.currentMaster = 1
	s.selection = map[int]bool{0: true, 1: true, 2: true}
	s.selected = 1
	before := cloneDeck(s.deck)
	font, line, align, revision := "mono", 1.6, "center", s.version
	action := nativeEditorAction{
		Action:           "set-modern-text-style",
		SceneMaster:      true,
		SceneRevision:    &revision,
		Slide:            1,
		ObjectIDs:        []string{"master-title", "master-body", "master-shape"},
		ModernFont:       &font,
		ModernLineHeight: &line,
		TextAlign:        &align,
	}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	target := s.deck.Masters.Layouts[0].Slide
	for _, index := range []int{0, 1} {
		q, _ := url.ParseQuery(target.Elements[index].Query)
		if q.Get("modern-font") != "mono" || q.Get("modern-line-height") != "1.6" || q.Get("text-align") != "center" || q.Get("text-box") != "1" {
			t.Fatalf("master text %d did not receive inherited Modern typography: %v", index, q)
		}
		if q.Has("element-style") {
			t.Fatalf("master text %d materialised an inherited style: %v", index, q)
		}
	}
	if !reflect.DeepEqual(target.Elements[2], before.Masters.Layouts[0].Slide.Elements[2]) {
		t.Fatal("incompatible master shape received text formatting")
	}
	if len(s.undo) != 1 {
		t.Fatalf("master batch created %d undo records, want one", len(s.undo))
	}
	data, err := serializeDeck("Masters.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Masters.md", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, 1} {
		q, _ := url.ParseQuery(reopened.Masters.Layouts[0].Slide.Elements[index].Query)
		if q.Get("modern-font") != "mono" || q.Get("modern-line-height") != "1.6" || q.Get("text-align") != "center" {
			t.Fatalf("reopened master text %d lost typography: %v", index, q)
		}
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("master typography undo did not restore the exact deck")
	}
}

func TestMixedTextSizeCommand(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Elements = []Element{
		{ID: "modern", Kind: "text", Text: "Modern", Query: "element-style=modern&modern-size=73.5&ttf-size=97&group=g"},
		{ID: "retro", Kind: "text", Text: "Retro", Query: "element-style=retro&render=truetype&modern-size=30&ttf-size=60&group=g"},
		{ID: "glyph", Kind: "text", Text: "Glyph", Query: "element-style=retro&render=text-image&source=bitmap&text-size=10&scale=2&group=g"},
		{ID: "shape", Kind: "shape", Query: "shape=square&group=g"},
	}
	s := newNativeEditorSession("MixedSizes.md", deck)
	before := cloneDeck(s.deck)
	delta, revision := 1.0, s.version
	action := nativeEditorAction{Action: "set-text-size", ObjectIDs: []string{"modern"}, TextSizeDelta: &delta, SceneRevision: &revision}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	queries := []url.Values{}
	for _, element := range s.deck.Slides[0].Elements {
		q, _ := url.ParseQuery(element.Query)
		queries = append(queries, q)
	}
	if queries[0].Get("modern-size") != "74.5" || queries[0].Get("ttf-size") != "97" {
		t.Fatal("Modern profile sizing", queries[0])
	}
	if queries[1].Get("ttf-size") != "60" || queries[1].Get("modern-size") != "31" {
		t.Fatal("C64 font sizing", queries[1])
	}
	if s.deck.modernSceneText(s.deck.Slides[0].Elements[2]).Size != s.deck.modernSceneText(before.Slides[0].Elements[2]).Size+1 {
		t.Fatal("upgraded legacy text increment lost")
	}
	if !reflect.DeepEqual(before.Slides[0].Elements[3], s.deck.Slides[0].Elements[3]) {
		t.Fatal("non-text group member changed")
	}
	if len(s.undo) != 1 {
		t.Fatal("batch did not create one undo")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("batch undo changed document")
	}
	// The compatibility adapter also handles old/custom glyph elements before
	// the editor's automatic standardization into TrueType text.
	legacy := cloneDeck(deck)
	if _, err := applyTextSizeCommand(&legacy, action, ""); err != nil {
		t.Fatal(err)
	}
	if textSize(legacy.Slides[0].Elements[2]) != 11 {
		t.Fatal("legacy glyph size increment lost")
	}
}

func TestMixedTextWidthTransaction(t *testing.T) {
	deck := sceneFixture(t)
	deck.Slides[0].Engagement, deck.Slides[0].EngagementResult = nil, nil
	deck.Slides[0].Elements = []Element{
		{ID: "m", Kind: "text", Text: "Modern", Query: "render=truetype&element-style=modern&modern-width=120&ttf-width=60&width=80&height=20&group=g"},
		{ID: "r", Kind: "text", Text: "Retro", Query: "render=truetype&element-style=retro&modern-width=110&ttf-width=70&width=60&height=10&group=g"},
	}
	s := newNativeEditorSession("Widths.md", deck)
	before := cloneDeck(s.deck)
	delta, revision := 1.0, s.version
	action := nativeEditorAction{Action: "set-text-width", ObjectIDs: []string{"m"}, TextWidthDelta: &delta, SceneRevision: &revision}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	for i, expected := range [][2]string{{"121", "60"}, {"111", "70"}} {
		q, _ := url.ParseQuery(s.deck.Slides[0].Elements[i].Query)
		old, _ := url.ParseQuery(before.Slides[0].Elements[i].Query)
		if q.Get("modern-width") != expected[0] || q.Get("ttf-width") != expected[1] {
			t.Fatal(q)
		}
		if q.Get("width") != old.Get("width") || q.Get("height") != old.Get("height") {
			t.Fatal("box changed")
		}
	}
	if err := s.apply(action); err == nil {
		t.Fatal("accepted stale width change")
	}
	path := t.TempDir() + "/widths.md"
	encoded, err := serializeDeck(path, s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(path, encoded)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range loaded.Slides[0].Elements {
		if e.Text == "Modern" {
			found = true
			if sceneTextFor(e).WidthScale != 1.21 {
				t.Fatal("lost modern width")
			}
		}
	}
	if !found {
		t.Fatal("missing text")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("width undo not exact")
	}
}

func TestBoldBatchPreservesRichFormatting(t *testing.T) {
	text := "[color=#55ff55]First[/color] second"
	runs := []sceneRun{{Text: "First", Color: "#55ff55", Italic: true}, {Text: " second", Bold: true, Underline: true}}
	encoded, err := encodeModernRuns(text, "", "text", runs)
	if err != nil {
		t.Fatal(err)
	}
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{ID: "m", Kind: "text", Text: text, Query: setQueryValue("render=truetype&element-style=modern&group=g&width=70&height=12", "modern-runs", encoded)},
		{ID: "r", Kind: "text", Text: text, Query: setQueryValue("render=truetype&element-style=retro&group=g", "modern-runs", encoded)},
		{ID: "s", Kind: "shape", Query: "shape=square&group=g"},
	}}}}
	s := newNativeEditorSession("Bold.md", deck)
	before := cloneDeck(s.deck)
	bold, revision := true, s.version
	action := nativeEditorAction{Action: "set-modern-text-style", ObjectIDs: []string{"m"}, TextBold: &bold, SceneRevision: &revision}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		e := s.deck.Slides[0].Elements[i]
		if e.Text != text {
			t.Fatal("rewrote source")
		}
		got := exportedRichRuns(e)
		if len(got) != 2 || !got[0].Bold || !got[1].Bold || !got[0].Italic || !got[1].Underline || got[0].Color != "#55ff55" {
			t.Fatalf("lost rich formatting: %+v", got)
		}
	}
	if !reflect.DeepEqual(before.Slides[0].Elements[2], s.deck.Slides[0].Elements[2]) {
		t.Fatal("formatted shape body")
	}
	if err := s.apply(action); err == nil {
		t.Fatal("accepted stale bold command")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("bold undo not exact")
	}
	deck = cloneDeck(before)
	deck.Slides[0].Elements[1].Query += "&object-locked=1"
	locked := cloneDeck(deck)
	if _, err := applyModernTextStyle(&deck, action, ""); err == nil {
		t.Fatal("accepted locked member")
	}
	if !reflect.DeepEqual(locked, deck) {
		t.Fatal("partial bold mutation")
	}
	bold = false
	deck = cloneDeck(before)
	if _, err := applyModernTextStyle(&deck, action, ""); err != nil {
		t.Fatal(err)
	}
	for _, e := range deck.Slides[0].Elements[:2] {
		got := exportedRichRuns(e)
		if len(got) != 2 || got[0].Bold || got[1].Bold || !got[0].Italic || !got[1].Underline {
			t.Fatalf("unbold lost styles: %+v", got)
		}
	}
}

func TestInspectorItalicUnderline(t *testing.T) {
	for _, kind := range []string{"text", "bullet", "code"} {
		for _, style := range []string{"retro", "modern"} {
			t.Run(kind+"/"+style, func(t *testing.T) {
				text := "[color=#55ff55]First[/color]\n  continuation\nSecond"
				deck := Deck{Slides: []Slide{{Elements: []Element{{ID: "text", Kind: kind, Text: text, Query: "render=truetype&ttf-weight=bold&element-style=" + style}}}}}
				on, off := true, false
				action := nativeEditorAction{ObjectIDs: []string{"text"}, TextItalic: &on, TextUnderline: &on}
				if changed, err := applyModernTextStyle(&deck, action, ""); err != nil || !changed {
					t.Fatalf("format: %v %v", changed, err)
				}
				e := deck.Slides[0].Elements[0]
				if e.Text != text {
					t.Fatal("rewrote multiline source")
				}
				runs := exportedRichRuns(e)
				if len(runs) == 0 {
					t.Fatal("missing run styles")
				}
				for _, run := range runs {
					if !run.Italic || !run.Underline || !run.Bold {
						t.Fatalf("missing emphasis: %+v", run)
					}
				}
				path := t.TempDir() + "/emphasis.md"
				data, err := serializeDeck(path, deck)
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := parseDeckData(path, data)
				if err != nil {
					t.Fatal(err)
				}
				if got := exportedRichRuns(loaded.Slides[0].Elements[0]); !reflect.DeepEqual(got, runs) {
					t.Fatalf("lost saved emphasis: %+v", got)
				}
				action.TextItalic = &off
				action.TextUnderline = &off
				if _, err := applyModernTextStyle(&deck, action, ""); err != nil {
					t.Fatal(err)
				}
				q, _ := url.ParseQuery(deck.Slides[0].Elements[0].Query)
				if q.Has("modern-runs") || q.Get("ttf-weight") != "bold" {
					t.Fatal("did not clean redundant runs / retain bold")
				}
			})
		}
	}
}

func TestInspectorParagraphBatch(t *testing.T) {
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{ID: "m", Kind: "bullet", Text: "One\nTwo", Query: "element-style=modern&render=truetype&group=g&width=90&height=20"},
		{ID: "r", Kind: "text", Text: "Retro", Query: "element-style=retro&render=truetype&group=g"},
	}}}}
	s := newNativeEditorSession("Paragraphs.md", deck)
	before := cloneDeck(s.deck)
	line, beforeSpace, afterSpace, revision := 1.6, .3, .7, s.version
	action := nativeEditorAction{Action: "set-modern-text-style", ObjectIDs: []string{"m"}, SceneRevision: &revision, ModernLineHeight: &line, ModernParagraphBefore: &beforeSpace, ModernParagraphAfter: &afterSpace}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	got := sceneTextFor(s.deck.Slides[0].Elements[0])
	if got.LineHeight != 1.6 || got.ParagraphBefore != .3 || got.ParagraphAfter != .7 {
		t.Fatalf("spacing: %+v", got)
	}
	second := sceneTextFor(s.deck.Slides[0].Elements[1])
	if second.LineHeight != 1.6 || second.ParagraphBefore != .3 || second.ParagraphAfter != .7 {
		t.Fatal("unified typography did not update the grouped C64 member")
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), -.1, .2, 4.1} {
		draft := cloneDeck(s.deck)
		original := cloneDeck(draft)
		action.ModernLineHeight = &value
		if _, err := applyModernTextStyle(&draft, action, ""); err == nil {
			t.Fatal("accepted invalid spacing")
		}
		if !reflect.DeepEqual(draft, original) {
			t.Fatal("invalid spacing mutated document")
		}
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.deck, before) {
		t.Fatal("spacing undo not exact")
	}
	zero := 0.0
	action.ModernLineHeight = &zero
	action.ModernParagraphBefore = &zero
	action.ModernParagraphAfter = &zero
	deck.Slides[0].Elements[0].Query += "&modern-line-height=2&modern-paragraph-after=1"
	if _, err := applyModernTextStyle(&deck, action, ""); err != nil {
		t.Fatal(err)
	}
	got = sceneTextFor(deck.Slides[0].Elements[0])
	if got.LineHeight != 1.25 || got.ParagraphAfter != 0 {
		t.Fatal("clear did not restore defaults")
	}
}

func TestLabelPaintTransaction(t *testing.T) {
	for _, treatment := range []string{"", "blocks", "braille", "ascii", "dense"} {
		if _, err := labelPaintQuery("render=truetype", map[string]string{"glyph": treatment}); err == nil {
			t.Fatalf("retired label glyph treatment accepted: %q", treatment)
		}
	}
	raw, _ := json.Marshal(shapeLabelData{Text: "[color=#55ff55]Keep[/color]", Query: "render=truetype&ttf-size=65&modern-size=42&extension=keep"})
	owner := Element{ID: "shape", Kind: "shape", Query: setQueryValue("shape=square&width=80&height=20&fg=%23ff0000", "shape-label", base64.StdEncoding.EncodeToString(raw))}
	s := newNativeEditorSession("Paint.md", Deck{Slides: []Slide{{Elements: []Element{owner}}}})
	before := cloneDeck(s.deck)
	revision := s.version
	action := nativeEditorAction{Action: "set-modern-text-style", Kind: "label", ObjectIDs: []string{"shape"}, SceneRevision: &revision, LabelPaint: map[string]string{"gradient-dir": "vertical"}}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	_, data, err := sceneEditableShapeLabel(s.deck.Slides[0].Elements[0])
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(data.Query)
	if q.Get("gradient-dir") != "vertical" || q.Get("gradient-start") != "#ffffff" || q.Get("gradient-end") != "#55aaff" || q.Get("extension") != "keep" {
		t.Fatal(q)
	}
	if data.Text != "[color=#55ff55]Keep[/color]" {
		t.Fatal("changed label text")
	}
	body, _ := url.ParseQuery(s.deck.Slides[0].Elements[0].Query)
	original, _ := url.ParseQuery(before.Slides[0].Elements[0].Query)
	body.Del("shape-label")
	original.Del("shape-label")
	if body.Encode() != original.Encode() {
		t.Fatal("changed shape body")
	}
	if err := s.apply(action); err == nil {
		t.Fatal("accepted stale paint")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("paint undo not exact")
	}
	for _, fields := range []map[string]string{{"fg": "javascript:bad"}, {"shape": "circle"}, {"gradient-dir": "sideways"}, {"shadow": "invalid"}, {"transparent": "2"}, {"glyph": "invalid"}, {"fg": "#ffffff", "width": "40"}} {
		deck := cloneDeck(before)
		action.LabelPaint = fields
		if _, err := applyModernTextStyle(&deck, action, ""); err == nil {
			t.Fatalf("accepted %+v", fields)
		}
		if !reflect.DeepEqual(before, deck) {
			t.Fatal("invalid paint changed document")
		}
	}
	query, err := labelPaintQuery(q.Encode(), map[string]string{"gradient-dir": ""})
	if err != nil {
		t.Fatal(err)
	}
	q, _ = url.ParseQuery(query)
	if q.Has("gradient-dir") || q.Has("gradient-start") || q.Has("gradient-end") {
		t.Fatal("partial gradient removal")
	}
}

func TestLabelOpacityValidation(t *testing.T) {
	for _, value := range []string{"0", "0.25", "1", ""} {
		query, err := labelPaintQuery("transparent=1&fg=%23aabbcc", map[string]string{"modern-opacity": value})
		if err != nil {
			t.Fatal(err)
		}
		q, _ := url.ParseQuery(query)
		if q.Get("modern-opacity") != value || q.Get("transparent") != "1" || q.Get("fg") != "#aabbcc" {
			t.Fatal(query)
		}
	}
	for _, value := range []string{"NaN", "Inf", "-0.1", "1.1", "oops"} {
		if _, err := labelPaintQuery("", map[string]string{"modern-opacity": value}); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestTextAlignmentKeepsBoxes(t *testing.T) {
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{ID: "m", Kind: "text", Text: "Modern", Query: "render=truetype&element-style=modern&top=3&left=8&width=80&height=14&group=g"},
		{ID: "r", Kind: "text", Text: "Retro", Query: "render=truetype&element-style=retro&top=25&left=12&group=g"},
		{ID: "shape", Kind: "shape", Query: "shape=square&width=10&height=5&left=90&top=2&group=g"},
	}}}}
	s := newNativeEditorSession("Alignment.md", deck)
	before := cloneDeck(s.deck)
	cols, rows := authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
	beforeScene, err := buildMixedSlideScene(s.deck, 0, cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	horizontal, vertical, revision := "justify", "middle", s.version
	action := nativeEditorAction{Action: "set-modern-text-style", ObjectIDs: []string{"m"}, TextAlign: &horizontal, TextVertical: &vertical, SceneRevision: &revision}
	if err := s.apply(action); err != nil {
		t.Fatal(err)
	}
	afterScene, err := buildMixedSlideScene(s.deck, 0, cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	for i, object := range afterScene.Objects {
		if object.Bounds != beforeScene.Objects[i].Bounds {
			t.Fatalf("alignment moved/resized %s: %+v -> %+v", object.ID, beforeScene.Objects[i].Bounds, object.Bounds)
		}
	}
	for _, e := range s.deck.Slides[0].Elements[:2] {
		q, _ := url.ParseQuery(e.Query)
		if q.Get("text-align") != "justify" || q.Get("text-valign") != "middle" {
			t.Fatal(q)
		}
	}
	if !reflect.DeepEqual(before.Slides[0].Elements[2], s.deck.Slides[0].Elements[2]) {
		t.Fatal("aligned shape")
	}
	if err := s.apply(action); err == nil {
		t.Fatal("accepted stale alignment")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, s.deck) {
		t.Fatal("alignment undo not exact")
	}
	invalid := "sideways"
	action.TextAlign = &invalid
	if _, err := applyModernTextStyle(&deck, action, ""); err == nil {
		t.Fatal("accepted invalid alignment")
	}
}
