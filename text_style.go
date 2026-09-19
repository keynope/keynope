package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
)

// Inspector typography changes never round-trip content through the editor's
// run model. Preflight the complete stable-ID selection, then commit queries
// together. Family/size are Modern-only; emphasis is shared across treatments.
func applyModernTextStyle(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	if action.Kind == "label" {
		return applyLabelTypography(deck, action, editingGroup)
	}
	if action.LabelPaint != nil {
		return false, fmt.Errorf("paint requires a shape-label target")
	}
	if action.Slide < 0 || action.Slide >= len(deck.Slides) || len(action.ObjectIDs) == 0 {
		return false, errInvalidEditorAction
	}
	emphasis := action.TextBold != nil || action.TextItalic != nil || action.TextUnderline != nil
	alignment := action.TextAlign != nil || action.TextVertical != nil
	if value := action.TextAlign; value != nil && *value != "left" && *value != "center" && *value != "right" && *value != "justify" {
		return false, fmt.Errorf("invalid text alignment")
	}
	if value := action.TextVertical; value != nil && *value != "top" && *value != "middle" && *value != "bottom" {
		return false, fmt.Errorf("invalid vertical text alignment")
	}
	spacing := map[string]*float64{"modern-line-height": action.ModernLineHeight, "modern-paragraph-before": action.ModernParagraphBefore, "modern-paragraph-after": action.ModernParagraphAfter}
	hasSpacing := false
	for key, value := range spacing {
		if value == nil {
			continue
		}
		hasSpacing = true
		if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 4 || (key == "modern-line-height" && *value != 0 && *value < .5) {
			return false, fmt.Errorf("invalid paragraph spacing")
		}
	}
	if action.ModernSize == nil && action.ModernFont == nil && !emphasis && !hasSpacing && !alignment {
		return false, errInvalidEditorAction
	}
	if size := action.ModernSize; size != nil && (math.IsNaN(*size) || math.IsInf(*size, 0) || *size < 0 || (*size > 0 && *size < 1) || *size > 1024) {
		return false, fmt.Errorf("Modern size must be 1–1024, or zero to inherit")
	}
	if font := action.ModernFont; font != nil && *font != "" && *font != "sans" && *font != "mono" && *font != "c64" {
		return false, fmt.Errorf("unsupported Modern font")
	}
	slide := &deck.Slides[action.Slide]
	selected, err := typographySelection(*slide, action.ObjectIDs, editingGroup)
	if err != nil {
		return false, err
	}
	updates := map[int]string{}
	boxes := map[string]sceneRect{}
	cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	if alignment {
		scene, err := buildMixedSlideScene(*deck, action.Slide, cols, rows)
		if err != nil {
			return false, err
		}
		for _, object := range scene.Objects {
			boxes[object.ID] = object.Bounds
		}
	}
	for index, element := range slide.Elements {
		style := deck.elementStyle(*slide, element)
		if deck.usesConcreteAppearance() {
			style = "modern"
		}
		modern := supportsTextCapability(element, style, "font")
		if !selected[element.ID] || (!modern && !emphasis && !alignment) {
			continue
		}
		if !supportsTextCapability(element, style, "size") {
			continue
		}
		query, err := url.ParseQuery(element.Query)
		if err != nil {
			return false, err
		}
		old := query.Encode()
		if alignment {
			if box, ok := boxes[element.ID]; ok {
				if !query.Has("width") {
					query.Set("width", strconv.Itoa(max(1, int(math.Round(box.Width*float64(cols)/1920)))))
				}
				if !query.Has("height") {
					query.Set("height", strconv.Itoa(max(1, int(math.Round(box.Height*float64(rows)/1080)))))
				}
				if !query.Has("top") && !query.Has("bottom") && !query.Has("valign") {
					query.Set("top", strconv.Itoa(int(math.Round(box.Y*float64(rows)/1080))))
				}
				anchor := query.Get("align")
				if !query.Has("left") && !query.Has("left_pct") && !query.Has("right") && !query.Has("right_pct") && anchor != "left" && anchor != "center" && anchor != "right" {
					query.Set("left", strconv.Itoa(int(math.Round(box.X*float64(cols)/1920))))
				}
			}
			query.Set("text-box", "1")
			if action.TextAlign != nil {
				query.Set("text-align", *action.TextAlign)
			}
			if action.TextVertical != nil {
				query.Set("text-valign", *action.TextVertical)
			}
		}
		if modern {
			for key, value := range spacing {
				if value == nil {
					continue
				}
				if *value == 0 {
					query.Del(key)
				} else {
					query.Set(key, strconv.FormatFloat(*value, 'f', -1, 64))
				}
			}
		}
		if action.ModernFont != nil && modern {
			if *action.ModernFont == "" {
				query.Del("modern-font")
			} else {
				query.Set("modern-font", *action.ModernFont)
			}
		}
		if action.ModernSize != nil && modern {
			if *action.ModernSize == 0 {
				query.Del("modern-size")
			} else {
				query.Set("modern-size", strconv.FormatFloat(*action.ModernSize, 'f', -1, 64))
			}
		}
		if emphasis {
			// Update valid rich runs before changing the fingerprint's weight.
			// Otherwise toggling bold silently discards italic/underline styles.
			runs := exportedRichRuns(element)
			if runs == nil && (action.TextItalic != nil || action.TextUnderline != nil) {
				plain := element
				if plain.Kind == "bullet" {
					plain.Kind = "text"
				}
				runs = sceneTextFor(plain).Runs
			}
			weight := query.Get("ttf-weight")
			if action.TextBold != nil {
				if *action.TextBold {
					weight = "bold"
					query.Set("ttf-weight", weight)
				} else {
					weight = ""
					query.Del("ttf-weight")
				}
			}
			if runs != nil {
				for i := range runs {
					if action.TextBold != nil {
						runs[i].Bold = *action.TextBold
					}
					if action.TextItalic != nil {
						runs[i].Italic = *action.TextItalic
					}
					if action.TextUnderline != nil {
						runs[i].Underline = *action.TextUnderline
					}
				}
				encoded, err := encodeModernRuns(element.Text, weight, element.Kind, runs)
				if err != nil {
					return false, err
				}
				if encoded == "" {
					query.Del("modern-runs")
				} else {
					query.Set("modern-runs", encoded)
				}
			}
		}
		if next := query.Encode(); next != old {
			updates[index] = next
		}
	}
	for index, query := range updates {
		slide.Elements[index].Query = query
	}
	return len(updates) > 0, nil
}

