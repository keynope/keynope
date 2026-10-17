package app

import "reflect"

// Compare the inputs consumed by one cached scene, not the root document's
// revision. Called under the command lock using the transaction's owned before
// snapshot. Unknown extension data/profile changes remain conservative. Known
// structured adapters compare resolved theme consumers too. No layout,
// conversion or JSON encoding happens.
func sceneProjectionInputsEqual(before, after Deck, key sceneResponseKey) bool {
	// A deck-wide conversion report consumes every slide, unlike the scene.
	if key.report == "deck" || len(before.Slides) != len(after.Slides) ||
		!reflect.DeepEqual(before.PageSize, after.PageSize) ||
		before.HideActivityQR != after.HideActivityQR ||
		before.usesElementStyles() != after.usesElementStyles() ||
		!reflect.DeepEqual(before.Appearance, after.Appearance) ||
		!reflect.DeepEqual(before.Typefaces, after.Typefaces) ||
		before.Masters.Version != after.Masters.Version ||
		!reflect.DeepEqual(before.Masters.Extra, after.Masters.Extra) ||
		!sceneObjectHeadersEqual(before.Objects, after.Objects) ||
		!reflect.DeepEqual(before.Extra, after.Extra) {
		return false
	}
	resolved := func(deck Deck) (Slide, []Element, bool) {
		if key.master {
			if key.index < 0 || key.index > len(deck.Masters.Layouts) {
				return Slide{}, nil, false
			}
			deck.Masters.Base.Slide.EngagementResult = nil
			deck.Masters.Layouts = append([]MasterLayout(nil), deck.Masters.Layouts...)
			for index := range deck.Masters.Layouts {
				deck.Masters.Layouts[index].Slide.EngagementResult = nil
			}
			return masterViewPreview(deck.Masters, key.index), masterSlideAt(&deck, key.index).Elements, true
		}
		if key.index < 0 || key.index >= len(deck.Slides) {
			return Slide{}, nil, false
		}
		deck.Slides = append([]Slide(nil), deck.Slides...)
		deck.Slides[key.index].EngagementResult = nil
		return deck.ResolveSlide(key.index, false), deck.Slides[key.index].Elements, true
	}
	a, authoredA, validA := resolved(before)
	b, authoredB, validB := resolved(after)
	if !validA || !validB {
		return false
	}
	// Notes and private result archives do not appear in scene JSON. Activity
	// definitions do: legacy activity-master artwork and diagnostics need them.
	a.Notes, b.Notes = "", ""
	a.EngagementResult, b.EngagementResult = nil, nil
	a.ModernScene, b.ModernScene = nil, nil
	// These sidecars contain theme colours/stroke defaults even when explicit
	// slide/object fields override them. Compare actual consumers below rather
	// than treating the sidecars as authored slide data. Unknown theme extensions
	// still invalidate conservatively.
	a.ShapeStrokeDefaults, b.ShapeStrokeDefaults = nil, nil
	a.ThemeColors, b.ThemeColors = nil, nil
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(authoredA, authoredB) {
		return false
	}
	if !sceneThemeInputsEqual(before, after, a, key) {
		return false
	}
	assets := map[string]bool{}
	type payloadReference struct{ kind, id string }
	payloads := map[payloadReference]bool{}
	for _, element := range a.Elements {
		if element.AssetID != "" {
			assets[element.AssetID] = true
		}
		if element.ObjectID != "" {
			payloads[payloadReference{element.Kind, element.ObjectID}] = true
		}
	}
	if a.BackgroundMedia != nil {
		assets[a.BackgroundMedia.AssetID] = true
	}
	for id := range assets {
		old, existed := before.Assets[id]
		next, exists := after.Assets[id]
		if existed != exists || !reflect.DeepEqual(old, next) {
			return false
		}
	}
	for reference := range payloads {
		if !scenePayloadEqual(before.Objects, after.Objects, reference.id, reference.kind) {
			return false
		}
	}
	// Build state is authored scene behavior; preserve this dependency even
	// before the complete timeline renderer lands. Unrelated builds are ignored.
	var oldBuild, nextBuild SlideBuild
	var hadBuild, hasBuild bool
	if before.Objects != nil {
		oldBuild, hadBuild = before.Objects.Builds[a.ID]
	}
	if after.Objects != nil {
		nextBuild, hasBuild = after.Objects.Builds[b.ID]
	}
	return hadBuild == hasBuild && reflect.DeepEqual(oldBuild, nextBuild)
}

func sceneObjectHeadersEqual(before, after *DeckObjects) bool {
	if before == nil || after == nil {
		return before == after
	}
	return before.Version == after.Version && reflect.DeepEqual(before.Extra, after.Extra)
}

func scenePayloadEqual(before, after *DeckObjects, id, kind string) bool {
	if before != nil && after != nil && (before.Version != after.Version || !reflect.DeepEqual(before.Extra, after.Extra)) {
		return false
	}
	// Unknown structured payload kinds require a conservative archive check.
	if kind != "table" && kind != "chart" {
		return reflect.DeepEqual(before, after)
	}
	if kind == "table" {
		var old, next TableData
		var had, has bool
		if before != nil {
			old, had = before.Tables[id]
		}
		if after != nil {
			next, has = after.Tables[id]
		}
		return had == has && reflect.DeepEqual(old, next)
	}
	var old, next ChartData
	var had, has bool
	if before != nil {
		old, had = before.Charts[id]
	}
	if after != nil {
		next, has = after.Charts[id]
	}
	return had == has && reflect.DeepEqual(old, next)
}

// A successful accepted command may change editor concurrency without changing
// this response's consumers. Publish a new immutable envelope/revision; readers
// already holding the old response retain their original consistent snapshot.
func (s *nativeEditorSession) retainSceneForDeckChange(before Deck, beforeRevision int64) {
	entry := s.sceneResponse
	if entry == nil || entry.key.revision != beforeRevision || s.version == beforeRevision ||
		!sceneProjectionInputsEqual(before, s.deck, entry.key) {
		return
	}
	retained := *entry
	retained.key.revision = s.version
	s.sceneResponse = &retained
}
