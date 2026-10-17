package app

import (
	"math"
	"net/url"
	"strconv"
	"strings"
)

// Concrete source-format defaults are weaker than normal object/master/slide
// fields. They contain only fixed supported values, never another live theme.
func concreteDefaultKey(key string) bool {
	switch key {
	case "default-font", "default-size", "default-width", "default-line-height", "default-color", "default-fill", "default-stroke", "default-stroke-width":
		return true
	}
	return false
}

func validConcreteDefault(key, value string) bool {
	switch key {
	case "default-font":
		return validTextFont(value)
	case "default-color", "default-fill", "default-stroke":
		return key == "default-fill" && value == "inherit" || (key != "default-color" && value == "none") || len(value) == 7 && validObjectColor(value)
	case "default-size", "default-width", "default-line-height", "default-stroke-width":
		low, high := 1.0, 1024.0
		switch key {
		case "default-width":
			low, high = 1, 200
		case "default-line-height":
			low, high = .5, 4
		case "default-stroke-width":
			low, high = 0, 64
		}
		n, err := strconv.ParseFloat(value, 64)
		return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= low && n <= high
	}
	return false
}

func concreteTextDefaults(q url.Values) url.Values {
	// Borrow unless a valid default actually supplies a missing property.
	owned := false
	set := func(key, value string) {
		if !owned {
			q = cloneQueryValues(q)
			owned = true
		}
		q.Set(key, value)
	}
	if !q.Has("modern-font") && validConcreteDefault("default-font", q.Get("default-font")) {
		set("modern-font", q.Get("default-font"))
	}
	if !q.Has("modern-size") && !q.Has("ttf-size") && validConcreteDefault("default-size", q.Get("default-size")) {
		n, _ := strconv.ParseFloat(q.Get("default-size"), 64)
		set("modern-size", q.Get("default-size"))
		set("ttf-size", strconv.Itoa(int(math.Round(n))))
	}
	if !q.Has("modern-width") && !q.Has("ttf-width") && validConcreteDefault("default-width", q.Get("default-width")) {
		set("modern-width", q.Get("default-width"))
		set("ttf-width", q.Get("default-width"))
	}
	if !q.Has("modern-line-height") && validConcreteDefault("default-line-height", q.Get("default-line-height")) {
		set("modern-line-height", q.Get("default-line-height"))
	}
	return q
}

func slideHasConcreteTextDefaults(slide Slide) bool {
	for _, element := range slide.Elements {
		query := element.Query
		for query != "" {
			part, rest, _ := strings.Cut(query, "&")
			query = rest
			key, _, _ := strings.Cut(part, "=")
			if strings.Contains(key, "default-") || strings.Contains(key, "%") {
				return true
			}
		}
	}
	return false
}

func (deck Deck) shapePaintDefaults(q url.Values) (ThemeDefaults, bool) {
	var defaults ThemeDefaults
	present := deck.Theme != nil
	if present {
		defaults = deck.Theme.Defaults
	}
	if value := q.Get("default-fill"); validConcreteDefault("default-fill", value) {
		defaults.ShapeFill = value
		if value == "inherit" {
			defaults.ShapeFill = ""
		}
		present = true
	}
	if value := q.Get("default-stroke"); validConcreteDefault("default-stroke", value) {
		defaults.ShapeLine = value
		present = true
	}
	if validConcreteDefault("default-stroke-width", q.Get("default-stroke-width")) {
		defaults.LineWidth, _ = strconv.ParseFloat(q.Get("default-stroke-width"), 64)
		present = true
	}
	return defaults, present
}

func validateConcreteDefaultField(key, value string) bool {
	if strings.HasPrefix(key, "label-") && labelStyleKey(strings.TrimPrefix(key, "label-")) {
		key = strings.TrimPrefix(key, "label-")
	}
	return !concreteDefaultKey(key) || validConcreteDefault(key, value)
}
