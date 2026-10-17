package app

import (
	"keynope/internal/pptx"
	"math"
	"net/url"
	"strconv"
)

func applyPPTXPicturePaint(object *pptx.Object, paint scenePaint) []pptx.Warning {
	if paint.ShadowColor != "" {
		// Picture alpha is applied inside the blip, unlike shape fill alpha.
		// Carry it explicitly onto the shadow, not onto the source a second time.
		object.Shadow = &pptx.ShapeShadow{Color: paint.ShadowColor, X: paint.ShadowX, Y: paint.ShadowY, Blur: paint.ShadowBlur}
		if paint.Opacity != nil {
			alpha := *paint.Opacity
			object.Shadow.Opacity = &alpha
		}
	}
	if paint.GradientStart != "" || paint.Stroke != "" {
		return []pptx.Warning{{Object: object.ID, Code: "image-paint-approximated", Message: "Image remains editable with its crop, mask, opacity and shadow; alpha-following gradient and outline treatments were omitted."}}
	}
	return nil
}

// Picture shadows stay independent of source pixels and survive crop/mask edits.
// Keynope currently has discrete offsets and two blur modes; report any loss.
func importPPTXPictureShadow(object pptx.Object, query url.Values, scale float64, canvas ...sceneRect) []pptx.Warning {
	shadow := object.Shadow
	if shadow == nil {
		return nil
	}
	width, height := pptxCanvasDimensions(canvas)
	sx, sy := width/245.0, height/56.0
	x := clampInt(int(math.Round(shadow.X*scale/sx)), -4, 4)
	y := clampInt(int(math.Round(shadow.Y*scale/sy)), -4, 4)
	mode, blur := "solid", 0.0
	if shadow.Blur > 0 {
		mode, blur = "soft", 8
	}
	if x == 0 && y == 0 {
		x, y = 1, 1
	}
	query.Set("shadow", mode)
	query.Set("shadow-color", shadow.Color)
	query.Set("shadow-x", strconv.Itoa(x))
	query.Set("shadow-y", strconv.Itoa(y))
	alpha := 1.0
	if object.ImageOpacity != nil {
		alpha = *object.ImageOpacity
	}
	if math.Abs(float64(x)*sx-shadow.X*scale) > .001 || math.Abs(float64(y)*sy-shadow.Y*scale) > .001 || math.Abs(blur-shadow.Blur*scale) > .001 || shadow.Opacity != nil && math.Abs(*shadow.Opacity-alpha) > 1e-6 {
		return []pptx.Warning{{Object: object.ID, Code: "image-shadow-approximated", Message: "Image shadow colour is retained; offset, blur and opacity use the closest supported Keynope settings."}}
	}
	return nil
}
