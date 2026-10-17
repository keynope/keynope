package app

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

type imageSelectionControl struct {
	key                string
	min, max, fallback float64
	choices            string
	integer            bool
	colour             bool
}

var imageSelectionControls = map[string]imageSelectionControl{
	"image.style":      {key: "image-style", choices: "modern retro"},
	"image.brightness": {key: "brightness", min: .2, max: 2, fallback: 1},
	"image.contrast":   {key: "contrast", min: .2, max: 2, fallback: 1},
	"image.saturation": {key: "saturation", min: 0, max: 2, fallback: 1},
	"image.sharpness":  {key: "sharpness", min: .2, max: 2, fallback: 1},
	"image.alpha":      {key: "alpha", min: 0, max: 255, fallback: 96, integer: true},
	"image.glyph":      {key: "glyph", choices: "blocks braille ascii dense"},
	"image.sampling":   {key: "shape", choices: "subject contrast saturation luma alpha"},
	"image.tint":       {key: "tint", colour: true},
	"image.mask":       {key: "modern-mask", choices: "none rounded ellipse"},
	"image.fit":        {key: "image-fit", choices: "legacy contain cover stretch"},
}

func imageSelectionValue(authored Element, q, rq url.Values, control imageSelectionControl) (interface{}, bool, bool) {
	if authored.Kind != "image" {
		return nil, false, false
	}
	raw := rq.Get(control.key)
	if control.colour {
		value := "none"
		if _, _, _, ok := parseHexColour(raw); ok {
			value = raw
		}
		clears := strings.Split(q.Get("master-clear"), ",")
		explicit := q.Has(control.key)
		for _, key := range clears {
			explicit = explicit || key == control.key
		}
		return value, explicit, true
	}
	if control.choices != "" {
		value := strings.ToLower(raw)
		if control.key == "glyph" && value == "block" {
			value = "blocks"
		}
		for _, choice := range strings.Fields(control.choices) {
			if value == choice {
				return value, q.Has(control.key), true
			}
		}
		explicit := q.Has(control.key)
		for _, key := range strings.Split(q.Get("master-clear"), ",") {
			explicit = explicit || strings.TrimSpace(key) == control.key
		}
		return strings.Fields(control.choices)[0], explicit, true
	}
	value, err := strconv.ParseFloat(raw, 64)
	if control.integer {
		integer, integerErr := strconv.Atoi(raw)
		value, err = float64(integer), integerErr
	}
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || (control.min > 0 && value <= 0) || (control.key == "saturation" && value < 0) || (control.integer && math.Trunc(value) != value) {
		value = control.fallback
	}
	value = math.Max(control.min, math.Min(control.max, value))
	return value, q.Has(control.key), true
}

func applyImageSelection(slide *Slide, ids []string, control imageSelectionControl, raw json.RawMessage) (bool, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return false, fmt.Errorf("image %s requires a value", control.key)
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		if value != "" {
			valid := false
			if control.colour {
				_, _, _, valid = parseHexColour(value)
				valid = valid || value == "none"
			}
			for _, choice := range strings.Fields(control.choices) {
				valid = valid || value == choice
			}
			if !valid {
				return false, fmt.Errorf("unsupported image %s value", control.key)
			}
		}
	} else {
		var number float64
		if control.colour || control.choices != "" || json.Unmarshal(raw, &number) != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < control.min || number > control.max || control.integer && math.Trunc(number) != number {
			return false, fmt.Errorf("image %s must be between %g and %g", control.key, control.min, control.max)
		}
		value = strconv.FormatFloat(number, 'f', -1, 64)
	}
	selected := map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	changed := false
	for i, element := range slide.Elements {
		if !selected[element.ID] || element.Kind != "image" {
			continue
		}
		q, _ := url.ParseQuery(element.Query)
		before := q.Encode()
		disabled := value == "none" && (control.colour || control.key == "modern-mask")
		if value == "" || disabled {
			q.Del(control.key)
		} else {
			q.Set(control.key, value)
		}
		// Inherit restores the master; an explicit neutral value stays explicit.
		if element.MasterSlotID != "" {
			setMasterQueryClears(q, []string{control.key}, nil)
			if disabled {
				setMasterQueryClears(q, nil, []string{control.key})
			}
		}
		if next := q.Encode(); next != before {
			slide.Elements[i].Query = next
			changed = true
		}
	}
	return changed, nil
}

