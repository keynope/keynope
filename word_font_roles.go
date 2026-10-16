package main

import (
	"fmt"
	"strings"
)

func validThemeFontRole(role string) bool {
	return role == "heading" || role == "body" || role == "mono"
}
func validBundledFont(font string) bool { return font == "c64" || font == "sans" || font == "mono" }

func wordFontRole(value string) string {
	if strings.HasPrefix(value, "theme:") && validThemeFontRole(strings.TrimPrefix(value, "theme:")) {
		return strings.TrimPrefix(value, "theme:")
	}
	return ""
}

// Roles affect the selected words' family only. Word sizes and the text box's
// width/paragraph settings remain independent. A concrete fallback makes an
// unthemed destination portable without importing a source theme.
func (deck Deck) wordFontID(role, fallback string) string {
	if deck.Theme == nil || !validThemeFontRole(role) {
		return fallback
	}
	switch role {
	case "heading":
		return deck.Theme.Fonts.Heading.FontID
	case "mono":
		return deck.Theme.Fonts.Mono.FontID
	default:
		return deck.Theme.Fonts.Body.FontID
	}
}

func (deck Deck) wordFontPalette(runs []sceneRun) map[string]string {
	var palette map[string]string
	for _, run := range runs {
		if run.FontRole != "" {
			if palette == nil {
				palette = map[string]string{}
			}
			palette[run.FontRole] = deck.wordFontID(run.FontRole, run.FontID)
		}
	}
	return palette
}

// Capability/clipboard equality compares linked roles in the current theme,
// not obsolete fallbacks stored when the words were authored. Keep the actual
// draft untouched, and retain a distinction between different role identities.
func (deck Deck) wordFontSnapshotRuns(runs []sceneRun) []sceneRun {
	if deck.Theme == nil {
		return runs
	}
	var copy []sceneRun
	for i, run := range runs {
		if run.FontRole == "" {
			continue
		}
		if copy == nil {
			copy = append([]sceneRun(nil), runs...)
		}
		copy[i].FontID = deck.wordFontID(run.FontRole, run.FontID)
	}
	if copy == nil {
		return runs
	}
	return copy
}

func hasWordFontRoles(text *sceneText) bool {
	if text == nil {
		return false
	}
	for _, run := range text.Runs {
		if run.FontRole != "" {
			return true
		}
	}
	for _, p := range text.Paragraphs {
		for _, run := range p.Runs {
			if run.FontRole != "" {
				return true
			}
		}
	}
	return false
}

func (deck Deck) resolveWordFonts(text *sceneText) {
	if text == nil {
		return
	}
	text.FontRoles = nil
	resolve := func(runs []sceneRun) {
		for i := range runs {
			if runs[i].FontRole == "" {
				continue
			}
			runs[i].FontID = deck.wordFontID(runs[i].FontRole, runs[i].FontID)
			if text.FontRoles == nil {
				text.FontRoles = map[string]string{}
			}
			text.FontRoles[runs[i].FontRole] = runs[i].FontID
		}
	}
	resolve(text.Runs)
	for i := range text.Paragraphs {
		resolve(text.Paragraphs[i].Runs)
	}
}

// Selecting an already linked family is a no-op in a themed deck. Do not
// refresh its stored fallback merely because the current theme changed; the
// renderer resolves that independently. Switching roles captures a fallback.
func applyTextRangeFontRole(runs []sceneRun, selection *textStyleRange, snapshot *ObjectStyleSnapshot) ([]sceneRun, bool, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, false, err
	}
	field := snapshot.Fields["text.font"]
	role := wordFontRole(field.Value)
	if snapshot.Context != "text-range" || len(snapshot.Fields) != 1 || role == "" {
		return nil, false, fmt.Errorf("select one valid word font role")
	}
	a, b, err := textRangeBytes(runs, selection)
	if err != nil {
		return nil, false, err
	}
	return rewriteTextRangeRuns(runs, a, b, func(run sceneRun) sceneRun {
		if run.FontRole != role {
			run.FontRole, run.FontID = role, field.Fallback
		}
		return run
	})
}
