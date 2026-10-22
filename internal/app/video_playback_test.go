package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func postVideoPlayback(t *testing.T, p *presenterCompanion, input presentationVideoRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	p.handleVideoPlayback(w, httptest.NewRequest(http.MethodPost, "/video-playback", bytes.NewReader(body)))
	return w
}

func TestVideoPlaybackLocalOrderedEphemeralAndScoped(t *testing.T) {
	p := cursorCompanionFixture()
	defer p.Close()
	p.pages[0][0].Scene.Objects = []sceneObject{{ID: "movie", Kind: "video", Video: &sceneVideo{}}}
	c := p.BuildSnapshot()
	input := presentationVideoRequest{Session: c.Session, SlideID: c.SlideID, ObjectID: "movie", ClientID: "video-test", Sequence: 2, Playing: true, Time: 1, Rate: 1, Volume: 1}
	deckVersion, build := p.state.DeckVersion, p.BuildSnapshot()
	if w := postVideoPlayback(t, p, input); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	first := p.state.Videos
	input.Sequence, input.Time, input.Playing = 1, 3, false
	if w := postVideoPlayback(t, p, input); w.Code != 200 || !reflect.DeepEqual(first, p.state.Videos) {
		t.Fatal("old command rewound playback", w.Code)
	}
	input.Sequence, input.Time = 3, 2
	if w := postVideoPlayback(t, p, input); w.Code != 200 || p.state.Videos["movie"].Playing || p.state.Videos["movie"].Time != 2 {
		t.Fatal("pause/seek did not reach shared state", w.Code)
	}
	if first["movie"].Time != 1 || p.state.DeckVersion != deckVersion || !reflect.DeepEqual(build, p.BuildSnapshot()) {
		t.Fatal("playback mutated a published snapshot, deck or build")
	}
	for _, invalid := range []presentationVideoRequest{
		{Session: c.Session, SlideID: c.SlideID, ObjectID: "missing", ClientID: "test", Sequence: 1, Rate: 1, Volume: 1},
		{Session: c.Session, SlideID: c.SlideID, ObjectID: "movie", ClientID: "test", Sequence: 1, Rate: 0, Volume: 1},
	} {
		if w := postVideoPlayback(t, p, invalid); w.Code == 200 {
			t.Fatal("accepted invalid playback command")
		}
	}
	p.Update(0, 1, true, nil)
	if len(p.state.Videos) != 0 || postVideoPlayback(t, p, input).Code != 409 {
		t.Fatal("navigation retained or accepted old video state")
	}
	p.Update(0, 0, false, nil)
	if len(p.state.Videos) != 0 || postVideoPlayback(t, p, input).Code != 409 {
		t.Fatal("stopped presentation accepted video commands")
	}
}

func TestVideoPlaybackPresentationPauseRetainsPosition(t *testing.T) {
	p := cursorCompanionFixture()
	defer p.Close()
	p.pages[0][0].Scene.Objects = []sceneObject{{ID: "movie", Kind: "video", Video: &sceneVideo{}}}
	c := p.BuildSnapshot()
	input := presentationVideoRequest{Session: c.Session, SlideID: c.SlideID, ObjectID: "movie", ClientID: "video-test", Sequence: 1, Playing: true, Time: 1, Rate: 1, Volume: 1}
	if w := postVideoPlayback(t, p, input); w.Code != 200 {
		t.Fatal(w.Code)
	}
	p.mu.Lock()
	p.helper, p.target, p.paused = true, "external", true
	p.maintainBuildCursorLocked()
	paused := p.state.Videos["movie"]
	p.paused = false
	p.maintainBuildCursorLocked()
	resumed := p.state.Videos["movie"]
	p.mu.Unlock()
	if paused.Playing || !resumed.Playing || paused.Time != resumed.Time || resumed.Revision <= paused.Revision {
		t.Fatal("presentation pause/resume lost video position", paused, resumed)
	}
}

func TestVideoPresenterBrowser(t *testing.T) {
	if os.Getenv("KEYNOPE_VIDEO_BROWSER") != "1" {
		t.Skip("set KEYNOPE_VIDEO_BROWSER=1 to verify the editor and external-display playback together")
	}
	useTestAuthoredSize(t, 245, 56)
	_, id, asset, path := videoFixture(t)
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{ID: "video-control-slide", PageNumber: pageNumberHide, Notes: "Speaker notes remain available."}, {ID: "second-slide", PageNumber: pageNumberHide, Elements: []Element{{ID: "next-text", Kind: "text", Text: "Next slide", Query: "render=truetype"}}}}}
	video := videoElement(id, path, asset, &deck)
	video.ID, video.Query = "controlled-video", video.Query+"&video-muted=1"
	deck.Slides[0].Elements = []Element{video}
	s := newNativeEditorSession(filepath.Join(t.TempDir(), "Video controls.md"), deck, true)
	server := newGeometryBrowserServer(t, s)
	if err := s.apply(nativeEditorAction{Action: "select-element", Element: 0}); err != nil {
		t.Fatal(err)
	}
	s.companion.Update(0, 0, true, nil)
	runGeometryBrowser(t, "internal/app/testdata/video_presenter.cjs", server.URL)
}
