package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func masterThumbnailFixture() Deck {
	d := workspaceConsumerFixture()
	d.Masters.Base.Slide.ID = "preview-base"
	d.Masters.Layouts[0].Slide.ID = "preview-alpha"
	d.Masters.Layouts[1].Slide.ID = "preview-beta"
	d.Masters.Layouts[2].Slide.ID = "preview-unused"
	d.Masters.Layouts[0].Slide.Elements = []Element{
		{ID: "alpha-text", Kind: "text", Text: "Alpha content", Query: "render=truetype&top=8&left=8&modern-size=64"},
		{ID: "alpha-table", Kind: "table", ObjectID: "shared-table", Query: "top=18&left=8&width=100&height=20"},
		{ID: "alpha-image", Kind: "image", AssetID: "asset-a", Query: "image-style=modern&top=8&left=160&width=30&height=12"},
	}
	d.Masters.Layouts[1].Slide.Elements = []Element{{ID: "beta-text", Kind: "text", Text: "Beta content", Query: "render=truetype&top=8&left=8&modern-size=64"}}
	d.Masters.Layouts[2].Slide.Elements = []Element{{ID: "unused-text", Kind: "text", Text: "Another layout", Query: "render=truetype&top=8&left=8&modern-size=64"}}
	return d
}

func readMasterThumbnail(t *testing.T, s *nativeEditorSession, index int, stamp *int64, owner string) []exportPage {
	t.Helper()
	query := fmt.Sprintf("master=%d&cols=245&rows=56", index)
	if stamp != nil {
		query += fmt.Sprintf("&previewRevision=%d&owner=%s", *stamp, url.QueryEscape(owner))
	}
	w := httptest.NewRecorder()
	s.handleWorkspace(w, httptest.NewRequest("GET", "/api/editor/workspace?"+query, nil))
	var pages []exportPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &pages) != nil {
		t.Fatal("thumbnail", index, w.Code, w.Body.String())
	}
	return pages
}

func TestMasterThumbnailCommandGenerationsAndColdParity(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	s := newNativeEditorSession("Master previews.md", masterThumbnailFixture())
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= len(s.deck.Masters.Layouts); i++ {
		readMasterThumbnail(t, s, i, nil, "")
	}
	check := func(name string, action nativeEditorAction, changed []int) {
		t.Helper()
		before := s.transportState().MasterPreviewRevisions
		if err := s.apply(action); err != nil {
			t.Fatal(name, err)
		}
		after := s.transportState().MasterPreviewRevisions
		var got []int
		for i := range after {
			if before[i] != after[i] {
				got = append(got, i)
			}
		}
		if !reflect.DeepEqual(got, changed) {
			t.Fatalf("%s invalidated %v want %v", name, got, changed)
		}
		deckBefore := cloneDeck(s.deck)
		for i := range after {
			warm := readMasterThumbnail(t, s, i, &after[i], s.notesSessionID)
			cold, err := editorWorkspacePages(cloneDeckForRender(s.deck), true, i, 245, 56)
			var normalized []exportPage
			raw, _ := json.Marshal(cold)
			_ = json.Unmarshal(raw, &normalized)
			if err != nil || !reflect.DeepEqual(warm, normalized) {
				t.Fatal(name, "cold parity", i, err)
			}
		}
		if !reflect.DeepEqual(deckBefore, s.deck) {
			t.Fatal(name, "rendering mutated document")
		}
	}
	check("select", nativeEditorAction{Action: "select-slide", Slide: 1}, nil)
	element := cloneElement(s.deck.Masters.Layouts[0].Slide.Elements[0])
	element.Text = "Edited Alpha"
	check("edit alpha", nativeEditorAction{Action: "update-element", Element: 0, ElementData: &element}, []int{1})
	check("equal replay", nativeEditorAction{Action: "update-element", Element: 0, ElementData: &element}, nil)
	check("undo", nativeEditorAction{Action: "undo"}, []int{1})
	check("redo", nativeEditorAction{Action: "redo"}, []int{1})
	theme := s.deck.Theme.Clone()
	theme.Colors.Accent[5] = "#112233"
	check("unused theme", nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}, nil)
	theme = s.deck.Theme.Clone()
	theme.Fonts.Body.Width = 83
	check("body font", nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}, []int{0, 1, 2, 3})
	check("select base", nativeEditorAction{Action: "select-slide", Slide: 0}, nil)
	element = cloneElement(s.deck.Masters.Base.Slide.Elements[0])
	element.Text = "New Base"
	check("base content", nativeEditorAction{Action: "update-element", Element: 0, ElementData: &element}, []int{0, 1, 2, 3})
	before := s.transportState().MasterPreviewRevisions
	bad := Element{Kind: " "}
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: 999, ElementData: &bad}); err == nil {
		t.Fatal("invalid edit accepted")
	}
	if !reflect.DeepEqual(before, s.transportState().MasterPreviewRevisions) {
		t.Fatal("rejection changed generations")
	}
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	if len(s.masterPreviews) != 0 || s.transportState().MasterPreviewRevisions != nil {
		t.Fatal("normal editing retained master work")
	}
}

