package app

import (
	"image"
	"math"
)

func imageFit(value string) string {
	switch value {
	case "contain", "cover", "stretch":
		return value
	}
	return ""
}

// Sample the cropped source into a fixed frame, retaining transparent margins
// for contain. Masking and colour processing happen after this projection.
func sampleFittedImage(img image.Image, source image.Rectangle, width, height int, fit string, lanczos bool) []rgba8 {
	return sampleFittedImageAspect(img, source, width, height, float64(width)/float64(height), fit, lanczos)
}

func sampleFittedImageAspect(img image.Image, source image.Rectangle, width, height int, aspect float64, fit string, lanczos bool) []rgba8 {
	drawW, drawH := width, height
	sourceAspect := float64(source.Dx()) / float64(source.Dy())
	if fit == "contain" {
		drawW = max(1, min(width, int(math.Round(float64(width)*sourceAspect/aspect))))
		drawH = max(1, min(height, int(math.Round(float64(height)*aspect/sourceAspect))))
	} else if fit == "cover" {
		w := max(1, min(source.Dx(), int(math.Round(float64(source.Dy())*aspect))))
		h := max(1, min(source.Dy(), int(math.Round(float64(source.Dx())/aspect))))
		x, y := source.Min.X+(source.Dx()-w)/2, source.Min.Y+(source.Dy()-h)/2
		source = image.Rect(x, y, x+w, y+h)
	}
	var pixels []rgba8
	if lanczos {
		pixels = resizeLanczosRGBA(img, source, drawW, drawH)
	} else {
		pixels = resizeNearestRGBA(img, source, drawW, drawH)
	}
	if drawW == width && drawH == height {
		return pixels
	}
	frame := make([]rgba8, width*height)
	x, y := (width-drawW)/2, (height-drawH)/2
	for row := 0; row < drawH; row++ {
		copy(frame[(row+y)*width+x:(row+y)*width+x+drawW], pixels[row*drawW:(row+1)*drawW])
	}
	return frame
}
