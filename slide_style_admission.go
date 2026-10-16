package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

func slideStyleMetadataKey(key string) bool {
	switch key {
	case "fg", "bg", "header", "ttf-default-size", "ttf-default-width", "default-style":
		return true
	}
	return false
}

// Called only where the existing parser treats a style-only comment as slide
// defaults. Mixed placement comments retain their own element-level semantics.
// Validate the complete owner delta before writing any fields or claiming keys.
func applySlideStyleChecked(line string, slide *Slide, owners map[string]bool) (bool, error) {
	match := commentRE.FindStringSubmatch(line)
	if match == nil || match[0] != line {
		return false, nil
	}
	body := match[1]
	if len(body) > 4096 {
		first := body
		if index := strings.IndexFunc(first, unicode.IsSpace); index >= 0 {
			first = first[:index]
		}
		key, _, _ := strings.Cut(first, "=")
		if slideStyleMetadataKey(strings.ToLower(key)) {
			return false, fmt.Errorf("slide style metadata exceeds 4096 bytes")
		}
		return false, nil
	}
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return false, nil
	}
	for _, field := range fields {
		key, _, ok := strings.Cut(field, "=")
		if !ok && slideStyleMetadataKey(strings.ToLower(key)) {
			return false, fmt.Errorf("malformed slide style %s metadata", strings.ToLower(key))
		}
		if !ok || !slideStyleMetadataKey(strings.ToLower(key)) {
			return false, nil
		}
	}
	seen := map[string]bool{}
	for _, field := range fields {
		key, value, _ := strings.Cut(field, "=")
		key = strings.ToLower(key)
		if seen[key] || owners["style:"+key] {
			return false, fmt.Errorf("duplicate slide style %s metadata", key)
		}
		seen[key] = true
		valid := false
		switch key {
		case "ttf-default-size":
			n, err := strconv.Atoi(value)
			valid = err == nil && n >= 1 && n <= 512
		case "ttf-default-width":
			n, err := strconv.ParseFloat(value, 64)
			valid = err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 1 && n <= 200
		case "default-style":
			valid = validElementStyle(value)
		case "fg", "bg", "header":
			value = strings.Trim(value, `"'`)
			valid = strings.EqualFold(value, "none")
			if !valid {
				if key == "bg" {
					_, valid = ansiBG(value)
				} else {
					_, valid = ansiFG(value)
				}
			}
		}
		if !valid {
			return false, fmt.Errorf("invalid slide style %s metadata", key)
		}
	}
	if !applySlideStyle(line, slide) {
		return false, fmt.Errorf("invalid slide style metadata")
	}
	for key := range seen {
		owners["style:"+key] = true
	}
	return true, nil
}

func validateSlideStyleSettings(slide Slide) error {
	if slide.TTFSize < 0 || slide.TTFSize > 512 {
		return fmt.Errorf("slide font size must be inherited or between 1 and 512")
	}
	if math.IsNaN(slide.TTFWidth) || math.IsInf(slide.TTFWidth, 0) || slide.TTFWidth != 0 && (slide.TTFWidth < 1 || slide.TTFWidth > 200) {
		return fmt.Errorf("slide font width must be inherited or between 1 and 200")
	}
	if slide.DefaultStyle != "" && !validElementStyle(slide.DefaultStyle) {
		return fmt.Errorf("invalid slide default style")
	}
	for _, setting := range []struct{ name, token string }{{"effect", slide.Effect}, {"background", slide.Background}} {
		name, token := setting.name, setting.token
		if len(token) > 128 {
			return fmt.Errorf("slide %s name exceeds 128 bytes", name)
		}
		for _, r := range token {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				return fmt.Errorf("invalid slide %s name", name)
			}
		}
	}
	return nil
}

func validateSlideStyleAction(action nativeEditorAction) error {
	if action.Action == "update-slide" && action.SlideData != nil {
		if err := validateSlideStyleSettings(*action.SlideData); err != nil {
			return editorFailure("invalid-value", "slide-style", "", err)
		}
	}
	return nil
}

func validateDeckSlideStyleSettings(deck Deck) error {
	if err := validateSlideStyleSettings(deck.Masters.Base.Slide); err != nil {
		return fmt.Errorf("base master: %w", err)
	}
	for _, layout := range deck.Masters.Layouts {
		if err := validateSlideStyleSettings(layout.Slide); err != nil {
			return fmt.Errorf("layout %s: %w", layout.ID, err)
		}
	}
	for i, slide := range deck.Slides {
		if err := validateSlideStyleSettings(slide); err != nil {
			return fmt.Errorf("slide %d: %w", i+1, err)
		}
	}
	return nil
}
