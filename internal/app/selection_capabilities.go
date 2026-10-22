package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strconv"
)

// A render-free, selection-scoped contract shared by both editor surfaces.
// Resolved equality and authored provenance are independent: equal values do
// not imply equal inheritance. IDs are authored identities, never DOM indices.
type selectionCapabilityState struct {
	Version    int                               `json:"version"`
	Revision   int64                             `json:"revision"`
	Context    string                            `json:"context"`
	Contexts   map[string][]string               `json:"contexts,omitempty"`
	TargetIDs  []string                          `json:"targetIds"`
	Properties map[string]selectionPropertyState `json:"properties"`
	Commands   map[string]selectionCommandState  `json:"commands"`
	Geometry   *sceneRect                        `json:"geometry,omitempty"`  // Explicit single-object box, in slide cells.
	TextRange  *textStyleRange                   `json:"textRange,omitempty"` // Draft UTF-16 grapheme-aligned word selection.
	TableWord  *tableWordContext                 `json:"tableWord,omitempty"` // Cell anchor, not a fake top-level object.
}

type selectionCommandState struct {
	Enabled     bool     `json:"enabled"`
	EligibleIDs []string `json:"eligibleIds"`
	TotalCount  int      `json:"totalCount"`
	Reason      string   `json:"reason,omitempty"`
}

type selectionPropertyState struct {
	State       string      `json:"state"`
	Value       interface{} `json:"value,omitempty"`
	Provenance  string      `json:"provenance,omitempty"`
	FontBinding string      `json:"fontBinding,omitempty"` // theme/concrete/mixed, independent of authored provenance.
	EligibleIDs []string    `json:"eligibleIds"`
	TotalCount  int         `json:"totalCount"`
	Editable    bool        `json:"editable"`
	Reason      string      `json:"reason,omitempty"`
}

var selectionPropertyIDs = []string{
	"line.width", "line.arrowWidth", "line.arrows", "line.path", "line.routing",
	"paint.fill",
	"paint.opacity", "text.size", "text.width", "text.font",
	"text.bold", "text.italic", "text.underline", "text.align", "text.vertical",
	"text.lineHeight", "text.paragraphBefore", "text.paragraphAfter",
	"geometry.rotation",
	"paint.color", "paint.outline", "paint.gradientStart", "paint.gradientEnd", "paint.gradientDirection",
	"paint.shadow", "paint.shadowColor", "paint.shadowX", "paint.shadowY",
	"paint.strokeColor", "paint.strokeWidth", "paint.strokeToken",
	"paint.strokePosition",
	"paint.colorToken",
	"image.brightness", "image.contrast", "image.saturation", "image.sharpness", "image.alpha", "image.glyph", "image.sampling", "image.tint", "image.mask", "image.fit", "image.style",
	"label.size", "label.width", "label.font", "label.bold", "label.italic", "label.underline",
	"label.align", "label.vertical", "label.lineHeight", "label.paragraphBefore", "label.paragraphAfter",
	"label.color", "label.colorToken", "label.outline", "label.gradientStart", "label.gradientEnd", "label.gradientDirection",
	"label.shadow", "label.shadowColor", "label.shadowX", "label.shadowY",
}

type mixedSelectionValue struct{}

func selectionTargetIDs(slide Slide, selection map[int]bool, primary int, editingGroup string) []string {
	groups := map[string]bool{}
	for i, element := range slide.Elements {
		if i == primary || selection[i] {
			if group := elementGroup(element); group != "" && group != editingGroup {
				groups[group] = true
			}
		}
	}
	ids := []string{}
	for i, element := range slide.Elements {
		if (i == primary || selection[i] || groups[elementGroup(element)]) && element.ID != "" {
			ids = append(ids, element.ID)
		}
	}
	return ids
}

func selectionPropertyValue(deck Deck, slide, resolvedSlide Slide, authored, resolved Element, text *sceneText, property string) (interface{}, bool, bool) {
	if labelCommonProperty(property) != "" {
		return selectionLabelValue(deck, resolvedSlide, authored, resolved, property)
	}
	q, err := url.ParseQuery(authored.Query)
	if err != nil {
		return nil, false, false
	}
	rq, _ := url.ParseQuery(resolved.Query)
	return selectionPropertyValueFromQueries(deck, slide, resolvedSlide, authored, resolved, text, q, rq, property)
}

