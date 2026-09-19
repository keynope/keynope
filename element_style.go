package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
)

func validElementStyle(style string) bool {
	return style == "retro" || style == "modern"
}

// Resolve defaults without materialising overrides in the authored document.
// Base defaults apply even to slides without a layout; layout defaults apply
// only when that layout exists. Explicit element styles remain authoritative.
func (d Deck) slideDefaultStyle(slide Slide) string {
	if validElementStyle(slide.DefaultStyle) {
		return slide.DefaultStyle
	}
	if layout, ok := d.Masters.Layout(slide.LayoutID); ok && validElementStyle(layout.Slide.DefaultStyle) {
		return layout.Slide.DefaultStyle
	}
	if validElementStyle(d.Masters.Base.Slide.DefaultStyle) {
		return d.Masters.Base.Slide.DefaultStyle
	}
	return d.AppearanceMode()
}

func (d Deck) elementStyle(slide Slide, element Element) string {
	q, _ := url.ParseQuery(element.Query)
	if style := q.Get("element-style"); validElementStyle(style) {
		return style
	}
	return d.slideDefaultStyle(slide)
}

// Preflight the entire identity-based batch before writing any query. The
// caller expands collapsed groups; inherited objects cannot be edited here.
func setElementStyles(slide *Slide, ids []string, style string) (bool, error) {
	if slide == nil || len(ids) == 0 || (style != "inherit" && !validElementStyle(style)) {
		return false, fmt.Errorf("select objects and a valid element style")
	}
	indices, err := editableObjectIndices(*slide, ids)
	if err != nil {
		return false, err
	}
	updates := map[int]string{}
	for _, i := range indices {
		e := slide.Elements[i]
		switch e.Kind {
		case "text", "heading", "bullet", "code", "text-image", "page-number", "shape", "image", "connector":
		default:
			return false, fmt.Errorf("object %s does not support element styles", e.ID)
		}
		q, err := url.ParseQuery(e.Query)
		if err != nil {
			return false, err
		}
		if style == "inherit" {
			if !q.Has("element-style") {
				continue
			}
			q.Del("element-style")
		} else {
			if q.Get("element-style") == style {
				continue
			}
			q.Set("element-style", style)
		}
		updates[i] = q.Encode()
	}
	for i, query := range updates {
		slide.Elements[i].Query = query
	}
	return len(updates) > 0, nil
}

func applyElementStyle(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	if action.Slide < 0 || action.Slide >= len(deck.Slides) {
		return false, fmt.Errorf("invalid style target slide")
	}
	slide := &deck.Slides[action.Slide]
	if action.Kind == "label" {
		return applyShapeLabelStyle(deck, slide, action, editingGroup)
	}
	if action.Kind != "" {
		return false, fmt.Errorf("invalid element style target")
	}
	ids := append([]string(nil), action.ObjectIDs...)
	selected := map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	groups := map[string]bool{}
	for _, e := range slide.Elements {
		if group := elementGroup(e); selected[e.ID] && group != "" && group != editingGroup {
			groups[group] = true
		}
	}
	for _, e := range slide.Elements {
		if groups[elementGroup(e)] && !selected[e.ID] {
			ids = append(ids, e.ID)
		}
	}
	// Validate the schema upgrade before touching any element. An invalid or
	// oversized appearance payload must not leave a partially applied command.
	appearance, err := deck.withDefaultStyle(deck.AppearanceMode())
	if err != nil {
		return false, err
	}
	changed, err := setElementStyles(slide, ids, action.Name)
	if err != nil || !changed {
		return changed, err
	}
	// Opt in to the resolved-style compositor only on an authored style change.
	// Legacy documents retain their original renderer until that point.
	deck.Appearance = appearance
	return true, nil
}

func applyShapeLabelStyle(deck *Deck, slide *Slide, action nativeEditorAction, editingGroup string) (bool, error) {
	if len(action.ObjectIDs) != 1 {
		return false, fmt.Errorf("select one labelled shape")
	}
	// Reuse identity, lock and protected-element validation without modifying
	// the shape's own style. Stage every change until all validation succeeds.
	draft := cloneSlide(*slide)
	if _, err := setElementStyles(&draft, action.ObjectIDs, action.Name); err != nil {
		return false, err
	}
	for i, e := range slide.Elements {
		if e.ID != action.ObjectIDs[0] {
			continue
		}
		if e.Kind != "shape" {
			return false, fmt.Errorf("select a labelled shape")
		}
		if g := elementGroup(e); g != "" && g != editingGroup {
			return false, fmt.Errorf("enter the group to style its shape label")
		}
		if _, ok := shapeLabel(e); !ok {
			return false, fmt.Errorf("add a shape label first")
		}
		_, data, err := sceneEditableShapeLabel(e)
		if err != nil {
			return false, err
		}
		q, err := url.ParseQuery(data.Query)
		if err != nil {
			return false, err
		}
		old := q.Get("element-style")
		if action.Name == "inherit" {
			if old == "" {
				return false, nil
			}
			q.Del("element-style")
		} else {
			if old == action.Name {
				return false, nil
			}
			q.Set("element-style", action.Name)
		}
		data.Query = q.Encode()
		raw, err := json.Marshal(data)
		if err != nil {
			return false, err
		}
		if len(raw) > 65536 {
			return false, fmt.Errorf("shape label exceeds its storage limit")
		}
		appearance, err := deck.withDefaultStyle(deck.AppearanceMode())
		if err != nil {
			return false, err
		}
		slide.Elements[i].Query = setQueryValue(e.Query, "shape-label", base64.StdEncoding.EncodeToString(raw))
		deck.Appearance = appearance
		return true, nil
	}
	return false, fmt.Errorf("shape no longer exists")
}

func (d Deck) usesElementStyles() bool { return d.Appearance != nil && d.Appearance.Version == 2 }

// Defaults change inheritance, never authored element overrides or geometry.
func applyStyleDefault(deck *Deck, action nativeEditorAction) (bool, error) {
	style := action.Name
	if action.Kind != "deck" && action.Kind != "slide" {
		return false, fmt.Errorf("invalid style-default scope")
	}
	if !validElementStyle(style) && !(action.Kind == "slide" && style == "inherit") {
		return false, fmt.Errorf("invalid style default")
	}
	if action.Kind == "deck" {
		if deck.AppearanceMode() == style {
			return false, nil
		}
		appearance, err := deck.withDefaultStyle(style)
		if err != nil {
			return false, err
		}
		deck.Appearance = appearance
		return true, nil
	}
	if action.Slide < 0 || action.Slide >= len(deck.Slides) {
		return false, fmt.Errorf("invalid style target slide")
	}
	if style == "inherit" {
		style = ""
	}
	if deck.Slides[action.Slide].DefaultStyle == style {
		return false, nil
	}
	appearance, err := deck.withDefaultStyle(deck.AppearanceMode())
	if err != nil {
		return false, err
	}
	deck.Slides[action.Slide].DefaultStyle = style
	deck.Appearance = appearance
	return true, nil
}

func buildDocumentSlideScene(deck Deck, index, cols, rows int) (slideScene, error) {
	// Appearance version controls persisted metadata, not the renderer. Missing
	// appearance means inherited Retro; legacy mode is an inherited default.
	return buildMixedSlideScene(deck, index, cols, rows)
}
