package main

// Caller holds the document read lock. Cell drafts supply their own validated
// table, so capture only authored identity/existence, not any registry payload.
// The empty table is an existence marker for shared target validation; this
// private preparation scope is never saved or rendered as a document.
func captureTableStyleScope(deck Deck, action nativeEditorAction) (slideResolutionScope, Element, error) {
	scope, err := captureSlideResolutionScope(deck, action.Slide, action.SceneMaster)
	if err != nil {
		return scope, Element{}, err
	}
	if scope.master && scope.index > 0 && !scope.layoutIsSource {
		return scope, Element{}, errInvalidEditorAction
	}
	scope.deck.Objects = &DeckObjects{Version: structuredObjectsVersion, Tables: map[string]TableData{}}
	if deck.Objects != nil {
		for _, element := range scope.source.Elements {
			if element.ID == action.ObjectID && element.Kind == "table" {
				if _, exists := deck.Objects.Tables[element.ObjectID]; exists {
					scope.deck.Objects.Tables[element.ObjectID] = TableData{}
				}
			}
		}
	}
	_, owner, _, err := tableCellStyleTargetFromSlide(scope.deck, action, scope.source)
	return scope, owner, err
}