// Convert natural-media sizing to an explicit frame when changing rendering
// style or entering a fit mode. Treatment changes must not change layout.
// Work on an owned candidate so failed projection cannot partially edit.
func applyImageFitSelection(deck *Deck, slide *Slide, resolved Slide, index int, ids []string, control imageSelectionControl, raw json.RawMessage) (bool, error) {
	candidate := cloneSlide(*slide)
	changed, err := applyImageSelection(&candidate, ids, control, raw)
	if err != nil || !changed {
		return changed, err
	}
	var value string
	_ = json.Unmarshal(raw, &value)
	if control.key != "image-style" && imageFit(value) == "" {
		*slide = candidate
		return true, nil
	}
	if err := freezeNaturalImageFrames(*deck, resolved, &candidate, index, ids); err != nil {
		return false, err
	}
	*slide = candidate
	return true, nil
}

// Capture displayed bounds before changing source-space treatment. A legacy
// natural image otherwise derives a new frame from the cropped source aspect.
// Only the owned candidate changes; explicit fit/stretch frames remain intact.
func freezeNaturalImageFrames(deck Deck, resolved Slide, candidate *Slide, index int, ids []string) error {
	selected := map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	targets := map[string]bool{}
	for _, e := range resolved.Elements {
		q, _ := url.ParseQuery(e.Query)
		if selected[e.ID] && imageFit(q.Get("image-fit")) == "" && q.Get("stretch") != "1" {
			targets[e.ID] = true
		}
	}
	if len(targets) > 0 {
		cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
		scene, err := buildStyledSceneTargetsMeasured(deck, resolved, index, cols, rows, true, targets, nil)
		if err != nil {
			return err
		}
		bounds := map[string]sceneRect{}
		for _, object := range scene.Objects {
			if object.Kind == "image" {
				bounds[object.ID] = object.Bounds
			}
		}
		for i, e := range candidate.Elements {
			if !targets[e.ID] {
				continue
			}
			b, ok := bounds[e.ID]
			if !ok || b.Width <= 0 || b.Height <= 0 {
				return fmt.Errorf("image frame is unavailable")
			}
			q, _ := url.ParseQuery(e.Query)
			owned := []string{"left_pct", "right", "right_pct", "bottom", "align", "valign", "row_delta", "object-offset-x", "object-offset-y", "scale"}
			for _, key := range owned {
				q.Del(key)
			}
			for _, axis := range []struct {
				key, offset string
				v           float64
			}{{"left", "object-offset-x", b.X * float64(cols) / scene.Width}, {"top", "object-offset-y", b.Y * float64(rows) / scene.Height}} {
				whole := math.Floor(axis.v)
				q.Set(axis.key, strconv.FormatFloat(whole, 'f', -1, 64))
				if fraction := axis.v - whole; fraction != 0 {
					q.Set(axis.offset, strconv.FormatFloat(fraction, 'f', -1, 64))
				}
			}
			q.Set("width", strconv.FormatFloat(b.Width*float64(cols)/scene.Width, 'f', -1, 64))
			q.Set("height", strconv.FormatFloat(b.Height*float64(rows)/scene.Height, 'f', -1, 64))
			q.Set("stretch", "1")
			if e.MasterSlotID != "" {
				var clears []string
				for _, key := range owned {
					if !q.Has(key) {
						clears = append(clears, key)
					}
				}
				setMasterQueryClears(q, owned, clears)
			}
			candidate.Elements[i].Query = q.Encode()
		}
	}
	return nil
}
