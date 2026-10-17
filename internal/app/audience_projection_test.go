package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func checkAudiencePage(t *testing.T, page exportPage) {
	t.Helper()
	if len(page.ShapePorts) != 0 || len(page.Connectors) != 0 {
		t.Errorf("audience page contains editor attachment geometry: %d ports, %d routes", len(page.ShapePorts), len(page.Connectors))
	}
	if page.Engagement != nil {
		t.Error("audience page contains private activity definition")
	}
	if page.Scene == nil {
		return
	}
	if page.Scene.Revision != nil || page.Scene.Conversion != nil || page.Scene.Master {
		t.Error("audience scene contains editor ownership/report fields")
	}
	for _, object := range page.Scene.Objects {
		if object.Editable || len(object.EditRuns) > 0 || len(object.TextCapabilities) > 0 {
			t.Error("audience object contains editing capabilities/source", object.ID)
		}
	}
}

func TestAudienceProjectionDropsEditorStateWithoutChangingArtwork(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, inherited := range []bool{false, true} {
		t.Run(fmt.Sprint(inherited), func(t *testing.T) {
			d := participantHiddenFlowFixture(inherited)
			d.Slides[0].Elements = append(d.Slides[0].Elements,
				Element{ID: "list", Kind: "bullet", Text: "One\n  Two", Query: "render=truetype&modern-font=sans&top=35&left=5"},
				Element{ID: "public-link", Kind: "text", Text: "Public website", Query: "render=truetype&top=49&left=5&link=https%3A%2F%2Fkeynope.sh"})
			d = newNativeEditorSession("Audience.md", d).deck
			before := cloneDeck(d)
			resolved := d.ResolvedSlides()
			editor := projectExportDeck("Audience.md", resolved, 245, 56, true)
			if len(editor.Pages[0].ShapePorts) == 0 {
				t.Fatal("fixture has no editor attachment records")
			}
			participant, err := participantRenderedDeck(d, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, pdf, err := projectPDFExport("Audience.md", d, 245, 56)
			if err != nil {
				t.Fatal(err)
			}
			md, err := participantSlideMarkdown(d, 0)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeckData("Presentation.md", md)
			if err != nil {
				t.Fatal(err)
			}
			restoreParticipantLayers(&loaded.Slides[0])
			for name, pages := range map[string][]exportPage{
				"standalone":       projectExportDeck("Audience.md", resolved, 245, 56, false).Pages,
				"participant":      participant.Pages,
				"pdf-input":        pdf.Pages,
				"markdown-adapter": participantMarkdownExportPages(loaded, 0, 1, 245, 56),
			} {
				t.Run(name, func(t *testing.T) {
					if len(pages) != 1 {
						t.Fatal("page count", len(pages))
					}
					page := pages[0]
					checkAudiencePage(t, page)
					raw, _ := json.Marshal(page)
					for _, private := range []string{"hidden-shape", "hidden-line", "AUDIENCE_PRIVATE", "textCapabilities", "editRuns", "editable", "shapePorts", "connectors"} {
						if bytes.Contains(raw, []byte(private)) {
							t.Error("private/editor field reached public wire", private)
						}
					}
					want, actual := publicParityScene(*editor.Pages[0].Scene), publicParityScene(*page.Scene)
					if !reflect.DeepEqual(want, actual) {
						t.Error("audience filter changed public artwork")
					}
					objects := sceneObjectsByID(*page.Scene)
					if objects["public-link"].Link != "https://keynope.sh" || objects["list"].Text == nil {
						t.Error("audience navigation or semantic list content was removed")
					}
				})
			}
			if !reflect.DeepEqual(before, d) {
				t.Fatal("audience projection changed authored document")
			}
			// Preserve the editor's borrowed cache and attachment records.
			if len(editor.Pages[0].ShapePorts) == 0 || len(sceneObjectsByID(*editor.Pages[0].Scene)["list"].TextCapabilities) == 0 {
				t.Fatal("audience projection mutated the editor projection")
			}
		})
	}
}

func TestAudienceProjectionEndpointAndSerializedOutputs(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := participantPrivacyFixture(t)
	s := newNativeEditorSession("Public.md", d)
	w := httptest.NewRecorder()
	s.handleParticipantPage(w, httptest.NewRequest(http.MethodGet, "/api/editor/participant-page?slide=0&format=rendered", nil))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Deck exportDeck `json:"rendered"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Deck.Pages) != 1 {
		t.Fatal("rendered participant HTTP contract", err, w.Code, len(response.Deck.Pages))
	}
	checkAudiencePage(t, response.Deck.Pages[0])
	html, err := exportHTMLDocument("Public.md", s.deck.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	payload := exportDataForTest(t, html)
	for _, page := range payload.Pages {
		checkAudiencePage(t, page)
	}
	// Check the JSON data, not the HTML's shared editor JavaScript vocabulary.
	raw, _ := json.Marshal(payload)
	if strings.Contains(string(raw), "AUDIENCE_PRIVATE") {
		t.Fatal("private nested formatting reached serialized standalone data")
	}
}

func exportDataForTest(t *testing.T, html string) exportDeck {
	t.Helper()
	_, rest, ok := strings.Cut(html, `<script id="keynope-data" type="application/json">`)
	payload, _, closed := strings.Cut(rest, "</script>")
	if !ok || !closed {
		t.Fatal("missing exported JSON document")
	}
	var deck exportDeck
	if err := json.Unmarshal([]byte(payload), &deck); err != nil {
		t.Fatal(err)
	}
	return deck
}

func TestAudienceProjectionGuardsDirectHTMLSinkAndOwnsEnvelopes(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	s := buildEditorFixture(false)
	if err := s.apply(buildAction(s, ownedBuild())); err != nil {
		t.Fatal(err)
	}
	scene, err := buildDocumentSlideScene(s.deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	revision := int64(123)
	scene.Revision, scene.Master = &revision, true
	scene.Conversion = &sceneConversionReport{Issues: []sceneConversionIssue{{Code: "private", Message: "AUDIENCE_PRIVATE_REPORT"}}}
	scene.Objects[0].Editable = true
	scene.Objects[0].EditRuns = []sceneRun{{Text: "AUDIENCE_PRIVATE_EDITOR_CONTINUATION"}}
	page := exportPage{Scene: &scene, ShapePorts: []shapePort{{ID: "AUDIENCE_PRIVATE_PORT"}}, Connectors: []shapeConnector{{ID: "AUDIENCE_PRIVATE_ROUTE"}}, Engagement: &EngagementDefinition{ID: "private", Kind: "impostor", Prompt: "AUDIENCE_PRIVATE_DEFINITION"}}
	input := exportDeck{Cols: 245, Rows: 56, Pages: []exportPage{page}}
	before, _ := json.Marshal(input)
	html, err := exportProjectedHTML(input, preservedExportHead{}, false)
	if err != nil {
		t.Fatal(err)
	}
	public := exportDataForTest(t, html)
	checkAudiencePage(t, public.Pages[0])
	raw, _ := json.Marshal(public)
	if bytes.Contains(raw, []byte("AUDIENCE_PRIVATE")) {
		t.Fatal("direct serialized HTML sink bypassed audience boundary")
	}
	actual, want := jsonForTest(t, publicParityScene(*public.Pages[0].Scene)), jsonForTest(t, publicParityScene(scene))
	if !bytes.Equal(actual, want) || public.Pages[0].Scene.Build == nil || len(public.Pages[0].Scene.Build.Steps) != 2 {
		t.Fatal("audience boundary changed artwork or progressive builds")
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("audience boundary mutated retained editor pages/cache")
	}
	first := audienceExportPage(page)
	second := audienceExportPage(first)
	if second.Scene != first.Scene {
		t.Fatal("already public scene incurred another envelope copy")
	}
	// Mutating the filtered envelopes cannot reach the editor's objects/slices.
	first.Scene.Objects[0].Name = "Changed public name"
	if scene.Objects[0].Name == "Changed public name" || len(scene.Objects[0].EditRuns) != 1 || !scene.Objects[0].Editable {
		t.Fatal("public envelope aliases mutable editor fields")
	}
}

func jsonForTest(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAudienceProjectionKeepsLegacyPaintAndAnimationRecords(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	// The raw legacy adapter has no typed scene. Its sampled lines, frames,
	// transparency and TrueType records remain the render source of truth.
	slide := participantHiddenFlowFixture(false).Slides[0]
	slide.Engagement = &EngagementDefinition{ID: "private", Kind: "storm", Prompt: "AUDIENCE_PRIVATE_DEFINITION"}
	pages := exportSlidePages(slide, 0, 1, 245, 56)
	if len(pages) != 1 || pages[0].Scene != nil || len(pages[0].Lines) == 0 || len(pages[0].ShapePorts) == 0 {
		t.Fatal("legacy fixture does not exercise attachment/paint sidecars")
	}
	// Exercise multi-frame records without adding an unrelated pixel codec.
	pages[0].ContentFrames = []exportContentFrame{{Lines: pages[0].Lines, DelayMS: 30}, {Lines: pages[0].Lines, DelayMS: 170}}
	before := jsonForTest(t, pages)
	input := exportDeck{Cols: 245, Rows: 56, Pages: pages}
	public := audienceExportDeck(input)
	for i, page := range public.Pages {
		checkAudiencePage(t, page)
		if !reflect.DeepEqual(page.Lines, pages[i].Lines) || !reflect.DeepEqual(page.ContentFrames, pages[i].ContentFrames) || !reflect.DeepEqual(page.Transparency, pages[i].Transparency) {
			t.Fatal("audience projection changed legacy artwork/animation")
		}
	}
	if !bytes.Equal(before, jsonForTest(t, pages)) {
		t.Fatal("audience projection changed original legacy pages")
	}
}
