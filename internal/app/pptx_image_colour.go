package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"

	"keynope/internal/pptx"
)

// Bake colour and spatial passes into an export-owned copy. Geometry, source crop,
// destination fitting and opacity remain independent native picture settings.
// Each pass clamps separately, matching the scene's ordered sRGB filters.
func bakePPTXImageColour(object *pptx.Object, media *sceneMedia, options ...*pptxExportWork) error {
	work := firstPPTXWork(options)
	if media == nil || len(media.ColourMatrices) == 0 && !pptxImageHasSharpness(media) {
		return nil
	}
	for _, matrix := range media.ColourMatrices {
		if len(matrix) != 20 {
			return fmt.Errorf("invalid image colour matrix")
		}
		for _, v := range matrix {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("non-finite image colour matrix")
			}
		}
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(object.Image))
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16_000_000 {
		return fmt.Errorf("image exceeds colour conversion pixel limit")
	}
	if err := work.pixels(float64(config.Width), float64(config.Height), 1); err != nil {
		return err
	}
	source, _, err := image.Decode(bytes.NewReader(object.Image))
	if err != nil {
		return err
	}
	bounds := source.Bounds()
	output := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	applyMatrices := func(channels [4]float64, matrices [][]float64) [4]float64 {
		for _, matrix := range matrices {
			var next [4]float64
			for row := 0; row < 4; row++ {
				v := matrix[row*5+4]
				for column := 0; column < 4; column++ {
					v += matrix[row*5+column] * channels[column]
				}
				next[row] = math.Max(0, math.Min(1, v))
			}
			channels = next
		}
		return channels
	}
	sharp := pptxImageHasSharpness(media)
	split := len(media.ColourMatrices)
	if sharp {
		split = max(0, min(split, media.SharpnessAfter))
	}
	pixelAt := func(x, y int) [4]float64 {
		pixel := color.NRGBAModel.Convert(source.At(bounds.Min.X+max(0, min(bounds.Dx()-1, x)), bounds.Min.Y+max(0, min(bounds.Dy()-1, y)))).(color.NRGBA)
		return applyMatrices([4]float64{float64(pixel.R) / 255, float64(pixel.G) / 255, float64(pixel.B) / 255, float64(pixel.A) / 255}, media.ColourMatrices[:split])
	}
	if sharp {
		pixelAt = cachedPPTXPixelRows(bounds.Dx(), bounds.Dy(), pixelAt)
	}
	for y := 0; y < bounds.Dy(); y++ {
		if err := work.check(); err != nil {
			return err
		}
		for x := 0; x < bounds.Dx(); x++ {
			channels := pixelAt(x, y)
			if sharp {
				var sum [3]float64
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						p := pixelAt(x+dx, y+dy)
						for c := range sum {
							sum[c] += p[c]
						}
					}
				}
				for c := range sum {
					channels[c] = math.Max(0, math.Min(1, media.Sharpness*channels[c]+(1-media.Sharpness)*sum[c]/9))
				}
			}
			channels = applyMatrices(channels, media.ColourMatrices[split:])
			output.SetNRGBA(x, y, color.NRGBA{uint8(math.Round(channels[0] * 255)), uint8(math.Round(channels[1] * 255)), uint8(math.Round(channels[2] * 255)), uint8(math.Round(channels[3] * 255))})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&pptxExportWriter{target: &encoded, work: work, left: 64 << 20}, output); err != nil {
		return err
	}
	object.Image, object.ImageMIME, object.ImageSVG = encoded.Bytes(), "image/png", nil
	return nil
}

// A three-row window bounds both retained memory and colour work during a
// 3x3 convolution. Very wide sources use the uncached path rather than exceed
// the temporary memory budget. Returned pixels are values, never shared slices.
func cachedPPTXPixelRows(width, height int, source func(int, int) [4]float64) func(int, int) [4]float64 {
	rows := min(3, height)
	const budget = 8 << 20
	if width <= 0 || rows <= 0 || width > budget/(rows*32) {
		return source
	}
	data := make([][][4]float64, rows)
	indices := make([]int, rows)
	for i := range data {
		data[i] = make([][4]float64, width)
		indices[i] = -1
	}
	return func(x, y int) [4]float64 {
		x = max(0, min(width-1, x))
		y = max(0, min(height-1, y))
		slot := y % rows
		if indices[slot] != y {
			for column := 0; column < width; column++ {
				data[slot][column] = source(column, y)
			}
			indices[slot] = y
		}
		return data[slot][x]
	}
}

func pptxImageHasSharpness(media *sceneMedia) bool {
	return media != nil && media.Sharpness >= .2 && media.Sharpness <= 2 && media.Sharpness != 1
}

// Per-export only: no stale data across documents and a bounded retained budget.
type pptxImageColourCache struct {
	images map[[32]byte][]byte
	bytes  int
}

func exportPPTXImageColour(object *pptx.Object, media *sceneMedia, slide int, cache *pptxImageColourCache, work ...*pptxExportWork) []pptx.Warning {
	if media == nil || len(media.ColourMatrices) == 0 && !pptxImageHasSharpness(media) {
		return nil
	}
	matrixBytes, err := json.Marshal(struct {
		Matrices  [][]float64
		Sharpness float64
		After     int
	}{media.ColourMatrices, media.Sharpness, media.SharpnessAfter})
	if err != nil {
		return []pptx.Warning{{Slide: slide, Object: object.ID, Code: "image-colour-unavailable", Message: "Image adjustments contain non-finite values; original source retained."}}
	}
	hash := sha256.New()
	sourceHash := sha256.Sum256(object.Image)
	hash.Write(sourceHash[:])
	hash.Write(matrixBytes)
	var key [32]byte
	copy(key[:], hash.Sum(nil))
	var cached []byte
	if cache != nil {
		cached = cache.images[key]
	}
	if cached != nil {
		object.Image, object.ImageMIME, object.ImageSVG = cached, "image/png", nil
	} else if err := bakePPTXImageColour(object, media, firstPPTXWork(work)); err != nil {
		return []pptx.Warning{{Slide: slide, Object: object.ID, Code: "image-colour-unavailable", Message: "Image colour adjustments could not be rendered; original source retained: " + err.Error()}}
	} else if cache != nil && cache.bytes+len(object.Image) <= 64<<20 {
		if cache.images == nil {
			cache.images = make(map[[32]byte][]byte)
		}
		cache.images[key] = object.Image
		cache.bytes += len(object.Image)
	}
	return []pptx.Warning{{Slide: slide, Object: object.ID, Code: "image-colour-baked", Message: "Image colour and sharpening adjustments were rendered into the exported image; crop, mask and opacity remain editable."}}
}
