package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

func labelStyleProperty(common string) bool {
	for _, property := range selectionPropertyIDs {
		if labelCommonProperty(property) == common && common != "paint.colorToken" {
			return true
		}
	}
	return false
}

func buildLabelStyleCommands(slide Slide, ids, labels []string, editingGroup, blocked string) map[string]selectionCommandState {
	groupsBlocked, inherited := false, false
	wanted := map[string]bool{}
	for _, id := range labels {
		wanted[id] = true
	}
	for _, element := range slide.Elements {
		if wanted[element.ID] {
			groupsBlocked = groupsBlocked || elementGroup(element) != "" && elementGroup(element) != editingGroup
			inherited = inherited || element.Inherited
		}
	}
	result := map[string]selectionCommandState{}
	for _, name := range []string{"style.label.copy", "style.label.paste", "style.label.reset"} {
		state := selectionCommandState{EligibleIDs: append([]string{}, labels...), TotalCount: len(ids), Enabled: len(labels) > 0 && blocked == "" && !groupsBlocked && !inherited, Reason: blocked}
		if name == "style.label.copy" {
			state.Enabled = len(ids) == 1 && len(labels) == 1 && !groupsBlocked && !inherited
			state.Reason = ""
		}
		if !state.Enabled && state.Reason == "" {
			state.Reason = "Select labelled shapes; enter their group to format labels."
			if name == "style.label.copy" {
				state.Reason = "Select one labelled shape; enter its group to copy label formatting."
			}
		}
		result[name] = state
	}
	return result
}

func labelPropertyKeys(property string) []string {
	switch property {
	case "paint.color":
		return []string{"fg", "theme-fg"}
	case "text.size":
		return []string{"modern-size", "ttf-size"}
	case "text.width":
		return []string{"modern-width", "ttf-width"}
	case "text.bold":
		return []string{"ttf-weight"}
	case "text.italic":
		return []string{"text-italic"}
	case "text.underline":
		return []string{"text-underline"}
	}
	return []string{objectStyleKeys[property]}
}

func captureShapeLabelStyle(deck Deck, authored, resolved Slide, id, editingGroup string) (*ObjectStyleSnapshot, error) {
	var owner, effective Element
	count := 0
	for _, element := range authored.Elements {
		if element.ID == id {
			owner, count = element, count+1
		}
	}
	if count != 1 || owner.Kind != "shape" || owner.Inherited || elementGroup(owner) != "" && elementGroup(owner) != editingGroup {
		return nil, fmt.Errorf("select one editable shape label; enter its group first")
	}
	for _, element := range resolved.Elements {
		if element.ID == id {
			effective = element
			break
		}
	}
	label, ok := shapeLabel(effective)
	if !ok {
		return nil, fmt.Errorf("the shape has no label")
	}
	raw, ok := rawShapeLabel(owner)
	if !ok {
		raw = Element{ID: id, Kind: "text", Text: label.Text, Query: shapeLabelStyleValues(owner, url.Values{}).Encode()}
	}
	resolved.Elements = []Element{label}
	authored.Elements = []Element{raw}
	authored.TTFSize, authored.TTFWidth = 0, 0
	resolved.TTFSize, resolved.TTFWidth = 0, 0
	snapshot, err := captureObjectStyle(deck, authored, resolved, id)
	if err != nil {
		return nil, err
	}
	for key := range snapshot.Fields {
		if !labelStyleProperty(key) {
			delete(snapshot.Fields, key)
		}
	}
	snapshot.Context = "shape-label"
	return snapshot, snapshot.Validate()
}

