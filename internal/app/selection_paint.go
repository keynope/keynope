package app

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func selectionPaintProperty(property string) bool {
	switch property {
	case "paint.fill", "paint.color", "paint.colorToken", "paint.strokeToken", "paint.outline", "paint.strokeColor", "paint.strokeWidth", "paint.gradientStart", "paint.gradientEnd", "paint.gradientDirection", "paint.shadow", "paint.shadowColor", "paint.shadowX", "paint.shadowY":
		return true
	}
	return false
}

// The base colour does not rewrite inline colour runs or shape-label styling.
// Effect applicability uses the same type contract as portable style transfer.
func selectionPaintValue(deck Deck, slide Slide, authored, resolved Element, q, rq url.Values, property string) (interface{}, bool, bool, bool) {
	if !selectionPaintProperty(property) {
		return nil, false, false, false
	}
	eligibility := property
	if property == "paint.colorToken" {
		eligibility = "paint.color"
	}
	if property == "paint.strokeToken" {
		eligibility = "paint.strokeColor"
	}
	if !styleFieldEligible(authored, eligibility) {
		return nil, false, false, true
	}
	key := objectStyleKeys[property]
	explicit := q.Has(key)
	value := rq.Get(key)
	switch property {
	case "paint.fill":
		paint := deck.applyThemeObjectPaint(resolved, slide, rq, scenePaintFromValues(resolved, slide, 1, 1, rq, nil))
		if paint.NoFill {
			return "none", explicit, true, true
		}
		return "solid", explicit, true, true
	case "paint.strokeToken":
		value := rq.Get("shape-stroke")
		if strings.HasPrefix(value, "theme:") {
			return strings.TrimPrefix(value, "theme:"), q.Has("shape-stroke"), true, true
		}
		if value != "" {
			return "custom", q.Has("shape-stroke"), true, true
		}
		return "", q.Has("shape-stroke"), true, true
	case "paint.colorToken":
		if rq.Has("fg") || rq.Has("header") {
			return "custom", q.Has("fg") || q.Has("header"), true, true
		}
		return rq.Get("theme-fg"), q.Has("theme-fg"), true, true
	case "paint.strokeColor", "paint.strokeWidth":
		paint := deck.applyThemeObjectPaint(resolved, slide, rq, scenePaintFromValues(resolved, slide, 1, 1, rq, nil))
		if property == "paint.strokeColor" {
			color := paint.Stroke
			if color == "" {
				color = "none"
			}
			return color, explicit, true, true
		}
		width := paint.StrokeWidth
		if !rq.Has("shape-stroke-width") && width <= 0 {
			width = 2
			if defaults, present := deck.shapePaintDefaults(rq); present {
				width = defaults.LineWidth
			}
		}
		return width, explicit, true, true
	case "paint.color":
		paint := deck.applyThemeObjectPaint(resolved, slide, rq, scenePaintFromValues(resolved, slide, 1, 1, rq, nil))
		return paint.Color, q.Has("fg") || q.Has("header") || q.Has("theme-fg"), true, true
	case "paint.outline":
		if rq.Has("shape-stroke") || rq.Has("shape-stroke-width") {
			return "custom", q.Has("outline"), true, true
		}
		if value == "1" {
			value = "dark" // The shared scene treats the legacy shorthand as dark.
		}
		if value == "" {
			value = "none"
			paint := deck.applyThemeObjectPaint(resolved, slide, rq, scenePaintFromValues(resolved, slide, 1, 1, rq, nil))
			if paint.StrokeWidth > 0 {
				// A theme can provide an arbitrary stroke colour; do not label it
				// as a dark or light explicit preset.
				value = "theme"
			}
		}
	case "paint.gradientDirection":
		if gradient, ok := textGradientFromValues(rq); ok {
			value = gradient.direction
		} else {
			value = "none"
		}
	case "paint.gradientStart", "paint.gradientEnd":
		if color, ok := normalizeHexColour(value); ok {
			value = color
		} else {
			value = ""
		}
	case "paint.shadow":
		if value != "soft" && value != "solid" {
			value = "none"
		}
	case "paint.shadowColor":
		if color, ok := normalizeHexColour(value); ok {
			value = color
		} else {
			value = "#ffffff"
		}
	case "paint.shadowX", "paint.shadowY":
		// Report effective offsets, including the renderer's (0,0) fallback.
		x := clampInt(intQueryDefault(rq, "shadow-x", 1), -4, 4)
		y := clampInt(intQueryDefault(rq, "shadow-y", 1), -4, 4)
		if x == 0 && y == 0 {
			x, y = 1, 1
		}
		if property == "paint.shadowX" {
			return float64(x), explicit, true, true
		}
		return float64(y), explicit, true, true
	}
	return value, explicit, true, true
}