func TestMasterThumbnailPayloadAndStructuralConsumers(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	tests := []struct {
		name    string
		edit    func(*Deck)
		changed []int
	}{
		{"normal text", func(d *Deck) { d.Slides[0].Elements[0].Text = "Normal edit" }, nil},
		{"private notes", func(d *Deck) { d.Masters.Base.Slide.Notes = "Private" }, nil},
		{"unused media", func(d *Deck) { d.Assets["unused"] = workspaceAsset(color.RGBA{R: 80, A: 255}) }, nil},
		{"used image", func(d *Deck) { d.Assets["asset-a"] = workspaceAsset(color.RGBA{R: 80, A: 255}) }, []int{1}},
		{"used backdrop", func(d *Deck) { d.Assets["backdrop"] = workspaceAsset(color.RGBA{R: 80, A: 255}) }, []int{1}},
		{"used table", func(d *Deck) {
			data := d.Objects.Tables["shared-table"]
			data.Cells[1][1].Text = "Updated data"
			d.Objects.Tables["shared-table"] = data
		}, []int{1}},
		{"other master", func(d *Deck) { d.Masters.Layouts[1].Slide.Elements[0].Text = "Changed Beta" }, []int{2}},
		{"reorder", func(d *Deck) { d.Masters.Layouts[0], d.Masters.Layouts[1] = d.Masters.Layouts[1], d.Masters.Layouts[0] }, []int{1, 2}},
		{"delete", func(d *Deck) { d.Masters.Layouts = d.Masters.Layouts[:2] }, []int{0, 1, 2}},
		{"unknown extension", func(d *Deck) { d.Extra = &jsonExtensions{"future": json.RawMessage(`true`)} }, []int{0, 1, 2, 3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newNativeEditorSession("Consumers.md", masterThumbnailFixture())
			s.masterMode = true
			for i := 0; i < 4; i++ {
				readMasterThumbnail(t, s, i, nil, "")
			}
			before := cloneDeck(s.deck)
			stamps := s.masterPreviewRevisionsLocked()
			test.edit(&s.deck)
			old := s.version
			s.version++
			s.invalidateMasterPreviewsLocked(before, old)
			after := s.masterPreviewRevisionsLocked()
			var changed []int
			for i := range after {
				if after[i] != stamps[i] {
					changed = append(changed, i)
				} else {
					a, err := editorWorkspacePages(before, true, i, 245, 56)
					b, other := editorWorkspacePages(s.deck, true, i, 245, 56)
					if err != nil || other != nil || !reflect.DeepEqual(a, b) {
						t.Fatal("unchanged token hid different output", i, err, other)
					}
				}
			}
			if !reflect.DeepEqual(changed, test.changed) {
				t.Fatal("consumers", changed, "want", test.changed)
			}
		})
	}
}