// Label style transfer reuses the ordinary validated style implementation on
// owned synthetic labels, then projects only style fields back to their owners.
// No content, shape geometry, assets or body paint enters the snapshot/commit.
func applyShapeLabelStyleTransfer(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	reset := action.Action == "reset-object-style"
	if !reset && action.StyleData.Context != "shape-label" {
		return false, fmt.Errorf("copy a label style before applying it to labels")
	}
	slide := deck.Slides[action.Slide]
	if _, err := typographySelection(slide, action.ObjectIDs, editingGroup); err != nil {
		return false, err
	}
	resolved := deck.ResolveSlide(action.Slide, false)
	caps := buildSelectionCapabilities(*deck, slide, resolved, action.ObjectIDs, editingGroup, 0)
	state := caps.Properties["label.size"]
	if !state.Editable {
		return false, fmt.Errorf("select editable shape labels; enter their group first")
	}
	ids := map[string]bool{}
	for _, id := range state.EligibleIDs {
		ids[id] = true
	}
	labels := []Element{}
	for _, owner := range resolved.Elements {
		if ids[owner.ID] {
			label, _ := shapeLabel(owner)
			labels = append(labels, label)
		}
	}
	isolated := *deck
	isolated.Masters = MasterDeck{}
	isolated.Slides = []Slide{{Elements: append([]Element(nil), labels...)}}
	command := action
	command.StyleContext, command.Slide, command.ObjectIDs = "", 0, state.EligibleIDs
	if !reset {
		command.StyleData = action.StyleData.Clone()
		command.StyleData.Context = ""
	}
	if _, err := applyObjectStyle(&isolated, command, ""); err != nil {
		return false, err
	}
	previous, updated := map[string]Element{}, map[string]Element{}
	for i, label := range labels {
		previous[label.ID], updated[label.ID] = label, isolated.Slides[0].Elements[i]
	}
	draft := cloneSlide(slide)
	for i, owner := range draft.Elements {
		next, ok := updated[owner.ID]
		if !ok {
			continue
		}
		if next.Text != previous[owner.ID].Text {
			return false, fmt.Errorf("label formatting cannot change content")
		}
		q := mustQuery(owner.Query)
		if reset {
			for _, key := range labelStyleKeys {
				q.Del("label-" + key)
			}
			setLabelColorClears(q, nil)
			removeLabelStyleClears(q, labelStyleKeys)
			// Authored nested styles predate sparse overrides. Reset those too,
			// while retaining label text and all compatible extension fields.
			if encoded := q.Get("shape-label"); encoded != "" {
				data, err := decodeShapeLabelData(encoded)
				if err != nil {
					return false, err
				}
				style := mustQuery(data.Query)
				for _, key := range labelStyleKeys {
					style.Del(key)
				}
				if style.Get("align") == "justify" {
					style.Del("align") // Legacy paragraph style, not a position anchor.
				}
				data.Query = style.Encode()
				raw, err := json.Marshal(data)
				if err != nil {
					return false, err
				}
				if len(raw) > 65536 {
					return false, fmt.Errorf("shape label exceeds its storage limit")
				}
				q.Set("shape-label", base64.StdEncoding.EncodeToString(raw))
				if owner.MasterSlotID != "" {
					q.Set("label-style-inherit", "1")
				}
			}
		} else {
			oldQ, nextQ := mustQuery(previous[owner.ID].Query), mustQuery(next.Query)
			for _, key := range labelStyleKeys {
				if key == "modern-runs" || key == "ttf-weight" {
					continue // Whole-label emphasis has independent sparse flags.
				}
				if reflect.DeepEqual(oldQ[key], nextQ[key]) {
					continue
				}
				if nextQ.Has(key) {
					q["label-"+key] = append([]string(nil), nextQ[key]...)
				} else if key == "modern-runs" || key == "ttf-weight" {
					q.Set("label-"+key, "none")
				} else {
					q.Del("label-" + key)
				}
			}
			for property, field := range action.StyleData.Fields {
				removeLabelStyleClears(q, labelPropertyKeys(property))
				if !field.Present {
					for _, key := range labelPropertyKeys(property) {
						q.Del("label-" + key)
					}
				} else if property == "text.bold" || property == "text.italic" || property == "text.underline" {
					key, value := labelPropertyKeys(property)[0], "0"
					if property == "text.bold" {
						value = "none"
					}
					if field.Value == "true" {
						value = "1"
						if property == "text.bold" {
							value = "bold"
						}
					}
					q.Set("label-"+key, value)
				} else if property != "paint.color" {
					key := objectStyleKeys[property]
					if key != "" && nextQ.Has(key) {
						q.Set("label-"+key, nextQ.Get(key))
					}
				}
			}
			if field, ok := action.StyleData.Fields["paint.color"]; ok {
				setLabelColorClears(q, nil)
				if field.Present {
					if nextQ.Has("theme-fg") {
						q.Del("label-fg")
						q.Set("label-theme-fg", nextQ.Get("theme-fg"))
						setLabelColorClears(q, []string{"label-fg"})
					} else {
						q.Del("label-theme-fg")
						q.Set("label-fg", nextQ.Get("fg"))
						setLabelColorClears(q, []string{"label-theme-fg"})
					}
				}
			}
			// Zero paragraph spacing must explicitly mask a nested nonzero value.
			for _, property := range []string{"text.paragraphBefore", "text.paragraphAfter"} {
				if field, ok := action.StyleData.Fields[property]; ok && field.Present {
					q.Set("label-"+objectStyleKeys[property], field.Value)
				}
			}
		}
		draft.Elements[i].Query = q.Encode()
	}
	if reflect.DeepEqual(slide, draft) {
		return false, nil
	}
	deck.Slides[action.Slide] = draft
	return true, nil
}

func removeLabelStyleClears(q url.Values, keys []string) {
	remove := map[string]bool{}
	for _, key := range keys {
		remove["label-"+key] = true
	}
	remaining := []string{}
	for _, key := range strings.Split(q.Get("master-clear"), ",") {
		if key = strings.TrimSpace(key); key != "" && !remove[key] {
			remaining = append(remaining, key)
		}
	}
	if len(remaining) == 0 {
		q.Del("master-clear")
	} else {
		q.Set("master-clear", strings.Join(remaining, ","))
	}
}

func decodeShapeLabelData(encoded string) (*shapeLabelData, error) {
	if len(encoded) > 90000 {
		return nil, fmt.Errorf("shape label exceeds its storage limit")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) > 65536 {
		return nil, fmt.Errorf("invalid shape label")
	}
	var data shapeLabelData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// Shape label content and style have independent ownership. When an instance
// owns its text, missing nested formatting fields still inherit from its master.
func mergeShapeLabelMetadata(base, override string) string {
	a, err := decodeShapeLabelData(base)
	if err != nil {
		return override
	}
	b, err := decodeShapeLabelData(override)
	if err != nil {
		return override
	}
	if mustQuery(a.Query).Has("shape-label") || mustQuery(b.Query).Has("shape-label") {
		return override // Labels cannot recursively contain other labels.
	}
	b.Query = mergeQueries(a.Query, b.Query)
	if a.Extra != nil {
		extra := cloneJSONExtensions(a.Extra)
		if extra == nil {
			values := jsonExtensions{}
			extra = &values
		}
		if b.Extra != nil {
			for key, value := range *b.Extra {
				(*extra)[key] = append(json.RawMessage(nil), value...)
			}
		}
		b.Extra = extra
	}
	raw, err := json.Marshal(b)
	if err != nil || len(raw) > 65536 || strings.Contains(b.Query, "shape-label=") {
		return override
	}
	return base64.StdEncoding.EncodeToString(raw)
}
