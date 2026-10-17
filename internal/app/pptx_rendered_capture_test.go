package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"image/gif"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"keynope/internal/pptx"
)

func pptxFontCaptureDeck(t *testing.T) Deck {
	id, animated := pptxAnimationFixture(t)
	stillID, still, stillPath, err := embeddedImageAsset(animated.Frames[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: animated, stillID: still}}
	first := Slide{ID: "fonts", Notes: "PRIVATE SPEAKER NOTES"}
	for i, glyph := range []string{"ascii", "dense", "braille"} {
		first.Elements = append(first.Elements, Element{ID: glyph, Kind: "image", AssetID: stillID, Path: stillPath, Query: "image-style=retro&glyph=" + glyph + "&top=4&left=" + decimal(float64(5+i*60)) + "&width=40&height=16&image-fit=stretch&stretch=1&object-name=" + glyph + "&modern-opacity=0.6&brightness=1.4"})
	}
	first.Elements = append(first.Elements, Element{ID: "private-qr", Kind: "text", Text: "PRIVATE JOIN DATA", PlaceholderRole: activityQRCodeRole})
	d.Slides = []Slide{first, {ID: "animated", Elements: []Element{{ID: "moving-braille", Kind: "image", AssetID: id, Path: embeddedAssetPath(id, animated), Query: "image-style=retro&glyph=braille&top=4&left=10&width=30&height=12&image-fit=stretch&object-name=moving-braille"}}}, {ID: "background", BackgroundMediaSet: true, BackgroundMedia: &slideBackgroundMedia{AssetID: stillID, Fit: "contain", Opacity: .7, OpacitySet: true, Query: "image-style=retro&glyph=braille"}}, {ID: "tab", TabID: "private-tab", Elements: []Element{{ID: "private", Kind: "image", AssetID: stillID, Path: stillPath, Query: "image-style=retro&glyph=braille&object-name=PRIVATE-TAB"}}}}
	return d
}

