package app

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func browserRuntimeTestScope(t *testing.T) {
	t.Helper()
	useTestAuthoredSize(t, 245, 56)
	t.Chdir(t.TempDir())
	stills, animations := map[string]DeckAsset{}, map[string]DeckAsset{}
	embeddedStillAssets.RLock()
	for id, asset := range embeddedStillAssets.assets {
		stills[id] = asset
	}
	embeddedStillAssets.RUnlock()
	embeddedAnimatedAssets.RLock()
	for id, asset := range embeddedAnimatedAssets.assets {
		animations[id] = asset
	}
	embeddedAnimatedAssets.RUnlock()
	epoch := browserDocumentEpoch
	t.Cleanup(func() {
		browserDocumentGate.Lock()
		defer browserDocumentGate.Unlock()
		releaseBrowserDocumentCaches(Deck{})
		browserDocumentEpoch = epoch
		for id, asset := range stills {
			registerEmbeddedStillAsset(id, asset)
		}
		for id, asset := range animations {
			registerEmbeddedAnimatedAsset(id, asset)
		}
	})
}

func browserLifetimeDecks(t testing.TB) (Deck, Deck) {
	t.Helper()
	old := powerupDenseEditingDeck(t)
	for index, e := range old.Slides[0].Elements {
		if e.Kind == "image" {
			old.Slides[0].Elements[index].Query = setQueryValue(e.Query, "image-style", "retro")
		}
	}
	photo := workspaceAsset(color.RGBA{R: 40, G: 190, B: 140, A: 255})
	hidden := workspaceAsset(color.RGBA{R: 190, G: 100, B: 40, A: 255})
	movie := DeckAsset{MIME: "image/gif", Width: 8, Height: 8, LoopCount: 2}
	for i := 0; i < 3; i++ {
		frame := workspaceAsset(color.RGBA{R: uint8(50 + i*50), G: 110, B: 180, A: 255})
		movie.Frames = append(movie.Frames, DeckAssetFrame{Data: frame.Data, DelayMS: int64(90 + i*40)})
	}
	next := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Assets: map[string]DeckAsset{"replacement-photo": photo, "replacement-movie": movie, "kept-hidden": hidden}, Masters: MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Base"}, Layouts: []MasterLayout{{ID: "hidden", Name: "Hidden artwork", Slide: Slide{Elements: []Element{{ID: "hidden-image", Kind: "image", AssetID: "kept-hidden", Query: "top=8&left=10&width=50&height=20&image-style=modern"}}}}}}, Slides: []Slide{{ID: "replacement", PageNumber: pageNumberHide, Elements: []Element{
		{ID: "new-title", Kind: "heading", Level: 1, Text: "New document", Query: "render=truetype&modern-font=c64&modern-size=70&top=2&left=10&width=220&height=7"},
		{ID: "new-body", Kind: "text", Text: "Portable pixels and fonts 😍", Query: "render=truetype&modern-font=sans&modern-size=38&top=11&left=10&width=220&height=5"},
		{ID: "new-code", Kind: "code", Text: "print(42)", Query: "render=truetype&modern-font=mono&modern-size=32&top=17&left=10&width=220&height=4"},
		{ID: "new-photo", Kind: "image", AssetID: "replacement-photo", Query: "top=25&left=10&width=55&height=20&image-style=modern&image-fit=contain"},
		{ID: "new-movie", Kind: "image", AssetID: "replacement-movie", Query: "top=25&left=100&width=55&height=20&image-style=retro&image-fit=contain&glyph=blocks"},
	}}}}
	return old, next
}