func applySelectionPaint(slide *Slide, resolved Slide, ids []string, property, text string, number float64) (bool, error) {
	if property == "paint.strokeToken" {
		if text != "" && !validThemeColorToken(text) {
			return false, fmt.Errorf("choose a supported theme colour role")
		}
		if text != "" {
			text = "theme:" + text
		}
		property = "paint.strokeColor"
	}
	key := objectStyleKeys[property]
	if property == "paint.colorToken" {
		key = "theme-fg"
	}
	value := text
	if property == "paint.colorToken" {
		if value != "" && !validThemeColorToken(value) {
			return false, fmt.Errorf("choose a supported theme colour role")
		}
	} else if property == "paint.strokeWidth" {
		if text == "inherit" {
			value = ""
		} else {
			if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 64 {
				return false, fmt.Errorf("stroke width must be between 0 and 64")
			}
			value = strconv.FormatFloat(number, 'f', -1, 64)
		}
	} else if property == "paint.shadowX" || property == "paint.shadowY" {
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < -4 || number > 4 {
			return false, fmt.Errorf("shadow offset must be an integer between -4 and 4")
		}
		value = strconv.FormatFloat(number, 'f', -1, 64)
	} else if value != "" {
		strokeRole := property == "paint.strokeColor" && strings.HasPrefix(value, "theme:") && validThemeColorToken(strings.TrimPrefix(value, "theme:"))
		if property == "paint.color" || property == "paint.strokeColor" && value != "none" && !strokeRole || property == "paint.gradientStart" || property == "paint.gradientEnd" || property == "paint.shadowColor" {
			color, ok := normalizeHexColour(value)
			if !ok {
				return false, fmt.Errorf("colour must be a hexadecimal RGB colour")
			}
			value = color
		}
		snapshot := &ObjectStyleSnapshot{Version: 1, Fields: map[string]ObjectStyleField{property: {Present: true, Value: value}}}
		if err := snapshot.Validate(); err != nil && !strokeRole {
			return false, err
		}
	}
	eligible := map[string]bool{}
	for _, id := range ids {
		eligible[id] = true
	}
	effective := map[string]url.Values{}
	if property == "paint.gradientDirection" || property == "paint.strokeColor" {
		for _, element := range resolved.Elements {
			if eligible[element.ID] {
				effective[element.ID], _ = url.ParseQuery(element.Query)
			}
		}
	}
	changed := false
	for i, element := range slide.Elements {
		if !eligible[element.ID] {
			continue
		}
		q, _ := url.ParseQuery(element.Query)
		old := q.Encode()
		if property == "paint.fill" && element.MasterSlotID != "" {
			removeMasterStyleClears(q, property)
		}
		if property == "paint.strokeColor" && value != "" && value != "none" && !q.Has("shape-stroke-width") {
			// Choosing a custom colour should not change the visible edge width
			// just because it leaves the preset representation. Presets have a
			// physical two-pixel width; explicit inherited widths still win.
			previous := effective[element.ID]
			if previous.Has("outline") && previous.Get("outline") != "none" && !previous.Has("shape-stroke-width") {
				q.Set("shape-stroke-width", "2")
			}
		}
		if value == "" {
			q.Del(key)
		} else {
			q.Set(key, value)
		}
		if property == "paint.color" || property == "paint.colorToken" {
			// Heading colour and theme token take precedence over fg in some
			// renderers. An explicit base colour replaces both aliases.
			q.Del("header")
			if property == "paint.color" {
				q.Del("theme-fg")
			} else {
				q.Del("fg")
			}
			// Alias precedence must apply across inheritance too: an explicit
			// token overrides a master's concrete fg/header, and explicit RGB
			// overrides a master's heading/token alias. Inherit restores all.
			if element.MasterSlotID != "" {
				clear := []string{}
				if value != "" {
					clear = []string{"header", "theme-fg"}
					if property == "paint.colorToken" {
						clear = []string{"fg", "header"}
					}
				}
				setMasterColorClears(q, clear)
			}
		}
		if property == "paint.outline" && element.Kind == "shape" {
			q.Del("shape-stroke")
			q.Del("shape-stroke-width")
		}
		if property == "paint.strokeColor" && value != "" {
			q.Del("outline")
		}
		if element.Kind == "shape" && element.MasterSlotID != "" && (property == "paint.outline" || property == "paint.strokeColor") {
			setMasterStrokeClears(q)
		}
		if property == "paint.gradientDirection" && value != "" && value != "none" {
			// Enabling a gradient is one transaction, not three transient edits.
			for key, fallback := range map[string]string{"gradient-start": "#55aaff", "gradient-end": "#ff55aa"} {
				if _, ok := parseRGBHex(q.Get(key)); ok {
					continue
				}
				// Do not overwrite valid inherited endpoints when changing mode.
				if _, ok := parseRGBHex(effective[element.ID].Get(key)); ok {
					continue
				}
				q.Set(key, fallback)
			}
		}
		if next := q.Encode(); next != old {
			slide.Elements[i].Query, changed = next, true
		}
	}
	return changed, nil
}

// Own only the three colour aliases; preserve every unrelated master clear.
func setMasterColorClears(q url.Values, additions []string) {
	setMasterQueryClears(q, []string{"fg", "header", "theme-fg"}, additions)
}

// Outline presets and arbitrary strokes are aliases for the same visual edge.
// Explicit local choices must suppress the competing master representation;
// clearing the choice restores inheritance. Keep unrelated suppressions intact.
func setMasterStrokeClears(q url.Values) {
	var additions []string
	if q.Has("outline") {
		additions = append(additions, "shape-stroke", "shape-stroke-width")
	}
	if q.Has("shape-stroke") {
		additions = append(additions, "outline")
	}
	setMasterQueryClears(q, []string{"outline", "shape-stroke", "shape-stroke-width"}, additions)
}

func setMasterQueryClears(q url.Values, owned, additions []string) {
	aliases := map[string]bool{}
	for _, key := range owned {
		aliases[key] = true
	}
	keys := map[string]bool{}
	for _, key := range strings.Split(q.Get("master-clear"), ",") {
		key = strings.TrimSpace(key)
		if key != "" && !aliases[key] {
			keys[key] = true
		}
	}
	for _, key := range additions {
		keys[key] = true
	}
	values := make([]string, 0, len(keys))
	for key := range keys {
		values = append(values, key)
	}
	sort.Strings(values)
	if len(values) == 0 {
		q.Del("master-clear")
	} else {
		q.Set("master-clear", strings.Join(values, ","))
	}
}
