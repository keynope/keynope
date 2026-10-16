package pptx

import (
	"fmt"
	"math"
	"strconv"
)

// Text effects live on character properties, not the text box outline.
type TextEffects struct {
	Gradient    *ShapeGradient
	Stroke      string
	StrokeWidth float64
	Shadow      *ShapeShadow
	Opacity     *float64 // Already composed with containing shape opacity.
}

func validateTextEffects(effect *TextEffects) error {
	if effect == nil {
		return nil
	}
	if effect.Stroke != "" {
		if _, _, _, ok := colourRGB(effect.Stroke); !ok {
			return fmt.Errorf("invalid text outline colour")
		}
	}
	if math.IsNaN(effect.StrokeWidth) || math.IsInf(effect.StrokeWidth, 0) || effect.StrokeWidth < 0 || effect.StrokeWidth > 64 {
		return fmt.Errorf("invalid text outline width")
	}
	if effect.Opacity != nil && (math.IsNaN(*effect.Opacity) || math.IsInf(*effect.Opacity, 0) || *effect.Opacity < 0 || *effect.Opacity > 1) {
		return fmt.Errorf("invalid text opacity")
	}
	return validateShapePaint(Object{Gradient: effect.Gradient, Shadow: effect.Shadow})
}

func textRunPaintXML(color string, object Object) string {
	effect := object.TextEffects
	if effect == nil {
		return objectSolidFill(color, object)
	}
	paint := object
	if effect.Opacity != nil {
		paint.Opacity, paint.OpacitySet = *effect.Opacity, true
	}
	outline := ""
	if effect.Stroke != "" && effect.StrokeWidth > 0 {
		outline = fmt.Sprintf(`<a:ln w="%d">%s<a:round/></a:ln>`, int64(math.Round(effect.StrokeWidth*12700)), objectSolidFill(effect.Stroke, paint))
	}
	paint.Shadow = effect.Shadow
	fill := objectSolidFill(color, paint)
	if effect.Gradient != nil {
		paint.Gradient = effect.Gradient
		fill = gradientFillXML(paint)
	}
	return outline + fill + shadowEffectXML(paint)
}

func parseTextEffects(n *node, resolver colourResolver) (*TextEffects, bool) {
	property := findFirst(n, "defRPr")
	if property == nil {
		property = findFirst(n, "rPr")
	}
	if property == nil {
		return nil, false
	}
	shadow, _ := parseShapeShadow(property, resolver)
	line := findChild(property, "ln")
	stroke, _ := resolvedPaint(findChild(line, "solidFill"), resolver)
	width := .75
	if raw := attr(line, "w"); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, true
		}
		width = value / 12700
	}
	alpha, alphaSet := 0.0, findFirst(findChild(property, "solidFill"), "alpha") != nil
	if alphaSet {
		_, alpha = resolvedPaint(findChild(property, "solidFill"), resolver)
	}
	gradient, _, gradientAlpha, gradientAlphaSet, gradientLoss := parseShapeGradient(property, resolver)
	if findChild(property, "gradFill") != nil {
		alpha, alphaSet = gradientAlpha, gradientAlphaSet
	}
	// The public model owns one character paint for the text object. Do not
	// silently claim preservation when later runs/paragraph defaults disagree.
	for _, kind := range []string{"rPr", "defRPr", "endParaRPr"} {
		for _, candidate := range descendants(n, kind) {
			if candidate == property {
				continue
			}
			if findChild(candidate, "gradFill") == nil {
				if gradient != nil && hasExplicitShapeFill(candidate) {
					gradientLoss = true
				}
				continue
			}
			other, _, otherAlpha, _, loss := parseShapeGradient(candidate, resolver)
			if loss || gradient == nil || other == nil || *gradient != *other || math.Abs(gradientAlpha-otherAlpha) > 1e-6 {
				gradientLoss = true
			}
		}
	}
	if stroke == "" && shadow == nil && !alphaSet && gradient == nil {
		return nil, gradientLoss
	}
	value := &TextEffects{Gradient: gradient, Stroke: stroke, StrokeWidth: width, Shadow: shadow}
	if alphaSet {
		value.Opacity = &alpha
	}
	if validateTextEffects(value) != nil {
		return nil, true
	}
	return value, gradientLoss
}
