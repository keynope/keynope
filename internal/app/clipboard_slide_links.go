package app

import (
	"fmt"
	"net/url"
	"strconv"
)

// Only destination IDs travel with selected objects: never target slide text,
// notes, activity data or the remainder of the source presentation.
func captureClipboardSlideLinks(deck Deck, elements []Element) map[string]string {
	counts := map[string]int{}
	for _, slide := range deck.Slides {
		counts[slide.ID]++
	}
	var links map[string]string
	for _, element := range elements {
		link, ok := linkTargetFromQuery(element.Query, 0)
		if !ok || link.Slide < 0 {
			continue
		}
		if links == nil {
			links = map[string]string{}
		}
		id := ""
		if link.Slide < len(deck.Slides) {
			candidate := deck.Slides[link.Slide].ID
			if validObjectID(candidate) && counts[candidate] == 1 {
				id = candidate
			}
		}
		links[element.ID] = id // Empty explicitly means no identifiable target.
	}
	return links
}

func clipboardHasSlideLinks(bundle ObjectClipboard) bool {
	for _, element := range bundle.Elements {
		if link, ok := linkTargetFromQuery(element.Query, 0); ok && link.Slide >= 0 {
			return true
		}
	}
	return false
}

func resolveClipboardSlideLinks(deck Deck, elements []Element, links map[string]string) {
	positions, counts := map[string]int{}, map[string]int{}
	for index, slide := range deck.Slides {
		positions[slide.ID], counts[slide.ID] = index, counts[slide.ID]+1
	}
	for index, element := range elements {
		link, ok := linkTargetFromQuery(element.Query, 0)
		if !ok || link.Slide < 0 {
			continue
		}
		q, _ := url.ParseQuery(element.Query)
		id := links[element.ID]
		if id != "" && counts[id] == 1 {
			if q.Get("slide") != "" {
				q.Set("slide", strconv.Itoa(positions[id]+1))
			} else {
				q.Set("link", "#"+strconv.Itoa(positions[id]+1))
			}
		} else {
			// A numeric slot in another deck is not the original destination.
			// This conservative fallback also handles old bundles without IDs.
			q.Del("slide")
			q.Del("link")
		}
		elements[index].Query = q.Encode()
	}
}

func clipboardSlideLinkWarnings(bundle ObjectClipboard, pasted []Element) []string {
	removed := 0
	for index, element := range bundle.Elements {
		if link, ok := linkTargetFromQuery(element.Query, 0); ok && link.Slide >= 0 && index < len(pasted) && elementLink(pasted[index].Query) == "" {
			removed++
		}
	}
	if removed == 0 {
		return nil
	}
	return []string{fmt.Sprintf("Removed %d slide link(s) whose original destination could not be identified in this deck.", removed)}
}
