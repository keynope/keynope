package app

import (
	"bytes"
	"container/list"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"unicode/utf8"

	"keynope/internal/pptx"
)

// Match the shared canvas's font-independent quadrant painter. Font-based
// glyphs must take a different capture path, never a substituted source image.
func pptxRetroBlockMask(r rune) (int, bool) {
	const glyphs = " ▘▝▀▖▌▞▛▗▚▐▜▄▙▟█"
	for mask, glyph := range []rune(glyphs) {
		if r == glyph {
			return mask, true
		}
	}
	if r == '\u00a0' {
		return 0, true
	}
	return 0, false
}

// Match backgroundObjectFor in the shared renderer: framing, mask, image
// treatment and effects have already been sampled into slide-coordinate rows.
// Do not apply any source-media transformation again to this captured artwork.
func pptxRetroBackgroundObject(background *sceneBackgroundMedia, index int, page ...sceneRect) sceneObject {
	paint := scenePaint{Opacity: &background.Opacity}
	if background.Paint != nil {
		paint = *background.Paint
	}
	bounds := sceneRect{Width: 1920, Height: 1080}
	if len(page) > 0 {
		bounds = sceneRect{Width: page[0].Width, Height: page[0].Height}
	}
	return sceneObject{ID: fmt.Sprintf("keynope-background-image-%d", index), Name: "Slide background", Kind: "image", Bounds: bounds, Paint: paint, RetroLines: background.RetroLines}
}

func rasterPPTXRetroImage(object sceneObject, options ...*pptxExportWork) (*pptx.Object, []pptx.Warning, error) {
	work := firstPPTXWork(options)
	picture, warnings, err := rasterPPTXRetroImageFrame(object, work)
	if err != nil || len(object.RetroLines.Frames) < 2 {
		return picture, warnings, err
	}
	data, losses, err := encodePPTXRetroAnimation(object, int(picture.Width), int(picture.Height), work)
	if err != nil {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "retro-image-animation-frozen", Message: "Rendered Retro animation could not be embedded within GIF limits; the first rendered frame was retained: " + err.Error()})
		return picture, warnings, nil
	}
	picture.Image, picture.ImageMIME = data, "image/gif"
	warnings[0].Message = "Rendered Retro blocks were embedded as image frames; sampled cells are not editable PowerPoint text."
	warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "retro-image-animation-embedded", Message: "Rendered Retro frames, delays and looping were embedded as GIF. Playback depends on the presentation reader."})
	for _, loss := range losses {
		loss.Object = object.ID
		warnings = append(warnings, loss)
	}
	return picture, warnings, nil
}

// Replay the same insertion-ordered row map used by the shared renderer.
// Full snapshots replace it, clears remove rows, and updating an existing key
// preserves its paint order. Bound work before rasterizing any extra frame.
func encodePPTXRetroAnimation(object sceneObject, width, height int, options ...*pptxExportWork) ([]byte, []pptx.Warning, error) {
	workState := firstPPTXWork(options)
	r := object.RetroLines
	if len(r.Frames) > 2048 || int64(width)*int64(height)*int64(len(r.Frames)) > 32_000_000 {
		return nil, nil, fmt.Errorf("rendered animation exceeds frame/pixel budget")
	}
	if r.LoopCount < -1 || r.LoopCount > 65535 {
		return nil, nil, fmt.Errorf("loop count cannot be represented")
	}
	media := &sceneMedia{Width: width, Height: height, LoopCount: r.LoopCount}
	var order list.List
	rows := map[string]*list.Element{}
	key := func(line exportLine) string {
		col := line.Col
		if len(line.Parts) > 0 {
			col = line.Parts[0].Col
		}
		return fmt.Sprintf("%d:%d:%d:%s", line.Row, col, line.Element, line.Role)
	}
	set := func(line exportLine) {
		k := key(line)
		if entry := rows[k]; entry != nil {
			entry.Value = line
		} else {
			rows[k] = order.PushBack(line)
		}
	}
	var work, encodedBytes int64
	for _, frame := range r.Frames {
		if err := workState.check(); err != nil {
			return nil, nil, err
		}
		if frame.DelayMS < 0 || frame.DelayMS > 655350 || frame.DelayMS > 0 && frame.DelayMS < 10 {
			return nil, nil, fmt.Errorf("frame delay cannot be represented")
		}
		work += int64(len(frame.Clear) + len(frame.Update) + len(frame.Lines))
		if work > 8_000_000 {
			return nil, nil, fmt.Errorf("rendered animation exceeds row-work budget")
		}
		if frame.Full {
			order.Init()
			rows = map[string]*list.Element{}
			for _, line := range frame.Lines {
				set(line)
			}
		} else {
			for _, k := range frame.Clear {
				if entry := rows[k]; entry != nil {
					order.Remove(entry)
					delete(rows, k)
				}
			}
			for _, line := range frame.Update {
				set(line)
			}
		}
		if order.Len() > 65536 {
			return nil, nil, fmt.Errorf("rendered animation exceeds row budget")
		}
		lines := make([]exportLine, 0, order.Len())
		for entry := order.Front(); entry != nil; entry = entry.Next() {
			line := entry.Value.(exportLine)
			for _, part := range line.Parts {
				work += int64(utf8.RuneCountInString(part.Text))
			}
			lines = append(lines, line)
		}
		if work > 8_000_000 {
			return nil, nil, fmt.Errorf("rendered animation exceeds glyph-work budget")
		}
		copy := object
		retro := *r
		retro.Lines, retro.Frames = lines, nil
		copy.RetroLines = &retro
		picture, _, err := rasterPPTXRetroImageFrame(copy, workState)
		if err != nil {
			return nil, nil, err
		}
		source := "data:image/png;base64," + base64.StdEncoding.EncodeToString(picture.Image)
		encodedBytes += int64(len(source))
		if encodedBytes > 96<<20 {
			return nil, nil, fmt.Errorf("rendered animation exceeds encoded frame budget")
		}
		delay := frame.DelayMS
		if delay == 0 {
			delay = 100
		}
		media.Frames = append(media.Frames, sceneMediaFrame{Source: source, DelayMS: delay})
	}
	return encodePPTXAnimation(media, workState)
}

