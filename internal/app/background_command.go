package app

import (
	"fmt"
	"math"
	"net/url"
	"reflect"
	"strconv"
)

// Promote one authored image without splitting the source visibility and
// background update into separate history entries. All validation precedes edits.
func promoteImageBackground(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	if action.Slide < 0 || action.Slide >= len(deck.Slides) {
		return false, fmt.Errorf("slide no longer exists")
	}
	slide := &deck.Slides[action.Slide]
	if err := requireSelectionObjectCommand(*deck, *slide, action.ObjectID, editingGroup, "image.background"); err != nil {
		return false, err
	}
	indices, err := editableObjectIndices(*slide, []string{action.ObjectID})
	if err != nil {
		return false, err
	}
	if err = checkObjectLocks(*slide, action, nil, editingGroup); err != nil {
		return false, err
	}
	index := indices[0]
	element := slide.Elements[index]
	if element.Kind != "image" || element.AssetID == "" {
		return false, fmt.Errorf("select an embedded image")
	}
	if _, ok := deck.Assets[element.AssetID]; !ok {
		return false, fmt.Errorf("image asset is unavailable")
	}
	effective := element
	for _, candidate := range deck.ResolveSlide(action.Slide, false).Elements {
		if candidate.ID == element.ID {
			effective = candidate
			break
		}
	}
	query, err := url.ParseQuery(effective.Query)
	if err != nil {
		return false, err
	}
	opacity := 1.0
	if value, parseErr := strconv.ParseFloat(query.Get("modern-opacity"), 64); parseErr == nil && !math.IsNaN(value) && value >= 0 && value <= 1 {
		opacity = value
	}
	fit := imageFit(query.Get("image-fit"))
	if fit == "" {
		fit = "cover"
	}
	query.Del("modern-opacity")
	query.Del("object-hidden")
	background := &slideBackgroundMedia{SourceObjectID: element.ID, AssetID: element.AssetID, Fit: fit, Opacity: opacity, OpacitySet: true, Query: query.Encode()}
	authored, _ := url.ParseQuery(element.Query)
	authored.Set("object-hidden", "1")
	if slide.BackgroundMediaSet && reflect.DeepEqual(slide.BackgroundMedia, background) && authored.Encode() == element.Query {
		return false, nil
	}
	slide.Elements[index].Query = authored.Encode()
	slide.BackgroundMedia = background
	slide.BackgroundMediaSet = true
	return true, nil
}

// Restore only the authored owner in this scope. Never unhide an inherited or
// merely matching asset: several independent objects can share source bytes.
func restoreBackgroundImage(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	if action.Slide < 0 || action.Slide >= len(deck.Slides) {
		return false, fmt.Errorf("slide no longer exists")
	}
	slide := &deck.Slides[action.Slide]
	if action.ObjectID == "" {
		return materializeBackgroundImage(deck, action.Slide)
	}
	background := slide.BackgroundMedia
	if !slide.BackgroundMediaSet || background == nil || background.SourceObjectID == "" || background.SourceObjectID != action.ObjectID {
		return false, fmt.Errorf("the original background image is not owned by this slide")
	}
	indices, err := editableObjectIndices(*slide, []string{action.ObjectID})
	if err != nil {
		return false, err
	}
	if err := checkObjectLocks(*slide, action, nil, editingGroup); err != nil {
		return false, err
	}
	index := indices[0]
	element := slide.Elements[index]
	if element.Kind != "image" || element.AssetID != background.AssetID {
		return false, fmt.Errorf("the original background image is no longer available")
	}
	if _, ok := deck.Assets[element.AssetID]; !ok {
		return false, fmt.Errorf("image asset is unavailable")
	}
	query, err := url.ParseQuery(element.Query)
	if err != nil {
		return false, err
	}
	treatment, err := url.ParseQuery(background.Query)
	if err != nil {
		return false, err
	}
	// Placement, groups, links, rotation and opaque authored extensions belong
	// to the retained foreground object. Only media treatment flows back.
	copyBackgroundTreatment(query, treatment, background)
	query.Del("object-hidden")
	slide.Elements[index].Query = query.Encode()
	// Explicit None avoids unexpectedly exposing an inherited background.
	slide.BackgroundMedia = nil
	slide.BackgroundMediaSet = true
	return true, nil
}

func copyBackgroundTreatment(query, treatment url.Values, background *slideBackgroundMedia) {
	for _, key := range []string{"image-style", "element-style", "glyph", "shape", "brightness", "contrast", "saturation", "tint", "sharpness", "alpha", "modern-crop", "modern-mask", "outline", "transparent", "fg", "bg", "gradient-start", "gradient-end", "gradient-dir", "shadow", "shadow-color", "shadow-x", "shadow-y"} {
		query.Del(key)
		if values, ok := treatment[key]; ok {
			query[key] = append([]string(nil), values...)
		}
	}
	fit := imageFit(background.Fit)
	if fit == "" {
		fit = "cover"
	}
	query.Set("image-fit", fit)
	opacity := background.Opacity
	if !background.OpacitySet && opacity == 0 || math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1 {
		opacity = 1
	}
	query.Set("modern-opacity", strconv.FormatFloat(opacity, 'g', -1, 64))
}

// Imported backgrounds have no foreground placement to restore. Make an
// independent full-canvas image instead; inherited sources are copied locally,
// never removed from the master that owns them.
func materializeBackgroundImage(deck *Deck, index int) (bool, error) {
	slide := &deck.Slides[index]
	background := slide.BackgroundMedia
	if !slide.BackgroundMediaSet {
		background = deck.ResolveSlide(index, false).BackgroundMedia
	} else if background != nil && background.SourceObjectID != "" {
		return false, fmt.Errorf("restore the original background image instead")
	}
	if background == nil || background.AssetID == "" {
		return false, fmt.Errorf("no background image is available")
	}
	asset, ok := deck.Assets[background.AssetID]
	if !ok {
		return false, fmt.Errorf("image asset is unavailable")
	}
	treatment, err := url.ParseQuery(background.Query)
	if err != nil {
		return false, err
	}
	query := url.Values{}
	copyBackgroundTreatment(query, treatment, background)
	if query.Get("image-style") == "" {
		query.Set("image-style", "modern")
	}
	cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	query.Set("left", "0")
	query.Set("top", "0")
	query.Set("width", strconv.Itoa(cols))
	query.Set("height", strconv.Itoa(rows))
	query.Set("stretch", "1")
	query.Set("z-index", "0")
	element := Element{ID: newStableID("image"), Kind: "image", AssetID: background.AssetID, Path: embeddedAssetPath(background.AssetID, asset), Query: query.Encode()}
	// Prepending keeps ties at rank zero underneath existing foreground objects,
	// without rewriting the ranks of locked objects or other groups.
	slide.Elements = append([]Element{element}, slide.Elements...)
	slide.BackgroundMedia, slide.BackgroundMediaSet = nil, true
	return true, nil
}
