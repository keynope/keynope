package app

import (
	"bytes"
	"image"
	"image/png"
	"keynope/internal/pptx"
	"math"
	"net/url"
	"testing"
)

func TestPPTXContainImportPreservesMaskedFrame(t *testing.T) {
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 400, 200))); err != nil {
		t.Fatal(err)
	}
	o := pptx.Object{ID: "fit", Kind: "image", X: 20, Y: 30, Width: 200, Height: 200, Shape: "ellipse", Image: source.Bytes()}
	zero := 0.0
	o.ImageOpacity = &zero
	applyPPTXImageFraming(&o, &sceneMedia{Width: 400, Height: 200, Fit: "contain", Crop: &sceneCrop{Left: .1, Right: .1}})
	e, warnings, _, err := pptxObjectToElement(o, 1, 0, 0)
	if err != nil || len(warnings) != 0 {
		t.Fatal(err, warnings)
	}
	q, _ := url.ParseQuery(e.Query)
	if q.Get("image-fit") != "contain" || q.Get("modern-mask") != "ellipse" || q.Get("modern-crop") == "" {
		t.Fatal("lost fitting, crop or mask", e.Query)
	}
	if q.Get("modern-opacity") != "0" {
		t.Fatal("transparent image became opaque", e.Query)
	}
	o.FillLeft = .1
	_, warnings, _, err = pptxObjectToElement(o, 1, 0, 0)
	if err != nil || len(warnings) != 1 || warnings[0].Code != "image-destination-approximated" {
		t.Fatal("custom destination must warn", err, warnings)
	}
}

func TestPPTXImageFraming(t *testing.T) {
	for _, tc := range []struct {
		fit              string
		crop             *sceneCrop
		x, y, w, h, l, r float64
	}{
		{"contain", nil, 10, 20, 200, 200, 0, 0},
		{"cover", nil, 10, 20, 200, 200, .25, .25},
		{"stretch", nil, 10, 20, 200, 200, 0, 0},
		{"cover", &sceneCrop{Left: .1, Right: .2}, 10, 20, 200, 200, .2, .3},
		{"stretch", &sceneCrop{Left: .1, Right: .2}, 10, 20, 200, 200, .1, .2},
	} {
		o := pptx.Object{X: 10, Y: 20, Width: 200, Height: 200, Rotation: 30}
		applyPPTXImageFraming(&o, &sceneMedia{Width: 400, Height: 200, Fit: tc.fit, Crop: tc.crop})
		for _, pair := range [][2]float64{{o.X, tc.x}, {o.Y, tc.y}, {o.Width, tc.w}, {o.Height, tc.h}, {o.CropLeft, tc.l}, {o.CropRight, tc.r}} {
			if math.Abs(pair[0]-pair[1]) > 1e-8 {
				t.Fatalf("%s: %+v", tc.fit, o)
			}
		}
		if o.Rotation != 30 {
			t.Fatal("rotation lost")
		}
		if tc.fit == "contain" && (o.FillTop != .25 || o.FillBottom != .25 || !pptxPictureUsesContain(o, 400, 200)) {
			t.Fatalf("contain fill lost: %+v", o)
		}
	}
}

func TestPPTXImageMaskProjection(t *testing.T) {
	for _, mask := range []string{"ellipse", "rounded"} {
		o := sceneObject{ID: "image", Kind: "image", Bounds: sceneRect{Width: 200, Height: 200}, Media: &sceneMedia{Width: 400, Height: 200, Source: "data:image/png;base64,iVBORw0KGgo=", Mask: mask, Fit: "cover"}}
		opacity := .4
		o.Paint.Opacity = &opacity
		got, warnings := sceneObjectToPPTX(o)
		want := "ellipse"
		if mask == "rounded" {
			want = "roundRect"
		}
		if got == nil || got.Shape != want || len(warnings) != 0 {
			t.Fatalf("mask projection: %+v %v", got, warnings)
		}
		if got.ImageOpacity == nil || *got.ImageOpacity != opacity {
			t.Fatal("opacity lost")
		}
		o.Media.Fit = "contain"
		got, warnings = sceneObjectToPPTX(o)
		if got == nil || got.Shape != want || len(warnings) != 0 || got.Width != 200 || got.Height != 200 || got.FillTop != .25 {
			t.Fatal("contain-mask frame changed", got, warnings)
		}
	}
}
