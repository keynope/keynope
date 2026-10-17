package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestClipboardLegacyObjectsAcrossSourceScopes(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	t.Chdir(t.TempDir()) // Reopened embedded-image caches are test-owned.
	assetID := newStableID("legacy-clipboard-image")
	asset := clipboardTestAsset(t)
	registerEmbeddedStillAsset(assetID, asset)
	t.Cleanup(func() {
		embeddedStillAssets.Lock()
		delete(embeddedStillAssets.assets, assetID)
		delete(embeddedStillAssets.identities, assetID)
		embeddedStillAssets.Unlock()
	})
	for _, version := range []int{1, 2} {
		for _, style := range []string{"retro", "modern"} {
			for _, scopeName := range []string{"slide", "tab", "base", "layout"} {
				t.Run(fmt.Sprintf("v%d/%s/%s", version, style, scopeName), func(t *testing.T) {
					d := structuredFixture()
					d.Objects.Builds = nil
					d.Masters = defaultMasterDeck()
					d.Masters.Base.Slide = Slide{PageNumber: "hide"}
					for i := range d.Masters.Layouts {
						d.Masters.Layouts[i].Slide = Slide{PageNumber: "hide"}
					}
					d.Appearance = &DeckAppearance{Version: version, Extra: map[string]json.RawMessage{"profiles": json.RawMessage(`{"modern":{"textScale":0.75},"retro":{"textScale":1.25}}`)}}
					if version == 1 {
						d.Appearance.Mode = style
					} else {
						d.Appearance.DefaultStyle = style
					}
					label, _ := json.Marshal(shapeLabelData{Text: "Portable label", Query: "render=truetype"})
					d.Assets = map[string]DeckAsset{assetID: asset}
					elements := append(d.Slides[0].Elements,
						Element{ID: "labelled", Kind: "shape", Query: "shape=square&top=2&left=110&width=50&height=16&shape-label=" + base64.StdEncoding.EncodeToString(label)},
						Element{ID: "photo", Kind: "image", AssetID: assetID, Path: embeddedAssetPath(assetID, asset), Query: "top=25&left=110&width=30&height=15&brightness=1.3"})
					d.Slides[0].Elements = nil
					d.Slides[0].PageNumber = "hide"
					sourceIndex, master := 0, false
					switch scopeName {
					case "base":
						d.Masters.Base.Slide.Elements = elements
						master = true
					case "layout":
						d.Masters.Layouts[0].Slide.Elements = elements
						master, sourceIndex = true, 1
					default:
						d.Slides[0].Elements = elements
					}
					if scopeName == "tab" {
						d.Slides[0].TabID = "guide"
						syncSlideTabs(&d)
					}
					source := newNativeEditorSession("Legacy.md", d)
					source.masterMode, source.currentMaster = master, sourceIndex
					before := cloneDeck(source.deck)
					resolved, err := captureSlideResolutionScope(source.deck, sourceIndex, master)
					if err != nil {
						t.Fatal(err)
					}
					original, err := buildStyledSlideScene(source.deck, resolved.resolved(), sourceIndex, 245, 56, true)
					if err != nil {
						t.Fatal(err)
					}
					ids := []string{}
					for _, e := range elements {
						ids = append(ids, e.ID)
					}
					request, _ := json.Marshal(map[string]interface{}{"version": source.version, "ids": ids})
					response := httptest.NewRecorder()
					source.handleObjectClipboard(response, httptest.NewRequest(http.MethodPost, "/api/editor/clipboard", bytes.NewReader(request)))
					if response.Code != http.StatusOK {
						t.Fatal(response.Code, response.Body.String())
					}
					var bundle ObjectClipboard
					if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
						t.Fatal(err)
					}
					target := newNativeEditorSession("Paste.md", Deck{Slides: []Slide{{PageNumber: "hide"}}})
					if err := target.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
						t.Fatal(err)
					}
					pasted, err := buildDocumentSlideScene(target.deck, 0, 245, 56)
					if err != nil {
						t.Fatal(err)
					}
					if len(original.Objects) != len(pasted.Objects) || len(pasted.Objects) != len(elements) {
						t.Fatal("missing objects", len(original.Objects), len(pasted.Objects))
					}
					for i, a := range original.Objects {
						b := pasted.Objects[i]
						// Render-line element indices are scoped to the slide's
						// ordering, not visual paint or portable object identity.
						for _, object := range []*sceneObject{&a, &b} {
							if object.RetroLines != nil {
								copy := *object.RetroLines
								copy.Lines = append([]exportLine(nil), copy.Lines...)
								for j := range copy.Lines {
									copy.Lines[j].Element = 0
								}
								object.RetroLines = &copy
							}
						}
						for property, pair := range map[string][2]interface{}{"paint": {a.Paint, b.Paint}, "label": {a.Label, b.Label}, "label-paint": {a.LabelPaint, b.LabelPaint}, "media": {a.Media, b.Media}, "table": {a.Table, b.Table}, "chart": {a.Chart, b.Chart}, "retro": {a.RetroLines, b.RetroLines}} {
							if !reflect.DeepEqual(pair[0], pair[1]) {
								t.Fatalf("%s %s changed:\nsource=%+v\npaste=%+v", a.Kind, property, pair[0], pair[1])
							}
						}
					}
					if !reflect.DeepEqual(source.deck, before) {
						t.Fatal("copy changed source")
					}
					if err := target.apply(nativeEditorAction{Action: "undo"}); err != nil || len(target.deck.Slides[0].Elements) != 0 {
						t.Fatal("paste undo failed", err)
					}
					if err := target.apply(nativeEditorAction{Action: "redo"}); err != nil {
						t.Fatal(err)
					}
					data, err := serializeDeck("Pasted.md", target.deck)
					if err != nil {
						t.Fatal(err)
					}
					reopened, err := parseDeckData("Pasted.md", data)
					if err != nil {
						t.Fatal(err)
					}
					final, err := buildDocumentSlideScene(reopened, 0, 245, 56)
					if err != nil || !reflect.DeepEqual(final.Objects, pasted.Objects) {
						t.Fatal("mixed legacy clipboard objects changed after history/save/reopen", err)
					}
				})
			}
		}
	}
}
