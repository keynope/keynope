package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestEngagementMetadataRoundTrip(t *testing.T) {
	definition := EngagementDefinition{
		Kind:   "sort",
		Prompt: "Where should these ideas go?",
		Zones:  []string{"Now", "Next", "Later"},
		Cards:  []string{"Ship it", "Test it", "Discuss it"},
	}
	metadata, err := encodeEngagementMetadata(&definition)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeEngagementMetadata(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID == "" || decoded.Code == "" || !activityCodeRE.MatchString(decoded.Code) {
		t.Fatalf("decoded engagement has no valid identity: %#v", decoded)
	}
	decoded.ID, decoded.Code = "", ""
	if !reflect.DeepEqual(decoded, &definition) {
		t.Fatalf("decoded engagement = %#v, want %#v", decoded, definition)
	}
}

func TestDeckSerializesEngagementWithoutRuntimeAnswers(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 80, 25
	t.Cleanup(func() { authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight })

	deck := Deck{Slides: []Slide{{
		Elements:   []Element{{Kind: "heading", Level: 1, Text: "Confidence"}},
		Engagement: &EngagementDefinition{Kind: "pulse", Prompt: "How ready are we?", Options: []string{"0", "1", "2", "3", "4", "5"}},
	}}}
	data, err := serializeDeck("Untitled.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("keynope-engagement version=1")) {
		t.Fatalf("serialized deck has no engagement metadata:\n%s", data)
	}
	parsed, err := parseDeckData("Untitled.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Slides) != 1 || parsed.Slides[0].Engagement == nil {
		t.Fatalf("parsed engagement missing: %#v", parsed.Slides)
	}
	if got := parsed.Slides[0].Engagement; got.Kind != "pulse" || got.Prompt != "How ready are we?" || len(got.Options) != 6 {
		t.Fatalf("parsed engagement = %#v", got)
	}
}

func TestNativeEditorEngagementUndo(t *testing.T) {
	session := newNativeEditorSession("Untitled.md", Deck{Slides: []Slide{{}}}, true)
	definition := EngagementDefinition{Kind: "storm", Prompt: "What are we missing?"}
	if err := session.apply(nativeEditorAction{Action: "set-engagement", EngagementData: &definition}); err != nil {
		t.Fatal(err)
	}
	if got := session.state().Slides[0].Engagement; got == nil || got.Kind != "storm" {
		t.Fatalf("engagement was not applied: %#v", got)
	}
	if err := session.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if got := session.state().Slides[0].Engagement; got != nil {
		t.Fatalf("undo retained engagement: %#v", got)
	}
}

func TestNativeEditorAddsActivitySlideAfterCurrent(t *testing.T) {
	session := newNativeEditorSession("Untitled.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Before"}}}, {Elements: []Element{{Kind: "text", Text: "After"}}}}}, true)
	definition := EngagementDefinition{Kind: "pulse", Prompt: "How are we?", Options: []string{"Good", "Great"}}
	if err := session.apply(nativeEditorAction{Action: "add-engagement-slide", EngagementData: &definition}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	if state.Current != 1 || len(state.Slides) != 3 {
		t.Fatalf("activity insertion state = current %d, %d slides", state.Current, len(state.Slides))
	}
	activity := state.Slides[1]
	if activity.LayoutID != activityLayoutID || activity.Engagement == nil || activity.Engagement.Code == "" || activity.Engagement.ID == "" {
		t.Fatalf("inserted activity slide = %#v", activity)
	}
	if got := state.Slides[2].Elements[0].Text; got != "After" {
		t.Fatalf("following slide moved incorrectly: %q", got)
	}
	resolved := state.Resolved[1]
	roles := map[string]Element{}
	for _, element := range resolved.Elements {
		roles[element.PlaceholderRole] = element
	}
	if roles[activityTitleRole].Text != definition.Prompt || roles[activityURLRole].Text != activityJoinURL(activity.Engagement) || !strings.Contains(roles[activityQRCodeRole].Text, "█") {
		t.Fatalf("resolved activity elements = %#v", roles)
	}
}

func TestActivityMasterRequiredElementsCannotBeDeleted(t *testing.T) {
	session := newNativeEditorSession("Untitled.md", Deck{Slides: []Slide{{}}}, true)
	if err := session.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	state := session.state()
	activityIndex := -1
	for index, layout := range state.Masters.Layouts {
		if layout.ID == activityLayoutID {
			activityIndex = index + 1
		}
	}
	if activityIndex < 1 {
		t.Fatal("activity master missing")
	}
	if err := session.apply(nativeEditorAction{Action: "select-slide", Slide: activityIndex}); err != nil {
		t.Fatal(err)
	}
	if err := session.apply(nativeEditorAction{Action: "delete-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	state = session.state()
	if got := len(state.Masters.Layouts[activityIndex-1].Slide.Elements); got != 3 {
		t.Fatalf("activity master has %d elements after deleting protected title", got)
	}
}

func TestNormalizeEngagementRejectsIncompleteSort(t *testing.T) {
	_, err := normalizeEngagement(EngagementDefinition{Kind: "sort", Zones: []string{"Only"}, Cards: []string{"Card"}})
	if err == nil {
		t.Fatal("expected incomplete sort to be rejected")
	}
}

func TestResolvedLayoutSlideCarriesEngagement(t *testing.T) {
	deck := Deck{
		Slides:  []Slide{{LayoutID: "blank", Engagement: &EngagementDefinition{Kind: "storm", Prompt: "Ideas?"}}},
		Masters: defaultMasterDeck(),
	}
	resolved := deck.ResolveSlide(0, false)
	if resolved.Engagement == nil || resolved.Engagement.Prompt != "Ideas?" {
		t.Fatalf("resolved slide lost engagement: %#v", resolved.Engagement)
	}
	resolved.Engagement.Prompt = "Changed"
	if deck.Slides[0].Engagement.Prompt != "Ideas?" {
		t.Fatal("resolved slide shares engagement storage with authored slide")
	}
}

func TestPresenterEngagementStateIsTransientAndSynchronized(t *testing.T) {
	companion := &presenterCompanion{}
	runtime := EngagementRuntimeState{
		Definition: EngagementDefinition{Kind: "pulse", Prompt: "Ready?", Options: []string{"No", "Yes"}},
		Slide:      2,
		Phase:      3,
		Counts:     []int{1, 4},
	}
	payload, err := json.Marshal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/engagement", bytes.NewReader(payload))
	response := httptest.NewRecorder()
	companion.handleEngagement(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if companion.state.Engagement == nil || companion.state.Engagement.Counts[1] != 4 || companion.state.Version != 1 {
		t.Fatalf("presenter state = %#v", companion.state)
	}

	clearRequest := httptest.NewRequest(http.MethodPost, "/engagement", bytes.NewBufferString("null"))
	clearResponse := httptest.NewRecorder()
	companion.handleEngagement(clearResponse, clearRequest)
	if clearResponse.Code != http.StatusNoContent || companion.state.Engagement != nil {
		t.Fatalf("engagement was not cleared: status=%d state=%#v", clearResponse.Code, companion.state.Engagement)
	}
}