func TestMasterThumbnailRequestOwnershipAndBounds(t *testing.T) {
	s := newNativeEditorSession("Owner.md", masterThumbnailFixture())
	s.masterMode = true
	state := s.transportState()
	stamp := state.MasterPreviewRevisions[1]
	readMasterThumbnail(t, s, 1, &stamp, s.notesSessionID)
	if err := s.apply(nativeEditorAction{Action: "select-slide", Slide: 2}); err != nil {
		t.Fatal(err)
	}
	readMasterThumbnail(t, s, 1, &stamp, s.notesSessionID) // View-only revision is allowed.
	for _, query := range []string{
		fmt.Sprintf("master=1&previewRevision=%d&owner=wrong", stamp),
		fmt.Sprintf("master=1&previewRevision=%d&owner=%s", stamp+100, s.notesSessionID),
		"master=1&previewRevision=broken",
	} {
		w := httptest.NewRecorder()
		s.handleWorkspace(w, httptest.NewRequest("GET", "/api/editor/workspace?"+query, nil))
		if w.Code != 409 {
			t.Fatal("invalid stamp/owner accepted", w.Code)
		}
	}
	state.MasterPreviewRevisions[1] = -100
	if s.transportState().MasterPreviewRevisions[1] == -100 {
		t.Fatal("state tokens alias session")
	}
	raw, err := serializeDeck("Owner.md", s.deck)
	if err != nil || strings.Contains(string(raw), "masterPreview") {
		t.Fatal("runtime tokens entered deck", err)
	}
	for len(s.deck.Masters.Layouts) < maxTrackedMasterPreviews+10 {
		s.deck.Masters.Layouts = append(s.deck.Masters.Layouts, MasterLayout{ID: fmt.Sprint("extra-", len(s.deck.Masters.Layouts))})
	}
	for i := 0; i <= len(s.deck.Masters.Layouts); i++ {
		s.trackMasterPreviewLocked(i, 245, 56)
		s.trackMasterPreviewLocked(1, 245, 56)
	}
	if len(s.masterPreviews) != maxTrackedMasterPreviews {
		t.Fatal("unbounded preview tracking")
	}
	if _, exists := s.masterPreviews[0]; exists {
		t.Fatal("old unused preview not evicted")
	}
	if _, exists := s.masterPreviews[1]; !exists {
		t.Fatal("hot preview evicted")
	}
}

func TestMasterThumbnailBrowserFixture(t *testing.T) {
	if root := os.Getenv("KEYNOPE_MASTER_THUMBNAIL_EVIDENCE"); root != "" {
		useTestAuthoredSize(t, 245, 56)
		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		raw, err := serializeDeck("Master thumbnails.md", masterThumbnailFixture())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "<!-- keynope width=245 height=56 -->") {
			t.Fatal("browser fixture inherited another test's canvas")
		}
		if err := os.WriteFile(filepath.Join(root, "masters.md"), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMasterThumbnailConcurrentViewChanges(t *testing.T) {
	s := newNativeEditorSession("Concurrent previews.md", masterThumbnailFixture())
	s.masterMode = true
	for i := 0; i < 4; i++ {
		readMasterThumbnail(t, s, i, nil, "")
	}
	stamps, owner := s.transportState().MasterPreviewRevisions, s.notesSessionID
	var workers sync.WaitGroup
	for index := 0; index < 4; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			for n := 0; n < 8; n++ {
				w := httptest.NewRecorder()
				query := fmt.Sprintf("/api/editor/workspace?master=%d&previewRevision=%d&owner=%s&cols=245&rows=56", index, stamps[index], owner)
				s.handleWorkspace(w, httptest.NewRequest("GET", query, nil))
				if w.Code != 200 {
					t.Errorf("view-only concurrency rejected preview %d: %d", index, w.Code)
				}
			}
		}(index)
	}
	for n := 0; n < 16; n++ {
		if err := s.apply(nativeEditorAction{Action: "select-slide", Slide: n % 4}); err != nil {
			t.Fatal(err)
		}
	}
	workers.Wait()
	if !reflect.DeepEqual(stamps, s.transportState().MasterPreviewRevisions) {
		t.Fatal("view-only concurrency invalidated previews")
	}
}
