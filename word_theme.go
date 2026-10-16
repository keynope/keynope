package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Word roles live in authored runs, not in text-box paint or the source string.
// The semantic projection resolves concrete colours for every output surface;
// editor-only raw runs retain the authored RGB fallback for portable metadata.
func hasWordColorTokens(text *sceneText) bool {
	if text == nil {
		return false
	}
	for _, run := range text.Runs {
		if run.ColorToken != "" {
			return true
		}
	}
	for _, paragraph := range text.Paragraphs {
		for _, run := range paragraph.Runs {
			if run.ColorToken != "" {
				return true
			}
		}
	}
	return false
}

func (deck Deck) wordColorPalette(runs []sceneRun) map[string]string {
	var palette map[string]string
	for _, run := range runs {
		if run.ColorToken != "" {
			if palette == nil {
				palette = map[string]string{}
			}
			palette[run.ColorToken] = deck.themeTokenColor(run.ColorToken)
		}
	}
	return palette
}

func (deck Deck) resolveWordColors(text *sceneText) {
	if text == nil {
		return
	}
	text.ColorTokens = nil
	resolve := func(runs []sceneRun) {
		for index := range runs {
			run := &runs[index]
			if run.ColorToken == "" {
				continue
			}
			color := deck.themeTokenColor(run.ColorToken)
			if color != "" {
				run.Color = color
				if text.ColorTokens == nil {
					text.ColorTokens = map[string]string{}
				}
				text.ColorTokens[run.ColorToken] = color
			}
		}
	}
	resolve(text.Runs)
	for index := range text.Paragraphs {
		resolve(text.Paragraphs[index].Runs)
	}
}

// Cheap theme-consumer comparison, without shaping, emoji/font preparation or
// touching the authored metadata. Only the referenced palette roles matter.
func wordThemeInputsEqual(before, after Deck, element Element) bool {
	q := mustQuery(element.Query)
	styles := decodeModernRuns(q.Get("modern-runs"))
	if styles == nil || styles.Source != modernRunFingerprint(element.Text, q.Get("ttf-weight")) {
		return true
	}
	for _, run := range styles.Runs {
		if run.ColorToken != "" && before.themeTokenColor(run.ColorToken) != after.themeTokenColor(run.ColorToken) {
			return false
		}
		if run.FontRole != "" && before.wordFontID(run.FontRole, run.FontID) != after.wordFontID(run.FontRole, run.FontID) {
			return false
		}
	}
	return true
}

func wordSnapshotInDeck(deck Deck, snapshot *ObjectStyleSnapshot) *ObjectStyleSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := snapshot.Clone()
	copy.ThemeSignature = objectStyleThemeSignature(deck)
	if field, ok := copy.Fields["paint.color"]; ok && field.Token != "" {
		field.Value = deck.themeTokenColor(field.Token)
		copy.Fields["paint.color"] = field
	}
	if field, ok := copy.Fields["text.font"]; ok && wordFontRole(field.Value) != "" {
		field.Fallback = deck.wordFontID(wordFontRole(field.Value), field.Fallback)
		copy.Fields["text.font"] = field
	}
	return copy
}

func wordSnapshotForDestination(deck Deck, snapshot *ObjectStyleSnapshot) *ObjectStyleSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := snapshot.Clone()
	if field, ok := copy.Fields["paint.color"]; ok && field.Token != "" && deck.Theme == nil && copy.ThemeSignature != objectStyleThemeSignature(deck) {
		field.Token = ""
		copy.Fields["paint.color"] = field
	}
	if field, ok := copy.Fields["text.font"]; ok && wordFontRole(field.Value) != "" && deck.Theme == nil && copy.ThemeSignature != objectStyleThemeSignature(deck) {
		field.Value, field.Fallback = field.Fallback, ""
		copy.Fields["text.font"] = field
	}
	return copy
}

func wordCapabilitiesInDeck(deck Deck, action nativeEditorAction, caps *selectionCapabilityState, snapshot *ObjectStyleSnapshot) *selectionCapabilityState {
	if caps == nil {
		return nil
	}
	role := selectionPropertyState{State: "mixed", Provenance: "mixed", EligibleIDs: []string{action.ObjectID}, TotalCount: 1, Editable: true}
	if snapshot != nil {
		if field, ok := snapshot.Fields["paint.color"]; ok {
			role.State, role.Provenance = "uniform", "inherited"
			color := caps.Properties["paint.color"]
			if field.Present {
				role.Value, role.Provenance = "custom", "explicit"
				color.Value = field.Value
				if field.Token != "" {
					role.Value, role.Provenance = field.Token, "theme"
					color.Provenance = "theme"
				}
			}
			caps.Properties["paint.color"] = color
		}
	}
	caps.Properties["paint.colorToken"] = role
	if font := caps.Properties["text.font"]; wordFontRole(fmt.Sprint(font.Value)) != "" {
		font.Provenance = "theme"
		caps.Properties["text.font"] = font
	}
	return caps
}