// Size changes use the active treatment's units. Relative changes start from
// resolved master/theme sizing, not a guessed toolbar value or raw query.
func applyTextSizeCommand(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	return applyTextMetricCommand(deck, action, editingGroup, false)
}

func applyTextWidthCommand(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	return applyTextMetricCommand(deck, action, editingGroup, true)
}

func applyTextMetricCommand(deck *Deck, action nativeEditorAction, editingGroup string, width bool) (bool, error) {
	if action.Kind == "label" {
		return applyLabelTypography(deck, action, editingGroup)
	}
	if width {
		action.TextSize, action.TextSizeDelta = action.TextWidth, action.TextWidthDelta
	}
	if action.Slide < 0 || action.Slide >= len(deck.Slides) || len(action.ObjectIDs) == 0 || (action.TextSize == nil) == (action.TextSizeDelta == nil) {
		return false, errInvalidEditorAction
	}
	value := action.TextSize
	if value == nil {
		value = action.TextSizeDelta
	}
	if math.IsNaN(*value) || math.IsInf(*value, 0) || math.Abs(*value) > 1024 || (action.TextSize != nil && *value < -1) {
		return false, fmt.Errorf("invalid text size")
	}
	if width && (math.Abs(*value) > 200 || (action.TextSize != nil && *value < 1)) {
		return false, fmt.Errorf("font width must be 1–200 percent")
	}
	slide := &deck.Slides[action.Slide]
	selected, err := typographySelection(*slide, action.ObjectIDs, editingGroup)
	if err != nil {
		return false, err
	}
	resolved := map[string]Element{}
	for _, element := range deck.ResolveSlide(action.Slide, true).Elements {
		resolved[element.ID] = element
	}
	updates := map[int]string{}
	for i, element := range slide.Elements {
		if !selected[element.ID] {
			continue
		}
		capability := "size"
		if width {
			capability = "width"
		}
		style := deck.elementStyle(*slide, element)
		if deck.usesConcreteAppearance() {
			style = "modern"
		}
		if !supportsTextCapability(element, style, capability) {
			continue
		}
		modern := style == "modern"
		base := element
		if r, ok := resolved[element.ID]; ok {
			base = r
		}
		if width && !modern && !isTrueType(element) {
			continue
		}
		if !width && !modern && !isTrueType(element) {
			current := textSize(base)
			next := *value
			if action.TextSizeDelta != nil {
				next += float64(current)
			}
			size := max(textSizeMin, min(textSizeMax, int(math.Round(next))))
			if size != current {
				updated := element
				applyTextSize(&updated, size)
				updates[i] = updated.Query
			}
			continue
		}
		key, limit, current := "ttf-size", 512.0, float64(trueTypeSize(base))
		if modern {
			key, limit, current = "modern-size", 1024, deck.modernSceneText(base).Size
		}
		if width {
			key, limit, current = "ttf-width", 200, trueTypeWidthPercent(base)
			if modern {
				key, current = "modern-width", deck.modernSceneText(base).WidthScale*100
				if deck.usesConcreteAppearance() {
					q, _ := url.ParseQuery(base.Query)
					current = 100
					if parsed, err := strconv.ParseFloat(q.Get("modern-width"), 64); err == nil && parsed >= 1 && parsed <= 200 {
						current = parsed
					}
				}
			}
		}
		size := *value
		if action.TextSizeDelta != nil {
			size = current + *value
		}
		size = math.Max(1, math.Min(limit, size))
		if !modern {
			size = math.Round(size)
		}
		if size == current {
			continue
		}
		q, err := url.ParseQuery(element.Query)
		if err != nil {
			return false, err
		}
		q.Set(key, strconv.FormatFloat(size, 'f', -1, 64))
		if width && isTrueType(base) {
			cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
			w, h := trueTypeBounds(base, cols, rows)
			if !q.Has("width") {
				q.Set("width", strconv.Itoa(w))
			}
			if !q.Has("height") {
				q.Set("height", strconv.Itoa(h))
			}
		}
		updates[i] = q.Encode()
	}
	for i, q := range updates {
		slide.Elements[i].Query = q
	}
	return len(updates) > 0, nil
}

