package app

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

// Legacy callers use square sample pixels. Scene frames additionally supply
// their displayed aspect, which can differ from the 2-by-4 sampling lattice.
func applySampledImageMask(pixels []rgba8, width, height int, mask string) {
	applySampledImageMaskAspect(pixels, width, height, float64(width)/float64(height), mask)
}

func applySampledImageMaskAspect(pixels []rgba8, width, height int, aspect float64, mask string) {
	if mask == "" || width <= 0 || height <= 0 || width > len(pixels)/height {
		return
	}
	if aspect <= 0 || math.IsNaN(aspect) || math.IsInf(aspect, 0) {
		aspect = float64(width) / float64(height)
	}
	// Measure both axes in units of displayed frame height. Rounded corners
	// use the same physical radius as the Modern renderer, not separate radii
	// stretched by the sample grid's pixel aspect.
	radius := .12 * math.Min(aspect, 1)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			px, py := (float64(x)+.5)/float64(width)*aspect, (float64(y)+.5)/float64(height)
			inside := true
			switch mask {
			case "ellipse":
				dx, dy := px/aspect*2-1, py*2-1
				inside = dx*dx+dy*dy <= 1
			case "rounded":
				dx := math.Max(radius-px, math.Max(px-(aspect-radius), 0))
				dy := math.Max(radius-py, math.Max(py-(1-radius), 0))
				inside = dx*dx+dy*dy <= radius*radius
			}
			if !inside {
				pixels[y*width+x] = rgba8{}
			}
		}
	}
}

func applySceneCrop(deck *Deck, action nativeEditorAction) (bool, error) {
	if action.Slide < 0 || action.Slide >= len(deck.Slides) {
		return false, errInvalidEditorAction
	}
	return applySceneCropResolved(deck, action, *deck, deck.ResolveSlide(action.Slide, false), action.Slide)
}

// Draft and master adapters supply the original resolved owner, so frame
// capture retains theme, page size, flow context and inherited image treatment.
func applySceneCropResolved(deck *Deck, action nativeEditorAction, frameDeck Deck, resolved Slide, index int) (bool, error) {
	if action.ModernMask != nil && *action.ModernMask != "" && *action.ModernMask != "rounded" && *action.ModernMask != "ellipse" {
		return false, fmt.Errorf("unsupported image mask")
	}
	if action.Crop == nil && action.ModernMask == nil || action.Crop != nil && !action.Crop.valid() || action.ObjectID == "" || action.Slide < 0 || action.Slide >= len(deck.Slides) {
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
		before := q.Encode()
		if action.Crop != nil {
			if *action.Crop == (sceneCrop{}) {
				q.Del("modern-crop")
			} else {
				q.Set("modern-crop", action.Crop.queryValue())
			}
		}
		if action.ModernMask != nil {
			if *action.ModernMask == "" {
				q.Del("modern-mask")
			} else {
				q.Set("modern-mask", *action.ModernMask)
			}
		}
		// A reset means the full source/rectangular mask, not the master's
		// crop or mask. Own only fields supplied by this command.
		if e.MasterSlotID != "" {
			var owned []string
			var cleared []string
			if action.Crop != nil {
				owned = append(owned, "modern-crop")
				if *action.Crop == (sceneCrop{}) {
					cleared = append(cleared, "modern-crop")
				}
			}
			if action.ModernMask != nil {
				owned = append(owned, "modern-mask")
				if *action.ModernMask == "" {
					cleared = append(cleared, "modern-mask")
				}
			}
			setMasterQueryClears(q, owned, cleared)
		}
		if q.Encode() == before {
			return false, nil
		}
		candidate := cloneSlide(deck.Slides[action.Slide])
		candidate.Elements[i].Query = q.Encode()
		for _, effective := range resolved.Elements {
			if action.Crop == nil || effective.ID != e.ID {
				continue
			}
			old := sceneCrop{}
			if crop := parseModernCrop(mustQuery(effective.Query).Get("modern-crop")); crop != nil {
				old = *crop
			}
			if old != *action.Crop {
				if err := freezeNaturalImageFrames(frameDeck, resolved, &candidate, index, []string{e.ID}); err != nil {
					return false, err
				}
			}
			break
		}
		deck.Slides[action.Slide] = candidate
		return true, nil
	}
	return false, fmt.Errorf("the image no longer exists")
}
