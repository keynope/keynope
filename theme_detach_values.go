package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// Pure building blocks for appearance-preserving theme detachment. Callers
// freeze theme-derived defaults, never slide/master structure. Structured
// payload fallbacks remain weaker than explicit owner/slide formatting, so one
// shared payload can retain different appearances in different instances.
func concreteThemeTextValues(deck Deck, element Element, values url.Values) (url.Values, error) {
	q := cloneQueryValues(values)
	if deck.Theme == nil {
		return q, nil
	}
	q = deck.resolveThemeTextValues(element, q)
	if token := q.Get("theme-fg"); token != "" {
		if !q.Has("fg") && !q.Has("header") {
			if color := deck.themeTokenColor(token); color != "" {
				q.Set("fg", color)
			}
		}
		q.Del("theme-fg")
	}
	if styles := decodeModernRuns(q.Get("modern-runs")); styles != nil {
		changed := false
		for i := range styles.Runs {
			run := &styles.Runs[i]
			if run.ColorToken != "" {
				run.Color = deck.themeTokenColor(run.ColorToken)
				run.ColorToken = ""
				changed = true
			}
			if run.FontRole != "" {
				run.FontID = deck.wordFontID(run.FontRole, run.FontID)
				run.FontRole = ""
				changed = true
			}
		}
		if changed {
			// Preserve the exact source fingerprint and text, even for a stale
			// dormant run payload. Do not round-trip through inline markup.
			data, err := json.Marshal(styles)
			if err != nil {
				return nil, err
			}
			q.Set("modern-runs", base64.StdEncoding.EncodeToString(data))
		}
	}
	return q, nil
}

// Materialize only theme-derived defaults on an authored source. Master-bound
// children keep missing fields missing so the master's converted defaults can
// still flow through. Explicit role choices are unlinked separately.
func concreteThemeSourceElement(deck Deck, element Element) (Element, error) {
	result := cloneElement(element)
	if deck.Theme == nil || !standardElementKind(element.Kind) {
		return result, nil
	}
	q, err := url.ParseQuery(element.Query)
	if err != nil {
		return Element{}, err
	}
	setDefault := func(key, value string) {
		if !q.Has(key) {
			q.Set(key, value)
		}
	}
	font := deck.Theme.Fonts.Body
	color := deck.Theme.Colors.Text
	switch element.Kind {
	case "heading":
		font = deck.Theme.Fonts.Heading
		color = deck.Theme.Colors.Heading
		if element.Level == 2 {
			font.Size = math.Max(1, font.Size/2)
		}
	case "code":
		font = deck.Theme.Fonts.Mono
	case "connector":
		color = deck.Theme.Colors.Line
	}
	if element.MasterSlotID == "" {
		switch element.Kind {
		case "heading", "text", "bullet", "code", "text-image", "page-number":
			setDefault("default-font", font.FontID)
			setDefault("default-size", strconv.FormatFloat(font.Size, 'f', -1, 64))
			setDefault("default-width", strconv.FormatFloat(font.Width, 'f', -1, 64))
			setDefault("default-line-height", strconv.FormatFloat(font.LineHeight, 'f', -1, 64))
		}
		if element.Kind != "table" && element.Kind != "chart" {
			setDefault("default-color", color)
		}
		if element.Kind == "shape" {
			fill, line := deck.Theme.Defaults.ShapeFill, deck.Theme.Defaults.ShapeLine
			if fill == "" {
				fill = "inherit"
			}
			if line == "" {
				line = "none"
			}
			setDefault("default-fill", fill)
			setDefault("default-stroke", line)
			setDefault("default-stroke-width", strconv.FormatFloat(deck.Theme.Defaults.LineWidth, 'f', -1, 64))
		}
	}
	if q.Get("modern-font") == "theme" {
		q.Set("modern-font", font.FontID)
	}
	if token := q.Get("theme-fg"); token != "" {
		if !q.Has("fg") && !q.Has("header") {
			q.Set("fg", deck.themeTokenColor(token))
		}
		q.Del("theme-fg")
	}
	if role, ok := strings.CutPrefix(q.Get("shape-stroke"), "theme:"); ok {
		q.Set("shape-stroke", deck.themeTokenColor(role))
	}
	if q.Has("modern-runs") {
		mapped, err := concreteThemeTextValues(deck, element, q)
		if err != nil {
			return Element{}, err
		}
		q.Set("modern-runs", mapped.Get("modern-runs"))
	}
	result.Query = q.Encode()
	if element.Kind == "shape" {
		var labelErr error
		result = mapShapeLabelQuery(result, func(label shapeLabelData, values url.Values) url.Values {
			if !values.Has("fg") && !values.Has("theme-fg") && !values.Has("default-color") {
				// Captions already have an independent white foreground (see
				// shapeLabel), even on slides with a foreground override. This
				// is not the slide-sensitive theme text fallback.
				values.Set("fg", "#ffffff")
			}
			converted, err := concreteThemeSourceElement(deck, Element{Kind: "text", Text: label.Text, Query: values.Encode()})
			if err != nil {
				labelErr = err
				return values
			}
			return mustQuery(converted.Query)
		})
		if labelErr != nil {
			return Element{}, labelErr
		}
		outer := mustQuery(result.Query)
		if outer.Get("label-modern-font") == "theme" {
			outer.Set("label-modern-font", deck.Theme.Fonts.Body.FontID)
		}
		if token := outer.Get("label-theme-fg"); token != "" {
			if !outer.Has("label-fg") {
				outer.Set("label-fg", deck.themeTokenColor(token))
			}
			outer.Del("label-theme-fg")
		}
		if raw := outer.Get("label-modern-runs"); raw != "" && raw != "none" {
			// The span transformer preserves the label's source and updates both
			// nested and sparse roles without flattening the authored shape.
			result.Query = outer.Encode()
			result = flattenWordColorTokens(deck.captureWordColorFallbacks(result))
			outer = mustQuery(result.Query)
		}
		result.Query = outer.Encode()
	}
	return result, nil
}