// Label typography uses the same commands, but its inheritance starts at the
// shape body. Work on an isolated label until validation and serialization pass.
func applyLabelTypography(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	if action.Slide < 0 || action.Slide >= len(deck.Slides) || len(action.ObjectIDs) != 1 {
		return false, errInvalidEditorAction
	}
	slide := &deck.Slides[action.Slide]
	draft := cloneSlide(*slide)
	if _, err := setElementStyles(&draft, action.ObjectIDs, "inherit"); err != nil {
		return false, err
	}
	for i, owner := range slide.Elements {
		if owner.ID != action.ObjectIDs[0] {
			continue
		}
		if owner.Kind != "shape" {
			return false, fmt.Errorf("select a labelled shape")
		}
		if group := elementGroup(owner); group != "" && group != editingGroup {
			return false, fmt.Errorf("enter the group to edit its shape label")
		}
		if _, ok := shapeLabel(owner); !ok {
			return false, fmt.Errorf("add a shape label first")
		}
		label, data, err := sceneEditableShapeLabel(owner)
		if err != nil {
			return false, err
		}
		isolated := *deck
		isolated.Slides = []Slide{{DefaultStyle: deck.elementStyle(*slide, owner), Elements: []Element{label}}}
		action.Kind, action.Slide = "", 0
		var changed bool
		if action.LabelPaint != nil {
			var next string
			next, err = labelPaintQuery(data.Query, action.LabelPaint)
			changed = err == nil && next != data.Query
			isolated.Slides[0].Elements[0].Query = next
		} else if action.TextWidth != nil || action.TextWidthDelta != nil {
			changed, err = applyTextWidthCommand(&isolated, action, "")
		} else if action.TextSize != nil || action.TextSizeDelta != nil {
			changed, err = applyTextSizeCommand(&isolated, action, "")
		} else {
			changed, err = applyModernTextStyle(&isolated, action, "")
		}
		if err != nil || !changed {
			return changed, err
		}
		updated, _ := url.ParseQuery(isolated.Slides[0].Elements[0].Query)
		original, _ := url.ParseQuery(data.Query)
		// The shape owns a label's box; do not materialise an independent one.
		for _, key := range []string{"width", "height"} {
			if original.Has(key) {
				updated[key] = original[key]
			} else {
				updated.Del(key)
			}
		}
		data.Query = updated.Encode()
		raw, err := json.Marshal(data)
		if err != nil {
			return false, err
		}
		if len(raw) > 65536 {
			return false, fmt.Errorf("shape label exceeds its storage limit")
		}
		slide.Elements[i].Query = setQueryValue(owner.Query, "shape-label", base64.StdEncoding.EncodeToString(raw))
		return true, nil
	}
	return false, fmt.Errorf("shape no longer exists")
}

