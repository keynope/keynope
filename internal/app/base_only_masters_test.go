package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func baseOnlyMasterFixture(t *testing.T) Deck {
	t.Helper()
	runs := []sceneRun{{Text: "Workshop Base", FontID: "mono", FontRole: "heading", Color: "#55aaff", Bold: true}}
	base := Slide{ID: "base-slide", BG: "48;2;12;22;32", FG: "38;2;238;238;238", BGSet: true, FGSet: true, PageNumber: pageNumberHide, Effect: "fireworks", EffectSet: true, Elements: []Element{withConvertedRuns(Element{ID: "base-brand", Kind: "text", Query: "render=truetype&modern-font=mono&modern-size=60&left=20&top=4&width=190&height=8"}, runs)}, Extra: &jsonExtensions{"futureBase": json.RawMessage(`true`)}}
	return Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-dark"), Masters: MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Workshop Branding", Slide: base, Extra: &jsonExtensions{"futureLayout": json.RawMessage(`"keep"`)}}, Extra: &jsonExtensions{"futureMasters": json.RawMessage(`1`)}}, Slides: []Slide{{ID: "slide", Elements: []Element{{ID: "body", Kind: "text", Text: "Slide body", Query: "render=truetype&modern-font=sans&modern-size=60&left=20&top=24&width=190&height=10"}}}}}
}

func TestEnsureDefaultMastersPreservesAuthoredBaseOnly(t *testing.T) {
	for _, scenario := range []string{"complete", "empty-archive", "style-only", "extensions-only"} {
		t.Run(scenario, func(t *testing.T) {
			d := baseOnlyMasterFixture(t)
			switch scenario {
			case "empty-archive":
				d.Masters = MasterDeck{Version: 1, Base: MasterLayout{ID: "base", Name: "Intentionally empty"}}
			case "style-only":
				d.Masters = MasterDeck{Base: MasterLayout{Slide: Slide{BG: "48;2;255;0;85", BGSet: true}}}
			case "extensions-only":
				d.Masters = MasterDeck{Extra: &jsonExtensions{"futureMasters": json.RawMessage(`true`)}}
			}
			before := cloneDeck(d)
			if !deckHasMasterData(d.Masters) {
				t.Fatal("authored master data treated as absent", scenario)
			}
			d.EnsureDefaultMasters()
			d.EnsureDefaultMasters()
			if !reflect.DeepEqual(d, before) {
				t.Fatal("authored master overwritten or default layouts injected", scenario, d.Masters, before.Masters)
			}
		})
	}
	var nilDeck *Deck
	nilDeck.EnsureDefaultMasters()
	d := Deck{}
	d.EnsureDefaultMasters()
	if !reflect.DeepEqual(d.Masters, defaultMasterDeck()) {
		t.Fatal("absent master archive didn't receive defaults")
	}
}

func TestBaseOnlyMasterSessionHistoryPersistenceAndNewSlides(t *testing.T) {
	d := baseOnlyMasterFixture(t)
	md, err := serializeDeck("Base only.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("Base only.md", md)
	if err != nil {
		t.Fatal(err)
	}
	s := newNativeEditorSession("Base only.md", loaded)
	if len(s.deck.Masters.Layouts) != 0 || s.deck.Masters.Base.Name != "Workshop Branding" || len(s.deck.Masters.Base.Slide.Elements) != 1 || s.deck.Masters.Base.Slide.Elements[0].ID != "base-brand" || s.deck.Masters.Base.Slide.BG != d.Masters.Base.Slide.BG || s.deck.Masters.Base.Slide.Effect != "fireworks" {
		t.Fatal("session replaced Base-only archive", s.deck.Masters)
	}
	if s.dirtyLocked() || len(s.undo) != 0 {
		t.Fatal("opening Base-only deck dirtied it")
	}
	before := cloneDeck(s.deck)
	for _, action := range []nativeEditorAction{{Action: "toggle-master-mode"}, {Action: "select-slide", Slide: 0}, {Action: "toggle-master-mode"}, {Action: "select-slide", Slide: 0}} {
		if err := s.apply(action); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.deck, before) || s.dirtyLocked() || len(s.undo) != 0 {
			t.Fatal("master navigation changed Base-only metadata", action)
		}
	}
	if err := s.apply(nativeEditorAction{Action: "add-slide"}); err != nil {
		t.Fatal(err)
	}
	if len(s.deck.Slides) != 2 || !reflect.DeepEqual(s.deck.Masters, before.Masters) || len(s.undo) != 1 {
		t.Fatal("new slide replaced Base-only master")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) || s.dirtyLocked() {
		t.Fatal("add slide undo changed master or saved baseline", err)
	}
	if err := s.apply(nativeEditorAction{Action: "add-layout", Name: "First authored layout"}); err != nil {
		t.Fatal(err)
	}
	if len(s.deck.Masters.Layouts) != 1 || s.deck.Masters.Layouts[0].Name != "First authored layout" || !reflect.DeepEqual(s.deck.Masters.Base, before.Masters.Base) {
		t.Fatal("first layout overwrote Base")
	}
	// Unbound legacy/new slides intentionally do not project master artwork.
	// Once a layout is explicitly assigned, its Base must project as before.
	if err := s.apply(nativeEditorAction{Action: "set-layout", Kind: s.deck.Masters.Layouts[0].ID}); err != nil {
		t.Fatal(err)
	}
	resolved := s.deck.ResolveSlide(0, false)
	found := false
	for _, e := range resolved.Elements {
		if e.ID == "base-brand" || e.MasterSlotID == "base-brand" {
			found = true
		}
	}
	if !found || resolved.BG != before.Masters.Base.Slide.BG {
		t.Fatal("assigned layout lost authored Base content/style", resolved)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("layout undo lost Base-only state", err)
	}
	if err := s.apply(nativeEditorAction{Action: "toggle-master-mode"}); err != nil {
		t.Fatal(err)
	}
	version := s.version
	if err := s.apply(nativeEditorAction{Action: "set-selection-property", SceneMaster: true, SceneRevision: &version, Slide: 0, ObjectIDs: []string{"base-brand"}, Property: "paint.opacity", PropertyValue: json.RawMessage(`0.6`)}); err != nil {
		t.Fatal(err)
	}
	after := cloneDeck(s.deck)
	if mustQuery(after.Masters.Base.Slide.Elements[0].Query).Get("modern-opacity") != "0.6" || len(after.Masters.Layouts) != 0 {
		t.Fatal("Base edit unavailable/lost ownership")
	}
	if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
		t.Fatal("Base edit undo failed", err)
	}
	if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(s.deck, after) {
		t.Fatal("Base edit redo failed", err)
	}
	saved, err := serializeDeck("Saved base.md", s.deck)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Saved base.md", saved)
	if err != nil {
		t.Fatal(err)
	}
	other := newNativeEditorSession("Saved base.md", reopened)
	if len(other.deck.Masters.Layouts) != 0 || !reflect.DeepEqual(other.deck.Masters, s.deck.Masters) || other.dirtyLocked() {
		t.Fatal("saved/reopened Base-only deck replaced or dirty")
	}
	if root := os.Getenv("KEYNOPE_BASE_ONLY_EVIDENCE"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "base-only.md"), md, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