func TestBrowserDocumentReplacementReleasesOldContentAndKeepsNew(t *testing.T) {
	browserRuntimeTestScope(t)
	old, next := browserLifetimeDecks(t)
	oldRaw, err := serializeDeck("Old.md", old)
	if err != nil {
		t.Fatal(err)
	}
	session, err := replaceBrowserDocument("Old.md", oldRaw, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := editorWorkspacePages(session.deck, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	before := browserDocumentRead(browserDocumentCacheStats)
	if before.Caches["text"].Entries < 40 || before.Caches["tables"].Entries == 0 || before.StillAssets == 0 || before.AnimatedAssets == 0 {
		t.Fatal("old fixture did not warm actual caches", before)
	}
	fonts := modernFontsCSS()
	// A failed candidate with replacement pixels may not publish or clear the
	// working document. Successful validation digests are not document state.
	var oldID string
	for id, asset := range old.Assets {
		if len(asset.Frames) == 0 {
			oldID = id
			break
		}
	}
	identity := embeddedImageCacheIdentity("keynope-asset:" + oldID + ".png")
	revision := embeddedImageRegistrationRevision.Load()
	envelope, err := encodeDeckAssets(map[string]DeckAsset{oldID: workspaceAsset(color.RGBA{R: 255, A: 255})})
	if err != nil {
		t.Fatal(err)
	}
	broken := []byte("<!-- keynope width=80 height=30 -->\n\n" + envelope + "\n\n<!-- element-id=bad object-id=missing -->\n[table]\n")
	if rejected, err := replaceBrowserDocument("Rejected.md", broken, false); err == nil || rejected != nil {
		t.Fatal("invalid document accepted")
	}
	afterFailure := browserDocumentRead(browserDocumentCacheStats)
	if afterFailure.Epoch != before.Epoch || afterFailure.Caches["text"] != before.Caches["text"] || embeddedImageRegistrationRevision.Load() != revision || embeddedImageCacheIdentity("keynope-asset:"+oldID+".png") != identity || authoredTerminalWidth != 245 || authoredTerminalHeight != 56 {
		t.Fatal("failed Open replaced runtime", before, afterFailure)
	}
	newRaw, err := serializeDeck("New.md", next)
	if err != nil {
		t.Fatal(err)
	}
	current, err := replaceBrowserDocument("New.md", newRaw, false)
	if err != nil {
		t.Fatal(err)
	}
	clean := browserDocumentRead(browserDocumentCacheStats)
	if clean.Epoch != before.Epoch+1 || clean.StillAssets != 2 || clean.AnimatedAssets != 1 {
		t.Fatal("registration lifetime", clean)
	}
	for name, cache := range clean.Caches {
		if cache.Entries != 0 || cache.Bytes != 0 {
			t.Fatal("old cache retained", name, cache)
		}
	}
	if _, exists := embeddedStillAsset("keynope-asset:" + oldID + ".png"); exists {
		t.Fatal("old source still reachable")
	}
	if modernFontsCSS() != fonts {
		t.Fatal("bundled fonts changed during cleanup")
	}
	scene, err := buildDocumentSlideScene(current.deck, 0, 245, 56)
	if err != nil || len(scene.Objects) != 5 {
		t.Fatal("new document did not render", err)
	}
	if _, exists := embeddedStillAsset("keynope-asset:kept-hidden.png"); !exists {
		t.Fatal("unshown master source discarded")
	}
	if animation, exists := embeddedAnimatedAsset("keynope-asset:replacement-movie.gif"); !exists || animation.Frames[1].DelayMS != 130 {
		t.Fatal("new animation timing lost")
	}
	beforeDeck := cloneDeck(current.deck)
	size := json.RawMessage(`88`)
	commandRevision := current.version
	if err := current.apply(nativeEditorAction{Action: "set-selection-property", Slide: 0, ObjectIDs: []string{"new-title"}, Property: "text.size", PropertyValue: size, SceneRevision: &commandRevision}); err != nil {
		t.Fatal(err)
	}
	if err := current.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(current.deck, beforeDeck) {
		t.Fatal("new document Undo failed", err)
	}
	if err := current.apply(nativeEditorAction{Action: "redo"}); err != nil {
		t.Fatal(err)
	}
	saved, err := serializeDeck("Saved.md", current.deck)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "browserDocumentEpoch") || strings.Contains(string(saved), "runtimeCaches") {
		t.Fatal("runtime counters entered Markdown")
	}
	if reopened, err := replaceBrowserDocument("Saved.md", saved, false); err != nil || reopened.deck.Slides[0].Elements[0].Text != "New document" {
		t.Fatal("save/reopen failed", err)
	}
}

func TestBrowserDocumentReplacementWaitsForActiveWork(t *testing.T) {
	browserRuntimeTestScope(t)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		browserDocumentRead(func() bool {
			sharedSceneSources.uri("image/png", []byte("old before"))
			close(started)
			<-release
			sharedSceneSources.uri("image/png", []byte("old late"))
			return true
		})
		close(finished)
	}()
	<-started
	replaced := make(chan error, 1)
	attempted := make(chan struct{})
	go func() {
		close(attempted)
		_, err := replaceBrowserDocument("Empty new.md", []byte("# New\n"), false)
		replaced <- err
	}()
	<-attempted
	select {
	case err := <-replaced:
		close(release)
		t.Fatal("replacement crossed an active render", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	<-finished
	select {
	case err := <-replaced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replacement deadlocked")
	}
	if state := browserDocumentRead(browserDocumentCacheStats); state.Caches["sources"].Entries != 0 {
		t.Fatal("old work repopulated released storage", state)
	}
}

func TestBrowserDocumentLifecycleFixtures(t *testing.T) {
	if root := os.Getenv("KEYNOPE_DOCUMENT_LIFETIME_EVIDENCE"); root != "" {
		useTestAuthoredSize(t, 245, 56)
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		old, next := browserLifetimeDecks(t)
		for name, deck := range map[string]Deck{"old.md": old, "new.md": next} {
			raw, err := serializeDeck(name, deck)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "<!-- keynope width=245 height=56 -->") {
				t.Fatal("unstable proof canvas")
			}
			if err := os.WriteFile(filepath.Join(root, name), raw, 0644); err != nil {
				t.Fatal(err)
			}
		}
		broken := []byte("<!-- keynope width=80 height=30 -->\n\n<!-- element-id=bad object-id=missing -->\n[table]\n")
		if err := os.WriteFile(filepath.Join(root, "rejected.md"), broken, 0644); err != nil {
			t.Fatal(err)
		}
		manifest := map[string]int{"oldObjects": 49, "newObjects": 5, "newStillAssets": 2, "newAnimatedAssets": 1}
		raw, _ := json.Marshal(manifest)
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
