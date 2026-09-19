package main

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestSceneImageColourMatrices(t *testing.T) {
	queries := []string{"", "brightness=1.3&contrast=1.6&saturation=0.4", "tint=%23ff8040", "brightness=1.8&contrast=2&saturation=1.7&tint=%2355aaff"}
	for _, query := range queries {
		matrices := sceneImageColourMatrices(query)
		opts := parseImageASCIIOptions(query)
		for _, pixel := range []rgba8{{0, 0, 0, 255}, {255, 255, 255, 128}, {128, 64, 192, 0}, {23, 217, 79, 255}} {
			values := []float64{float64(pixel.r) / 255, float64(pixel.g) / 255, float64(pixel.b) / 255, float64(pixel.a) / 255}
			for _, matrix := range matrices {
				next := make([]float64, 4)
				for row := 0; row < 4; row++ {
					next[row] = matrix[row*5+4]
					for col := 0; col < 4; col++ {
						next[row] += matrix[row*5+col] * values[col]
					}
					next[row] = clampFloat(next[row], 0, 1)
				}
				values = next
			}
			r, g, b := adjustRGB(pixel.r, pixel.g, pixel.b, opts.brightness, opts.contrast, opts.saturation)
			want := rgba8{r, g, b, pixel.a}
			if opts.tint != nil {
				want = monochromeImageColour(want, *opts.tint)
			}
			for i, v := range []uint8{want.r, want.g, want.b, want.a} {
				if math.Abs(values[i]*255-float64(v)) > 1.5 {
					t.Fatalf("%s: matrix differs from Retro adjustment at channel %d", query, i)
				}
			}
		}
	}
	if len(sceneImageColourMatrices("")) != 0 {
		t.Fatal("default image should not incur filter work")
	}
	if path := os.Getenv("KEYNOPE_IMAGE_MATRIX_FIXTURE"); path != "" {
		data, _ := json.Marshal(sceneImageColourMatrices("tint=%23ff0000"))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSceneImageAdjustmentsPreserveSources(t *testing.T) {
	deck := sceneFixture(t)
	index := len(deck.Slides[0].Elements) - 1
	deck.Slides[0].Elements[index].Query += "&tint=%2355aaff&brightness=1.2&contrast=1.4&saturation=0.6&sharpness=1.5"
	before, err := json.Marshal(deck)
	if err != nil {
		t.Fatal(err)
	}
	scene, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range scene.Objects {
		if object.ID == "image" {
			found = object.Media != nil && len(object.Media.ColourMatrices) == 2 && object.Media.Width == 768 && object.Media.Sharpness == 1.5 && object.Media.SharpnessAfter == 1
		}
	}
	if !found {
		t.Fatal("source image adjustment projection missing")
	}
	for _, issue := range scene.Diagnostics {
		if issue.Code == "image-adjustments" {
			t.Fatal("supported adjustments still diagnosed as unsupported")
		}
		if issue.Code == "image-sharpness" {
			t.Fatal("supported sharpening still diagnosed as unsupported")
		}
	}
	after, err := json.Marshal(deck)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("colour projection modified embedded sources or authoring data")
	}
}
