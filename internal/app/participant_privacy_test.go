package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func audienceRunFixture(t *testing.T, text, fingerprintText string, extra bool) string {
	t.Helper()
	raw, err := json.Marshal(modernRunStyles{Version: 1, Source: modernRunFingerprint(fingerprintText, ""), Runs: []sceneRun{{Text: text, Color: "#ff0055", Italic: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if extra {
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		fields["private"] = "AUDIENCE_PRIVATE_RUN_ROOT"
		fields["runs"].([]any)[0].(map[string]any)["private"] = "AUDIENCE_PRIVATE_RUN_FIELD"
		raw, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func participantPrivacyFixture(t *testing.T) Deck {
	t.Helper()
	d := sceneFixture(t)
	d.Appearance = &DeckAppearance{Version: 3}
	d.Slides[0].ID = "public-slide"
	d.Slides[0].Engagement, d.Slides[0].EngagementResult = nil, nil
	q := mustQuery(d.Slides[0].Elements[2].Query)
	data, _ := decodeShapeLabelData(q.Get("shape-label"))
	data.Extra = extensionFixture("private", `"AUDIENCE_PRIVATE_LABEL_EXTENSION"`)
	data.Query = setQueryValue(data.Query, "future-private", "AUDIENCE_PRIVATE_LABEL_QUERY")
	data.Query = setQueryValue(data.Query, "modern-runs", audienceRunFixture(t, data.Text, data.Text, true))
	raw, _ := json.Marshal(data)
	q.Set("shape-label", base64.StdEncoding.EncodeToString(raw))
	q.Set("label-future-private", "AUDIENCE_PRIVATE_OWNER_QUERY")
	q.Set("future-private", "AUDIENCE_PRIVATE_QUERY")
	d.Slides[0].Elements[2].Query = q.Encode()
	d.Slides[0].Elements[0].Query = setQueryValue(d.Slides[0].Elements[0].Query, "modern-runs", audienceRunFixture(t, "AUDIENCE_PRIVATE_STALE_TEXT", "old source", false))
	d.Slides[0].Elements[1].Query = setQueryValue(d.Slides[0].Elements[1].Query, "modern-runs", audienceRunFixture(t, "AUDIENCE_PRIVATE_MISMATCHED_TEXT", d.Slides[0].Elements[1].Text, false))
	d.Slides[0].Elements[1].Query = setQueryValue(d.Slides[0].Elements[1].Query, "object-name", "Public accessible name")
	return newNativeEditorSession("Privacy.md", d).deck
}

func checkAudienceQueryPrivacy(t *testing.T, raw string) {
	t.Helper()
	if strings.Contains(raw, "AUDIENCE_PRIVATE") {
		t.Fatal("opaque authoring query reached the audience", raw)
	}
	q := mustQuery(raw)
	for _, key := range []string{"modern-runs", "label-modern-runs", "shape-label"} {
		payload := q.Get(key)
		if payload == "" || payload == "none" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil || bytes.Contains(decoded, []byte("AUDIENCE_PRIVATE")) {
			t.Fatal("private/stale nested formatting reached the audience", key, err, string(decoded))
		}
		if key == "shape-label" {
			data, err := decodeShapeLabelData(payload)
			if err != nil {
				t.Fatal(err)
			}
			checkAudienceQueryPrivacy(t, data.Query)
		}
	}
}

func TestParticipantMarkdownOnlyActiveKnownFormatting(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := participantPrivacyFixture(t)
	before := cloneDeck(d)
	want, err := participantRenderedDeck(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := participantSlideMarkdown(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Presentation.md", wire)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range loaded.Slides[0].Elements {
		checkAudienceQueryPrivacy(t, e.Query)
	}
	got, err := participantRenderedDeck(loaded, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range want.Pages[0].Scene.Objects {
		actual := sceneObjectsByID(*got.Pages[0].Scene)[object.ID]
		if !reflect.DeepEqual(object.Bounds, actual.Bounds) || !reflect.DeepEqual(object.Text, actual.Text) || !reflect.DeepEqual(object.Label, actual.Label) || !reflect.DeepEqual(object.Paint, actual.Paint) || !reflect.DeepEqual(object.LabelPaint, actual.LabelPaint) {
			t.Fatal("private formatting removal changed visible content", object.ID)
		}
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("public projection changed authoring extensions/history source")
	}
}

func participantHiddenFlowFixture(inherited bool) Deck {
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "flow", PageNumber: pageNumberHide, Elements: []Element{
		{ID: "hidden-text", Kind: "text", Text: "AUDIENCE_PRIVATE_HIDDEN_TEXT", Query: "render=truetype&modern-size=40&object-hidden=1"},
		{ID: "visible-text", Kind: "text", Text: "Visible following flow", Query: "render=truetype&modern-size=40"},
		{ID: "hidden-shape", Kind: "shape", Query: "shape=square&left=20&top=20&width=20&height=8&object-hidden=1"},
		{ID: "visible-shape", Kind: "shape", Query: "shape=circle&left=100&top=20&width=20&height=8"},
		{ID: "hidden-line", Kind: "connector", Query: "connector-from=hidden-shape&connector-from-side=right&connector-to=visible-shape&connector-to-side=left"},
	}}}}
	if inherited {
		d.Masters = MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base", Slide: Slide{Elements: append([]Element(nil), d.Slides[0].Elements[:1]...)}}}
		d.Slides[0].Elements = d.Slides[0].Elements[1:]
	}
	return d
}

func TestParticipantMarkdownOmitsHiddenContentAndKeepsFlow(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, inherited := range []bool{false, true} {
		t.Run(fmt.Sprint(inherited), func(t *testing.T) {
			d := participantHiddenFlowFixture(inherited)
			before := cloneDeck(d)
			original, err := participantRenderedDeck(d, 0)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := participantSlideMarkdown(d, 0)
			if err != nil || bytes.Contains(wire, []byte("AUDIENCE_PRIVATE")) {
				t.Fatal("hidden text sent to audience", err)
			}
			loaded, err := parseDeckData("Presentation.md", wire)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range loaded.Slides[0].Elements {
				if strings.HasPrefix(e.ID, "hidden-") {
					t.Fatal("hidden object/connector sent to audience", e.ID)
				}
			}
			got, err := participantRenderedDeck(loaded, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(original.Pages[0].Scene.Objects, got.Pages[0].Scene.Objects) {
				a, _ := json.Marshal(original.Pages[0].Scene.Objects)
				b, _ := json.Marshal(got.Pages[0].Scene.Objects)
				at := 0
				for at < len(a) && at < len(b) && a[at] == b[at] {
					at++
				}
				t.Fatalf("removing hidden content changed scene at %d: before %s after %s", at, a[max(0, at-50):min(len(a), at+180)], b[max(0, at-50):min(len(b), at+180)])
			}
			if !reflect.DeepEqual(before, d) {
				t.Fatal("hidden source changed during public projection")
			}
		})
	}
}

// Query-archive checks need decoded payloads too: scanning only Markdown would
// miss base64 and gzip encoding, while source extras must remain round-trippable.
func TestParticipantPrivacyFixtureSanity(t *testing.T) {
	d := participantPrivacyFixture(t)
	if !strings.Contains(d.Slides[0].Elements[2].Query, "AUDIENCE_PRIVATE") {
		t.Fatal("privacy fixture lacks opaque input")
	}
	q, _ := url.ParseQuery(d.Slides[0].Elements[2].Query)
	raw, _ := base64.StdEncoding.DecodeString(q.Get("shape-label"))
	if !bytes.Contains(raw, []byte("AUDIENCE_PRIVATE")) {
		t.Fatal("privacy fixture lacks nested encoded input")
	}
}

func TestParticipantLabelRunMaskAndEmphasis(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, mask := range []string{"invalid", "stale", "none", "valid"} {
		for _, emphasis := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", mask, emphasis), func(t *testing.T) {
				d := labelledSelectionFixture(t)
				q := mustQuery(d.Slides[0].Elements[0].Query)
				data, _ := decodeShapeLabelData(q.Get("shape-label"))
				data.Query = setQueryValue(data.Query, "modern-runs", audienceRunFixture(t, data.Text, data.Text, true))
				raw, _ := json.Marshal(data)
				q.Set("shape-label", base64.StdEncoding.EncodeToString(raw))
				value := "AUDIENCE_PRIVATE_INVALID"
				switch mask {
				case "stale":
					value = audienceRunFixture(t, "AUDIENCE_PRIVATE_OLD_LABEL", "old", false)
				case "none":
					value = "none"
				case "valid":
					value = audienceRunFixture(t, data.Text, data.Text, true)
				}
				q.Set("label-modern-runs", value)
				if emphasis {
					q.Set("label-ttf-weight", "bold")
					q.Set("label-text-italic", "0")
				}
				d.Slides[0].Elements[0].Query = q.Encode()
				before := cloneDeck(d)
				want, err := participantRenderedDeck(d, 0)
				if err != nil {
					t.Fatal(err)
				}
				wire, err := participantSlideMarkdown(d, 0)
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := parseDeckData("Presentation.md", wire)
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range loaded.Slides[0].Elements {
					checkAudienceQueryPrivacy(t, e.Query)
				}
				got, err := participantRenderedDeck(loaded, 0)
				if err != nil {
					t.Fatal(err)
				}
				original, actual := sceneObjectsByID(*want.Pages[0].Scene)["label-a"], sceneObjectsByID(*got.Pages[0].Scene)["label-a"]
				if !reflect.DeepEqual(original.Label, actual.Label) || !reflect.DeepEqual(original.LabelPaint, actual.LabelPaint) || !reflect.DeepEqual(original.Bounds, actual.Bounds) {
					t.Fatal("privacy filtering changed nested mask/emphasis appearance", original.Label, actual.Label)
				}
				if !reflect.DeepEqual(before, d) {
					t.Fatal("label source changed")
				}
			})
		}
	}
}

func TestParticipantMarkdownCorpusSceneParity(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	var manifest struct {
		Decks []struct {
			File  string
			Pages int
		}
	}
	wire, err := os.ReadFile(testRepositoryPath("testdata/powerup/manifest.json"))
	if err != nil || json.Unmarshal(wire, &manifest) != nil {
		t.Fatal("proof manifest", err)
	}
	pages := 0
	for _, entry := range manifest.Decks {
		t.Run(entry.File, func(t *testing.T) {
			raw, err := os.ReadFile(testRepositoryPath("testdata", "powerup", entry.File))
			if err != nil {
				t.Fatal(err)
			}
			d, err := parseDeckData(entry.File, raw)
			if err != nil || len(d.Slides) != entry.Pages {
				t.Fatal("proof deck", err)
			}
			for _, size := range []*DeckPageSize{nil, {Version: 1, Width: 1080, Height: 1920}, {Version: 1, Width: 2560, Height: 1080}} {
				d.PageSize = size.Clone()
				before := cloneDeck(d)
				for index := range d.Slides {
					want, err := participantRenderedDeck(d, index)
					if err != nil {
						t.Fatal(err)
					}
					md, err := participantSlideMarkdown(d, index)
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := parseDeckData("Presentation.md", md)
					if err != nil {
						t.Fatal(err)
					}
					restoreParticipantLayers(&loaded.Slides[0])
					got, err := participantRenderedDeck(loaded, 0)
					if err != nil {
						t.Fatal(err)
					}
					a, b := publicParityScene(*want.Pages[0].Scene), publicParityScene(*got.Pages[0].Scene)
					// The old Markdown adapter deliberately carries no timeline.
					a.Build, b.Build = nil, nil
					a.SlideID, b.SlideID = "", ""
					x, _ := json.Marshal(a)
					y, _ := json.Marshal(b)
					if !bytes.Equal(x, y) {
						at := 0
						for at < len(x) && at < len(y) && x[at] == y[at] {
							at++
						}
						t.Fatalf("slide %d canvas %+v changed public rendering at %d: before %s after %s", index, size, at, x[max(0, at-70):min(len(x), at+200)], y[max(0, at-70):min(len(y), at+200)])
					}
					pages++
				}
				if !reflect.DeepEqual(before, d) {
					t.Fatal("public corpus projection changed authored data")
				}
			}
		})
	}
	if pages != 60 {
		t.Fatal("public corpus page contract changed", pages)
	}
}

func TestParticipantHiddenStructuredPayloadsAndMediaAreNotArchived(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := structuredFixture()
	d.Appearance = &DeckAppearance{Version: 3}
	for i := range d.Slides[0].Elements {
		d.Slides[0].Elements[i].Query = setQueryValue(d.Slides[0].Elements[i].Query, "object-hidden", "1")
	}
	image := sceneFixture(t)
	d.Assets = image.Assets
	private := image.Slides[0].Elements[len(image.Slides[0].Elements)-1]
	private.Query = setQueryValue(private.Query, "object-hidden", "1")
	d.Slides[0].Elements = append(d.Slides[0].Elements, private, Element{ID: "public", Kind: "text", Text: "Public visible text", Query: "render=truetype&left=5&top=2"})
	before := cloneDeck(d)
	wire, err := participantSlideMarkdown(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Presentation.md", wire)
	if err != nil || len(loaded.Slides[0].Elements) != 1 || loaded.Slides[0].Elements[0].Text != "Public visible text" || len(loaded.Assets) != 0 || loaded.Objects != nil {
		t.Fatal("hidden payload/media archive reached audience", err)
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("public pruning changed hidden source")
	}
}

func TestParticipantPrivacyBrowserFixtures(t *testing.T) {
	root := os.Getenv("KEYNOPE_AUDIENCE_PRIVACY_PROOF")
	if root == "" {
		t.Skip("audience privacy browser fixtures not requested")
	}
	useTestAuthoredSize(t, 245, 56)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	cases := []publicSurfaceBrowserCase{}
	for _, item := range []struct {
		name string
		deck Deck
	}{{"public", participantPrivacyFixture(t)}, {"hidden-local", participantHiddenFlowFixture(false)}, {"hidden-master", participantHiddenFlowFixture(true)}} {
		wire, err := participantSlideMarkdown(item.deck, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, item.name+".md"), wire, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := parseDeckData("Presentation.md", wire)
		if err != nil {
			t.Fatal(err)
		}
		restoreParticipantLayers(&loaded.Slides[0])
		public, err := participantRenderedDeck(loaded, 0)
		if err != nil {
			t.Fatal(err)
		}
		original := projectExportDeck("Privacy.md", item.deck.ResolvedSlides(), 245, 56, false)
		html, err := exportProjectedHTML(original, preservedExportHead{}, false)
		if err != nil {
			t.Fatal(err)
		}
		file := item.name + ".html"
		if err := os.WriteFile(filepath.Join(root, file), []byte(html), 0600); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, publicSurfaceBrowserCase{File: file, Scenes: []slideScene{*public.Pages[0].Scene}})
	}
	wire, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cases.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
}
