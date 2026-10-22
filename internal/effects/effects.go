// Package effects ports selected EffectCraft and PhotoCraft rendering algorithms
// to Go. SVG is an execution format, not a second implementation of the maths.
// See assets/licenses/CRAFT-EFFECTS-LICENSE.txt for upstream paths and licences.
package effects

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"
)

// Primitive is a deliberately small, typed SVG instruction. No script, CSS,
// external resources, IDs or arbitrary markup can be supplied by a document.
type Primitive struct {
	Tag      string            `json:"tag"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Children []Primitive       `json:"children,omitempty"`
}
type Filter struct {
	Padding    float64     `json:"padding"`
	Primitives []Primitive `json:"primitives"`
}
type Settings struct {
	Blur                                     float64
	Axes                                     string
	Posterize                                int
	Colour                                   string
	Shadows, Midtones, Highlights            string
	GlowRadius, GlowThreshold, GlowIntensity float64
	GlowColour                               string
	Gradient, GradientStart, GradientEnd     string
}

func number(n float64) string { return strconv.FormatFloat(n, 'g', -1, 64) }
func primitive(tag string, attrs ...string) Primitive {
	p := Primitive{Tag: tag, Attrs: map[string]string{}}
	for i := 0; i+1 < len(attrs); i += 2 {
		p.Attrs[attrs[i]] = attrs[i+1]
	}
	return p
}

// BoxRadii is EffectCraft raster/blur.rs::box_radii, including its rounding.
// Its Gaussian is three box passes per axis, NOT an SVG Gaussian blur.
func BoxRadii(sigma float64) []int {
	if math.IsNaN(sigma) || math.IsInf(sigma, 0) || sigma <= .05 {
		return nil
	}
	sigma = math.Min(sigma, 48)
	wl := int(math.Floor(math.Sqrt(4*sigma*sigma + 1)))
	if wl%2 == 0 {
		wl--
	}
	m := int(math.Round((12*sigma*sigma - float64(3*wl*wl+12*wl+9)) / float64(-4*wl-4)))
	out := make([]int, 3)
	for i := range out {
		width := wl + 2
		if i < m {
			width = wl
		}
		out[i] = max(0, (width-1)/2)
	}
	return out
}

// PosterizeTable encodes Rust round(clamp(v)*levels)/levels exactly as a
// discrete transfer function; evenly spaced N-entry tables have wrong edges.
func PosterizeTable(levels int) []float64 {
	levels = max(2, min(255, levels)) - 1
	values := make([]float64, 2*levels)
	for i := range values {
		values[i] = math.Round(float64(i)/2) / float64(levels)
	}
	return values
}
func list(values []float64) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = number(v)
	}
	return strings.Join(parts, " ")
}
func encodedColour(value string) [3]float64 {
	var r, g, b uint8
	if _, err := fmt.Sscanf(value, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return [3]float64{1, 1, 1}
	}
	return [3]float64{float64(r) / 255, float64(g) / 255, float64(b) / 255}
}

// Compile emits the exact separable kernels and sRGB colour operations
// used by the Rust CPU renderer. The bounded recipe is cached with the scene.
func Compile(s Settings, width, height float64) *Filter {
	f := &Filter{}
	add := func(p Primitive) { f.Primitives = append(f.Primitives, p) }
	if s.Gradient != "" {
		if source := GradientPNG(s.Gradient, s.GradientStart, s.GradientEnd, width, height); source != "" {
			add(primitive("feImage", "href", source, "x", "0", "y", "0", "width", number(width), "height", number(height), "preserveAspectRatio", "none", "result", "gradient"))
			add(primitive("feComposite", "in", "gradient", "in2", "SourceAlpha", "operator", "in"))
		}
	}
	switch s.Colour {
	case "invert":
		add(primitive("feColorMatrix", "type", "matrix", "values", "-1 0 0 0 1 0 -1 0 0 1 0 0 -1 0 1 0 0 0 1 0"))
	case "duotone", "tritone":
		// EffectCraft colour_fx.rs tint/tritone use Rec.709 weights on its
		// encoded-sRGB raster values (not FilmCraft's linear-light buffer).
		add(primitive("feColorMatrix", "type", "matrix", "values", ".2126 .7152 .0722 0 0 .2126 .7152 .0722 0 0 .2126 .7152 .0722 0 0 0 0 0 1 0"))
		lo, mid, hi := encodedColour(s.Shadows), encodedColour(s.Midtones), encodedColour(s.Highlights)
		p := primitive("feComponentTransfer")
		for i, channel := range []string{"R", "G", "B"} {
			v := []float64{lo[i], hi[i]}
			if s.Colour == "tritone" {
				v = []float64{lo[i], mid[i], hi[i]}
			}
			p.Children = append(p.Children, primitive("feFunc"+channel, "type", "table", "tableValues", list(v)))
		}
		add(p)
	}
	if s.Posterize >= 2 {
		p := primitive("feComponentTransfer")
		v := list(PosterizeTable(s.Posterize))
		for _, channel := range []string{"R", "G", "B"} {
			p.Children = append(p.Children, primitive("feFunc"+channel, "type", "discrete", "tableValues", v))
		}
		add(p)
	}
	blur := func(sigma float64, axes string) {
		for _, vertical := range []bool{false, true} {
			if vertical && axes == "horizontal" || !vertical && axes == "vertical" {
				continue
			}
			for _, radius := range BoxRadii(sigma) {
				if radius == 0 {
					continue
				}
				size := radius*2 + 1
				order := fmt.Sprintf("%d 1", size)
				if vertical {
					order = fmt.Sprintf("1 %d", size)
				}
				add(primitive("feConvolveMatrix", "order", order, "kernelMatrix", strings.TrimSpace(strings.Repeat("1 ", size)), "divisor", strconv.Itoa(size), "edgeMode", "none", "preserveAlpha", "false"))
			}
		}
	}
	if s.Blur > 0 {
		// EffectCraft misc.rs::blur_sigma: Blurriness is twice sigma.
		blur(s.Blur/2, s.Axes)
		for _, r := range BoxRadii(s.Blur / 2) {
			f.Padding += float64(r)
		}
	}
	if s.GlowRadius > 0 && s.GlowIntensity > 0 {
		// Precisely the alpha-based, single-colour, Behind variant of
		// EffectCraft misc.rs::glow. Bright pass: clamp((a-t)/(1-t))*a.
		add(primitive("feColorMatrix", "type", "matrix", "values", "1 0 0 0 0 0 1 0 0 0 0 0 1 0 0 0 0 0 1 0", "result", "original"))
		threshold := s.GlowThreshold / 100
		denom := math.Max(.001, 1-threshold)
		p := primitive("feComponentTransfer", "in", "original", "result", "bright-threshold")
		p.Children = []Primitive{primitive("feFuncA", "type", "linear", "slope", number(1/denom), "intercept", number(-threshold/denom))}
		add(p)
		add(primitive("feComposite", "in", "bright-threshold", "in2", "original", "operator", "in"))
		blur(math.Max(.5, s.GlowRadius/2), "both")
		add(primitive("feColorMatrix", "type", "matrix", "values", "0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 "+number(s.GlowIntensity/100)+" 0", "result", "glow-alpha"))
		add(primitive("feFlood", "flood-color", s.GlowColour, "result", "glow-colour"))
		add(primitive("feComposite", "in", "glow-colour", "in2", "glow-alpha", "operator", "in", "result", "glow"))
		add(primitive("feComposite", "in", "original", "in2", "glow", "operator", "over"))
		f.Padding += math.Ceil(s.GlowRadius*1.5) + 2
	}
	if len(f.Primitives) == 0 {
		return nil
	}
	f.Padding += 2
	return f
}

// GradientT is PhotoCraft compose/effects.rs::gradient_t (100% scale, centred,
// angle 0). Using its frame-dependent lengths avoids stretched CSS gradients.
func GradientT(style string, w, h, x, y float64) float64 {
	w, h = math.Max(w, 1), math.Max(h, 1)
	dx, dy := x-w/2, y-h/2
	var t float64
	switch style {
	case "reflected":
		t = math.Abs((dx - .5) / (w / 2))
	case "radial":
		t = math.Hypot(dx, dy) / (w / 2)
	case "diamond":
		t = (math.Abs(dx) + math.Abs(dy)) / (w / 2)
	case "angle":
		t = math.Mod((math.Atan2(dy, dx)/(2*math.Pi))+1, 1)
	default:
		t = (dx-.5)/w + .5
	}
	return math.Max(0, math.Min(1, t))
}

func GradientPNG(style, start, end string, width, height float64) string {
	if width <= 0 || height <= 0 || math.IsInf(width+height, 0) || math.IsNaN(width+height) {
		return ""
	}
	return cachedGradient(gradientKey{style, start, end, width, height}, func() string { return renderGradientPNG(style, start, end, width, height) })
}
func renderGradientPNG(style, start, end string, width, height float64) string {
	// Paint at physical pixel centres, bounded to one megapixel. Long gradients
	// retain aspect ratio and span; the renderer scales the immutable ramp only.
	scale := math.Min(1, 1024/math.Max(width, height))
	w, h := max(1, int(math.Ceil(width*scale))), max(1, int(math.Ceil(height*scale)))
	// PhotoCraft's Color::to_rgb returns encoded sRGB, unlike EffectCraft's
	// linear-premultiplied raster pipeline. Keep these colour spaces separate.
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	a, b := encodedColour(start), encodedColour(end)
	encode := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255)) }
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := GradientT(style, width, height, (float64(x)+.5)*width/float64(w), (float64(y)+.5)*height/float64(h))
			img.SetNRGBA(x, y, color.NRGBA{encode(a[0] + (b[0]-a[0])*t), encode(a[1] + (b[1]-a[1])*t), encode(a[2] + (b[2]-a[2])*t), 255})
		}
	}
	var data bytes.Buffer
	if png.Encode(&data, img) != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
}
