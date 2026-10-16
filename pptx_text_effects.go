package main

import (
	"keynope/internal/pptx"
	"math"
	"net/url"
	"strconv"
	"strings"
)

func applyPPTXTextEffects(query url.Values, object pptx.Object, scale float64, label bool, canvas ...sceneRect) []pptx.Warning {
	effect := object.TextEffects
	if effect == nil {
		return nil
	}
	var warnings []pptx.Warning
	if gradient := effect.Gradient; gradient != nil {
		query.Set("gradient-start", gradient.Start)
		query.Set("gradient-end", gradient.End)
		query.Set("gradient-dir", gradient.Direction)
	}
	warn := func(code, message string) {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: code, Message: message})
	}
	parentAlpha := object.Opacity
	if !object.OpacitySet && parentAlpha == 0 {
		parentAlpha = 1
	}
	composedAlpha := parentAlpha
	if effect.Opacity != nil {
		composedAlpha = *effect.Opacity
		alpha := composedAlpha
		if label {
			if parentAlpha > 0 {
				alpha = composedAlpha / parentAlpha
			} else {
				alpha = 1
				warn("label-opacity-ambiguous", "A fully transparent shape hides its authored label-opacity multiplier; the visible zero alpha is retained.")
			}
			if alpha > 1 {
				alpha = 1
				warn("label-opacity-clamped", "Character alpha exceeded the containing shape alpha; label opacity was clamped to Keynope's supported range.")
			}
		}
		query.Set("modern-opacity", decimal(alpha))
	}
	if effect.Stroke != "" && effect.StrokeWidth > 0 {
		mode := "dark"
		if strings.EqualFold(effect.Stroke, "#ffffff") {
			mode = "light"
		}
		query.Set("outline", mode)
		expected := "#111111"
		if mode == "light" {
			expected = "#ffffff"
		}
		if !strings.EqualFold(effect.Stroke, expected) || math.Abs(effect.StrokeWidth*2*scale-2) > .001 {
			warn("text-outline-approximated", "Text outline uses the nearest supported Keynope light/dark outline colour and width.")
		}
	}
	if shadow := effect.Shadow; shadow != nil {
		width, height := pptxCanvasDimensions(canvas)
		sx, sy := width/245.0, height/56.0
		x, y := clampInt(int(math.Round(shadow.X*scale/sx)), -4, 4), clampInt(int(math.Round(shadow.Y*scale/sy)), -4, 4)
		if x == 0 && y == 0 {
			x, y = 1, 1
		}
		mode := "solid"
		blur := 0.0
		if shadow.Blur > 0 {
			mode = "soft"
			blur = 8
		}
		query.Set("shadow", mode)
		query.Set("shadow-color", shadow.Color)
		query.Set("shadow-x", strconv.Itoa(x))
		query.Set("shadow-y", strconv.Itoa(y))
		if math.Abs(float64(x)*sx-shadow.X*scale) > .001 || math.Abs(float64(y)*sy-shadow.Y*scale) > .001 || math.Abs(blur-shadow.Blur*scale) > .001 {
			warn("text-shadow-approximated", "Shadow colour is retained; text shadow offset and blur use the nearest supported Keynope settings.")
		}
		if shadow.Opacity != nil && math.Abs(*shadow.Opacity-composedAlpha) > 1e-6 {
			warn("text-shadow-opacity-approximated", "Independent character-shadow opacity follows the text opacity in Keynope.")
		}
	}
	return warnings
}
