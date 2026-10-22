package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func videoFixture(t *testing.T) ([]byte, string, DeckAsset, string) {
	t.Helper()
	data, err := os.ReadFile("testdata/video-sample.mp4")
	if err != nil {
		t.Fatal(err)
	}
	id, asset, path, err := embeddedVideoAsset(context.Background(), data, nil)
	if err != nil {
		t.Fatal(err)
	}
	return data, id, asset, path
}

func TestEmbeddedVideoValidationAndOwnership(t *testing.T) {
	data, _, asset, _ := videoFixture(t)
	if asset.Video.Width != 320 || asset.Video.Height != 180 || asset.Video.DurationMS != 4000 || asset.Video.MIME != "video/mp4" {
		t.Fatal(asset.Video.Width, asset.Video.Height, asset.Video.DurationMS)
	}
	if err := validateClipboardAsset(asset); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*DeckAsset){
		"mime": func(a *DeckAsset) { a.Video.MIME = "video/webm" }, "width": func(a *DeckAsset) { a.Video.Width++ }, "duration": func(a *DeckAsset) { a.Video.DurationMS++ }, "bytes": func(a *DeckAsset) { a.Video.Data = a.Video.Data[:25] },
	} {
		t.Run(name, func(t *testing.T) {
			bad := cloneDeckAsset(asset)
			mutate(&bad)
			if validateClipboardAsset(bad) == nil {
				t.Fatal("accepted forged video metadata")
			}
		})
	}
	external := append([]byte(nil), data...)
	at := bytes.Index(external, []byte("url "))
	if at < 0 {
		t.Fatal("fixture has no data reference")
	}
	binary.BigEndian.PutUint32(external[at+4:], 0)
	if _, err := inspectMovie(external); err == nil {
		t.Fatal("accepted external movie references")
	}
	for _, bad := range [][]byte{nil, []byte("<video src='https://example.com/movie'>"), data[:len(data)-1]} {
		if _, err := inspectMovie(bad); err == nil {
			t.Fatal("accepted malformed movie")
		}
	}
	clone := cloneDeckAsset(asset)
	clone.Video.Data[0] ^= 1
	if bytes.Equal(clone.Video.Data, asset.Video.Data) {
		t.Fatal("video history aliases mutable bytes")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := embeddedVideoAsset(cancelled, data, nil); err == nil {
		t.Fatal("cancelled import accepted")
	}
}

