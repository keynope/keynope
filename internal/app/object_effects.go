package app

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"keynope/internal/effects"
)

type effectControl struct {
	key, fallback string
	max           float64
	choices       string
}

var effectControls = map[string]effectControl{
	"paint.blur":             {"fx-blur", "0", 48, ""},
	"paint.blurAxes":         {"fx-blur-axes", "both", 0, "both,horizontal,vertical"},
	"paint.posterize":        {"fx-posterize", "0", 32, ""},
	"paint.colourEffect":     {"fx-colour", "none", 0, "none,invert,duotone,tritone"},
	"paint.effectShadows":    {"fx-shadows", "#172554", 0, "colour"},
	"paint.effectMidtones":   {"fx-midtones", "#db2777", 0, "colour"},
	"paint.effectHighlights": {"fx-highlights", "#fff1b8", 0, "colour"},
	"paint.glowRadius":       {"fx-glow-radius", "0", 48, ""},
	"paint.glowColour":       {"fx-glow-colour", "#55aaff", 0, "colour"},
	"paint.glowThreshold":    {"fx-glow-threshold", "60", 100, ""},
	"paint.glowIntensity":    {"fx-glow-intensity", "100", 100, ""},
}

func init() {
	// Registration extends the existing identity-scoped, undoable paint and
	// style-transfer contract; effects never get a separate mutation path.
	for property, control := range effectControls {
		objectStyleKeys[property] = control.key
	}
	selectionPropertyIDs = append(selectionPropertyIDs,
		"paint.blur", "paint.blurAxes", "paint.posterize", "paint.colourEffect",
		"paint.effectShadows", "paint.effectMidtones", "paint.effectHighlights",
		"paint.glowRadius", "paint.glowColour", "paint.glowThreshold", "paint.glowIntensity")
}
func validEffectValue(c effectControl, value string) bool {
	if c.choices == "colour" {
		_, ok := normalizeHexColour(value)
		return ok
	}
	if c.choices != "" {
		for _, choice := range strings.Split(c.choices, ",") {
			if value == choice {
				return true
			}
		}
		return false
	}
	n, err := strconv.ParseFloat(value, 64)
	return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= c.max && (c.key != "fx-posterize" || math.Trunc(n) == n && n != 1)
}
func effectMetadataKey(key string) bool {
	for _, c := range effectControls {
		if c.key == key {
			return true
		}
	}
	return false
}
func effectValue(q url.Values, property string) string {
	c := effectControls[property]
	v := q.Get(c.key)
	if !validEffectValue(c, v) {
		return c.fallback
	}
	return v
}
func effectNumber(q url.Values, property string) float64 {
	n, _ := strconv.ParseFloat(effectValue(q, property), 64)
	return n
}
func effectSettings(q url.Values) *effects.Settings {
	active := extendedGradient(q.Get("gradient-dir"))
	if !active {
		for _, c := range effectControls {
			if q.Has(c.key) {
				active = true
				break
			}
		}
	}
	if !active {
		return nil
	}
	s := &effects.Settings{Blur: effectNumber(q, "paint.blur"), Axes: effectValue(q, "paint.blurAxes"), Posterize: int(effectNumber(q, "paint.posterize")), Colour: effectValue(q, "paint.colourEffect"), Shadows: effectValue(q, "paint.effectShadows"), Midtones: effectValue(q, "paint.effectMidtones"), Highlights: effectValue(q, "paint.effectHighlights"), GlowRadius: effectNumber(q, "paint.glowRadius"), GlowColour: effectValue(q, "paint.glowColour"), GlowThreshold: effectNumber(q, "paint.glowThreshold"), GlowIntensity: effectNumber(q, "paint.glowIntensity")}
	if extendedGradient(q.Get("gradient-dir")) {
		if _, a := parseRGBHex(q.Get("gradient-start")); a {
			if _, b := parseRGBHex(q.Get("gradient-end")); b {
				s.Gradient = q.Get("gradient-dir")
				s.GradientStart = q.Get("gradient-start")
				s.GradientEnd = q.Get("gradient-end")
			}
		}
	}
	if s.Blur == 0 && s.Posterize == 0 && s.Colour == "none" && (s.GlowRadius == 0 || s.GlowIntensity == 0) && s.Gradient == "" {
		return nil
	}
	return s
}
func extendedGradient(value string) bool {
	return value == "radial" || value == "reflected" || value == "diamond" || value == "angle"
}
func compileSceneEffects(o *sceneObject, q url.Values) {
	if o.Paint.StrokePosition != "" {
		inside, outside := effects.StrokeWidths(o.Paint.StrokePosition, o.Paint.StrokeWidth)
		o.Paint.StrokeSpan = 2 * math.Max(inside, outside)
	}
	compilePaintEffects(&o.Paint, q, o.Bounds)
}
func compilePaintEffects(p *scenePaint, q url.Values, bounds sceneRect) {
	if s := effectSettings(q); s != nil {
		settings := *s
		if settings.Gradient != "" {
			p.GradientImage = effects.GradientPNG(settings.Gradient, settings.GradientStart, settings.GradientEnd, bounds.Width, bounds.Height)
			settings.Gradient = ""
		}
		p.Effects = effects.Compile(settings, bounds.Width, bounds.Height)
		if p.Effects != nil {
			// The effect samples already-styled artwork. Preserve the existing
			// outline/shadow overflow as well as the new convolution support.
			p.Effects.Padding = math.Min(1024, p.Effects.Padding+p.StrokeWidth+3*p.ShadowBlur+math.Max(math.Abs(p.ShadowX), math.Abs(p.ShadowY)))
		}
	}
}
func effectPaintValue(q url.Values, property string) interface{} {
	c := effectControls[property]
	if c.choices != "" {
		return effectValue(q, property)
	}
	return effectNumber(q, property)
}
func validateEffectProperty(property, value string) error {
	if c, ok := effectControls[property]; ok && !validEffectValue(c, value) {
		return fmt.Errorf("invalid effect setting %s", property)
	}
	return nil
}
