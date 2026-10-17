package app

// The windowed editors materialize legacy deck/element style inheritance once
// on open. Direct conversion must not resurrect the retired sampled-shape/text
// path: export the same supported vector shapes and font choices the user sees.
// Work on an owned render snapshot, preserving the source/saved baseline.
func preparePPTXEditorModel(deck Deck) Deck {
	deck = cloneDeckForRender(deck)
	if !deck.usesConcreteAppearance() {
		canonicalizeConcreteAppearance(&deck)
		standardizeDeckText(&deck)
	}
	return deck
}