func selectionPropertyValueFromQueries(deck Deck, slide, resolvedSlide Slide, authored, resolved Element, text *sceneText, q, rq url.Values, property string) (interface{}, bool, bool) {
	if control, ok := connectorSelectionControls[property]; ok {
		return connectorSelectionValue(authored, q, rq, control)
	}
	if control, ok := imageSelectionControls[property]; ok {
		return imageSelectionValue(authored, q, rq, control)
	}
	if value, explicit, eligible, handled := selectionPaintValue(deck, resolvedSlide, authored, resolved, q, rq, property); handled {
		return value, explicit, eligible
	}
	if property == "geometry.rotation" {
		if !selectionObjectCanRotate(deck, slide, authored) {
			return nil, false, false
		}
		angle := 0.0
		switch rq.Get("orientation") {
		case "cw":
			angle = 90
		case "ccw":
			angle = -90
		case "down", "flip":
			angle = 180
		}
		if value, err := strconv.ParseFloat(rq.Get("object-rotation"), 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
			angle = math.Mod(value, 360)
		}
		return angle, q.Has("object-rotation") || q.Has("orientation"), true
	}
	if property == "paint.opacity" {
		switch authored.Kind {
		case "shape", "image", "video", "table", "chart", "connector", "text", "heading", "bullet", "code", "text-image", "page-number":
		default:
			return nil, false, false
		}
		value := 1.0
		if parsed, err := strconv.ParseFloat(rq.Get("modern-opacity"), 64); err == nil && parsed >= 0 && parsed <= 1 {
			value = parsed
		}
		return value, q.Has("modern-opacity"), true
	}
	style := deck.elementStyle(slide, authored)
	if deck.usesConcreteAppearance() {
		style = "modern"
	}
	capability := map[string]string{
		"text.size": "size", "text.width": "width", "text.font": "font",
		"text.bold": "emphasis", "text.italic": "emphasis", "text.underline": "emphasis",
		"text.align": "alignment", "text.vertical": "alignment",
		"text.lineHeight": "paragraph", "text.paragraphBefore": "paragraph", "text.paragraphAfter": "paragraph",
	}[property]
	if capability == "" || !supportsTextCapability(authored, style, capability) {
		return nil, false, false
	}
	if isTrueType(resolved) {
		rq = trueTypeValuesWithDefaults(resolved, resolvedSlide, rq)
	}
	if text == nil {
		text = deck.modernSceneTextFromValues(resolved, rq)
	}
	switch property {
	case "text.size":
		if style != "modern" && !isTrueType(resolved) {
			return float64(textSize(resolved)), q.Has("text-size") || q.Has("scale"), true
		}
		if style != "modern" {
			return float64(trueTypeSize(resolved)), q.Has("ttf-size"), true
		}
		return text.Size, q.Has("modern-size") || q.Has("ttf-size") || q.Has("text-size"), true
	case "text.width":
		// Resolve weak fixed/theme defaults just as the renderer does. Authored
		// provenance still comes from q, not these projected values.
		rq = deck.resolveThemeTextValues(resolved, rq)
		value := trueTypeWidthWithValues(rq)
		if style == "modern" {
			value = text.WidthScale * 100
			if deck.usesConcreteAppearance() {
				value = trueTypeWidthWithValues(rq)
				if parsed, err := strconv.ParseFloat(rq.Get("modern-width"), 64); err == nil {
					value = parsed
				}
			}
		}
		return value, q.Has("modern-width") || q.Has("ttf-width"), true
	case "text.font":
		return resolvedModernFont(deck, resolved, rq), q.Has("modern-font") && q.Get("modern-font") != "theme", true
	case "text.align":
		value := text.Align
		if value == "start" {
			value = "left"
		}
		if value == "end" {
			value = "right"
		}
		return value, q.Has("text-align") || q.Get("align") == "justify", true
	case "text.vertical":
		return text.Vertical, q.Has("text-valign"), true
	case "text.lineHeight":
		return text.LineHeight, q.Has("modern-line-height"), true
	case "text.paragraphBefore":
		return text.ParagraphBefore, q.Has("modern-paragraph-before"), true
	case "text.paragraphAfter":
		return text.ParagraphAfter, q.Has("modern-paragraph-after"), true
	default:
		runs := text.Runs
		for _, paragraph := range text.Paragraphs {
			runs = append(runs, paragraph.Runs...)
		}
		value, visible, any := true, false, false
		for _, run := range runs {
			if run.Text == "" {
				continue
			}
			visible = true
			var emphasized bool
			switch property {
			case "text.bold":
				emphasized = run.Bold
			case "text.italic":
				emphasized = run.Italic
			case "text.underline":
				emphasized = run.Underline
			}
			value = value && emphasized
			any = any || emphasized
		}
		if !visible {
			value = false
		}
		key := map[string]string{"text.bold": "ttf-weight", "text.italic": "text-italic", "text.underline": "text-underline"}[property]
		if any && !value {
			return mixedSelectionValue{}, q.Has(key) || q.Has("modern-runs"), true
		}
		return value, q.Has(key) || property == "text.bold" && q.Has("text-bold") || q.Has("modern-runs"), true
	}
}

