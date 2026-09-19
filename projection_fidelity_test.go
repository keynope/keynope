package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sceneObjectsByID(scene slideScene) map[string]sceneObject {
	objects := make(map[string]sceneObject, len(scene.Objects))
	for _, object := range scene.Objects {
		objects[object.ID] = object
	}
	return objects
}

func scenePlainText(object sceneObject) string {
	if object.Text == nil {
		return ""
	}
	var text strings.Builder
	for _, run := range object.Text.Runs {
		text.WriteString(run.Text)
	}
	for _, paragraph := range object.Text.Paragraphs {
		for _, run := range paragraph.Runs {
			text.WriteString(run.Text)
		}
	}
	return text.String()
}

// This fixture deliberately crosses the document boundaries that used to be
// tested separately: inherited masters, tab-only pages, activity access
// artwork, private workshop state, source media and an unavailable reference.
// Every output surface must consume the same resolved scene while applying its
// own explicit privacy projection.
func TestRepresentativeDeckProjectionFidelityAndPrivacy(t *testing.T) {
	deck := sceneFixture(t)
	deck.EnsureDefaultMasters()
	deck.Masters.Base.Slide.Elements = append(deck.Masters.Base.Slide.Elements,
		Element{ID: "master-brand", Kind: "text", Text: "Inherited public brand", Query: "top=52&left=4&render=truetype"},
	)
	deck.Masters.Layouts = append(deck.Masters.Layouts, MasterLayout{
		ID:   "mixed-fidelity",
		Name: "Mixed fidelity",
		Slide: Slide{Elements: []Element{
			{ID: "master-title", SlotID: "title-slot", PlaceholderRole: placeholderTitle, Placeholder: true, Kind: "heading", Level: 1, Text: "Master title", Query: "top=1&left=5&render=truetype"},
		}},
	})
	deck.Slides[0].LayoutID = "mixed-fidelity"
	deck.Slides[0].Elements = append(deck.Slides[0].Elements,
		Element{ID: "bound-title", MasterSlotID: "title-slot", Kind: "heading", Level: 1, Text: "Resolved authored title"},
		Element{ID: "missing-image", Kind: "image", Path: "https://invalid.example/missing.png", Query: "top=42&left=100&width=30&height=8"},
	)
	deck.Slides[0].Engagement = &EngagementDefinition{ID: "private-activity", Kind: "storm", Prompt: "VISIBLE ACTIVITY PROMPT", Code: "PRIVATE_SESSION_CODE"}
	deck.Slides[0].EngagementResult = &EngagementResult{Version: 1, State: map[string]json.RawMessage{"answers": json.RawMessage(`{"SECRET_PARTICIPANT":"SECRET_ANSWER"}`)}}
	deck.Slides[0].Notes = "SECRET_SPEAKER_NOTES"
	deck.Slides[0].Elements = append(deck.Slides[0].Elements,
		Element{ID: "activity-qr", Kind: "code", Text: "PRIVATE_QR_PIXELS", PlaceholderRole: activityQRCodeRole},
		Element{ID: "activity-url", Kind: "text", Text: "PRIVATE_JOIN_URL", PlaceholderRole: activityURLRole},
	)

	tabSlide := cloneSlide(deck.Slides[0])
	tabSlide.LayoutID = ""
	tabSlide.TabID = "reference-tab"
	tabSlide.Notes, tabSlide.Engagement, tabSlide.EngagementResult = "", nil, nil
	tabSlide.Elements = []Element{{ID: "tab-copy", Kind: "text", Text: "Reference tab content", Query: "render=truetype"}}
	deck.Slides = append(deck.Slides, tabSlide)
	deck.Tabs = []DeckTab{{ID: "reference-tab", Name: "Reference", Page: 2, SlideTab: true}}

	before, err := json.Marshal(deck)
	if err != nil {
		t.Fatal(err)
	}
	local, err := buildDocumentSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	objects := sceneObjectsByID(local)
	if scenePlainText(objects["master-brand"]) != "Inherited public brand" || scenePlainText(objects["bound-title"]) != "Resolved authored title" {
		t.Fatal("resolved master or bound placeholder content was lost")
	}
	if objects["image"].Media == nil {
		t.Fatal("source-quality embedded image was not retained")
	}
	if scenePlainText(objects["missing-image"]) != "[IMG]" {
		t.Fatalf("unavailable reference did not degrade to [IMG]: %+v", objects["missing-image"])
	}

	participant, err := participantRenderedDeck(deck, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(participant.Pages) != 1 || participant.Pages[0].Scene == nil || !reflect.DeepEqual(*participant.Pages[0].Scene, local) {
		t.Fatal("participant projection diverged from the resolved local scene")
	}

	resolved := deck.ResolvedSlides()
	_, presenterPages, err := projectPresenterDocument(filepath.Join(t.TempDir(), "Representative.md"), resolved, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(presenterPages[0]) != 1 || presenterPages[0][0].Scene == nil || !reflect.DeepEqual(*presenterPages[0][0].Scene, local) {
		t.Fatal("presenter projection diverged from the resolved local scene")
	}
	if len(presenterPages[1]) != 1 || !presenterPages[1][0].TabOnly {
		t.Fatal("tab-only slide lost its presentation exclusion marker")
	}

	standalone, err := exportHTMLDocument("Representative.md", resolved, 245, 56, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, public := range []string{"Inherited public brand", "Resolved authored title", "Reference tab content", "[IMG]"} {
		if !strings.Contains(standalone, public) {
			t.Fatalf("standalone export lost public content %q", public)
		}
	}
	for _, private := range []string{"SECRET_SPEAKER_NOTES", "SECRET_PARTICIPANT", "SECRET_ANSWER", "PRIVATE_SESSION_CODE", "PRIVATE_QR_PIXELS", "PRIVATE_JOIN_URL"} {
		if strings.Contains(standalone, private) {
			t.Fatalf("standalone export leaked %q", private)
		}
	}
	participantJSON, err := json.Marshal(participant)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"SECRET_SPEAKER_NOTES", "SECRET_PARTICIPANT", "SECRET_ANSWER", "PRIVATE_SESSION_CODE"} {
		if strings.Contains(string(participantJSON), private) {
			t.Fatalf("participant projection leaked %q", private)
		}
	}
	after, err := json.Marshal(deck)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("cross-surface projection mutated the authored deck")
	}
}
