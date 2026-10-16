package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Own the document before calling this. Also migrates older decks that embedded
// restricted faces, including references in masters, rich runs and chart/table
// data. The runtime font metrics can remain available without storing the font.
func normalizeTypefaceReferences(deck *Deck) {
	replacements := map[string]string{}
	fonts := make(map[string]DeckTypeface, len(deck.Typefaces))
	for oldID, face := range deck.Typefaces {
		if !face.LocalOnly && face.referenceOnly() {
			face.EmbeddingFlags = typefaceEmbeddingFlags(face.Data)
			face.LocalOnly, face.Data = true, nil
		}
		id := face.ID()
		fonts[id] = face
		if oldID != id {
			replacements[oldID] = id
		}
	}
	if len(replacements) != 0 {
		replaceExportTypefaces(deck, replacements)
	}
	if len(deck.Typefaces) != 0 {
		deck.Typefaces = fonts
	}
}

func markPreviewOnlyText(scene *slideScene) {
	mark := func(text *sceneText) {
		if text == nil {
			return
		}
		for _, font := range sceneTypefaceResources(slideScene{Objects: []sceneObject{{Text: text}}}, scene.Typefaces) {
			if font.previewOnly() {
				text.ReadOnly = true
				return
			}
		}
	}
	for i := range scene.Objects {
		o := &scene.Objects[i]
		mark(o.Text)
		mark(o.Label)
		if o.Table != nil {
			for j := range o.Table.Cells {
				mark(o.Table.Cells[j].Text)
			}
		}
		if o.Chart != nil {
			for j := range o.Chart.Labels {
				mark(o.Chart.Labels[j].Text)
			}
		}
	}
}

// Enforce content protection in the command core, not only in browser controls.
// It is deliberately text-local: unrelated text, notes, layout, moving/deleting
// objects and replacing a font all stay available. Undo/redo remain usable.
func checkPreviewOnlyTextChanges(before, after Deck, action nativeEditorAction, current int, master bool) error {
	switch action.Action {
	case "update-element", "update-elements", "update-slide", "set-scene-text", "set-table", "set-chart-data", "set-chart-options", "convert-text-kind", "convert-selected-text-kind", "set-selection-property", "paste-object-style", "reset-object-style":
	default:
		return nil
	}
	hasPreview := false
	for _, face := range before.Typefaces {
		if face.previewOnly() {
			hasPreview = true
			break
		}
	}
	if !hasPreview {
		return nil
	}
	if master {
		old, next := masterSlideAt(&before, current), masterSlideAt(&after, current)
		if old == nil || next == nil {
			return nil
		}
		before.Slides, after.Slides = []Slide{cloneSlide(*old)}, []Slide{cloneSlide(*next)}
		current = 0
	}
	if current < 0 || current >= len(before.Slides) || current >= len(after.Slides) {
		return nil
	}
	if reflect.DeepEqual(before.Slides[current].Elements, after.Slides[current].Elements) && reflect.DeepEqual(before.Objects, after.Objects) {
		return nil
	}
	oldScene, err := buildDocumentSlideScene(before, current, 245, 56)
	if err != nil {
		return err
	}
	newScene, err := buildDocumentSlideScene(after, current, 245, 56)
	if err != nil {
		return err
	}
	type content struct {
		text     string
		readOnly bool
	}
	collect := func(scene slideScene) map[string]content {
		out := map[string]content{}
		add := func(id string, text *sceneText) {
			if text == nil {
				return
			}
			var value strings.Builder
			if len(text.Paragraphs) != 0 {
				for _, p := range text.Paragraphs {
					value.WriteString(fmt.Sprint(p.Bullet))
					value.WriteByte('\n')
					for _, r := range p.Runs {
						value.WriteString(r.Text)
					}
					value.WriteByte('\n')
				}
			} else {
				for _, r := range text.Runs {
					value.WriteString(r.Text)
				}
			}
			out[id] = content{value.String(), text.ReadOnly}
		}
		for _, o := range scene.Objects {
			add(o.ID+"/text", o.Text)
			add(o.ID+"/label", o.Label)
			if o.Table != nil {
				for i, c := range o.Table.Cells {
					add(fmt.Sprintf("%s/cell/%d", o.ID, i), c.Text)
				}
			}
			if o.Chart != nil {
				for i, l := range o.Chart.Labels {
					add(fmt.Sprintf("%s/chart/%d", o.ID, i), l.Text)
				}
			}
		}
		return out
	}
	next := collect(newScene)
	for id, old := range collect(oldScene) {
		if value, exists := next[id]; exists && old.readOnly && old.text != value.text {
			return editorFailure("font-preview-only", "text", strings.Split(id, "/")[0], fmt.Errorf("This text uses a Preview & Print font and is read-only. Choose another font to edit it. The rest of the presentation remains editable."))
		}
	}
	return nil
}

// The registration response returns the canonical identity: a restricted input
// file has become a local reference, so its ID is no longer the file hash.
func registeredTypefaceID(action nativeEditorAction) string {
	if action.Action != "register-typeface" || action.TypefaceData == nil {
		return ""
	}
	return action.TypefaceData.ID()
}

func (s *nativeEditorSession) replaceDocumentTypefaces(action nativeEditorAction) (resultErr error) {
	s.mu.Lock()
	if err := s.checkCommandOwnerLocked(action); err != nil {
		s.mu.Unlock()
		return err
	}
	if action.SceneRevision == nil || *action.SceneRevision != s.version || len(action.TypefaceReplacements) == 0 {
		s.mu.Unlock()
		return fmt.Errorf("The deck changed. Review its fonts again.")
	}
	fonts := mergeTypefaces(s.deck.Typefaces, s.fontLibrary)
	for source, target := range action.TypefaceReplacements {
		if _, ok := s.deck.Typefaces[source]; !ok || !validTextFont(target) {
			s.mu.Unlock()
			return fmt.Errorf("invalid replacement font")
		}
		if typefaceIDRE.MatchString(target) {
			face, ok := fonts[target]
			if !ok || face.LocalOnly || !editableTypefaceEmbedding(face.Data) {
				s.mu.Unlock()
				return fmt.Errorf("Choose an editable, embeddable replacement font.")
			}
		}
	}
	tx := beginEditorCommand(s, true)
	defer tx.finish(&resultErr)
	s.deck = cloneDeckForRender(s.deck)
	s.deck.Typefaces = fonts
	replaceExportTypefaces(&s.deck, action.TypefaceReplacements)
	s.undo = append(s.undo, tx.beforeDeck)
	if len(s.undo) > 100 {
		s.undo = s.undo[len(s.undo)-100:]
	}
	s.redo = nil
	s.version++
	companion, path, revision := s.companion, s.deckPath, s.version
	var deck Deck
	if companion != nil {
		deck = cloneDeckForScene(s.deck)
	}
	tx.release()
	if companion != nil {
		companion.RefreshEditorDeckAsync(s, path, deck, revision, authoredTerminalWidth, authoredTerminalHeight)
	}
	return nil
}

// Keep references safe even when callers marshal resources directly (HTML,
// clipboard and live audience scenes), not only when saving Markdown.
func (f DeckTypeface) MarshalJSON() ([]byte, error) {
	type plain DeckTypeface
	if !f.LocalOnly && f.referenceOnly() {
		f.EmbeddingFlags = typefaceEmbeddingFlags(f.Data)
		f.LocalOnly, f.Data = true, nil
	}
	return json.Marshal(plain(f))
}
