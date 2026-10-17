package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func themeDependencyFixture(kind string, explicit bool) Deck {
	query := "top=4&left=10&width=180&height=20&render=truetype"
	if kind == "shape" {
		query = "top=4&left=10&width=70&height=25&shape=round-rectangle"
	}
	if explicit {
		query += "&fg=%23112233&modern-font=mono&modern-size=64&modern-width=100&modern-line-height=1.25&ttf-size=64&ttf-width=100&shape-stroke=%23445566&shape-stroke-width=4"
	}
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Slides: []Slide{{ID: "one", Elements: []Element{{ID: "object", Kind: kind, Level: 1, Text: "Visible consumer", Query: query}}}}}
	if explicit {
		d.Slides[0].FG, d.Slides[0].BG, d.Slides[0].HeaderFG = "#112233", "#eeeecc", "#445566"
		d.Slides[0].FGSet, d.Slides[0].BGSet, d.Slides[0].HeaderFGSet = true, true, true
	}
	return d
}

func TestSceneThemeConsumerDependencies(t *testing.T) {
	tests := []struct {
		name, kind      string
		explicit, reuse bool
		edit            func(*DeckTheme)
	}{
		{"name-only", "text", false, true, func(t *DeckTheme) { t.Name = "Renamed" }},
		{"unused-accent", "text", false, true, func(t *DeckTheme) { t.Colors.Accent[5] = "#ff00aa" }},
		{"unused-font", "text", false, true, func(t *DeckTheme) { t.Fonts.Mono.Size += 10 }},
		{"unused-stroke", "text", false, true, func(t *DeckTheme) { t.Defaults.LineWidth += 2 }},
		{"body-font", "text", false, false, func(t *DeckTheme) { t.Fonts.Body.Size += 10 }},
		{"heading-font", "heading", false, false, func(t *DeckTheme) { t.Fonts.Heading.Width += 10 }},
		{"mono-font", "code", false, false, func(t *DeckTheme) { t.Fonts.Mono.Size += 10 }},
		{"explicit-body", "text", true, true, func(t *DeckTheme) { t.Fonts.Body.Size += 10; t.Fonts.Body.FontID = "c64" }},
		{"canvas-background", "text", false, false, func(t *DeckTheme) { t.Colors.Background = "#ff00aa" }},
		{"explicit-canvas", "text", true, true, func(t *DeckTheme) { t.Colors.Background = "#ff00aa" }},
		{"inherited-line", "shape", false, false, func(t *DeckTheme) { t.Defaults.ShapeLine = "#ff00aa" }},
		{"inherited-line-width", "shape", false, false, func(t *DeckTheme) { t.Defaults.LineWidth = 9 }},
		{"explicit-line", "shape", true, true, func(t *DeckTheme) { t.Defaults.ShapeLine = "#ff00aa"; t.Defaults.LineWidth = 9 }},
		{"latent-label-font", "shape", true, false, func(t *DeckTheme) { t.Fonts.Body.Size += 10 }},
		{"unknown-extension", "text", false, false, func(t *DeckTheme) { t.Extra = &jsonExtensions{"future": json.RawMessage(`true`)} }},
		{"unknown-colour-extension", "text", false, false, func(t *DeckTheme) { t.Colors.Extra = &jsonExtensions{"future": json.RawMessage(`true`)} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := themeDependencyFixture(test.kind, test.explicit)
			s := newNativeEditorSession("Theme.md", before)
			before = cloneDeck(s.deck)
			get := func(cold bool) []byte {
				path := "/api/editor/scene?slide=0"
				if cold {
					path += "&timing=1"
				}
				w := httptest.NewRecorder()
				s.handleScene(w, httptest.NewRequest("GET", path, nil))
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				if cold && w.Header().Get("X-Keynope-Scene-Cache") != "bypass" {
					t.Fatal("profiling did not bypass scene cache")
				}
				return w.Body.Bytes()
			}
			get(false)
			original := s.sceneResponse
			if original == nil {
				t.Fatal("missing response cache")
			}
			theme := s.deck.Theme.Clone()
			test.edit(theme)
			if err := s.apply(nativeEditorAction{Action: "set-deck-theme", ThemeData: theme}); err != nil {
				t.Fatal(err)
			}
			unchanged := cloneDeck(s.deck)
			reused := &s.sceneResponse.data[0] == &original.data[0] && s.sceneResponse.key.revision == s.version
			if reused != test.reuse {
				t.Fatalf("retained=%v, want %v", reused, test.reuse)
			}
			warm, cold := get(false), get(true)
			if !bytes.Equal(warm, cold) {
				t.Fatal("retained response differs from cold production projection")
			}
			if !reflect.DeepEqual(s.deck, unchanged) {
				t.Fatal("consumer comparison mutated authored state")
			}
			if original.key.revision == s.version {
				t.Fatal("old reader envelope mutated")
			}
			if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, s.deck) {
				t.Fatal("theme history lost original deck")
			}
			if !bytes.Equal(get(false), get(true)) {
				t.Fatal("history retained stale projection")
			}
		})
	}
}

