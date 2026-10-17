package app

// Downloaded/participant pages are immutable render output, not an editor
// session. Keep the same artwork, links and timeline but omit authoring
// continuations, conversion reports and attachment hit-test records. Own only
// the envelopes we change; large render resources remain immutable and shared.
func audienceExportPage(page exportPage) exportPage {
	page.ShapePorts, page.Connectors, page.Engagement = nil, nil, nil
	if page.Scene == nil {
		return page
	}
	source := page.Scene
	needsCopy := source.Revision != nil || source.Conversion != nil || source.Master
	for _, object := range source.Objects {
		needsCopy = needsCopy || object.Editable || len(object.EditRuns) != 0 || len(object.TextCapabilities) != 0
	}
	if !needsCopy {
		return page
	}
	scene := *source
	scene.Revision, scene.Conversion, scene.Master = nil, nil, false
	scene.Objects = append([]sceneObject(nil), source.Objects...)
	for i := range scene.Objects {
		object := &scene.Objects[i]
		object.Editable, object.EditRuns, object.TextCapabilities = false, nil, nil
	}
	page.Scene = &scene
	return page
}

func audienceExportDeck(deck exportDeck) exportDeck {
	pages := make([]exportPage, len(deck.Pages))
	for i, page := range deck.Pages {
		pages[i] = audienceExportPage(page)
	}
	deck.Pages = pages
	return deck
}
