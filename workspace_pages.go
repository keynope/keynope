package main

// All editor hosts use the same master/slide projection at the requested
// viewport size. Master previews must not bypass the mixed-style scene.
func editorWorkspacePages(deck Deck, masterMode bool, currentMaster, cols, rows int) ([]exportPage, error) {
	if cols <= 0 || rows <= 0 {
		return nil, errInvalidEditorAction
	}
	if masterMode {
		if currentMaster < 0 || currentMaster > len(deck.Masters.Layouts) {
			return nil, errInvalidEditorAction
		}
		preview := deck.masterRenderPreview(currentMaster, cols, rows)
		if preview.ModernScene != nil {
			authored := deck.Masters.Base.Slide.Elements
			if currentMaster > 0 {
				authored = deck.Masters.Layouts[currentMaster-1].Slide.Elements
			}
			annotateSceneEditing(preview.ModernScene, deck, authored)
		}
		return exportSlidePages(preview, currentMaster, len(deck.Masters.Layouts)+1, cols, rows), nil
	}
	var pages []exportPage
	for index := range deck.Slides {
		pages = append(pages, editorSlideWorkspacePages(deck, index, cols, rows)...)
	}
	return pages, nil
}

// Workspace scenes are authoring surfaces; presenter/export scenes are not.
// Add editability only at this boundary so crop/text/label dialogs are
// available in the unified editor without exposing authoring affordances in
// presentation or participant payloads.
func editorSlideWorkspacePages(deck Deck, index, cols, rows int) []exportPage {
	preview := deck.slideRenderPreview(index, cols, rows)
	if preview.ModernScene != nil && index >= 0 && index < len(deck.Slides) {
		annotateSceneEditing(preview.ModernScene, deck, deck.Slides[index].Elements)
	}
	return exportSlidePages(preview, index, len(deck.Slides), cols, rows)
}
