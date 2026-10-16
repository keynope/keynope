package main

// Owned inheritance/style context, not a deck to save or a complete media scene.
// It omits unrelated slides, media bytes, registry payloads and private archives.
type slideResolutionScope struct {
	deck           Deck
	source         Slide
	index          int
	master         bool
	layout         string
	layoutIsSource bool
}

func cloneSlideResolutionInput(slide Slide) Slide {
	slide.Notes, slide.EngagementResult, slide.BackgroundMedia, slide.ModernScene = "", nil, nil, nil
	// Definitions affect protected master projections; private results do not.
	return cloneSlide(slide)
}

func captureSlideResolutionScope(deck Deck, index int, master bool) (slideResolutionScope, error) {
	scope := slideResolutionScope{index: index, master: master}
	if master {
		if index < 0 || index > len(deck.Masters.Layouts) {
			return scope, errInvalidEditorAction
		}
		scope.source = cloneSlideResolutionInput(*masterSlideAt(&deck, index))
		if index > 0 {
			scope.layout = deck.Masters.Layouts[index-1].ID
		}
	} else {
		if index < 0 || index >= len(deck.Slides) {
			return scope, errInvalidEditorAction
		}
		scope.source = cloneSlideResolutionInput(deck.Slides[index])
		scope.layout = scope.source.LayoutID
	}
	scope.deck.Theme = deck.Theme.Clone()
	scope.deck.Appearance = deck.Appearance.Clone()
	scope.deck.PageSize = deck.PageSize.Clone()
	scope.deck.HideActivityQR = deck.HideActivityQR
	scope.deck.Masters.Version = deck.Masters.Version
	scope.deck.Masters.Base = MasterLayout{ID: deck.Masters.Base.ID, Name: deck.Masters.Base.Name, Slide: cloneSlideResolutionInput(deck.Masters.Base.Slide)}
	// Preserve first-match lookup semantics, including compatible duplicate IDs.
	if layout, ok := deck.Masters.Layout(scope.layout); ok && scope.layout != "" {
		scope.deck.Masters.Layouts = []MasterLayout{{ID: layout.ID, Name: layout.Name, Slide: cloneSlideResolutionInput(layout.Slide)}}
		if master && index > 0 {
			for i, candidate := range deck.Masters.Layouts {
				if candidate.ID == scope.layout {
					scope.layoutIsSource = i == index-1
					break
				}
			}
		}
	}
	return scope, nil
}

// Resolve edits to this owned source without manufacturing unrelated slide or
// master entries. Ambiguous duplicate master IDs cannot target the wrong owner.
func (scope slideResolutionScope) resolvedEdited() (Slide, error) {
	if !scope.master {
		return scope.resolved(), nil
	}
	if scope.index == 0 {
		scope.deck.Masters.Base.Slide = scope.source
		return scope.resolved(), nil
	}
	if !scope.layoutIsSource || len(scope.deck.Masters.Layouts) != 1 {
		return Slide{}, errInvalidEditorAction
	}
	scope.deck.Masters.Layouts = append([]MasterLayout(nil), scope.deck.Masters.Layouts...)
	scope.deck.Masters.Layouts[0].Slide = scope.source
	return scope.resolved(), nil
}

func (scope slideResolutionScope) resolved() Slide {
	if !scope.master {
		return scope.deck.resolveSlideSource(scope.source, scope.index, false)
	}
	if scope.index == 0 {
		return scope.deck.applyShapeStrokeDefaults(masterViewPreview(scope.deck.Masters, 0))
	}
	return scope.deck.applyShapeStrokeDefaults(masterLayoutPreview(scope.deck.Masters, scope.layout))
}
