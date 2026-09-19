package main

import "fmt"

// standaloneExportSlide removes generated workshop access artwork as well as
// private definitions. Ordinary authored text/links are not guessed to be secrets.
// Resolved mixed scenes must be filtered too: their pixels already contain QR data.
func standaloneExportSlide(slide Slide, index int) Slide {
	removed := make(map[string]bool)
	elements := make([]Element, 0, len(slide.Elements))
	for i, element := range slide.Elements {
		if element.PlaceholderRole == activityQRCodeRole || element.PlaceholderRole == activityURLRole {
			id := element.ID
			if id == "" {
				id = fmt.Sprintf("legacy-%d-%d", index, i)
			}
			removed[id] = true
			continue
		}
		elements = append(elements, element)
	}
	slide.Elements = elements
	slide.Notes, slide.Engagement, slide.EngagementResult = "", nil, nil
	if slide.ModernScene != nil {
		scene := *slide.ModernScene
		scene.Objects = make([]sceneObject, 0, len(slide.ModernScene.Objects))
		for _, object := range slide.ModernScene.Objects {
			if !removed[object.ID] {
				scene.Objects = append(scene.Objects, object)
			}
		}
		slide.ModernScene = &scene
	}
	return slide
}