func concreteThemeTable(deck Deck, table TableData) (TableData, error) {
	if err := table.Validate(); err != nil {
		return TableData{}, err
	}
	if deck.Theme == nil {
		return cloneTable(table), nil
	}
	// Capture defaults, not one instance's foreground. Concrete default colours
	// remain weaker than owner/slide paint, so a shared payload need not fork.
	fallback := captureClipboardTableTheme(deck, Element{Kind: "table"}, table)
	if err := fallback.validate(table); err != nil {
		return TableData{}, fmt.Errorf("table theme fallback: %w", err)
	}
	result := applyClipboardTableTheme(table, fallback)
	if result.Style.Cell.DefaultColor == "" {
		result.Style.Cell.DefaultColor = deck.Theme.Colors.Text
	}
	for _, paint := range fallback.Cells {
		original := mergeTableCellStyle(table.Style.Cell, table.Cells[paint.Row][paint.Column].Style)
		if original.Color == "" {
			style := result.Cells[paint.Row][paint.Column].Style
			style.Color = ""
			style.DefaultColor = paint.Color
		}
	}
	result = mapTableWordColors(result, deck, false)
	result = mapTableWordColors(result, deck, true)
	if err := result.Validate(); err != nil {
		return TableData{}, err
	}
	return result, nil
}

func concreteThemeChart(deck Deck, owner Element, chart ChartData) (ChartData, Element, error) {
	if err := chart.Validate(); err != nil {
		return ChartData{}, Element{}, err
	}
	if deck.Theme == nil {
		return cloneChart(chart), cloneElement(owner), nil
	}
	owner = cloneElement(owner)
	q, err := url.ParseQuery(owner.Query)
	if err != nil {
		return ChartData{}, Element{}, err
	}
	if token := q.Get("theme-fg"); token != "" {
		if !q.Has("fg") && !q.Has("header") {
			q.Set("fg", deck.themeTokenColor(token))
		}
		q.Del("theme-fg")
	}
	if q.Get("modern-font") == "theme" {
		q.Set("modern-font", deck.Theme.Fonts.Body.FontID)
	}
	owner.Query = q.Encode()
	fallback := captureClipboardChartTheme(deck, Element{Kind: "chart"}, chart)
	if err := fallback.validate(chart); err != nil {
		return ChartData{}, Element{}, fmt.Errorf("chart theme fallback: %w", err)
	}
	unusedOwner := Element{Kind: "chart"}
	result := applyClipboardChartTheme(chart, &unusedOwner, fallback)
	if result.Options.DefaultColor == "" {
		result.Options.DefaultColor = deck.Theme.Colors.Text
	}
	if chart.Options.FontID == "" {
		result.Options.FontID = ""
		if result.Options.DefaultFontID == "" {
			result.Options.DefaultFontID = deck.Theme.Fonts.Body.FontID
		}
	}
	if !chart.Options.Stacked {
		for i := range result.Series {
			result.Series[i].LabelColor = deck.tableColor(chart.Series[i].LabelColor)
		}
	}
	if err := result.Validate(); err != nil {
		return ChartData{}, Element{}, err
	}
	return result, owner, nil
}
