package main

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
)

func extensionFixture(key, value string) *jsonExtensions {
	fields := jsonExtensions{key: json.RawMessage(value)}
	return &fields
}

func requireExtension(t *testing.T, fields *jsonExtensions, key, want string) {
	t.Helper()
	if fields == nil || string((*fields)[key]) != want {
		t.Fatalf("extension %q = %v, want %s", key, fields, want)
	}
}

func TestCompatibleMetadataSurvivesCommandsUndoAndSave(t *testing.T) {
	labelData := shapeLabelData{Text: "Label", Query: "render=truetype&ttf-size=97", Extra: extensionFixture("futureLabel", `{"curve":2}`)}
	labelJSON, err := json.Marshal(labelData)
	if err != nil {
		t.Fatal(err)
	}
	shapeQuery := url.Values{
		"shape": {"square"}, "top": {"4"}, "width": {"40"}, "height": {"10"},
		"shape-label":      {base64.StdEncoding.EncodeToString(labelJSON)},
		"future-placement": {`{"snap":"optical"}`},
	}.Encode()
	deck := Deck{
		Extra:      extensionFixture("futureDocument", `{"generation":7}`),
		Appearance: &DeckAppearance{Version: 2, DefaultStyle: "retro", Extra: map[string]json.RawMessage{"futureAppearance": json.RawMessage(`{"material":"phosphor"}`)}},
		Masters:    defaultMasterDeck(),
		Tabs:       []DeckTab{{ID: "guide", Name: "Guide", Page: 1, Extra: extensionFixture("futureTab", `{"icon":"book"}`)}},
		Slides: []Slide{{
			Extra:      extensionFixture("futureSlide", `{"transition":"later"}`),
			Engagement: &EngagementDefinition{ID: "activity", Code: "AbCd1234", Kind: "storm", Prompt: "Ideas?", Extra: extensionFixture("futureActivity", `{"moderation":"assisted"}`)},
			Elements: []Element{
				{ID: "shape", Kind: "shape", Query: shapeQuery, Extra: extensionFixture("futureElement", `{"constraint":"keep"}`)},
				{ID: "text", Kind: "text", Text: "Second", Query: "top=20&future-layout=%7B%22flow%22%3Atrue%7D", Extra: extensionFixture("futureText", `{"role":"caption"}`)},
			},
		}},
	}
	deck.Masters.Extra = extensionFixture("futureMasters", `{"schema":3}`)
	deck.Masters.Base.Extra = extensionFixture("futureLayout", `{"baseline":"cap"}`)
	deck.Masters.Base.Slide.Extra = extensionFixture("futureMasterSlide", `{"grid":12}`)
	deck.Masters.Base.Slide.Elements[0].Extra = extensionFixture("futureMasterElement", `{"token":"page"}`)

	s := newNativeEditorSession("future.md", deck)
	revision := s.version
	if err := s.apply(nativeEditorAction{Action: "set-scene-text", SceneRevision: &revision, ObjectID: "shape", TextRuns: []sceneRun{{Text: "Edited label"}}}); err != nil {
		t.Fatal(err)
	}
	shapeUpdate := s.deck.Slides[0].Elements[0]
	shapeUpdate.Extra = nil
	shapeUpdate.Query = setQueryValue(shapeUpdate.Query, "fg", "#55aaff")
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: 0, ElementData: &shapeUpdate}); err != nil {
		t.Fatal(err)
	}
	batch := append([]Element(nil), s.deck.Slides[0].Elements...)
	for index := range batch {
		batch[index].Extra = nil
	}
	batch[1].Text = "Updated second"
	if err := s.apply(nativeEditorAction{Action: "update-elements", ElementIndices: []int{0, 1}, ElementsData: batch}); err != nil {
		t.Fatal(err)
	}
	s.selection = map[int]bool{0: true, 1: true}
	if err := s.apply(nativeEditorAction{Action: "group-elements"}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "move-element", Element: 0, Kind: "front"}); err != nil {
		t.Fatal(err)
	}
	updated := cloneSlide(s.deck.Slides[0])
	updated.Background, updated.BackgroundSet = "aurora", true
	updated.Extra = nil // Browser slide settings only return fields they edit.
	if err := s.apply(nativeEditorAction{Action: "update-slide", SlideData: &updated}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "set-tabs", Tabs: []DeckTab{{ID: "guide", Name: "Updated guide", Page: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "set-engagement", EngagementData: &EngagementDefinition{ID: "activity", Code: "AbCd1234", Kind: "storm", Prompt: "Updated ideas?"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}

	data, err := serializeDeck("future.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("future.md", data)
	if err != nil {
		t.Fatal(err)
	}

	requireExtension(t, loaded.Extra, "futureDocument", `{"generation":7}`)
	if loaded.Appearance == nil || string(loaded.Appearance.Extra["futureAppearance"]) != `{"material":"phosphor"}` {
		t.Fatal("appearance extension lost")
	}
	requireExtension(t, loaded.Masters.Extra, "futureMasters", `{"schema":3}`)
	requireExtension(t, loaded.Masters.Base.Extra, "futureLayout", `{"baseline":"cap"}`)
	requireExtension(t, loaded.Masters.Base.Slide.Extra, "futureMasterSlide", `{"grid":12}`)
	requireExtension(t, loaded.Masters.Base.Slide.Elements[0].Extra, "futureMasterElement", `{"token":"page"}`)
	requireExtension(t, loaded.Slides[0].Extra, "futureSlide", `{"transition":"later"}`)
	requireExtension(t, loaded.Slides[0].Engagement.Extra, "futureActivity", `{"moderation":"assisted"}`)
	requireExtension(t, loaded.Tabs[0].Extra, "futureTab", `{"icon":"book"}`)

	byID := map[string]Element{}
	for _, element := range loaded.Slides[0].Elements {
		byID[element.ID] = element
	}
	requireExtension(t, byID["shape"].Extra, "futureElement", `{"constraint":"keep"}`)
	requireExtension(t, byID["text"].Extra, "futureText", `{"role":"caption"}`)
	shapeValues, _ := url.ParseQuery(byID["shape"].Query)
	if shapeValues.Get("future-placement") != `{"snap":"optical"}` {
		t.Fatal("unknown shape placement lost")
	}
	decodedLabel, _ := base64.StdEncoding.DecodeString(shapeValues.Get("shape-label"))
	var label shapeLabelData
	if json.Unmarshal(decodedLabel, &label) != nil {
		t.Fatal("shape label no longer decodes")
	}
	if label.Text != "Edited label" {
		t.Fatal("shape label edit did not persist")
	}
	requireExtension(t, label.Extra, "futureLabel", `{"curve":2}`)
	textValues, _ := url.ParseQuery(byID["text"].Query)
	if textValues.Get("future-layout") != `{"flow":true}` {
		t.Fatal("unknown text placement lost")
	}
}

func TestJSONExtensionCloneDoesNotAlias(t *testing.T) {
	original := Element{Kind: "text", Extra: extensionFixture("future", `{"value":1}`)}
	copy := cloneSlide(Slide{Elements: []Element{original}})
	(*copy.Elements[0].Extra)["future"] = json.RawMessage(`{"value":2}`)
	if reflect.DeepEqual(original.Extra, copy.Elements[0].Extra) {
		t.Fatal("element extension clone aliases source")
	}
}

func TestUnknownMasterJSONFieldsDecodeAndReencode(t *testing.T) {
	raw := []byte(`{"version":1,"futureMasters":{"schema":3},"base":{"id":"base","name":"Base","futureLayout":[1,2],"slide":{"futureSlide":true,"elements":[{"kind":"text","text":"Hello","futureElement":{"keep":"exact"}}]}},"layouts":[]}`)
	var masters MasterDeck
	if err := json.Unmarshal(raw, &masters); err != nil {
		t.Fatal(err)
	}
	requireExtension(t, masters.Extra, "futureMasters", `{"schema":3}`)
	requireExtension(t, masters.Base.Extra, "futureLayout", `[1,2]`)
	requireExtension(t, masters.Base.Slide.Extra, "futureSlide", `true`)
	requireExtension(t, masters.Base.Slide.Elements[0].Extra, "futureElement", `{"keep":"exact"}`)
	encoded, err := json.Marshal(masters)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(encoded, &fields) != nil || string(fields["futureMasters"]) != `{"schema":3}` {
		t.Fatal("master extension was not re-encoded")
	}
}
