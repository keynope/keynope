package main

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// Insets are fractions of the original source, never of a previous crop.
type sceneCrop struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

func (c sceneCrop) valid() bool {
	for _, v := range []float64{c.Left, c.Top, c.Right, c.Bottom} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= 1 {
			return false
		}
	}
	return c.Left+c.Right <= .99 && c.Top+c.Bottom <= .99
}

func parseModernCrop(raw string) *sceneCrop {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return nil
	}
	c := sceneCrop{}
	for i, p := range []*float64{&c.Left, &c.Top, &c.Right, &c.Bottom} {
		v, err := strconv.ParseFloat(parts[i], 64)
		if err != nil {
			return nil
		}
		*p = v
	}
	if !c.valid() || c == (sceneCrop{}) {
		return nil
	}
	return &c
}

func (c sceneCrop) queryValue() string {
	parts := make([]string, 4)
	for i, v := range []float64{c.Left, c.Top, c.Right, c.Bottom} {
		parts[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(parts, ",")
}

// Mask the source samples before glyph classification. Sample pixels have a
// 1:1 design-space aspect (two across/four down per terminal-style cell).
func applySampledImageMask(pixels []rgba8, width, height int, mask string) {
	if mask == "" || width <= 0 || height <= 0 {
		return
	}
	radius := .12 * float64(min(width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			px, py := float64(x)+.5, float64(y)+.5
			inside := true
			switch mask {
			case "ellipse":
				dx, dy := px/float64(width)*2-1, py/float64(height)*2-1
				inside = dx*dx+dy*dy <= 1
			case "rounded":
				dx := math.Max(radius-px, math.Max(px-(float64(width)-radius), 0))
				dy := math.Max(radius-py, math.Max(py-(float64(height)-radius), 0))
				inside = dx*dx+dy*dy <= radius*radius
			}
			if !inside {
				pixels[y*width+x] = rgba8{}
			}
		}
	}
}

func applySceneCrop(deck *Deck, action nativeEditorAction) (bool, error) {
	if action.ModernMask != nil && *action.ModernMask != "" && *action.ModernMask != "rounded" && *action.ModernMask != "ellipse" {
		return false, fmt.Errorf("unsupported image mask")
	}
	if action.Crop == nil || !action.Crop.valid() || action.ObjectID == "" || action.Slide < 0 || action.Slide >= len(deck.Slides) {
		return false, errInvalidEditorAction
	}
	if _, err := editableObjectIndices(deck.Slides[action.Slide], []string{action.ObjectID}); err != nil {
		return false, err
	}
	for i, e := range deck.Slides[action.Slide].Elements {
		if e.ID != action.ObjectID {
			continue
		}
		if e.Kind != "image" || protectedActivityElement(e) || objectLocked(e) {
			return false, errInvalidEditorAction
		}
		if _, ok := deck.Assets[e.AssetID]; !ok {
			return false, fmt.Errorf("image source is unavailable")
		}
		q, _ := url.ParseQuery(e.Query)
		old := parseModernCrop(q.Get("modern-crop"))
		maskChanged := action.ModernMask != nil && q.Get("modern-mask") != *action.ModernMask
		if !maskChanged && (old != nil && *old == *action.Crop || old == nil && *action.Crop == (sceneCrop{})) {
			return false, nil
		}
		if *action.Crop == (sceneCrop{}) {
			q.Del("modern-crop")
		} else {
			q.Set("modern-crop", action.Crop.queryValue())
		}
		if maskChanged {
			if *action.ModernMask == "" {
				q.Del("modern-mask")
			} else {
				q.Set("modern-mask", *action.ModernMask)
			}
		}
		deck.Slides[action.Slide].Elements[i].Query = q.Encode()
		return true, nil
	}
	return false, fmt.Errorf("the image no longer exists")
}
