package main

import "fmt"

// The editor has a stricter admission rule than metadata utilities: it needs
// at least one slide. Do not publish a candidate's pixels/files before this
// caller-level check, even when the Markdown itself parsed successfully.
func parseEditorDeckData(path string, data []byte) (Deck, error) {
	width, height, order := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	accepted := false
	defer func() {
		if !accepted {
			authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = width, height, order
		}
	}()
	deck, err := parseDeckDataWithAssetPublication(path, data, false)
	if err != nil {
		return Deck{}, err
	}
	if len(deck.Slides) == 0 {
		return Deck{}, fmt.Errorf("deck has no slides")
	}
	materializeDeckAssets(&deck)
	accepted = true
	return deck, nil
}
