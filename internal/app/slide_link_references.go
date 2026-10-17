package app

import (
	"net/url"
	"strconv"
)

// Called before changing Slides, using the same old-index -> new-index mapping
// as participant tabs. Keep existing slide-number metadata compatible while
// making the link follow the target slide, rather than its previous slot.
// All mutations belong to the structural command's one history transaction.
func remapDeckNavigationReferences(deck *Deck, mapping func(int) int) {
	remapTabPages(deck, mapping)
	remap := func(slide *Slide) {
		for index, element := range slide.Elements {
			q, err := url.ParseQuery(element.Query)
			if err != nil {
				continue // Do not rewrite opaque/malformed legacy parameters.
			}
			target, ok := linkTargetFromQuery(element.Query, len(deck.Slides))
			if !ok || target.Slide < 0 {
				continue // HTTP(S) links are not deck references.
			}
			next := mapping(target.Slide)
			if next == target.Slide {
				continue // Preserve unrelated authored query bytes exactly.
			}
			if next < 0 {
				q.Del("slide")
				q.Del("link")
				if element.MasterSlotID != "" {
					// Removing a local link must not resurrect another master link.
					setMasterQueryClears(q, nil, []string{"slide", "link"})
				}
			} else if q.Get("slide") != "" {
				q.Set("slide", strconv.Itoa(next+1))
			} else {
				q.Set("link", "#"+strconv.Itoa(next+1))
			}
			slide.Elements[index].Query = q.Encode()
		}
	}
	for index := range deck.Slides {
		remap(&deck.Slides[index])
	}
	remap(&deck.Masters.Base.Slide)
	for index := range deck.Masters.Layouts {
		remap(&deck.Masters.Layouts[index].Slide)
	}
}