func TestVideoDeckRoundTripClipboardGeometryAndExport(t *testing.T) {
	data, id, asset, path := videoFixture(t)
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "video-slide", PageNumber: pageNumberHide}}}
	element := videoElement(id, path, asset, &deck)
	element.ID = "video-one"
	element.Query += "&video-autoplay=1&video-loop=1&video-muted=1"
	deck.Slides[0].Elements = []Element{element}
	encoded, err := serializeDeck("Video.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckDataWithAssetPublication("Video.md", encoded, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Assets) != 1 || len(reopened.Slides[0].Elements) != 1 || reopened.Slides[0].Elements[0].Kind != "video" || !bytes.Equal(reopened.Assets[id].Video.Data, data) {
		t.Fatal("video did not survive save/reopen")
	}
	scene, err := buildDocumentSlideScene(reopened, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 1 || scene.Objects[0].Video == nil || !scene.Objects[0].Video.Autoplay || !scene.Objects[0].Video.Loop || !scene.Objects[0].Video.Muted || scene.Objects[0].Bounds.Width < 900 {
		t.Fatalf("video scene missing or wrong: %+v", scene.Diagnostics)
	}
	bundle, err := makeObjectClipboard(reopened, reopened.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bundle.Assets[id].Video.Data, data) {
		t.Fatal("clipboard lost video")
	}
	caps := buildSelectionCapabilities(reopened, reopened.Slides[0], reopened.ResolveSlide(0, false), []string{element.ID}, "", 0)
	if !caps.Commands["arrange.geometry"].Enabled || !caps.Commands["arrange.rotate"].Enabled {
		t.Fatal("video cannot be arranged", caps.Commands)
	}
	html, err := exportHTMLDocument("Video.md", reopened.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
	if err != nil || !strings.Contains(html, "data:video/mp4;base64,") {
		t.Fatal("HTML lost embedded movie", err)
	}
	picture, warnings := sceneObjectToPPTX(scene.Objects[0])
	if picture == nil || picture.Kind != "image" || len(warnings) == 0 || warnings[0].Code != "video-poster-only" {
		t.Fatal("PowerPoint must explicitly export a poster", warnings)
	}
	if out := os.Getenv("KEYNOPE_VIDEO_EVIDENCE"); out != "" {
		if err := os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string][]byte{"video.html": []byte(html), "video.md": encoded, "video.mp4": data} {
			if err := os.WriteFile(filepath.Join(out, name), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestVideoUploadNativeUndoAndMaster(t *testing.T) {
	data, _, _, _ := videoFixture(t)
	for _, master := range []bool{false, true} {
		s := newNativeEditorSession(filepath.Join(t.TempDir(), "Video.md"), Deck{Slides: []Slide{{}}}, true)
		if master {
			if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
				t.Fatal(err)
			}
		}
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("video", "clip.mp4")
		part.Write(data)
		writer.Close()
		request := httptest.NewRequest("POST", "/api/editor/upload-video", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		s.handleVideoUpload(response, request)
		if response.Code != 200 {
			t.Fatal(master, response.Code, response.Body.String())
		}
		var state nativeEditorState
		if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if state.Selected < 0 || len(s.deck.Assets) != 1 {
			t.Fatal("video not inserted")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
			t.Fatal(err)
		}
		if len(s.deck.Assets) != 0 {
			t.Fatal("undo retained imported movie")
		}
	}
}

func TestVideoBrowser(t *testing.T) {
	if os.Getenv("KEYNOPE_VIDEO_BROWSER") != "1" {
		t.Skip("set KEYNOPE_VIDEO_BROWSER=1 for real media playback and editor import checks")
	}
	useTestAuthoredSize(t, 245, 56)
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "video-import-slide", PageNumber: pageNumberHide, Elements: []Element{{ID: "video-title", Kind: "text", Text: "Embedded video", Query: "render=truetype&modern-font=sans&modern-size=72&left=12&top=3"}}}}}
	s := newNativeEditorSession(filepath.Join(t.TempDir(), "Video.md"), deck, true)
	server := newGeometryBrowserServer(t, s)
	runGeometryBrowser(t, "internal/app/testdata/video.cjs", server.URL)
}

func TestParticipantBroadcastOmitsVideosWithoutChangingLocalDeck(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	data, id, asset, path := videoFixture(t)
	for _, inherited := range []bool{false, true} {
		for _, reusePoster := range []bool{false, true} {
			deck := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "video-broadcast-slide", PageNumber: pageNumberHide}}}
			video := videoElement(id, path, asset, &deck)
			video.ID = "local-only-video"
			deck.Slides[0].Elements = []Element{video, {ID: "public-text", Kind: "text", Text: "This stays visible", Query: "render=truetype&modern-font=sans&left=8&top=2"}}
			if inherited {
				deck.Masters = MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base", Slide: Slide{Elements: []Element{video}}}, Layouts: []MasterLayout{{ID: "video-layout", Name: "Video layout"}}}
				deck.Slides[0].LayoutID = "video-layout"
				deck.Slides[0].Elements = deck.Slides[0].Elements[1:]
				deck.Slides[0].TabID = "reference-tab"
			}
			if reusePoster {
				deck.Slides[0].Elements = append(deck.Slides[0].Elements, Element{ID: "public-poster-image", Kind: "image", AssetID: id, Path: path, Query: "image-style=modern&left=3&top=40&width=20&height=10"})
			}
			before := cloneDeck(deck)
			local, err := buildDocumentSlideScene(deck, 0, 245, 56)
			if err != nil || sceneObjectsByID(local)[video.ID].Video == nil {
				t.Fatal("local video missing", inherited, err)
			}
			public, err := participantRenderedDeck(deck, 0)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := participantSlideMarkdown(deck, 0)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := parseDeckData("Presentation.md", wire)
			if err != nil {
				t.Fatal(err)
			}
			for _, element := range loaded.Slides[0].Elements {
				if element.Kind == "video" || element.ID == video.ID {
					t.Fatal("broadcast Markdown contains a video object")
				}
			}
			if reusePoster && loaded.Assets[id].MIME != "image/png" || !reusePoster && len(loaded.Assets) != 0 {
				t.Fatal("broadcast failed to prune video-only assets or keep a reused poster")
			}
			for _, media := range loaded.Assets {
				if media.Video != nil {
					t.Fatal("movie bytes leaked through a shared poster asset")
				}
			}
			for _, page := range append(public.Pages, participantMarkdownExportPages(loaded, 0, 1, 245, 56)...) {
				objects := sceneObjectsByID(*page.Scene)
				if _, ok := objects[video.ID]; ok {
					t.Fatal("participant scene retained the video/poster")
				}
				if objects["public-text"].Bounds != sceneObjectsByID(local)["public-text"].Bounds {
					t.Fatal("omitting a video moved public text")
				}
				encoded, _ := json.Marshal(page)
				if bytes.Contains(encoded, []byte("data:video/")) || bytes.Contains(encoded, []byte(base64.StdEncoding.EncodeToString(data))) {
					t.Fatal("movie bytes reached participant scene JSON")
				}
			}
			// Normal authoring and offline HTML exports still retain playable video.
			html, err := exportHTMLDocument("Video.md", deck.ResolvedSlides(), 245, 56, preservedExportHead{}, false)
			if err != nil || !strings.Contains(html, "data:video/mp4;base64,") {
				t.Fatal("broadcast filtering changed standalone playback", err)
			}
			if !reflect.DeepEqual(before, deck) {
				t.Fatal("broadcast filtering mutated the authored deck")
			}
		}
	}
}