func rasterPPTXRetroImageFrame(object sceneObject, options ...*pptxExportWork) (*pptx.Object, []pptx.Warning, error) {
	work := firstPPTXWork(options)
	retro := object.RetroLines
	if object.Kind != "image" || retro == nil || retro.Cols <= 0 || retro.Rows <= 0 || retro.Cols > 4096 || retro.Rows > 4096 {
		return nil, nil, fmt.Errorf("unsupported sampled image grid")
	}
	b := object.Bounds
	for _, v := range []float64{b.X, b.Y, b.Width, b.Height, object.Rotation} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, nil, fmt.Errorf("non-finite sampled image geometry")
		}
	}
	const padding = 64
	w, h := math.Ceil(b.Width+padding*2), math.Ceil(b.Height+padding*2)
	if b.Width <= 0 || b.Height <= 0 || w > 8192 || h > 8192 || w*h > 16_000_000 {
		return nil, nil, fmt.Errorf("sampled image exceeds raster pixel limit")
	}
	if err := work.pixels(w, h, 1); err != nil {
		return nil, nil, err
	}
	sx, sy, ox, oy := 1.0, 1.0, 0.0, 0.0
	if object.SampleScale != nil {
		sx, sy = object.SampleScale.X, object.SampleScale.Y
	}
	if object.SampleOffset != nil {
		ox, oy = object.SampleOffset.X, object.SampleOffset.Y
	}
	for _, v := range []float64{sx, sy, ox, oy} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1_000_000 {
			return nil, nil, fmt.Errorf("invalid sampled image transform")
		}
	}
	if sx <= 0 || sy <= 0 {
		return nil, nil, fmt.Errorf("invalid sampled image scale")
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, int(w), int(h)))
	canvasWidth, canvasHeight := 1920.0, 1080.0
	if retro.CanvasWidth != 0 || retro.CanvasHeight != 0 {
		canvasWidth, canvasHeight = retro.CanvasWidth, retro.CanvasHeight
		if math.IsNaN(canvasWidth+canvasHeight) || math.IsInf(canvasWidth+canvasHeight, 0) || canvasWidth <= 0 || canvasHeight <= 0 || canvasWidth > 7680 || canvasHeight > 7680 {
			return nil, nil, fmt.Errorf("invalid sampled image canvas")
		}
	}
	cw, ch := canvasWidth/float64(retro.Cols), canvasHeight/float64(retro.Rows)
	colours := map[string]color.NRGBA{}
	parseColour := func(css string) (color.NRGBA, error) {
		if css == "" {
			css = "#f3efe0"
		}
		if c, ok := colours[css]; ok {
			return c, nil
		}
		code := cssColourToFG(css, "")
		if code == "" {
			return color.NRGBA{}, fmt.Errorf("unsupported sampled image colour")
		}
		rgb, ok := parseRGBHex(sceneColor(code))
		if !ok {
			return color.NRGBA{}, fmt.Errorf("unsupported sampled image colour")
		}
		c := color.NRGBA{uint8(rgb.r), uint8(rgb.g), uint8(rgb.b), 255}
		if len(colours) < 1024 {
			colours[css] = c
		}
		return c, nil
	}
	opacity := func(value *float64) (float64, error) {
		if value == nil {
			return 1, nil
		}
		if math.IsNaN(*value) || math.IsInf(*value, 0) {
			return 0, fmt.Errorf("non-finite sampled image opacity")
		}
		return math.Max(0, math.Min(1, *value)), nil
	}
	content, err := opacity(retro.ContentOpacity)
	if err != nil {
		return nil, nil, err
	}
	shape, err := opacity(retro.ShapeOpacity)
	if err != nil {
		return nil, nil, err
	}
	backdrop, err := opacity(retro.BackdropOpacity)
	if err != nil {
		return nil, nil, err
	}
	// Canvas rounds after its local transform, including translation/padding.
	x := func(v float64) float64 { return math.Floor(padding + sx*(v*cw-b.X+ox) + .5) }
	y := func(v float64) float64 { return math.Floor(padding + sy*(v*ch-b.Y+oy) + .5) }
	paintRect := func(x0, y0, x1, y1 float64, c color.NRGBA, a float64) {
		x1 = math.Max(x0+1, x1)
		y1 = math.Max(y0+1, y1)
		if x0 >= w || y0 >= h || x1 <= 0 || y1 <= 0 || a <= 0 {
			return
		}
		r := image.Rect(int(math.Max(0, x0)), int(math.Max(0, y0)), int(math.Min(w, x1)), int(math.Min(h, y1)))
		c.A = uint8(math.Floor(a*255 + .5))
		draw.Draw(canvas, r, &image.Uniform{C: c}, image.Point{}, draw.Over)
	}
	glyphCount := 0
	for _, line := range retro.Lines {
		if err := work.check(); err != nil {
			return nil, nil, err
		}
		if line.TrueType != nil {
			return nil, nil, fmt.Errorf("sampled text needs a font-aware capture")
		}
		alpha := content
		if line.Role == "shape" {
			alpha = shape
		}
		if line.Role == "outline" {
			alpha = 1
		}
		for _, part := range line.Parts {
			foreground, err := parseColour(part.Color)
			if err != nil {
				return nil, nil, err
			}
			var background color.NRGBA
			if part.Background != "" {
				background, err = parseColour(part.Background)
				if err != nil {
					return nil, nil, err
				}
			}
			col := float64(part.Col)
			row := float64(line.Row)
			if math.Abs(col) > 1_000_000 || math.Abs(row) > 1_000_000 {
				return nil, nil, fmt.Errorf("invalid sampled image cell")
			}
			for _, glyph := range part.Text {
				glyphCount++
				if glyphCount > 1_000_000 {
					return nil, nil, fmt.Errorf("sampled image exceeds glyph limit")
				}
				mask, ok := pptxRetroBlockMask(glyph)
				if line.Role == "outline" {
					mask = 15
					ok = true
					if glyph == ' ' || glyph == '\u00a0' {
						mask = 0
					}
				}
				if !ok {
					return nil, nil, fmt.Errorf("sampled character %U needs a font-aware capture", glyph)
				}
				if part.Background != "" {
					a := alpha
					if retro.BackdropOpacity != nil {
						a = backdrop
					}
					paintRect(x(col), y(row), x(col+1), y(row+1), background, a)
				}
				if mask == 15 {
					paintRect(x(col), y(row), x(col+1), y(row+1), foreground, alpha)
				} else {
					for yy := 0; yy < 2; yy++ {
						for xx := 0; xx < 2; xx++ {
							if mask&(1<<(yy*2+xx)) != 0 {
								paintRect(x(col+float64(xx)/2), y(row+float64(yy)/2), x(col+float64(xx+1)/2), y(row+float64(yy+1)/2), foreground, alpha)
							}
						}
					}
				}
				col++
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&pptxExportWriter{target: &encoded, work: work, left: 64 << 20}, canvas); err != nil {
		return nil, nil, err
	}
	picture, err := pptxRetroPicture(object, encoded.Bytes(), "image/png", w, h)
	if err != nil {
		return nil, nil, err
	}
	warnings := []pptx.Warning{{Object: object.ID, Code: "retro-image-rasterized", Message: "Rendered Retro blocks were embedded as a PNG picture; the sampled cells are not editable PowerPoint text."}}
	return picture, warnings, nil
}

func pptxRetroPicture(object sceneObject, data []byte, mime string, w, h float64) (*pptx.Object, error) {
	b := object.Bounds
	const padding = 64
	// PNG storage rounds its dimensions up, while the authored frame may be
	// fractional. Compensate the picture centre so native rotation still pivots
	// around the original object centre, not the rounded padded bitmap centre.
	a := math.Mod(object.Rotation, 360) * math.Pi / 180
	dx, dy := (w-b.Width-padding*2)/2, (h-b.Height-padding*2)/2
	shiftX, shiftY := math.Cos(a)*dx-math.Sin(a)*dy-dx, math.Sin(a)*dx+math.Cos(a)*dy-dy
	picture := &pptx.Object{ID: object.ID, Name: object.Name, Kind: "image", X: b.X - padding + shiftX, Y: b.Y - padding + shiftY, Width: w, Height: h, Rotation: object.Rotation, Link: object.Link, Image: data, ImageMIME: mime}
	if object.Paint.Opacity != nil {
		a := *object.Paint.Opacity
		if math.IsNaN(a) || math.IsInf(a, 0) {
			return nil, fmt.Errorf("non-finite sampled image opacity")
		}
		a = math.Max(0, math.Min(1, a))
		picture.ImageOpacity = &a
	}
	return picture, nil
}