func buildSelectionCapabilities(deck Deck, slide, resolved Slide, ids []string, editingGroup string, revision int64) selectionCapabilityState {
	result := selectionCapabilityState{Version: 1, Revision: revision, Context: "objects", TargetIDs: append([]string{}, ids...), Properties: map[string]selectionPropertyState{}}
	if editingGroup != "" {
		result.Context = "group-child"
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	resolvedByID := map[string]Element{}
	for _, element := range resolved.Elements {
		resolvedByID[element.ID] = element
	}
	texts := map[string]*sceneText{}
	labels := map[string]selectionLabelProjection{}
	type queryProjection struct {
		authored, resolved url.Values
		valid              bool
	}
	queries := map[string]queryProjection{}
	for _, element := range slide.Elements {
		if !wanted[element.ID] {
			continue
		}
		projected, ok := resolvedByID[element.ID]
		if !ok {
			projected = element
		}
		q, err := url.ParseQuery(element.Query)
		rq, _ := url.ParseQuery(projected.Query)
		if isTrueType(projected) {
			rq = trueTypeValuesWithDefaults(projected, resolved, rq)
		}
		queries[element.ID] = queryProjection{authored: q, resolved: rq, valid: err == nil}
		if element.Kind == "shape" {
			if projection, ok := prepareSelectionLabel(deck, element, projected); ok {
				labels[element.ID] = projection
			}
		}
		style := deck.elementStyle(slide, element)
		if deck.usesConcreteAppearance() {
			style = "modern"
		}
		if !supportsTextCapability(element, style, "size") {
			continue
		}
		texts[element.ID] = deck.modernSceneTextFromValues(projected, rq)
	}
	blocked := ""
	if len(ids) > 0 {
		if _, err := editableObjectIndices(slide, ids); err != nil {
			blocked = err.Error()
		}
	}
	for _, property := range selectionPropertyIDs {
		state := selectionPropertyState{State: "unavailable", EligibleIDs: []string{}, TotalCount: len(ids)}
		for _, element := range slide.Elements {
			if !wanted[element.ID] {
				continue
			}
			projected, ok := resolvedByID[element.ID]
			if !ok {
				projected = element
			}
			var value interface{}
			var explicit, supported bool
			if common := labelCommonProperty(property); common != "" {
				if projection, ok := labels[element.ID]; ok {
					value, explicit, supported = projection.value(deck, resolved, common)
				}
			} else {
				if input := queries[element.ID]; input.valid {
					value, explicit, supported = selectionPropertyValueFromQueries(deck, slide, resolved, element, projected, texts[element.ID], input.authored, input.resolved, property)
				}
			}
			if !supported {
				continue
			}
			binding := ""
			if property == "text.font" {
				binding = fontBindingFromValues(queries[element.ID].resolved)
			} else if property == "label.font" {
				binding = labels[element.ID].fontBinding
			}
			if state.FontBinding == "" {
				state.FontBinding = binding
			} else if binding != state.FontBinding {
				state.FontBinding = "mixed"
			}
			state.EligibleIDs = append(state.EligibleIDs, element.ID)
			if labelCommonProperty(property) != "" && elementGroup(element) != "" && elementGroup(element) != editingGroup {
				state.Reason = "Enter the group to edit its shape labels."
			}
			provenance := "inherited"
			if explicit {
				provenance = "explicit"
			}
			if state.State == "unavailable" {
				state.State, state.Value, state.Provenance = "value", value, provenance
			} else {
				if !reflect.DeepEqual(state.Value, value) {
					state.State = "mixed"
				}
				if state.Provenance != provenance {
					state.Provenance = "mixed"
				}
			}
			if _, mixed := value.(mixedSelectionValue); mixed {
				state.State = "mixed"
			}
		}
		if state.State == "mixed" {
			state.Value = nil
		}
		state.Editable = len(state.EligibleIDs) > 0 && blocked == "" && state.Reason == ""
		if property == "geometry.rotation" && len(ids) > 1 {
			state.Editable = false
			state.Reason = "Use Rotate together to rotate the selection about its shared centre."
		}
		if blocked != "" {
			state.Reason = blocked
		}
		result.Properties[property] = state
	}
	if labels := result.Properties["label.size"].EligibleIDs; len(labels) > 0 {
		result.Contexts = map[string][]string{"shape-label": append([]string(nil), labels...)}
	}
	result.Commands = buildSelectionCommands(deck, slide, ids, editingGroup, blocked)
	for key, command := range buildSelectionObjectCommands(deck, slide, resolvedByID, ids, editingGroup, blocked) {
		result.Commands[key] = command
	}
	for key, command := range buildLabelStyleCommands(slide, ids, result.Properties["label.size"].EligibleIDs, editingGroup, blocked) {
		result.Commands[key] = command
	}
	if len(ids) == 1 {
		if element, ok := resolvedByID[ids[0]]; ok {
			result.Geometry = explicitSelectionGeometry(element)
		}
	}
	return result
}

// Retained-art text has no arbitrary-angle semantic surface. Do not promise a
// control that its renderer cannot honor. C64 TTF is semantic text and qualifies.
func selectionObjectCanRotate(deck Deck, slide Slide, element Element) bool {
	q, err := url.ParseQuery(element.Query)
	if err != nil {
		return false
	}
	style := deck.elementStyle(slide, element)
	if deck.usesConcreteAppearance() {
		style = "modern"
	}
	switch element.Kind {
	case "image", "video", "table", "chart":
		return true
	case "shape":
		return style == "modern"
	case "text", "heading", "bullet", "code", "text-image", "page-number":
		return q.Get("font") == "" && q.Get("glyph") == "" && (style == "modern" || sharedRetroText(element))
	}
	return false
}

func selectionObjectCanPosition(element Element) bool {
	switch element.Kind {
	case "text", "heading", "bullet", "code", "text-image", "page-number", "shape", "image", "video", "table", "chart":
		return true
	}
	return false
}

// This describes whole-selection commands, not a permissive subset. A command
// either targets every declared member or is disabled with an explicit reason.
func buildSelectionCommands(deck Deck, slide Slide, ids []string, editingGroup, blocked string) map[string]selectionCommandState {
	selected := map[string]Element{}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	for _, element := range slide.Elements {
		if wanted[element.ID] {
			selected[element.ID] = element
		}
	}
	units, individual := map[string]bool{}, true
	for _, id := range ids {
		element := selected[id]
		unit := "object:" + id
		if group := elementGroup(element); group != "" && group != editingGroup {
			unit, individual = "group:"+group, false
		}
		units[unit] = true
	}
	result := map[string]selectionCommandState{}
	for _, name := range []string{"arrange.geometry", "arrange.move", "arrange.resize", "arrange.rotate", "arrange.align", "arrange.match", "arrange.distribute"} {
		state := selectionCommandState{EligibleIDs: []string{}, TotalCount: len(ids), Reason: blocked}
		for _, id := range ids {
			element, exists := selected[id]
			eligible := exists && selectionObjectCanPosition(element)
			if name == "arrange.rotate" {
				eligible = exists && selectionObjectCanRotate(deck, slide, element)
			}
			if name == "arrange.resize" && len(ids) > 1 {
				eligible = exists && selectionObjectCanRotate(deck, slide, element)
				if exists && element.Kind == "connector" {
					q, _ := url.ParseQuery(element.Query)
					from, fromOK := selected[q.Get("connector-from")]
					to, toOK := selected[q.Get("connector-to")]
					eligible = fromOK && toOK && from.Kind == "shape" && to.Kind == "shape"
				}
			}
			if eligible {
				state.EligibleIDs = append(state.EligibleIDs, id)
			}
		}
		state.Enabled = len(ids) > 0 && len(state.EligibleIDs) == len(ids) && blocked == ""
		if name == "arrange.geometry" && len(ids) != 1 {
			state.Enabled = false
			if state.Reason == "" {
				state.Reason = "Use selection movement or Resize together for multiple objects."
			}
		}
		if len(state.EligibleIDs) != len(ids) && state.Reason == "" {
			state.Reason = "This selection contains an object that does not support this operation."
		}
		if name == "arrange.match" && (len(units) < 2 || !individual) {
			state.Enabled = false
			if state.Reason == "" {
				state.Reason = "Select at least two individual objects; enter groups to edit their members."
			}
		}
		if name == "arrange.distribute" && len(units) < 3 {
			state.Enabled = false
			if state.Reason == "" {
				state.Reason = "Select at least three objects or groups."
			}
		}
		result[name] = state
	}
	for _, name := range []string{"style.copy", "style.paste", "style.reset"} {
		state := selectionCommandState{EligibleIDs: []string{}, TotalCount: len(ids), Reason: blocked}
		for _, id := range ids {
			if styleObjectSupported(selected[id]) {
				state.EligibleIDs = append(state.EligibleIDs, id)
			}
		}
		state.Enabled = len(ids) > 0 && len(state.EligibleIDs) == len(ids) && blocked == ""
		if name == "style.copy" {
			// Reading the style of a locked source is harmless; writing is not.
			state.Enabled = len(ids) == 1 && len(state.EligibleIDs) == 1
			state.Reason = ""
			if !state.Enabled {
				state.Reason = "Select one object to copy its style."
			}
		} else if len(state.EligibleIDs) != len(ids) && state.Reason == "" {
			state.Reason = "This selection contains an unsupported style target."
		}
		result[name] = state
	}
	return result
}

// Recompute eligibility under the mutation lock, not from the client-supplied
// capability snapshot. Validate all declared members before filtering by type.
func applySelectionProperty(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	if action.StyleContext == "table-cells" {
		return applyTableCellPropertyCommand(deck, action, editingGroup)
	}
	if action.Slide < 0 || action.Slide >= len(deck.Slides) || len(action.PropertyValue) == 0 || bytes.Equal(bytes.TrimSpace(action.PropertyValue), []byte("null")) {
		return false, errInvalidEditorAction
	}
	if labelCommonProperty(action.Property) != "" {
		return applySelectionLabelProperty(deck, action, editingGroup)
	}
	slide := &deck.Slides[action.Slide]
	selected, err := typographySelection(*slide, action.ObjectIDs, editingGroup)
	if err != nil {
		return false, err
	}
	ids := []string{}
	for _, element := range slide.Elements {
		if selected[element.ID] {
			ids = append(ids, element.ID)
		}
	}
	resolved := deck.ResolveSlide(action.Slide, false)
	caps := buildSelectionCapabilities(*deck, *slide, resolved, ids, editingGroup, 0)
	property, ok := caps.Properties[action.Property]
	if !ok || !property.Editable {
		return false, editorFailure("unsupported-property", action.Property, "", fmt.Errorf("property %q does not apply to this selection", action.Property))
	}
	if textPropertyCanInherit(action.Property) {
		var inherit string
		if json.Unmarshal(action.PropertyValue, &inherit) == nil && inherit == "" {
			// Scope the reset to capability-eligible text, not a second group
			// expansion through generic style transfer (tables are not text).
			eligible := map[string]bool{}
			for _, id := range property.EligibleIDs {
				eligible[id] = true
			}
			changed := false
			for i, element := range slide.Elements {
				if !eligible[element.ID] {
					continue
				}
				q := mustQuery(element.Query)
				q.Del(objectStyleKeys[action.Property])
				if action.Property == "text.size" {
					q.Del("ttf-size")
				} else if action.Property == "text.width" {
					q.Del("ttf-width")
				} else if action.Property == "text.align" && q.Get("align") == "justify" {
					q.Del("align") // Legacy paragraph marker, never a position anchor.
				}
				removeMasterStyleClears(q, action.Property)
				if next := q.Encode(); next != element.Query {
					slide.Elements[i].Query, changed = next, true
				}
			}
			return changed, nil
		}
	}
	if control, ok := connectorSelectionControls[action.Property]; ok {
		return applyConnectorSelection(slide, property.EligibleIDs, control, action.PropertyValue)
	}
	if control, ok := imageSelectionControls[action.Property]; ok {
		if action.Property == "image.fit" || action.Property == "image.style" {
			return applyImageFitSelection(deck, slide, resolved, action.Slide, property.EligibleIDs, control, action.PropertyValue)
		}
		return applyImageSelection(slide, property.EligibleIDs, control, action.PropertyValue)
	}
	var number float64
	var flag bool
	var text string
	command := action
	command.ObjectIDs = ids
	if control, ok := effectControls[action.Property]; ok && control.choices == "" {
		if err := json.Unmarshal(action.PropertyValue, &number); err != nil {
			var reset string
			if json.Unmarshal(action.PropertyValue, &reset) != nil || reset != "" {
				return false, fmt.Errorf("effect setting requires a number or empty text to inherit")
			}
			text = "inherit"
		}
		return applySelectionPaint(slide, resolved, property.EligibleIDs, action.Property, text, number)
	}
	switch action.Property {
	case "paint.strokeWidth":
		if err := json.Unmarshal(action.PropertyValue, &number); err != nil {
			var reset string
			if json.Unmarshal(action.PropertyValue, &reset) != nil || reset != "" {
				return false, fmt.Errorf("stroke width requires a number or empty text to inherit")
			}
			text = "inherit"
		}
	case "paint.opacity", "paint.shadowX", "paint.shadowY", "geometry.rotation", "text.size", "text.width", "text.lineHeight", "text.paragraphBefore", "text.paragraphAfter":
		if err := json.Unmarshal(action.PropertyValue, &number); err != nil {
			return false, fmt.Errorf("property requires a number")
		}
	case "text.bold", "text.italic", "text.underline":
		if err := json.Unmarshal(action.PropertyValue, &flag); err != nil {
			return false, fmt.Errorf("property requires a boolean")
		}
	default:
		if err := json.Unmarshal(action.PropertyValue, &text); err != nil {
			return false, fmt.Errorf("property requires text")
		}
	}
	if selectionPaintProperty(action.Property) {
		return applySelectionPaint(slide, resolved, property.EligibleIDs, action.Property, text, number)
	}
	switch action.Property {
	case "geometry.rotation":
		if number < -360 || number > 360 || math.IsNaN(number) || math.IsInf(number, 0) {
			return false, fmt.Errorf("rotation must be between -360 and 360 degrees")
		}
		changed := false
		for i, element := range slide.Elements {
			if !selected[element.ID] {
				continue
			}
			q, _ := url.ParseQuery(element.Query)
			previous := q.Encode()
			q.Del("orientation")
			q.Set("object-rotation", strconv.FormatFloat(math.Mod(number, 360), 'f', -1, 64))
			if next := q.Encode(); next != previous {
				slide.Elements[i].Query, changed = next, true
			}
		}
		return changed, nil
	case "paint.opacity":
		if number < 0 || number > 1 {
			return false, fmt.Errorf("opacity must be between zero and one")
		}
		changed := false
		eligible := map[string]bool{}
		for _, id := range property.EligibleIDs {
			eligible[id] = true
		}
		for i, element := range slide.Elements {
			if !eligible[element.ID] {
				continue
			}
			q, _ := url.ParseQuery(element.Query)
			old := q.Encode()
			q.Set("modern-opacity", strconv.FormatFloat(number, 'f', -1, 64))
			if q.Encode() != old {
				slide.Elements[i].Query = q.Encode()
				changed = true
			}
		}
		return changed, nil
	case "text.size":
		command.TextSize = &number
		return applyTextSizeCommand(deck, command, editingGroup)
	case "text.width":
		command.TextWidth = &number
		return applyTextWidthCommand(deck, command, editingGroup)
	case "text.font":
		command.ModernFont = &text
	case "text.bold":
		command.TextBold = &flag
	case "text.italic":
		command.TextItalic = &flag
	case "text.underline":
		command.TextUnderline = &flag
	case "text.align":
		command.TextAlign = &text
	case "text.vertical":
		command.TextVertical = &text
	case "text.lineHeight":
		command.ModernLineHeight = &number
	case "text.paragraphBefore":
		command.ModernParagraphBefore = &number
	case "text.paragraphAfter":
		command.ModernParagraphAfter = &number
	}
	return applyModernTextStyle(deck, command, editingGroup)
}
