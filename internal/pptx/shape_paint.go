package pptx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type ShapeGradient struct{ Start, End, Direction string }
type ShapeShadow struct {
	Color      string
	X, Y, Blur float64
	Opacity    *float64 // Independent shadow alpha; nil follows the containing object.
}

func cloneShapeShadow(value *ShapeShadow) *ShapeShadow {
	if value == nil {
		return nil
	}
	copy := *value
	if value.Opacity != nil {
		alpha := *value.Opacity
		copy.Opacity = &alpha
	}
	return &copy
}

func hasExplicitShapeFill(pr *node) bool {
	for _, kind := range []string{"noFill", "solidFill", "gradFill", "blipFill", "pattFill", "grpFill"} {
		if findChild(pr, kind) != nil {
			return true
		}
	}
	return false
}

func parseShapeGradient(pr *node, resolver colourResolver) (*ShapeGradient, string, float64, bool, bool) {
	g := findChild(pr, "gradFill")
	if g == nil {
		return nil, "", 0, false, false
	}
	stops := findChild(g, "gsLst")
	if stops == nil || len(stops.Children) == 0 {
		return nil, "", 0, false, true
	}
	first := stops.Children[0]
	color, alpha := resolvedPaint(first, resolver)
	alphaSet := findFirst(first, "alpha") != nil
	if len(stops.Children) != 2 || attr(first, "pos") != "0" || attr(stops.Children[1], "pos") != "100000" {
		return nil, color, alpha, alphaSet, true
	}
	end, endAlpha := resolvedPaint(stops.Children[1], resolver)
	lin := findChild(g, "lin")
	direction := ""
	switch attr(lin, "ang") {
	case "0":
		direction = "horizontal"
	case "5400000":
		direction = "vertical"
	case "2700000":
		direction = "diagonal"
	}
	value := &ShapeGradient{Start: color, End: end, Direction: direction}
	if direction == "" || alpha != endAlpha || lin == nil || attr(lin, "scaled") == "0" || attr(g, "rotWithShape") == "0" || validateShapePaint(Object{Gradient: value}) != nil {
		return nil, color, alpha, alphaSet, true
	}
	return value, color, alpha, alphaSet, false
}

func parseShapeShadow(pr *node, resolver colourResolver) (*ShapeShadow, bool) {
	effect := findChild(pr, "effectLst")
	if effect == nil {
		return nil, false
	}
	shadow := findChild(effect, "outerShdw")
	if shadow == nil {
		return nil, len(effect.Children) > 0
	}
	if len(effect.Children) != 1 {
		return nil, true
	}
	read := func(key string) (float64, bool) {
		raw := attr(shadow, key)
		if raw == "" {
			return 0, true
		}
		v, err := strconv.ParseFloat(raw, 64)
		return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
	}
	distance, dok := read("dist")
	angle, aok := read("dir")
	blur, bok := read("blurRad")
	color, alpha := resolvedPaint(shadow, resolver)
	r := angle / 60000 * math.Pi / 180
	value := &ShapeShadow{Color: color, X: distance / 6350 * math.Cos(r), Y: distance / 6350 * math.Sin(r), Blur: blur / 6350, Opacity: &alpha}
	if !dok || !aok || !bok || distance < 0 || attr(shadow, "sx") != "" && attr(shadow, "sx") != "100000" || attr(shadow, "sy") != "" && attr(shadow, "sy") != "100000" || validateShapePaint(Object{Shadow: value}) != nil {
		return nil, true
	}
	return value, false
}

func validateShapePaint(o Object) error {
	if (o.Gradient != nil || o.Shadow != nil) && o.Kind != "" && o.Kind != "shape" && o.Kind != "text" && !(o.Kind == "image" && o.Gradient == nil) {
		return fmt.Errorf("shape paint requires a shape or text container")
	}
	rgb := func(value string) bool { _, _, _, ok := colourRGB(value); return ok }
	if g := o.Gradient; g != nil {
		if !rgb(g.Start) || !rgb(g.End) {
			return fmt.Errorf("invalid shape gradient colour")
		}
		if g.Direction != "horizontal" && g.Direction != "vertical" && g.Direction != "diagonal" {
			return fmt.Errorf("invalid shape gradient direction")
		}
	}
	if s := o.Shadow; s != nil {
		if s.Opacity != nil && (math.IsNaN(*s.Opacity) || math.IsInf(*s.Opacity, 0) || *s.Opacity < 0 || *s.Opacity > 1) {
			return fmt.Errorf("invalid shadow opacity")
		}
		if !rgb(s.Color) || s.Blur < 0 {
			return fmt.Errorf("invalid shape shadow")
		}
		for _, v := range []float64{s.X, s.Y, s.Blur} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e6 {
				return fmt.Errorf("invalid shape shadow geometry")
			}
		}
	}
	return nil
}

func gradientFillXML(o Object) string {
	g := o.Gradient
	angle := 0
	if g.Direction == "vertical" {
		angle = 90 * 60000
	}
	if g.Direction == "diagonal" {
		angle = 45 * 60000
	}
	color := func(value string) string {
		alpha := ""
		if o.OpacitySet || o.Opacity != 0 {
			alpha = fmt.Sprintf(`<a:alpha val="%d"/>`, int64(math.Round(o.Opacity*100000)))
		}
		return `<a:srgbClr val="` + strings.TrimPrefix(strings.ToUpper(value), "#") + `">` + alpha + `</a:srgbClr>`
	}
	return fmt.Sprintf(`<a:gradFill rotWithShape="1"><a:gsLst><a:gs pos="0">%s</a:gs><a:gs pos="100000">%s</a:gs></a:gsLst><a:lin ang="%d" scaled="1"/></a:gradFill>`, color(g.Start), color(g.End), angle)
}

func shadowEffectXML(o Object) string {
	if o.Shadow == nil {
		return ""
	}
	s := o.Shadow
	angle := math.Atan2(s.Y, s.X) * 180 / math.Pi
	if angle < 0 {
		angle += 360
	}
	alpha := 1.0
	if o.OpacitySet || o.Opacity != 0 {
		alpha = o.Opacity
	}
	if s.Opacity != nil {
		alpha = *s.Opacity
	}
	return fmt.Sprintf(`<a:effectLst><a:outerShdw blurRad="%d" dist="%d" dir="%d" algn="ctr" rotWithShape="1"><a:srgbClr val="%s"><a:alpha val="%d"/></a:srgbClr></a:outerShdw></a:effectLst>`, emu(s.Blur), emu(math.Hypot(s.X, s.Y)), int64(math.Round(angle*60000))%21600000, strings.TrimPrefix(strings.ToUpper(s.Color), "#"), int64(math.Round(alpha*100000)))
}