func TestSceneThemeRoleConsumersAndMasterPreview(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, role := range []string{"text", "heading", "background", "surface", "muted", "line", "accent-1", "accent-2", "accent-3", "accent-4", "accent-5", "accent-6"} {
			d := themeDependencyFixture("text", false)
			d.Slides[0].Elements[0].Query += "&theme-fg=" + role
			d.EnsureDefaultMasters()
			if master {
				d.Masters.Base.Slide = cloneSlide(d.Slides[0])
				d.Masters.Base.Slide.ID = "master-slide"
			}
			changed := cloneDeck(d)
			c := &changed.Theme.Colors
			switch role {
			case "text":
				c.Text = "#ff00aa"
			case "heading":
				c.Heading = "#ff00aa"
			case "background":
				c.Background = "#ff00aa"
			case "surface":
				c.Surface = "#ff00aa"
			case "muted":
				c.Muted = "#ff00aa"
			case "line":
				c.Line = "#ff00aa"
			default:
				var index int
				fmt.Sscanf(role, "accent-%d", &index)
				c.Accent[index-1] = "#ff00aa"
			}
			if sceneProjectionInputsEqual(d, changed, sceneResponseKey{index: 0, master: master}) {
				t.Fatal("lost role consumer", role, master)
			}
			changed = cloneDeck(d)
			changed.Theme.Fonts.Mono.Size += 10
			if !sceneProjectionInputsEqual(d, changed, sceneResponseKey{index: 0, master: master}) {
				t.Fatal("unused font invalidated role consumer", role, master)
			}
		}
	}
	// Structured/unknown adapters and compatibility transitions remain safe.
	for _, kind := range []string{"table", "chart", "future"} {
		d := themeDependencyFixture(kind, false)
		after := cloneDeck(d)
		after.Theme.Colors.Accent[5] = "#ff00aa"
		if sceneProjectionInputsEqual(d, after, sceneResponseKey{index: 0}) {
			t.Fatal("unrecorded adapter reused", kind)
		}
	}
	d := themeDependencyFixture("text", false)
	after := cloneDeck(d)
	after.Theme = nil
	if sceneProjectionInputsEqual(d, after, sceneResponseKey{index: 0}) {
		t.Fatal("theme removal reused")
	}
}

func TestSceneThemeDependencyFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_THEME_DEPENDENCY_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	d := themeDependencyFixture("text", false)
	d.Slides[0].Elements = nil
	for i := 0; i < 40; i++ {
		q := fmt.Sprintf("left=%d&top=%d&width=40&height=4&render=truetype&modern-size=28", 6+i%5*47, 3+i/5*6)
		if i == 0 {
			q += "&theme-fg=accent-1"
		}
		d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: fmt.Sprintf("text-%d", i), Kind: "text", Text: fmt.Sprintf("Object %02d", i), Query: q})
	}
	d.Slides = append(d.Slides, Slide{ID: "second", Elements: []Element{{ID: "mono", Kind: "code", Text: "Unseen consumer", Query: "top=5&left=10&width=220&height=10&render=truetype&theme-fg=accent-6"}}})
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "consumers.md"), d); err != nil {
		t.Fatal(err)
	}
}

