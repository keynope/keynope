package app

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// Placement parsing intentionally preserves compatible future fields for
// authoring. Audience documents instead carry only implemented render inputs.
func participantRenderKey(key string) bool {
	if placementMetadataKey(key) {
		return true
	}
	switch key {
	case "modern-crop", "modern-mask", "image-fit", "alt-text", "image-decorative", "object-name", "layer", "qr", "video-autoplay", "video-loop", "video-muted":
		return true
	}
	for _, definition := range shapeDefinitions {
		for _, adjustment := range definition.Adjustments {
			if key == "shape-"+adjustment.Key {
				return true
			}
		}
	}
	return false
}

// A valid archive may still contain ignored stale text or unknown JSON fields.
// Re-encode only active typed runs, with the same source/plain-text checks as
// the painter. Never publish an archive merely because its base64 is valid.
func participantRunStyles(value string, element Element, values url.Values) string {
	styles := decodeModernRuns(value)
	if styles == nil || styles.Source != modernRunFingerprint(element.Text, values.Get("ttf-weight")) {
		return ""
	}
	kind := element.Kind
	if kind == "bullet" {
		kind = "text"
	}
	fallback := sceneTextFromValues(Element{Kind: kind, Text: element.Text}, url.Values{})
	var plain strings.Builder
	for _, run := range styles.Runs {
		plain.WriteString(run.Text)
	}
	var expected strings.Builder
	for _, run := range fallback.Runs {
		expected.WriteString(run.Text)
	}
	if plain.String() != expected.String() {
		return ""
	}
	raw, err := json.Marshal(styles)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func participantElementQuery(element Element) string {
	source := mustQuery(element.Query)
	public := url.Values{}
	for key, values := range source {
		if participantRenderKey(key) {
			public[key] = append([]string(nil), values...)
		}
	}
	// These fields affect authoring or resolution only, not the detached page.
	for _, key := range []string{"font", "master-clear", "label-style-inherit", "object-locked"} {
		public.Del(key)
	}
	if public.Has("modern-runs") {
		if value := participantRunStyles(public.Get("modern-runs"), element, source); value != "" {
			public.Set("modern-runs", value)
		} else {
			public.Del("modern-runs")
		}
	}
	if element.Kind != "shape" {
		public.Del("shape-label")
		for key := range public {
			if strings.HasPrefix(key, "label-") {
				public.Del(key)
			}
		}
		return public.Encode()
	}
	if public.Has("shape-label") {
		data, err := decodeShapeLabelData(public.Get("shape-label"))
		if err != nil {
			public.Del("shape-label") // Malformed labels have no visible content.
		} else {
			data.Extra = nil
			data.Query = participantElementQuery(Element{Kind: "text", Text: data.Text, Query: data.Query})
			raw, err := json.Marshal(data)
			if err == nil {
				public.Set("shape-label", base64.StdEncoding.EncodeToString(raw))
			}
		}
	}
	if value := public.Get("label-modern-runs"); value != "" && value != "none" {
		if label, ok := rawShapeLabel(element); ok {
			values := mustQuery(label.Query)
			if clean := participantRunStyles(values.Get("modern-runs"), label, values); clean != "" {
				public.Set("label-modern-runs", clean)
			} else {
				// An ignored sparse archive still masks nested runs. Deleting
				// the override would resurrect formatting not currently visible.
				public.Set("label-modern-runs", "none")
			}
		} else {
			public.Del("label-modern-runs")
		}
	}
	return public.Encode()
}

// Hidden objects reserve space in authored flow. Removing their content from
// public Markdown must not pull following visible objects upward. Freeze only
// flow's vertical origin. Already positioned objects and all horizontal
// anchors stay byte-for-byte unchanged, including fractional shape offsets.
func participantFixedOrigin(element Element, bounds sceneRect, rows int, canvas sceneRect) Element {
	q := mustQuery(element.Query)
	if imagePlacementFromValues(q).hasVerticalOffset() {
		return element
	}
	y := bounds.Y*float64(rows)/canvas.Height - objectPlacementOffset(q, "y")
	if element.Kind == "shape" {
		y -= shapeSubcellOffset(q, "shape-offset-y")
	}
	// Flow rows are integral; undo the physical conversion's floating noise
	// without changing genuinely fractional authored placement offsets.
	if rounded := math.Round(y); math.Abs(y-rounded) < 1e-9 {
		y = rounded
	}
	whole := math.Floor(y)
	q.Set("top", strconv.FormatFloat(whole, 'f', 0, 64))
	if fraction := y - whole; fraction != 0 {
		q.Set("object-offset-y", strconv.FormatFloat(fraction+objectPlacementOffset(q, "y"), 'f', -1, 64))
	}
	element.Query = q.Encode()
	return element
}