func TestParticipantVideoOnlySlideKeepsItsBackground(t *testing.T) {
	_, id, asset, path := videoFixture(t)
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "video-only", BG: "48;2;34;51;68", BGSet: true, PageNumber: pageNumberHide}}}
	deck.Slides[0].Elements = []Element{videoElement(id, path, asset, &deck)}
	public, err := participantRenderedDeck(deck, 0)
	if err != nil || len(public.Pages) != 1 || len(public.Pages[0].Scene.Objects) != 0 || public.Pages[0].Scene.Background != "#223344" {
		t.Fatal("video-only broadcast should retain an empty slide and its background", err)
	}
	wire, err := participantSlideMarkdown(deck, 0)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Presentation.md", wire)
	if err != nil || len(loaded.Slides) != 1 || len(loaded.Slides[0].Elements) != 0 || len(loaded.Assets) != 0 {
		t.Fatal("video-only Markdown broadcast retained video or lost the slide", err)
	}
}

func TestVideoAboveFormerLimitAndAggregateAdmission(t *testing.T) {
	if os.Getenv("KEYNOPE_VIDEO_LARGE") != "1" {
		t.Skip("set KEYNOPE_VIDEO_LARGE=1 to exercise real 41 MiB movie embedding and saving")
	}
	fixture, err := os.ReadFile("testdata/video-sample.mp4")
	if err != nil {
		t.Fatal(err)
	}
	// ISO BMFF permits padding atoms. Keep a real decodable H.264/AAC movie,
	// adding free space beyond the old limit without storing a huge test file.
	data := make([]byte, 41<<20)
	copy(data, fixture)
	binary.BigEndian.PutUint32(data[len(fixture):], uint32(len(data)-len(fixture)))
	copy(data[len(fixture)+4:], "free")
	id, asset, path, err := embeddedVideoAsset(context.Background(), data, nil)
	if err != nil {
		t.Fatal("movie above the former 40 MiB limit was rejected", err)
	}
	if cached, ok := embeddedStillAsset("keynope-asset:" + id + ".png"); !ok || cached.Video != nil {
		t.Fatal("poster cache retained original movie bytes")
	}
	s := newNativeEditorSession(filepath.Join(t.TempDir(), "Large video.md"), Deck{Slides: []Slide{{}}}, true)
	element := videoElement(id, path, asset, &s.deck)
	if err := s.apply(nativeEditorAction{Action: "add-element", Kind: "video", ElementData: &element, AssetData: &asset}); err != nil {
		t.Fatal("could not insert a 41 MiB movie", err)
	}
	encoded, err := serializeDeck("Large video.md", s.deck)
	if err != nil {
		t.Fatal("accepted large movie could not be saved", err)
	}
	reopened, err := parseDeckDataWithAssetPublication("Large video.md", encoded, false)
	if err != nil || len(reopened.Assets[id].Video.Data) != len(data) {
		t.Fatal("large movie could not be reopened", err)
	}
	version, history := s.version, len(s.undo)
	// A distinct asset of this size would exceed the existing shared archive.
	other := element
	other.AssetID = "second-video"
	err = s.apply(nativeEditorAction{Action: "add-element", Kind: "video", ElementData: &other, AssetData: &asset})
	if err == nil || !strings.Contains(err.Error(), "embedded-media capacity") || s.version != version || len(s.undo) != history || len(s.deck.Assets) != 1 || len(s.deck.Slides[0].Elements) != 1 {
		t.Fatal("over-capacity import did not fail atomically", err)
	}
	// Reusing the exact asset consumes no additional archive capacity.
	if err := s.apply(nativeEditorAction{Action: "add-element", Kind: "video", ElementData: &element, AssetData: &asset}); err != nil || len(s.deck.Assets) != 1 {
		t.Fatal("reusing an existing movie was incorrectly charged twice", err)
	}
}