func mapWordColorRuns(element Element, transform func(sceneRun) sceneRun) Element {
	if element.Kind == "shape" {
		outer := mustQuery(element.Query)
		raw, err := base64.StdEncoding.DecodeString(outer.Get("shape-label"))
		var data shapeLabelData
		if err != nil || len(raw) > 65536 || json.Unmarshal(raw, &data) != nil {
			return element
		}
		before := data.Query
		// Nested and sparse rich styles share the same label source text.
		// Keep that text stable so transforming one does not invalidate the
		// other's fingerprint or replace nested extension metadata.
		data.Query = mapLabelWordColorQuery(data.Text, data.Query, transform)
		outerChanged := false
		if value := outer.Get("label-modern-runs"); value != "" && value != "none" {
			values := shapeLabelStyleValues(element, mustQuery(data.Query))
			query := values.Encode()
			if mapped := mapLabelWordColorQuery(data.Text, query, transform); mapped != query {
				outer.Set("label-modern-runs", mustQuery(mapped).Get("modern-runs"))
				outerChanged = true
			}
		}
		if before == data.Query && !outerChanged {
			return element
		}
		encoded, err := json.Marshal(data)
		if err == nil && len(encoded) <= 65536 {
			outer.Set("shape-label", base64.StdEncoding.EncodeToString(encoded))
			element.Query = outer.Encode()
		}
		return element
	}
	q := mustQuery(element.Query)
	styles := decodeModernRuns(q.Get("modern-runs"))
	if styles == nil || styles.Source != modernRunFingerprint(element.Text, q.Get("ttf-weight")) {
		return element
	}
	changed := false
	runs := append([]sceneRun(nil), styles.Runs...)
	for index, run := range runs {
		if run.ColorToken != "" || run.FontRole != "" {
			runs[index] = transform(run)
			changed = changed || !sameSceneRunStyle(run, runs[index])
		}
	}
	if changed {
		return withConvertedRuns(element, runs)
	}
	return element
}

func mapLabelWordColorQuery(text, query string, transform func(sceneRun) sceneRun) string {
	q := mustQuery(query)
	styles := decodeModernRuns(q.Get("modern-runs"))
	if styles == nil || styles.Source != modernRunFingerprint(text, q.Get("ttf-weight")) {
		return query
	}
	runs := append([]sceneRun(nil), styles.Runs...)
	changed := false
	for index, run := range runs {
		if run.ColorToken != "" || run.FontRole != "" {
			runs[index] = transform(run)
			changed = changed || !sameSceneRunStyle(run, runs[index])
		}
	}
	if changed {
		if encoded, err := encodeModernRunsMetadata(text, q.Get("ttf-weight"), runs, true); err == nil && encoded != "" {
			q.Set("modern-runs", encoded)
			return q.Encode()
		}
	}
	return query
}

func (deck Deck) captureWordColorFallbacks(element Element) Element {
	return mapWordColorRuns(element, func(run sceneRun) sceneRun {
		if color := deck.themeTokenColor(run.ColorToken); color != "" {
			run.Color = strings.ToLower(color)
		}
		if run.FontRole != "" {
			run.FontID = deck.wordFontID(run.FontRole, run.FontID)
		}
		return run
	})
}

func flattenWordColorTokens(element Element) Element {
	return mapWordColorRuns(element, func(run sceneRun) sceneRun { run.ColorToken, run.FontRole = "", ""; return run })
}

// Clipboard adaptation owns the copy. Preserve extensions, default/cell styles,
// spans and tracks; only linked words need source colour/font fallbacks or unlink.
func mapTableWordColors(table TableData, deck Deck, flatten bool) TableData {
	copy := cloneTable(table)
	for r := range copy.Cells {
		for c := range copy.Cells[r] {
			for i := range copy.Cells[r][c].Runs {
				run := &copy.Cells[r][c].Runs[i]
				if run.ColorToken == "" && run.FontRole == "" {
					continue
				}
				if flatten {
					run.ColorToken = ""
					run.FontRole = ""
				} else if color := deck.themeTokenColor(run.ColorToken); color != "" {
					run.Color = strings.ToLower(color)
				}
				if !flatten && run.FontRole != "" {
					run.FontID = deck.wordFontID(run.FontRole, run.FontID)
				}
			}
		}
	}
	return copy
}
