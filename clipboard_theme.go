package main

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// A fallback is a bounded formatting delta, never alternate content, placement,
// IDs or executable metadata. Empty theme-fg deliberately unlinks that role.
func validateClipboardThemeFallback(query string) error {
	q, err := url.ParseQuery(query)
	if err != nil || len(query) > 4096 || len(q) > 9 {
		return fmt.Errorf("invalid clipboard theme fallback")
	}
	for key, values := range q {
		if len(values) != 1 {
			return fmt.Errorf("duplicate clipboard theme fallback")
		}
		value := values[0]
		valid := false
		switch key {
		case "shape-fill":
			valid = value == "solid" || value == "none"
		case "theme-fg":
			valid = value == ""
		case "fg", "shape-stroke":
			_, valid = normalizeHexColour(value)
			valid = valid || key == "shape-stroke" && value == "none"
		case "modern-font":
			valid = validTextFont(value)
		case "modern-size", "ttf-size", "modern-width", "ttf-width", "modern-line-height", "shape-stroke-width":
			n, err := strconv.ParseFloat(value, 64)
			low, high := 1.0, 1024.0
			if key == "modern-width" || key == "ttf-width" {
				high = 200
			} else if key == "modern-line-height" {
				low, high = .5, 4
			} else if key == "shape-stroke-width" {
				low, high = 0, 64
			}
			valid = err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= low && n <= high
		}
		if !valid {
			return fmt.Errorf("invalid clipboard theme fallback property %q", key)
		}
	}
	return nil
}

func validateClipboardLabelThemeFallback(query string) error {
	if err := validateClipboardThemeFallback(query); err != nil {
		return err
	}
	q, _ := url.ParseQuery(query)
	for key := range q {
		if key == "shape-fill" || key == "shape-stroke" || key == "shape-stroke-width" {
			return fmt.Errorf("label fallback cannot change shape paint")
		}
	}
	return nil
}

func clipboardThemeFallback(deck Deck, element Element) string {
	if deck.Theme == nil {
		return ""
	}
	q, err := url.ParseQuery(element.Query)
	if err != nil {
		return ""
	}
	fallback := url.Values{}
	switch element.Kind {
	case "text", "heading", "bullet", "code", "text-image", "page-number":
		resolved := deck.resolveThemeTextValues(element, q)
		for _, key := range []string{"modern-font", "modern-size", "ttf-size", "modern-width", "ttf-width", "modern-line-height"} {
			if resolved.Get(key) != q.Get(key) {
				fallback.Set(key, resolved.Get(key))
			}
		}
	case "shape", "connector":
	default:
		return ""
	}
	if !q.Has("fg") && !q.Has("header") {
		paint := deck.applyThemeObjectPaint(element, Slide{ThemeColors: deck.themeColors("modern")}, q, scenePaintFor(element, Slide{ThemeColors: deck.themeColors("modern")}, 1, 1))
		fallback.Set("fg", paint.Color)
		if q.Has("theme-fg") {
			fallback.Set("theme-fg", "")
		}
	}
	if element.Kind == "shape" {
		paint := deck.applyThemeObjectPaint(element, Slide{}, q, scenePaintFor(element, Slide{}, 1, 1))
		fallback.Set("shape-fill", "solid")
		if paint.NoFill {
			fallback.Set("shape-fill", "none")
		}
		if !q.Has("outline") || strings.HasPrefix(q.Get("shape-stroke"), "theme:") {
			fallback.Set("shape-stroke", "none")
			fallback.Set("shape-stroke-width", strconv.FormatFloat(paint.StrokeWidth, 'f', -1, 64))
			if paint.Stroke != "" {
				fallback.Set("shape-stroke", paint.Stroke)
			}
		}
	}
	return fallback.Encode()
}
