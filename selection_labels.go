package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

var labelStyleKeys = []string{"default-font", "default-size", "default-width", "default-line-height", "default-color", "modern-font", "modern-size", "modern-width", "ttf-size", "ttf-width", "ttf-weight", "text-italic", "text-underline", "modern-runs", "text-align", "text-valign", "modern-line-height", "modern-paragraph-before", "modern-paragraph-after", "fg", "theme-fg", "outline", "gradient-start", "gradient-end", "gradient-dir", "shadow", "shadow-color", "shadow-x", "shadow-y"}

func shapeLabelStyleKeysJSON() string {
	data, _ := json.Marshal(labelStyleKeys)
	return string(data)
}

func labelStyleKey(key string) bool {
	for _, k := range labelStyleKeys {
		if k == key {
			return true
		}
	}
	return false
}

func labelCommonProperty(property string) string {
	if !strings.HasPrefix(property, "label.") {
		return ""
	}
	name := strings.TrimPrefix(property, "label.")
	switch name {
	case "color", "colorToken", "outline", "gradientStart", "gradientEnd", "gradientDirection", "shadow", "shadowColor", "shadowX", "shadowY":
		return "paint." + name
	}
	for _, property := range selectionPropertyIDs {
		if property == "text."+name {
			return property
		}
	}
	return ""
}

// Sparse owner-query overrides preserve inherited label content and unknown
// nested metadata. Defaults are applied only for rendering, not authored state.
func shapeLabelStyleValues(owner Element, values url.Values) url.Values {
	q, _ := url.ParseQuery(owner.Query)
	base := cloneQueryValues(values)
	values = cloneQueryValues(values)
	for _, key := range labelStyleKeys {
		if q.Has("label-" + key) {
			if (key == "ttf-weight" || key == "modern-runs") && q.Get("label-"+key) == "none" {
				values.Del(key) // Explicit clear, not removal of the owner override.
			} else {
				values[key] = append([]string(nil), q["label-"+key]...)
			}
		}
	}
	if q.Has("label-fg") {
		values.Del("theme-fg")
	} else if q.Has("label-theme-fg") {
		values.Del("fg")
		values.Del("header")
	}
	if q.Has("label-ttf-weight") || q.Has("label-text-italic") || q.Has("label-text-underline") {
		// Whole-label emphasis is independent per property. Recompose rich
		// runs with the matching fingerprint so switching bold doesn't drop
		// inherited italic/underline, colours or font overrides.
		if data, err := decodeShapeLabelData(q.Get("shape-label")); err == nil {
			label := Element{Kind: "text", Text: data.Text, Query: values.Encode()}
			runs := exportedRichRuns(label)
			if runs == nil {
				label.Query = base.Encode()
				runs = sceneTextFor(label).Runs
			}
			for i := range runs {
				if q.Has("label-ttf-weight") {
					runs[i].Bold = q.Get("label-ttf-weight") == "bold"
				}
				if q.Has("label-text-italic") {
					runs[i].Italic = q.Get("label-text-italic") == "1"
				}
				if q.Has("label-text-underline") {
					runs[i].Underline = q.Get("label-text-underline") == "1"
				}
			}
			if encoded, err := encodeModernRuns(data.Text, values.Get("ttf-weight"), "text", runs); err == nil {
				values.Del("modern-runs")
				if encoded != "" {
					values.Set("modern-runs", encoded)
				}
			}
		}
	}
	return values
}

func rawShapeLabel(owner Element) (Element, bool) {
	if owner.Kind != "shape" {
		return Element{}, false
	}
	q, _ := url.ParseQuery(owner.Query)
	raw, err := base64.StdEncoding.DecodeString(q.Get("shape-label"))
	var data shapeLabelData
	if err != nil || len(raw) > 65536 || json.Unmarshal(raw, &data) != nil || data.Text == "" {
		return Element{}, false
	}
	values, err := url.ParseQuery(data.Query)
	if err != nil {
		return Element{}, false
	}
	return Element{Kind: "text", ID: owner.ID, Text: data.Text, Query: shapeLabelStyleValues(owner, values).Encode()}, true
}

type selectionLabelProjection struct {
	fontBinding        string
	authored, resolved Element
	owner              url.Values
	authoredValues     url.Values
	resolvedValues     url.Values
	text               *sceneText
}