// Retention is allowed only when a fresh, fully annotated production response
// is identical. Sweep independent palette/typography/default changes through
// ordinary, labelled and master contexts, not only the positive cache cases.
func TestSceneThemeDifferentialColdParity(t *testing.T) {
	edits := []func(*DeckTheme){
		func(t *DeckTheme) { t.Name = "Other name" },
		func(t *DeckTheme) { t.Colors.Background = "#121212" },
		func(t *DeckTheme) { t.Colors.Text = "#ff0055" },
		func(t *DeckTheme) { t.Colors.Heading = "#0055ff" },
		func(t *DeckTheme) { t.Colors.Surface = "#00ff55" },
		func(t *DeckTheme) { t.Colors.Muted = "#55ff00" },
		func(t *DeckTheme) { t.Colors.Line = "#5500ff" },
		func(t *DeckTheme) { t.Defaults.ShapeFill = "#aaff55" },
		func(t *DeckTheme) { t.Defaults.ShapeLine = "#55aaff" },
		func(t *DeckTheme) { t.Defaults.LineWidth = 0 },
		func(t *DeckTheme) { t.Defaults.LineWidth = 12 },
	}
	for index := 0; index < 6; index++ {
		i := index
		edits = append(edits, func(t *DeckTheme) { t.Colors.Accent[i] = "#ff00aa" })
	}
	for index := 0; index < 3; index++ {
		i := index
		font := func(t *DeckTheme) *ThemeFont {
			switch i {
			case 0:
				return &t.Fonts.Body
			case 1:
				return &t.Fonts.Heading
			default:
				return &t.Fonts.Mono
			}
		}
		edits = append(edits, func(t *DeckTheme) { font(t).FontID = "c64" }, func(t *DeckTheme) { font(t).Size += 20 }, func(t *DeckTheme) { font(t).Width = 75 }, func(t *DeckTheme) { font(t).LineHeight = 2 })
	}
	retained := 0
	for _, kind := range []string{"text", "heading", "bullet", "code", "text-image", "shape", "labelled-shape"} {
		for _, explicit := range []bool{false, true} {
			d := themeDependencyFixture(kind, explicit)
			if kind == "labelled-shape" {
				d.Slides[0].Elements[0].Kind = "shape"
				label, _ := json.Marshal(shapeLabelData{Text: "Label", Query: "render=truetype&theme-fg=accent-2"})
				d.Slides[0].Elements[0].Query += "&shape=square&shape-label=" + base64.StdEncoding.EncodeToString(label)
			}
			d.EnsureDefaultMasters()
			d.Masters.Base.Slide = cloneSlide(d.Slides[0])
			d.Masters.Base.Slide.ID = "base-proof"
			for _, master := range []bool{false, true} {
				key := sceneResponseKey{index: 0, cols: 245, rows: 56, master: master}
				project := func(d Deck) []byte {
					s := newNativeEditorSession("Differential.md", d)
					s.masterMode = master
					w := httptest.NewRecorder()
					s.handleScene(w, httptest.NewRequest("GET", "/api/editor/scene?slide=0&timing=1", nil))
					if w.Code != 200 {
						t.Fatal(kind, w.Code, w.Body.String())
					}
					return w.Body.Bytes()
				}
				original := project(d)
				for editIndex, edit := range edits {
					after := cloneDeck(d)
					edit(after.Theme)
					if sceneProjectionInputsEqual(d, after, key) {
						retained++
						if !bytes.Equal(original, project(after)) {
							t.Fatalf("stale retained scene kind=%s explicit=%v master=%v edit=%d", kind, explicit, master, editIndex)
						}
					}
				}
			}
		}
	}
	if retained < 100 {
		t.Fatal("sweep did not exercise reuse", retained)
	}
}

func BenchmarkSceneThemeUnusedRoleComparison(b *testing.B) {
	d := themeDependencyFixture("text", false)
	for i := 1; i < 500; i++ {
		e := d.Slides[0].Elements[0]
		e.ID = fmt.Sprintf("object-%d", i)
		d.Slides[0].Elements = append(d.Slides[0].Elements, e)
	}
	after := cloneDeck(d)
	after.Theme.Colors.Accent[5] = "#ff00aa"
	key := sceneResponseKey{index: 0, cols: 245, rows: 56}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !sceneProjectionInputsEqual(d, after, key) {
			b.Fatal("unused role invalidated")
		}
	}
}

func TestWorkspaceThemeConsumerDeltasAndColdParity(t *testing.T) {
	d := themeDependencyFixture("text", false)
	d.Slides[0].Elements[0].Query += "&theme-fg=accent-1"
	d.Slides = append(d.Slides, Slide{ID: "mono-slide", Elements: []Element{{ID: "mono", Kind: "code", Text: "Other consumer", Query: "render=truetype&theme-fg=accent-6"}}})
	var cache workspacePageCache
	if _, err := cache.render(d, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []struct {
		change  func(*DeckTheme)
		changed []int
	}{
		{func(t *DeckTheme) { t.Name = "Renamed" }, nil},
		{func(t *DeckTheme) { t.Colors.Accent[5] = "#ff00aa" }, []int{1}},
		{func(t *DeckTheme) { t.Colors.Accent[0] = "#aa00ff" }, []int{0}},
		{func(t *DeckTheme) { t.Fonts.Mono.Size += 10 }, []int{1}},
		{func(t *DeckTheme) { t.Fonts.Body.Width = 110 }, []int{0}},
		{func(t *DeckTheme) { t.Defaults.ShapeFill = "#111222" }, nil},
	} {
		edit.change(d.Theme)
		got, err := cache.render(d, false, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := editorWorkspacePages(d, false, 0, 245, 56)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, fresh) {
			t.Fatal("theme-scoped workspace differs from uncached pages")
		}
		if !reflect.DeepEqual(cache.changed, edit.changed) {
			t.Fatalf("changed slides %v, want %v", cache.changed, edit.changed)
		}
	}
}
