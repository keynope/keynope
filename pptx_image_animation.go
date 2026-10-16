package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"math"

	"keynope/internal/pptx"
)

// Authoring frames are complete PNG canvases, not GIF delta rectangles. Encode
// them as full frames with background disposal so transparent areas cannot keep
// pixels from a previous frame. Never replace the source until encoding succeeds.
func exportPPTXAnimatedImage(object *pptx.Object, media *sceneMedia, slide int, work ...*pptxExportWork) (bool, []pptx.Warning) {
	if media == nil || len(media.Frames) < 2 {
		return false, nil
	}
	data, losses, err := encodePPTXAnimation(media, firstPPTXWork(work))
	if err != nil {
		return false, []pptx.Warning{{Slide: slide, Object: object.ID, Code: "image-animation-frozen", Message: "Animation could not be exported within GIF limits; first frame retained: " + err.Error()}}
	}
	object.Image, object.ImageMIME, object.ImageSVG = data, "image/gif", nil
	warnings := []pptx.Warning{{Slide: slide, Object: object.ID, Code: "image-animation-embedded", Message: "Animation frames and looping were embedded as GIF; crop, mask and opacity remain editable. Playback depends on the presentation reader."}}
	for _, loss := range losses {
		warnings = append(warnings, pptx.Warning{Slide: slide, Object: object.ID, Code: loss.Code, Message: loss.Message})
	}
	if len(media.ColourMatrices) > 0 || pptxImageHasSharpness(media) {
		warnings = append(warnings, pptx.Warning{Slide: slide, Object: object.ID, Code: "image-colour-baked", Message: "Image colour and sharpening adjustments were rendered into every animation frame; crop, mask and opacity remain editable."})
	}
	return true, warnings
}

