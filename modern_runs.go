package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Modern run styles accompany the editable Markdown text, never replace it.
// A source fingerprint prevents stale styles from resurrecting text edited by
// a legacy surface or an external Markdown editor.
type modernRunStyles struct {
	Version int        `json:"version"`
	Source  string     `json:"source"`
	Runs    []sceneRun `json:"runs"`
}

func modernRunFingerprint(text, weight string) string {
	sum := sha256.Sum256([]byte(weight + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

func decodeModernRuns(value string) *modernRunStyles {
	if value == "" || len(value) > 350000 {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(data) > 262144 {
		return nil
	}
	var styles modernRunStyles
	if json.Unmarshal(data, &styles) != nil || styles.Version != 1 || len(styles.Runs) > 10000 || len(styles.Source) != 64 {
		return nil
	}
	total := 0
	for _, run := range styles.Runs {
		total += len(run.Text)
		if total > 100000 {
			return nil
		}
		if run.Color != "" {
			if _, ok := normalizeHexColour(run.Color); !ok {
				return nil
			}
		}
	}
	return &styles
}

func encodeModernRuns(text, weight, kind string, runs []sceneRun) (string, error) {
	needed := false
	clean := make([]sceneRun, 0, len(runs))
	for _, run := range runs {
		if run.Text == "" {
			continue
		}
		if run.Color != "" {
			color, ok := normalizeHexColour(run.Color)
			if !ok {
				return "", fmt.Errorf("invalid text colour")
			}
			run.Color = color
		}
		needed = needed || run.Bold != (weight == "bold") || run.Italic || run.Underline || (kind == "code" && run.Color != "")
		if len(clean) > 0 && clean[len(clean)-1].Bold == run.Bold && clean[len(clean)-1].Italic == run.Italic && clean[len(clean)-1].Underline == run.Underline && clean[len(clean)-1].Color == run.Color {
			clean[len(clean)-1].Text += run.Text
		} else {
			clean = append(clean, run)
		}
	}
	if !needed {
		return "", nil
	}
	if len(clean) > 10000 || len(text) > 100000 {
		return "", fmt.Errorf("text formatting exceeds editing limit")
	}
	data, err := json.Marshal(modernRunStyles{Version: 1, Source: modernRunFingerprint(text, weight), Runs: clean})
	if err != nil {
		return "", err
	}
	if len(data) > 262144 {
		return "", fmt.Errorf("text formatting exceeds storage limit")
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func resolvedModernRuns(value, text, weight string, fallback []sceneRun) []sceneRun {
	styles := decodeModernRuns(value)
	if styles == nil || styles.Source != modernRunFingerprint(text, weight) {
		return fallback
	}
	plain := func(runs []sceneRun) string {
		var b strings.Builder
		for _, run := range runs {
			b.WriteString(run.Text)
		}
		return b.String()
	}
	if plain(styles.Runs) != plain(fallback) {
		return fallback
	}
	return styles.Runs
}

func exportedRichRuns(element Element) []sceneRun {
	q, _ := url.ParseQuery(element.Query)
	styles := decodeModernRuns(q.Get("modern-runs"))
	if styles == nil || styles.Source != modernRunFingerprint(element.Text, q.Get("ttf-weight")) {
		return nil
	}
	if element.Kind == "bullet" {
		element.Kind = "text"
	}
	return sceneTextFor(element).Runs
}

// Preserve untouched run styles across a legacy editor's single replacement.
// New text inherits its neighbour's emphasis; colour tags remain authoritative.
func preserveRunStyles(before, after Element) Element {
	if after.Extra == nil {
		after.Extra = cloneJSONExtensions(before.Extra)
	}
	if before.Kind != after.Kind {
		return after
	}
	if before.Kind == "shape" {
		oldLabel, _, oldErr := sceneEditableShapeLabel(before)
		newLabel, data, newErr := sceneEditableShapeLabel(after)
		if oldErr != nil || newErr != nil || data == nil {
			return after
		}
		updated := preserveRunStyles(oldLabel, newLabel)
		if updated.Query != newLabel.Query {
			data.Query = updated.Query
			if encoded, err := json.Marshal(data); err == nil && len(encoded) <= 65536 {
				after.Query = setQueryValue(after.Query, "shape-label", base64.StdEncoding.EncodeToString(encoded))
			}
		}
		return after
	}
	oldQ, _ := url.ParseQuery(before.Query)
	newQ, _ := url.ParseQuery(after.Query)
	value := oldQ.Get("modern-runs")
	if value == "" || newQ.Get("modern-runs") != value || (before.Text == after.Text && oldQ.Get("ttf-weight") == newQ.Get("ttf-weight")) {
		return after
	}
	oldRuns := exportedRichRuns(before)
	if oldRuns == nil {
		return after
	}
	plainElement := after
	newQ.Del("modern-runs")
	plainElement.Query = newQ.Encode()
	if plainElement.Kind == "bullet" {
		plainElement.Kind = "text"
	}
	flatten := func(runs []sceneRun) []sceneRun {
		var out []sceneRun
		for _, run := range runs {
			for _, ch := range run.Text {
				r := run
				r.Text = string(ch)
				out = append(out, r)
			}
		}
		return out
	}
	old := flatten(oldRuns)
	next := flatten(sceneTextFor(plainElement).Runs)
	start, end := 0, 0
	for start < len(old) && start < len(next) && old[start].Text == next[start].Text {
		start++
	}
	for end < len(old)-start && end < len(next)-start && old[len(old)-1-end].Text == next[len(next)-1-end].Text {
		end++
	}
	inherit := sceneRun{}
	if start > 0 {
		inherit = old[start-1]
	} else if len(old) > 0 {
		inherit = old[0]
	}
	weightChanged := oldQ.Get("ttf-weight") != newQ.Get("ttf-weight")
	for i := range next {
		style := inherit
		if i < start {
			style = old[i]
		} else if i >= len(next)-end {
			style = old[len(old)-(len(next)-i)]
		}
		next[i].Bold, next[i].Italic = style.Bold, style.Italic
		next[i].Underline = style.Underline
		if after.Kind == "code" {
			next[i].Color = style.Color
		}
		if weightChanged {
			next[i].Bold = newQ.Get("ttf-weight") == "bold"
		}
	}
	if encoded, err := encodeModernRuns(after.Text, newQ.Get("ttf-weight"), after.Kind, next); err == nil {
		if encoded != "" {
			newQ.Set("modern-runs", encoded)
		}
		after.Query = newQ.Encode()
	}
	return after
}

func withConvertedRuns(element Element, runs []sceneRun) Element {
	var text strings.Builder
	for _, run := range runs {
		if run.Color != "" && element.Kind != "code" {
			text.WriteString("[color=" + run.Color + "]" + run.Text + "[/color]")
		} else {
			text.WriteString(run.Text)
		}
	}
	element.Text = text.String()
	q, _ := url.ParseQuery(element.Query)
	q.Del("modern-runs")
	if value, err := encodeModernRuns(element.Text, q.Get("ttf-weight"), element.Kind, runs); err == nil && value != "" {
		q.Set("modern-runs", value)
	}
	element.Query = q.Encode()
	return element
}

func splitStyledLines(runs []sceneRun, trimRight bool) [][]sceneRun {
	lines := [][]sceneRun{{}}
	for _, run := range runs {
		for i, text := range strings.Split(strings.ReplaceAll(run.Text, "\r\n", "\n"), "\n") {
			if i > 0 {
				lines = append(lines, []sceneRun{})
			}
			if text != "" {
				part := run
				part.Text = text
				lines[len(lines)-1] = append(lines[len(lines)-1], part)
			}
		}
	}
	for i, line := range lines {
		for len(line) > 0 {
			line[0].Text = strings.TrimLeft(line[0].Text, " \t")
			if line[0].Text != "" {
				break
			}
			line = line[1:]
		}
		if trimRight {
			for len(line) > 0 {
				last := len(line) - 1
				line[last].Text = strings.TrimRight(line[last].Text, " \t")
				if line[last].Text != "" {
					break
				}
				line = line[:last]
			}
		}
		lines[i] = line
	}
	return lines
}
