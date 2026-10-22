package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

func placementFieldOwner(key string) string {
	switch key {
	case "color":
		return "fg"
	case "slide":
		return "link"
	}
	return key
}

// Keep legacy UI normalization inside a generous document safety envelope.
const maxPlacementCoordinate = 1_000_000

// These values participate in integer row/column arithmetic or conversion to
// pixel geometry. A finite float alone is not a safe coordinate. The envelope
// is not a canvas-size limit, nor permission to allocate a raster of this size;
// existing per-renderer dimension/work limits still apply.
func boundedPlacementCoordinate(key string, value float64) bool {
	switch key {
	case "top", "bottom", "left", "right", "row_delta", "width", "height":
		return value >= -maxPlacementCoordinate && value <= maxPlacementCoordinate
	}
	return true
}

// Validate before normalizers can drop malformed input or turn unsafe values
// into a limit/default. Compatible future keys remain opaque.
func validatePlacementNumber(key, value string) error {
	for _, control := range effectControls {
		if control.key == key && !validEffectValue(control, value) {
			return fmt.Errorf("invalid object effect field %q", key)
		}
	}
	if strings.HasPrefix(key, "label-") && labelStyleKey(strings.TrimPrefix(key, "label-")) {
		key = strings.TrimPrefix(key, "label-")
	}
	integer := false
	switch key {
	case "text-bold", "text-italic", "text-underline":
		if value == "0" || value == "1" {
			return nil
		}
		return fmt.Errorf("text emphasis field %q must be 0 or 1", key)
	case "top", "bottom", "left", "right", "row_delta", "ttf-size", "text-size", "z-index", "shadow-x", "shadow-y", "alpha", "slide":
		integer = true
	case "default-size", "default-width", "default-line-height", "default-stroke-width", "width", "height", "shape-offset-x", "shape-offset-y", "object-offset-x", "object-offset-y", "left_pct", "right_pct", "modern-line-height", "modern-paragraph-before", "modern-paragraph-after", "modern-size", "modern-width", "ttf-width", "object-rotation", "modern-opacity", "scale", "brightness", "contrast", "saturation", "sharpness", "shape-stroke-width", "connector-width", "connector-arrow-width":
	default:
		return nil
	}
	if integer {
		if n, err := strconv.Atoi(value); err == nil && boundedPlacementCoordinate(key, float64(n)) {
			return nil
		}
	} else if n, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && boundedPlacementCoordinate(key, n) {
		return nil
	}
	return fmt.Errorf("invalid numeric placement field %q", key)
}

func validateElementPlacementQuery(element Element) error {
	// Future element kinds own their opaque queries. Do not reinterpret them.
	if !standardElementKind(element.Kind) || element.Query == "" {
		return nil
	}
	if err := validatePlacementQuery(element.Query); err != nil {
		return err
	}
	if element.Kind == "shape" {
		q, _ := url.ParseQuery(element.Query)
		payload := q.Get("shape-label")
		if len(payload) > base64.StdEncoding.EncodedLen(65536) {
			return nil // The label codec owns oversized/malformed payload policy.
		}
		raw, err := base64.StdEncoding.DecodeString(payload)
		var label shapeLabelData
		if err == nil && len(raw) <= 65536 && json.Unmarshal(raw, &label) == nil {
			if err := validatePlacementQuery(label.Query); err != nil {
				return fmt.Errorf("shape label: %w", err)
			}
		}
	}
	return nil
}

func validatePlacementQuery(query string) error {
	q, err := url.ParseQuery(query)
	if err != nil {
		return fmt.Errorf("invalid placement query: %w", err)
	}
	owners := map[string]bool{}
	for rawKey, candidates := range q {
		key := strings.ToLower(rawKey)
		if !placementMetadataKey(key) {
			continue
		}
		owner := placementFieldOwner(key)
		if owners[owner] || len(candidates) != 1 {
			return fmt.Errorf("duplicate placement field %q", owner)
		}
		owners[owner] = true
		if !validateConcreteDefaultField(key, candidates[0]) {
			return fmt.Errorf("invalid concrete style default %q", key)
		}
		if err := validatePlacementNumber(key, candidates[0]); err != nil {
			return err
		}
	}
	return nil
}

func validateSlidePlacementQueries(slide Slide, owner string) error {
	for i, element := range slide.Elements {
		if err := validateElementPlacementQuery(element); err != nil {
			return fmt.Errorf("%s element %d: %w", owner, i+1, err)
		}
	}
	return nil
}

// Admission/save only: avoid a full-deck scan on ordinary numeric mutations.
func validateDeckPlacementQueries(deck Deck) error {
	if err := validateSlidePlacementQueries(deck.Masters.Base.Slide, "base master"); err != nil {
		return err
	}
	for _, layout := range deck.Masters.Layouts {
		if err := validateSlidePlacementQueries(layout.Slide, "layout "+layout.ID); err != nil {
			return err
		}
	}
	for i, slide := range deck.Slides {
		if err := validateSlidePlacementQueries(slide, fmt.Sprintf("slide %d", i+1)); err != nil {
			return err
		}
	}
	return nil
}
