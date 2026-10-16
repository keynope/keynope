package main

// Appearance settings are not a whole-slide replacement. Notes, layout/tab
// bindings, elements, activity archives, identity and future fields have their
// own owners/commands; a partial or stale settings form cannot erase them.
func prepareSlideAppearanceUpdate(original, incoming Slide) Slide {
	updated := original
	updated.DefaultStyle = incoming.DefaultStyle
	updated.Effect, updated.EffectSet = incoming.Effect, incoming.EffectSet
	updated.Background, updated.BackgroundSet = incoming.Background, incoming.BackgroundSet
	updated.BackgroundMedia, updated.BackgroundMediaSet = cloneSlideBackgroundMedia(incoming.BackgroundMedia), incoming.BackgroundMediaSet
	if updated.BackgroundMedia != nil && updated.BackgroundMedia.Extra == nil && original.BackgroundMedia != nil && updated.BackgroundMedia.AssetID == original.BackgroundMedia.AssetID {
		updated.BackgroundMedia.Extra = cloneJSONExtensions(original.BackgroundMedia.Extra)
	}
	updated.FG, updated.FGSet = nativeEditorColorCode(incoming.FG, false), incoming.FGSet
	updated.BG, updated.BGSet = nativeEditorColorCode(incoming.BG, true), incoming.BGSet
	updated.HeaderFG, updated.HeaderFGSet = nativeEditorColorCode(incoming.HeaderFG, false), incoming.HeaderFGSet
	updated.TTFSize, updated.TTFWidth = incoming.TTFSize, incoming.TTFWidth
	updated.PageNumber = incoming.PageNumber
	return updated
}
