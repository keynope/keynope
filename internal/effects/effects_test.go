package effects

import (
	"encoding/base64"
	"encoding/json"
	"image/png"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestGaussianBoxKernels(t *testing.T) {
	for _, test := range []struct {
		sigma float64
		want  []int
	}{{0, nil}, {.05, nil}, {.5, []int{0, 0, 0}}, {1, []int{0, 0, 1}}, {2, []int{1, 1, 2}}, {4, []int{3, 3, 4}}, {24, []int{23, 23, 24}}} {
		if got := BoxRadii(test.sigma); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("sigma %v: %v != %v", test.sigma, got, test.want)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), -1} {
		if len(BoxRadii(v)) != 0 {
			t.Fatal("unbounded radius")
		}
	}
}
func TestPosterizeThresholds(t *testing.T) {
	for levels := 2; levels <= 32; levels++ {
		table := PosterizeTable(levels)
		for i := 0; i <= 65536; i++ {
			v := float64(i) / 65536
			got := table[min(len(table)-1, int(v*float64(len(table))))]
			want := math.Round(v*float64(levels-1)) / float64(levels-1)
			if got != want {
				t.Fatalf("%d levels at %v: %v != %v", levels, v, got, want)
			}
		}
	}
}
func TestGradientGeometryAndRaster(t *testing.T) {
	for _, style := range []string{"radial", "diamond", "reflected", "angle"} {
		data := GradientPNG(style, "#000000", "#ffffff", 200, 100)
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(data, "data:image/png;base64,"))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 100 {
			t.Fatal("aspect changed")
		}
	}
	if GradientT("radial", 200, 100, 100, 50) != 0 || GradientT("diamond", 200, 100, 150, 100) != 1 || GradientT("reflected", 200, 100, 100.5, 50) != 0 {
		t.Fatal("geometry differs from PhotoCraft")
	}
	if GradientPNG("radial", "#fff", "#fff", math.Inf(1), 2) != "" {
		t.Fatal("accepted hostile dimensions")
	}
}
func TestRecipes(t *testing.T) {
	if Compile(Settings{}, 200, 100) != nil {
		t.Fatal("empty effects allocated a filter")
	}
	f := Compile(Settings{Blur: 4, Axes: "horizontal", Posterize: 6, GlowRadius: 10, GlowThreshold: 60, GlowIntensity: 100, GlowColour: "#00ffff"}, 200, 100)
	convolutions := 0
	for _, p := range f.Primitives {
		if p.Tag == "feGaussianBlur" {
			t.Fatal("not the Rust algorithm")
		}
		if p.Tag == "feConvolveMatrix" {
			convolutions++
		}
	}
	if convolutions != 9 || f.Padding != 23 {
		t.Fatal("wrong pass count or padding", convolutions, f.Padding)
	}
	if len(f.Primitives) > 32 {
		t.Fatal("recipe exceeds renderer admission")
	}
}

// Optional local fixture export used by the headless browser parity harness.
// Normal test runs neither write files nor require Rust, browsers or reference repos.
func TestExportBrowserFixtures(t *testing.T) {
	path := os.Getenv("KEYNOPE_EFFECT_FIXTURES")
	if path == "" {
		t.Skip("local browser fixture output not requested")
	}
	settings := map[string]Settings{
		"blur": {Blur: 4, Axes: "both"}, "horizontal": {Blur: 4, Axes: "horizontal"},
		"posterize": {Posterize: 6}, "invert": {Colour: "invert"},
		"duotone": {Colour: "duotone", Shadows: "#172554", Highlights: "#fff1b8"},
		"tritone": {Colour: "tritone", Shadows: "#172554", Midtones: "#db2777", Highlights: "#fff1b8"},
		"glow":    {GlowRadius: 10, GlowThreshold: 60, GlowIntensity: 100, GlowColour: "#55aaff"},
	}
	filters := map[string]*Filter{}
	for key, s := range settings {
		filters[key] = Compile(s, 160, 100)
	}
	gradients := map[string]string{}
	for _, style := range []string{"radial", "diamond", "reflected", "angle"} {
		gradients[style] = GradientPNG(style, "#ff0055", "#55ffff", 160, 100)
	}
	data, err := json.Marshal(map[string]interface{}{"filters": filters, "gradients": gradients, "dips": map[string][]DipSample{"dip-black": DipCurve(false), "dip-white": DipCurve(true)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
