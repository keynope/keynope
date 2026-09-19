package main

import "strings"

func extractEditedMasterSlide(original, working Slide, hasBaseOverlay bool) Slide {
	if !hasBaseOverlay {
		working.Elements = stripRuntimeElementState(working.Elements)
		return working
	}
	out := original
	out.PageNumber = working.PageNumber
	out.Elements = nil
	for _, element := range working.Elements {
		if element.Inherited {
			continue
		}
		element.Inherited = false
		out.Elements = append(out.Elements, element)
	}
	return out
}

func masterEditorSlide(deck *Deck, selected int) Slide {
	target := masterSlideAt(deck, selected)
	working := cloneSlide(*target)
	if selected > 0 {
		base := cloneSlide(deck.Masters.Base.Slide)
		layoutID := deck.Masters.Layouts[selected-1].ID
		working = inheritedSlideStyle(deck.Masters, deck.Masters.Layouts[selected-1].ID)
		working.PageNumber = target.PageNumber
		working.Elements = nil
		for _, element := range base.Elements {
			if element.Kind == "page-number" {
				continue
			}
			element.Inherited = true
			working.Elements = append(working.Elements, element)
		}
		for _, element := range cloneSlide(*target).Elements {
			if element.Kind != "page-number" || target.PageNumber == pageNumberShow {
				working.Elements = append(working.Elements, element)
			}
		}
		if deck.effectivePageNumberMode(Slide{LayoutID: layoutID}) == pageNumberShow && target.PageNumber != pageNumberShow {
			working.Elements = append(working.Elements, resolvedPageNumberElement(deck.Masters, layoutID, 0))
		}
	}
	return working
}

func applyPlaceholderRole(element *Element, role string) {
	if element == nil {
		return
	}
	if role == placeholderFixed || role == "" {
		element.PlaceholderRole = ""
		element.SlotID = ""
		return
	}
	if element.ID == "" {
		element.ID = newStableID("master-element")
	}
	element.SlotID = element.ID
	element.PlaceholderRole = normalizePlaceholderRole(role)
	switch element.PlaceholderRole {
	case placeholderTitle:
		element.Kind, element.Level = "heading", 1
		if strings.TrimSpace(element.Text) == "" {
			element.Text = "Title"
		}
	case placeholderSubtitle:
		element.Kind, element.Level = "heading", 2
		if strings.TrimSpace(element.Text) == "" {
			element.Text = "Subtitle"
		}
	case placeholderCode:
		element.Kind, element.Level = "code", 0
		if strings.TrimSpace(element.Text) == "" {
			element.Text = "Code"
		}
	case placeholderImage:
		element.Kind, element.Level = "image", 0
	case placeholderBody:
		element.Kind, element.Level = "text", 0
		if strings.TrimSpace(element.Text) == "" {
			element.Text = "Body text"
		}
	}
}

const placeholderFixed = "fixed"

func masterLayoutPreview(masters MasterDeck, layoutID string) Slide {
	previewDeck := Deck{Masters: masters, Slides: []Slide{{LayoutID: layoutID}}}
	return previewDeck.ResolveSlide(0, true)
}

func masterViewPreview(masters MasterDeck, selected int) Slide {
	if selected <= 0 {
		preview := cloneSlide(masters.Base.Slide)
		if preview.PageNumber == pageNumberShow {
			for index := range preview.Elements {
				if preview.Elements[index].Kind == "page-number" {
					preview.Elements[index].Text = "1"
				}
			}
		} else {
			removePageNumberElements(&preview)
		}
		return preview
	}
	if selected-1 >= len(masters.Layouts) {
		return Slide{}
	}
	return masterLayoutPreview(masters, masters.Layouts[selected-1].ID)
}

func masterSlideAt(deck *Deck, selected int) *Slide {
	if selected <= 0 {
		return &deck.Masters.Base.Slide
	}
	return &deck.Masters.Layouts[selected-1].Slide
}

func cloneMasterLayoutFresh(source MasterLayout) MasterLayout {
	clone := source
	clone.Extra = cloneJSONExtensions(source.Extra)
	clone.ID = newStableID("layout")
	clone.Name = source.Name + " Copy"
	clone.Slide = cloneSlide(source.Slide)
	for index := range clone.Slide.Elements {
		element := &clone.Slide.Elements[index]
		element.ID = newStableID(clone.ID + "-element")
		if element.PlaceholderRole != "" {
			element.SlotID = element.ID
		}
	}
	return clone
}
