package app

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
)

// Only explicit independent geometry can be returned before scene layout.
// Flow/centre/right anchors continue to use the current scene's measured box;
// once edited, canonical origins let the next response return accepted values
// immediately, without running image conversion or text measurement in state.
func explicitSelectionGeometry(element Element) *sceneRect {
	if element.Kind == "connector" {
		return nil
	}
	q, err := url.ParseQuery(element.Query)
	if err != nil || !q.Has("left") || !q.Has("top") || q.Has("left_pct") || q.Has("right") || q.Has("right_pct") || q.Has("bottom") {
		return nil
	}
	if element.Kind == "image" && q.Get("stretch") != "1" && imageFit(q.Get("image-fit")) == "" {
		return nil // Aspect-fit bounds require the scene's natural-media geometry.
	}
	values := make([]float64, 4)
	for i, key := range []string{"left", "top", "width", "height"} {
		v, err := strconv.ParseFloat(q.Get(key), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		values[i] = v
	}
	values[0] += objectPlacementOffset(q, "x")
	values[1] += objectPlacementOffset(q, "y")
	if element.Kind == "shape" {
		values[0] += shapeSubcellOffset(q, "shape-offset-x")
		values[1] += shapeSubcellOffset(q, "shape-offset-y")
	}
	if values[0] < 0 || values[1] < 0 || values[2] <= 0 || values[3] <= 0 {
		return nil
	}
	return &sceneRect{X: values[0], Y: values[1], Width: values[2], Height: values[3]}
}

func applySelectionGeometry(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	if action.Geometry == nil || action.Slide < 0 || action.Slide >= len(deck.Slides) {
		return false, errInvalidEditorAction
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
	if !caps.Commands["arrange.geometry"].Enabled {
		return false, fmt.Errorf("independent geometry requires one editable object")
	}
	b := *action.Geometry
	for _, v := range []float64{b.X, b.Y, b.Width, b.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false, fmt.Errorf("geometry must contain finite numbers")
		}
	}
	cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	if b.X < 0 || b.Y < 0 || b.X+b.Width > float64(cols) || b.Y+b.Height > float64(rows) {
		return false, fmt.Errorf("keep the object box within the slide")
	}
	for i, element := range slide.Elements {
		if !selected[element.ID] {
			continue
		}
		minimum := 1.0
		if element.Kind == "shape" {
			minimum = .5
		}
		if b.Width < minimum || b.Height < minimum {
			return false, fmt.Errorf("object width and height must be at least %g slide cells", minimum)
		}
		q, _ := url.ParseQuery(element.Query)
		previous := q.Encode()
		// Older decks used align=justify for paragraph alignment as well as
		// positioning. Moving the box must not remove its text formatting.
		for _, effective := range resolved.Elements {
			if effective.ID == element.ID {
				rq, _ := url.ParseQuery(effective.Query)
				if rq.Get("align") == "justify" && !rq.Has("text-align") {
					q.Set("text-align", "justify")
				}
				break
			}
		}
		anchors := []string{"left_pct", "right", "right_pct", "bottom", "align", "valign", "row_delta", "shape-offset-x", "shape-offset-y", "object-offset-x", "object-offset-y"}
		for _, key := range anchors {
			q.Del(key)
		}
		for _, axis := range []struct {
			anchor, name string
			value        float64
		}{{"left", "x", b.X}, {"top", "y", b.Y}} {
			whole := math.Floor(axis.value)
			q.Set(axis.anchor, strconv.FormatFloat(whole, 'f', -1, 64))
			fraction := axis.value - whole
			if element.Kind == "shape" {
				half := math.Floor(fraction*2) / 2
				if half != 0 {
					q.Set("shape-offset-"+axis.name, strconv.FormatFloat(half, 'f', -1, 64))
				}
				fraction -= half
			}
			if fraction != 0 {
				q.Set("object-offset-"+axis.name, strconv.FormatFloat(fraction, 'f', -1, 64))
			}
		}
		if element.MasterSlotID != "" {
			// An explicit box position also overrides inherited competing
			// anchors and offsets. Local deletion alone lets those reappear
			// during master projection (including an inherited fractional
			// offset when the new position has an integer coordinate).
			var clears []string
			for _, key := range anchors {
				if !q.Has(key) {
					clears = append(clears, key)
				}
			}
			setMasterQueryClears(q, anchors, clears)
		}
		if action.GeometryResize {
			q.Set("width", strconv.FormatFloat(b.Width, 'f', -1, 64))
			q.Set("height", strconv.FormatFloat(b.Height, 'f', -1, 64))
			if element.Kind == "image" {
				q.Del("scale")
				q.Set("stretch", "1")
			} else if element.Kind != "shape" && element.Kind != "table" && element.Kind != "chart" {
				q.Set("text-box", "1")
			}
		}
		if next := q.Encode(); next != previous {
			slide.Elements[i].Query = next
			return true, nil
		}
	}
	return false, nil
}
