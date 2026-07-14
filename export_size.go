package main

// exportRenderSize keeps exported geometry identical to the deck that was
// authored. Terminal dimensions are useful only for legacy decks that do not
// carry Keynope size metadata.
func exportRenderSize(slides []Slide) (int, int) {
	if authoredTerminalWidth > 0 && authoredTerminalHeight > 0 {
		return authoredTerminalWidth, authoredTerminalHeight
	}
	if width, height, ok := terminalSizeOK(); ok {
		return width, height
	}
	return inferExportSize(slides)
}
