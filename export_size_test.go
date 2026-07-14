package main

import "testing"

func TestExportRenderSizePrefersAuthoredDeckSize(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	t.Cleanup(func() {
		authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight
	})

	authoredTerminalWidth, authoredTerminalHeight = 245, 56
	t.Setenv("COLUMNS", "100")
	t.Setenv("LINES", "32")

	width, height := exportRenderSize(nil)
	if width != 245 || height != 56 {
		t.Fatalf("export size = %dx%d, want authored 245x56", width, height)
	}
}

func TestExportRenderSizeUsesTerminalForLegacyDeck(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	t.Cleanup(func() {
		authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight
	})

	authoredTerminalWidth, authoredTerminalHeight = 0, 0
	t.Setenv("COLUMNS", "120")
	t.Setenv("LINES", "40")

	width, height := exportRenderSize(nil)
	if width != 120 || height != 40 {
		t.Fatalf("legacy export size = %dx%d, want terminal 120x40", width, height)
	}
}