// A single inspector choice may update several dependent gradient fields.
// Validate the requested property first; retain all unrelated label metadata.
func labelPaintQuery(query string, fields map[string]string) (string, error) {
	if len(fields) != 1 {
		return "", fmt.Errorf("select one label paint property")
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return "", err
	}
	before := q.Encode()
	for key, value := range fields {
		switch key {
		case "modern-opacity":
			if value != "" {
				opacity, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1 {
					return "", fmt.Errorf("label opacity must be between 0 and 1")
				}
				value = strconv.FormatFloat(opacity, 'f', -1, 64)
			}
		case "fg", "gradient-start", "gradient-end", "shadow-color":
			if value != "" {
				var ok bool
				value, ok = normalizeHexColour(value)
				if !ok {
					return "", fmt.Errorf("invalid label colour")
				}
			}
		case "gradient-dir":
			if value != "" && value != "horizontal" && value != "vertical" && value != "diagonal" {
				return "", fmt.Errorf("invalid gradient direction")
			}
		case "shadow":
			if value != "" && value != "soft" && value != "solid" {
				return "", fmt.Errorf("invalid shadow")
			}
		case "outline":
			if value != "" && value != "dark" && value != "light" {
				return "", fmt.Errorf("invalid outline")
			}
		case "transparent":
			if value != "" && value != "1" {
				return "", fmt.Errorf("invalid transparency")
			}
		default:
			return "", fmt.Errorf("unsupported label paint property")
		}
		if value == "" {
			q.Del(key)
		} else {
			q.Set(key, value)
		}
		if key == "gradient-dir" && value == "" {
			q.Del("gradient-start")
			q.Del("gradient-end")
		} else if (key == "gradient-dir" || key == "gradient-start" || key == "gradient-end") && value != "" {
			if !q.Has("gradient-dir") {
				q.Set("gradient-dir", "horizontal")
			}
			if !q.Has("gradient-start") {
				q.Set("gradient-start", "#ffffff")
			}
			if !q.Has("gradient-end") {
				q.Set("gradient-end", "#55aaff")
			}
		}
	}
	if q.Encode() == before {
		return query, nil
	}
	return q.Encode(), nil
}
