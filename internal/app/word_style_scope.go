package app

// Word preparation resolves authored ownership and inheritance, not artwork or
// structured cell data. Keep this projection private; it is never a deck to save
// or render. Mutable metadata needed by resolution is copied before unlocking.
type wordTableInfo struct{ version, rows, columns int }
type wordStyleScope struct {
	slideResolutionScope
	tables map[string]wordTableInfo
}

func captureWordStyleScope(deck Deck, action nativeEditorAction) (wordStyleScope, error) {
	resolution, err := captureSlideResolutionScope(deck, action.Slide, action.SceneMaster)
	scope := wordStyleScope{slideResolutionScope: resolution, tables: map[string]wordTableInfo{}}
	if err != nil {
		return scope, err
	}
	if deck.Objects != nil {
		capture := func(slide Slide) {
			for _, e := range slide.Elements {
				if e.Kind == "table" {
					if data, ok := deck.Objects.Tables[e.ObjectID]; ok {
						scope.tables[e.ObjectID] = wordTableInfo{data.Version, len(data.Rows), len(data.Columns)}
					}
				}
			}
		}
		capture(scope.source)
		capture(scope.deck.Masters.Base.Slide)
		for _, layout := range scope.deck.Masters.Layouts {
			capture(layout.Slide)
		}
	}
	return scope, nil
}

func (scope wordStyleScope) owner(action nativeEditorAction, group string) (Element, error) {
	resolved := scope.resolved()
	return textRangeStyleOwner(scope.deck, scope.source, resolved, action, group, scope.tables)
}