// Request-owned preparation: no document/selection identity or mutable query
// maps are retained beyond the capability response that consumes this value.
func prepareSelectionLabel(deck Deck, authored, resolved Element) (selectionLabelProjection, bool) {
	label, ok := shapeLabel(resolved)
	if !ok {
		return selectionLabelProjection{}, false
	}
	raw, _ := rawShapeLabel(authored)
	if raw.ID == "" {
		// A master-bound instance can own style without owning label content.
		// Keep those sparse authored fields in the provenance input too.
		raw = Element{Kind: "text", ID: authored.ID, Text: label.Text, Query: shapeLabelStyleValues(authored, url.Values{}).Encode()}
	}
	query := mustQuery(label.Query)
	binding := fontBindingFromValues(query)
	label.Query = deck.resolveThemeTextValues(label, query).Encode()
	return selectionLabelProjection{fontBinding: binding, authored: raw, resolved: label, owner: mustQuery(authored.Query), authoredValues: mustQuery(raw.Query), resolvedValues: mustQuery(label.Query), text: deck.modernSceneText(label)}, true
}

func (p selectionLabelProjection) value(deck Deck, slide Slide, common string) (interface{}, bool, bool) {
	// Shape labels have independent typography. The scene does not apply the
	// containing slide's legacy TrueType size/width defaults to nested labels.
	slide.TTFSize, slide.TTFWidth = 0, 0
	value, explicit, supported := selectionPropertyValueFromQueries(deck, slide, slide, p.authored, p.resolved, p.text, p.authoredValues, p.resolvedValues, common)
	q := p.owner
	if common == "text.bold" && q.Has("label-ttf-weight") || (common == "text.bold" || common == "text.italic" || common == "text.underline") && q.Has("label-modern-runs") {
		explicit = true
	}
	return value, explicit, supported
}

func selectionLabelValue(deck Deck, slide Slide, authored, resolved Element, property string) (interface{}, bool, bool) {
	common := labelCommonProperty(property)
	if common == "" {
		return nil, false, false
	}
	projection, ok := prepareSelectionLabel(deck, authored, resolved)
	if !ok {
		return nil, false, false
	}
	return projection.value(deck, slide, common)
}

func mustQuery(query string) url.Values { q, _ := url.ParseQuery(query); return q }