func TestPPTXModernPaintCaptureManifest(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	id, animated := pptxAnimationFixture(t)
	stillID, still, stillPath, err := embeddedImageAsset(animated.Frames[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: animated, stillID: still}, Slides: []Slide{{ID: "paint", Elements: []Element{
		{ID: "gradient", Kind: "image", AssetID: stillID, Path: stillPath, Query: "image-style=modern&width=30&height=10&left=5&top=3&image-fit=contain&modern-mask=ellipse&gradient-start=%23ff0055&gradient-end=%2355ffff&shadow=soft&shadow-color=%23ffffff&modern-opacity=0.6"},
		{ID: "animated-outline", Kind: "image", AssetID: id, Path: embeddedAssetPath(id, animated), Query: "image-style=modern&width=30&height=10&left=50&top=3&outline=light"},
	}}, {ID: "background", BackgroundMediaSet: true, BackgroundMedia: &slideBackgroundMedia{AssetID: stillID, Fit: "contain", Opacity: .6, OpacitySet: true, Query: "image-style=modern&gradient-start=%23ff0055&gradient-end=%2355ffff"}}}}
	before := cloneDeck(d)
	items, err := buildPPTXCaptureItems(context.Background(), d)
	if err != nil || len(items) != 3 {
		t.Fatal(len(items), err)
	}
	if len(items[1].Times) != 2 || items[1].Times[1] != 70 || items[0].Object.Media == nil || items[2].Object.Media.Fit != "contain" {
		t.Fatal("modern capture lost source/frame/background geometry", items)
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("capture mutated source")
	}
	work := newPPTXExportWork(context.Background())
	work.captures = map[string]pptxRenderedCapture{}
	if _, _, err := deckToPPTXWithWork(d, work); err == nil {
		t.Fatal("missing required modern capture silently ignored")
	}
	if root := os.Getenv("KEYNOPE_IMAGE_ARTWORK_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "deck.md"), d); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPPTXCaptureSourcesAggregateBudget(t *testing.T) {
	media := &sceneMedia{Source: "1234", Frames: []sceneMediaFrame{{Source: "5678"}, {Source: "90"}}}
	remaining := int64(10)
	if err := chargePPTXCaptureSources(media, &remaining); err != nil || remaining != 0 {
		t.Fatal(err, remaining)
	}
	if err := chargePPTXCaptureSources(media, &remaining); err == nil {
		t.Fatal("repeated source escaped aggregate manifest budget")
	}
	remaining = 9
	if err := chargePPTXCaptureSources(media, &remaining); err == nil {
		t.Fatal("late frame escaped source budget")
	}
}

func TestPPTXModernPaintBrowserPackage(t *testing.T) {
	root := os.Getenv("KEYNOPE_IMAGE_ARTWORK_VERIFY")
	if root == "" {
		t.Skip("opt-in browser package evidence")
	}
	for _, engine := range []string{"chromium", "webkit"} {
		t.Run(engine, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "export-"+engine+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var exported struct {
				Request pptxCaptureRequest `json:"request"`
				Payload struct {
					Content string `json:"content"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(data, &exported); err != nil {
				t.Fatal(err)
			}
			binary, err := base64.StdEncoding.DecodeString(exported.Payload.Content)
			if err != nil {
				t.Fatal(err)
			}
			presentation, _, err := pptx.Read(context.Background(), bytes.NewReader(binary), int64(len(binary)), pptx.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if len(presentation.Slides) != 2 || len(exported.Request.Captures) != 3 {
				t.Fatal("missing exported slides/captures")
			}
			for _, capture := range exported.Request.Captures {
				found := false
				for _, object := range presentation.Slides[capture.Slide].Objects {
					if object.Kind != "image" {
						continue
					}
					if len(capture.Frames) == 1 {
						png, _, ok := dataURI(capture.Frames[0])
						if !ok {
							t.Fatal("invalid capture")
						}
						if !bytes.Equal(png, object.Image) {
							continue
						}
						found = true
						if object.ImageOpacity == nil || *object.ImageOpacity != .6 {
							t.Fatal("capture opacity lost")
						}
						if capture.ObjectID == "gradient" && object.Shadow == nil {
							t.Fatal("native picture shadow lost")
						}
					} else if object.ImageMIME == "image/gif" {
						animation, err := gif.DecodeAll(bytes.NewReader(object.Image))
						if err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(animation.Delay, []int{7, 19}) || animation.LoopCount != 2 {
							t.Fatal("effect GIF timing lost", animation.Delay, animation.LoopCount)
						}
						found = true
					}
				}
				if !found {
					t.Fatal("captured artwork replaced or treated twice", capture.ObjectID)
				}
			}
		})
	}
}

func TestPPTXFontCaptureManifestScopeAndOwnership(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := pptxFontCaptureDeck(t)
	before := cloneDeck(d)
	items, err := buildPPTXCaptureItems(context.Background(), d)
	if err != nil || len(items) != 5 {
		t.Fatal("font capture targets missing", len(items), err)
	}
	data, _ := json.Marshal(items)
	if strings.Contains(string(data), "PRIVATE") || strings.Contains(string(data), `"media"`) {
		t.Fatal("capture manifest leaked private/source data")
	}
	if len(items[3].Times) != 2 || items[3].Times[1] != 70 {
		t.Fatal("scene frame timing changed", items[3].Times)
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("manifest mutated authored deck")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := buildPPTXCaptureItems(ctx, d); err == nil {
		t.Fatal("manifest ignored cancellation")
	}
	if root := os.Getenv("KEYNOPE_FONT_CAPTURE_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := saveDeck(filepath.Join(root, "font-art.md"), d); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPPTXCaptureAdmissionAndRevision(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, data := range []string{`null`, `{}`, `{"owner":"a","OWNER":"b","version":1,"captures":[]}`, `{"owner":"a","version":1,"captures":[{"slide":0,"objectId":"a","frames":[],"Frames":[]}]}`, `{"owner":"a","version":1,"captures":[{"slide":0,"objectId":"a","frames":null}]}`, `{"owner":"a","version":1,"captures":[],"unexpected":1}`} {
		if err := decodePPTXCaptureRequest([]byte(data), &pptxCaptureRequest{}); err == nil {
			t.Fatal("invalid capture request accepted", data)
		}
	}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{{ID: "text", Kind: "text", Text: "Keep"}}}}}
	s := newNativeEditorSession("Owned.md", d)
	for _, request := range []pptxCaptureRequest{{Owner: s.notesSessionID, Version: s.version - 1}, {Owner: "another-document", Version: s.version}} {
		body, _ := json.Marshal(request)
		recorder := httptest.NewRecorder()
		s.handleExportPPTX(recorder, httptest.NewRequest("POST", "/api/editor/export-pptx", bytes.NewReader(body)))
		if recorder.Code != 409 {
			t.Fatal("stale capture ownership accepted", recorder.Code, recorder.Body.String())
		}
	}
	request := pptxCaptureRequest{Owner: s.notesSessionID, Version: s.version, Captures: []pptxRenderedCapture{{ObjectID: "missing", Frames: []string{"x"}}}}
	body, _ := json.Marshal(request)
	recorder := httptest.NewRecorder()
	s.handleExportPPTX(recorder, httptest.NewRequest("POST", "/api/editor/export-pptx", bytes.NewReader(body)))
	if recorder.Code == 200 || strings.Contains(recorder.Body.String(), `"content"`) {
		t.Fatal("unknown capture target produced package")
	}
	object := retroAnimationFixture()
	object.RetroLines.Frames = nil
	for _, frames := range [][]string{nil, {"data:image/png;base64,broken"}, {"https://example.invalid/image.png"}} {
		if _, _, err := capturedPPTXRetroImage(object, pptxRenderedCapture{Frames: frames}, newPPTXExportWork(context.Background())); err == nil {
			t.Fatal("bad capture pixels accepted")
		}
	}
	picture, _, err := rasterPPTXRetroImage(object)
	if err != nil {
		t.Fatal(err)
	}
	valid := "data:image/png;base64," + base64.StdEncoding.EncodeToString(picture.Image)
	for _, source := range []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPPTXPNG(t)), "data:image/png;base64," + base64.StdEncoding.EncodeToString(picture.Image[:len(picture.Image)-5])} {
		if _, _, err := capturedPPTXRetroImage(object, pptxRenderedCapture{Frames: []string{source}}, newPPTXExportWork(context.Background())); err == nil {
			t.Fatal("wrong-sized/corrupt capture admitted")
		}
	}
	work := newPPTXExportWork(context.Background())
	work.pixelsLeft = 1
	if _, _, err := capturedPPTXRetroImage(object, pptxRenderedCapture{Frames: []string{valid}}, work); err == nil {
		t.Fatal("capture bypassed aggregate pixel budget")
	}
	fontDeck := pptxFontCaptureDeck(t)
	work = newPPTXExportWork(context.Background())
	work.captures = map[string]pptxRenderedCapture{}
	if data, _, err := exportPPTXDocumentWithWork(fontDeck, work); err == nil || data != nil {
		t.Fatal("missing required font capture produced a partial export", err)
	}
}

func TestPPTXFontCaptureBrowserReadback(t *testing.T) {
	root := os.Getenv("KEYNOPE_FONT_CAPTURE_EVIDENCE")
	if root == "" {
		t.Skip("browser capture proof not requested")
	}
	data, err := os.ReadFile(filepath.Join(root, "browser-export.json"))
	if os.IsNotExist(err) {
		t.Skip("browser capture has not run")
	}
	if err != nil {
		t.Fatal(err)
	}
	var proof struct {
		Content string             `json:"content"`
		Request pptxCaptureRequest `json:"request"`
	}
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	zip, err := base64.StdEncoding.DecodeString(proof.Content)
	if err != nil {
		t.Fatal(err)
	}
	presentation, _, err := pptx.Read(context.Background(), bytes.NewReader(zip), int64(len(zip)), pptx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, capture := range proof.Request.Captures {
		name := capture.ObjectID
		if strings.HasPrefix(name, "keynope-background-image-") {
			name = "Slide background"
		}
		var found *pptx.Object
		for _, o := range presentation.Slides[capture.Slide].Objects {
			if o.Name == name {
				copy := o
				found = &copy
			}
		}
		if found == nil {
			t.Fatal("captured artwork missing from package", name)
		}
		if len(capture.Frames) == 1 {
			want, _, _ := dataURI(capture.Frames[0])
			if !bytes.Equal(found.Image, want) {
				t.Fatal("font artwork pixels changed", name)
			}
		} else {
			animation, err := gif.DecodeAll(bytes.NewReader(found.Image))
			if err != nil || len(animation.Image) != len(capture.Frames) || animation.LoopCount != 2 || !reflect.DeepEqual(animation.Delay, []int{7, 19}) {
				t.Fatal("captured animation timing lost", err)
			}
			for i, frame := range animation.Image {
				encoded, _, _ := dataURI(capture.Frames[i])
				want, err := png.Decode(bytes.NewReader(encoded))
				if err != nil {
					t.Fatal(err)
				}
				for y := 0; y < frame.Bounds().Dy(); y++ {
					for x := 0; x < frame.Bounds().Dx(); x++ {
						pixel := color.NRGBAModel.Convert(want.At(x, y)).(color.NRGBA)
						if pixel.A < 128 {
							pixel = color.NRGBA{}
						} else {
							pixel.A = 255
						}
						if got := color.NRGBAModel.Convert(frame.At(x, y)).(color.NRGBA); got != pixel {
							t.Fatal("GIF pixels differ beyond diagnosed alpha threshold", i, x, y, got, pixel)
						}
					}
				}
			}
		}
	}
}
