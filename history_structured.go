package main

// History snapshots are immutable once captured. Ordinary cloneDeck remains
// the mutable/external ownership boundary; this helper is private to history.
func cloneEditorHistoryDeck(deck Deck, history []Deck) Deck {
	// All other deck fields retain their ordinary deep ownership. Only this
	// immutable history store may share equal payloads with another snapshot;
	// no entry ever borrows mutable values from the active deck.
	registry := deck.Objects
	typefaces := deck.Typefaces
	deck.Typefaces = nil
	deck.Objects = nil
	snapshot := cloneDeck(deck)
	snapshot.Typefaces = cloneTypefaces(typefaces, false)
	if registry == nil {
		return snapshot
	}
	var previous *DeckObjects
	if len(history) > 0 {
		previous = history[len(history)-1].Objects
	}
	if previous == nil {
		previous = &DeckObjects{}
	}
	objects := registry.cloneForPayloadReplacement()
	for id, current := range registry.Tables {
		if old, exists := previous.Tables[id]; exists && historyTableEqual(current, old) {
			objects.Tables[id] = old
		} else {
			objects.Tables[id] = cloneTable(current)
		}
	}
	for id, current := range registry.Charts {
		if old, exists := previous.Charts[id]; exists && historyChartEqual(current, old) {
			objects.Charts[id] = old
		} else {
			objects.Charts[id] = cloneChart(current)
		}
	}
	for id, current := range registry.Builds {
		if old, exists := previous.Builds[id]; exists && historyBuildEqual(current, old) {
			objects.Builds[id] = old
		} else {
			objects.Builds[id] = cloneBuild(current)
		}
	}
	snapshot.Objects = objects
	return snapshot
}

// Popped history data becomes mutable editor state. Keep the structured archive
// independent even when immutable snapshots reuse unchanged payloads.
func restoreEditorHistoryDeck(deck Deck) Deck {
	deck.Objects = deck.Objects.Clone()
	return deck
}