func setLabelColorClears(q url.Values, additions []string) {
	keys := map[string]bool{}
	for _, key := range strings.Split(q.Get("master-clear"), ",") {
		key = strings.TrimSpace(key)
		if key != "" && key != "label-fg" && key != "label-theme-fg" {
			keys[key] = true
		}
	}
	for _, key := range additions {
		keys[key] = true
	}
	values := []string{}
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

func syncExistingLabelOverrides(ownerQuery, labelQuery string) string {
	q, style := mustQuery(ownerQuery), mustQuery(labelQuery)
	for _, key := range labelStyleKeys {
		outer := "label-" + key
		if q.Has(outer) {
			if style.Has(key) {
				q[outer] = style[key]
			} else {
				q.Del(outer)
			}
		}
	}
	return q.Encode()
}

func applySelectionLabelProperty(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	common := labelCommonProperty(action.Property)
	if common == "" || action.Slide < 0 || action.Slide >= len(deck.Slides) {
		return false, errInvalidEditorAction
	}
	slide := &deck.Slides[action.Slide]
	if _, err := typographySelection(*slide, action.ObjectIDs, editingGroup); err != nil {
		return false, err
	}
	resolved := deck.ResolveSlide(action.Slide, false)
	caps := buildSelectionCapabilities(*deck, *slide, resolved, action.ObjectIDs, editingGroup, 0)
	state := caps.Properties[action.Property]
	if !state.Editable {
		return false, fmt.Errorf("label property does not apply to this selection")
	}
	if textPropertyCanInherit(common) {
		var inherit string
		if json.Unmarshal(action.PropertyValue, &inherit) == nil && inherit == "" {
			command := action
			command.Action, command.StyleContext = "paste-object-style", "shape-label"
			command.StyleData = &ObjectStyleSnapshot{Version: 1, Context: "shape-label", Fields: map[string]ObjectStyleField{common: {}}}
			return applyObjectStyle(deck, command, editingGroup)
		}
	}
	eligible := map[string]bool{}
	for _, id := range state.EligibleIDs {
		eligible[id] = true
	}
	labels := []Element{}
	for _, owner := range resolved.Elements {
		if eligible[owner.ID] {
			label, _ := shapeLabel(owner)
			labels = append(labels, label)
		}
	}
	isolated := *deck
	isolated.Masters = MasterDeck{}
	isolated.Slides = []Slide{{ID: "label-draft", Elements: append([]Element(nil), labels...)}}
	if common == "text.size" || common == "text.width" {
		var value float64
		if json.Unmarshal(action.PropertyValue, &value) != nil || value < 1 || common == "text.size" && value > 512 || common == "text.width" && value > 200 {
			return false, fmt.Errorf("label metric is outside its supported range")
		}
	}
	command := action
	command.Slide = 0
	command.Property = common
	command.Kind = ""
	command.ObjectIDs = state.EligibleIDs
	if _, err := applySelectionProperty(&isolated, command, ""); err != nil {
		return false, err
	}
	updated := map[string]Element{}
	for _, e := range isolated.Slides[0].Elements {
		updated[e.ID] = e
	}
	draft := cloneSlide(*slide)
	changed := false
	for i, owner := range draft.Elements {
		next, ok := updated[owner.ID]
		if !ok {
			continue
		}
		var previous Element
		for _, label := range labels {
			if label.ID == owner.ID {
				previous = label
				break
			}
		}
		if previous.Text != next.Text {
			return false, fmt.Errorf("label style must not change its content")
		}
		oldQ, nextQ := mustQuery(previous.Query), mustQuery(next.Query)
		q := mustQuery(owner.Query)
		for _, key := range labelStyleKeys {
			if (common == "text.bold" || common == "text.italic" || common == "text.underline") && (key == "modern-runs" || key == "ttf-weight") {
				continue // Independent flags are projected into rich runs at render time.
			}
			if reflect.DeepEqual(oldQ[key], nextQ[key]) {
				continue
			}
			outer := "label-" + key
			if nextQ.Has(key) {
				q[outer] = append([]string(nil), nextQ[key]...)
			} else if key == "ttf-weight" || key == "modern-runs" {
				q.Set(outer, "none")
			} else {
				q.Del(outer)
			}
		}
		if common == "text.bold" || common == "text.italic" || common == "text.underline" {
			var on bool
			_ = json.Unmarshal(action.PropertyValue, &on)
			key, value := "ttf-weight", "none"
			if common != "text.bold" {
				key = "text-" + strings.TrimPrefix(common, "text.")
				value = "0"
			}
			if on {
				value = "1"
				if key == "ttf-weight" {
					value = "bold"
				}
			}
			q.Set("label-"+key, value)
		}
		if common == "text.lineHeight" || common == "text.paragraphBefore" || common == "text.paragraphAfter" || common == "text.align" || common == "text.vertical" {
			key := objectStyleKeys[common]
			pin := true
			if common == "text.align" || common == "text.vertical" {
				if authored, ok := rawShapeLabel(owner); ok && mustQuery(authored.Query).Has(key) && oldQ.Get(key) == nextQ.Get(key) {
					pin = false // Already explicit; do not duplicate a nested value.
				}
			}
			if pin {
				removeLabelStyleClears(q, labelPropertyKeys(common))
				if nextQ.Has(key) {
					q.Set("label-"+key, nextQ.Get(key))
				} else {
					q.Del("label-" + key)
				}
			}
		}
		// Colour role/RGB aliases must not resurrect an inherited competing
		// owner override. Inherit removes only this label's colour overrides.
		if common == "paint.colorToken" || common == "paint.color" {
			var value string
			_ = json.Unmarshal(action.PropertyValue, &value)
			if value == "" {
				q.Del("label-fg")
				q.Del("label-theme-fg")
				setLabelColorClears(q, nil)
			} else if common == "paint.colorToken" {
				q.Del("label-fg")
				setLabelColorClears(q, []string{"label-fg"})
			} else {
				q.Del("label-theme-fg")
				setLabelColorClears(q, []string{"label-theme-fg"})
			}
		}
		if query := q.Encode(); query != owner.Query {
			draft.Elements[i].Query = query
			changed = true
		}
	}
	if changed {
		*slide = draft
	}
	return changed, nil
}