func encodePPTXAnimation(media *sceneMedia, options ...*pptxExportWork) ([]byte, []pptx.Warning, error) {
	work := firstPPTXWork(options)
	if len(media.Frames) > 10000 || media.Width <= 0 || media.Height <= 0 || media.Width > 32768 || media.Height > 32768 || int64(media.Width)*int64(media.Height) > 16_000_000 || int64(media.Width)*int64(media.Height)*int64(len(media.Frames)) > 32_000_000 {
		return nil, nil, fmt.Errorf("animation exceeds frame/pixel budget")
	}
	if media.LoopCount < -1 || media.LoopCount > 65535 {
		return nil, nil, fmt.Errorf("loop count cannot be represented")
	}
	if err := work.pixels(float64(media.Width), float64(media.Height), len(media.Frames)); err != nil {
		return nil, nil, err
	}
	var encodedSourceBytes int64
	for _, frame := range media.Frames {
		encodedSourceBytes += int64(len(frame.Source))
		if len(frame.Source) > 64<<20 || encodedSourceBytes > 96<<20 {
			return nil, nil, fmt.Errorf("animation exceeds encoded source budget")
		}
	}
	animation := &gif.GIF{LoopCount: media.LoopCount, Config: image.Config{Width: media.Width, Height: media.Height}}
	quantized, partialAlpha, roundedTiming := false, false, false
	var elapsedMS, elapsedTicks int64
	for _, frame := range media.Frames {
		if err := work.check(); err != nil {
			return nil, nil, err
		}
		data, mime, ok := dataURI(frame.Source)
		if !ok || mime != "image/png" {
			return nil, nil, fmt.Errorf("frame is not an embedded PNG")
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != media.Width || config.Height != media.Height {
			return nil, nil, fmt.Errorf("frame dimensions do not match animation")
		}
		converted := pptx.Object{Image: data, ImageMIME: mime}
		if err := bakePPTXImageColour(&converted, media, work); err != nil {
			return nil, nil, err
		}
		pixels, _, err := image.Decode(bytes.NewReader(converted.Image))
		if err != nil {
			return nil, nil, err
		}
		paletted, loss, alpha := palettizePPTXFrame(pixels, work)
		if err := work.check(); err != nil {
			return nil, nil, err
		}
		quantized, partialAlpha = quantized || loss, partialAlpha || alpha
		if frame.DelayMS < 10 || frame.DelayMS > 655350 {
			return nil, nil, fmt.Errorf("frame delay cannot be represented")
		}
		elapsedMS += frame.DelayMS
		ticks := int64(math.Round(float64(elapsedMS) / 10))
		delay := ticks - elapsedTicks
		if delay < 1 || delay > 65535 {
			return nil, nil, fmt.Errorf("rounded frame delay cannot be represented")
		}
		elapsedTicks = ticks
		roundedTiming = roundedTiming || frame.DelayMS%10 != 0
		animation.Image = append(animation.Image, paletted)
		animation.Delay = append(animation.Delay, int(delay))
		animation.Disposal = append(animation.Disposal, gif.DisposalBackground)
	}
	// Every local palette reserves transparent index zero. Keep the logical
	// background consistent even when a later frame has a different colour set.
	animation.Config.ColorModel = animation.Image[0].Palette
	var out bytes.Buffer
	if err := gif.EncodeAll(&pptxExportWriter{target: &out, work: work, left: 64 << 20}, animation); err != nil {
		return nil, nil, err
	}
	if out.Len() > 64<<20 {
		return nil, nil, fmt.Errorf("encoded animation exceeds 64 MiB")
	}
	var losses []pptx.Warning
	if quantized {
		losses = append(losses, pptx.Warning{Code: "image-animation-colours-quantized", Message: "Animation uses a GIF palette; frames with more than 255 opaque colours were dithered."})
	}
	if partialAlpha {
		losses = append(losses, pptx.Warning{Code: "image-animation-alpha-approximated", Message: "GIF cannot retain partial pixel transparency; animation alpha was thresholded. Object opacity remains editable."})
	}
	if roundedTiming {
		losses = append(losses, pptx.Warning{Code: "image-animation-timing-rounded", Message: "Frame delays were rounded cumulatively to GIF's 10 ms units to avoid accumulated timing drift."})
	}
	return out.Bytes(), losses, nil
}

func palettizePPTXFrame(source image.Image, options ...*pptxExportWork) (*image.Paletted, bool, bool) {
	work := firstPPTXWork(options)
	bounds := source.Bounds()
	normalized := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	colors := color.Palette{color.Transparent}
	indices := map[color.NRGBA]uint8{}
	quantized, partialAlpha := false, false
	for y := 0; y < bounds.Dy(); y++ {
		if work.check() != nil {
			return nil, false, false
		}
		for x := 0; x < bounds.Dx(); x++ {
			p := color.NRGBAModel.Convert(source.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			partialAlpha = partialAlpha || p.A != 0 && p.A != 255
			if p.A < 128 {
				p = color.NRGBA{}
			} else {
				p.A = 255
			}
			normalized.SetNRGBA(x, y, p)
			if p.A == 0 || quantized {
				continue
			}
			if _, exists := indices[p]; !exists {
				if len(colors) == 256 {
					quantized = true
					continue
				}
				indices[p] = uint8(len(colors))
				colors = append(colors, p)
			}
		}
	}
	if quantized {
		colors = append(color.Palette{color.Transparent}, palette.Plan9[:255]...)
	}
	frame := image.NewPaletted(normalized.Bounds(), colors)
	if quantized {
		draw.FloydSteinberg.Draw(frame, frame.Bounds(), normalized, image.Point{})
	} else {
		for y := 0; y < bounds.Dy(); y++ {
			if work.check() != nil {
				return nil, false, false
			}
			for x := 0; x < bounds.Dx(); x++ {
				frame.SetColorIndex(x, y, indices[normalized.NRGBAAt(x, y)])
			}
		}
	}
	return frame, quantized, partialAlpha
}

func exportPPTXMediaTreatment(object *pptx.Object, media *sceneMedia, slide int, cache *pptxImageColourCache, options ...*pptxExportWork) []pptx.Warning {
	work := firstPPTXWork(options)
	animated, warnings := exportPPTXAnimatedImage(object, media, slide, work)
	if animated {
		return warnings
	}
	return append(warnings, exportPPTXImageColour(object, media, slide, cache, work)...)
}
