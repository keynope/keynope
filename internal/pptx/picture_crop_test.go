package pptx

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"
)

func TestPictureWritesSourceCrop(t *testing.T) {
	xml := pictureXML(Object{ID: "cropped", Width: 100, Height: 100, CropLeft: .1, CropTop: .2, CropRight: .3, CropBottom: .05}, 2, "rId1", "")
	want := `</a:blip><a:srcRect l="10000" t="20000" r="30000" b="5000"/><a:stretch>`
	if !strings.Contains(xml, want) {
		t.Fatal(xml)
	}
	if strings.Contains(pictureXML(Object{Width: 100, Height: 100}, 2, "rId1", ""), "srcRect") {
		t.Fatal("uncropped image changed")
	}
}

func TestPictureCropPackageRoundTrip(t *testing.T) {
	o := Object{ID: "crop", Kind: "image", X: 12, Y: 34, Width: 200, Height: 100, Rotation: 30, Image: []byte{137, 80, 78, 71, 13, 10, 26, 10}, ImageMIME: "image/png", CropLeft: .1, CropTop: .2, CropRight: .3, CropBottom: .05, FillTop: .125, FillBottom: .125, Shape: "ellipse"}
	var output bytes.Buffer
	if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err != nil {
		t.Fatal(err)
	}
	got, _, err := Read(context.Background(), byteReaderAt(output.Bytes()), int64(output.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slides) != 1 || len(got.Slides[0].Objects) != 1 {
		t.Fatal("missing image")
	}
	g := got.Slides[0].Objects[0]
	if g.FillTop != o.FillTop || g.FillBottom != o.FillBottom || g.Shape != o.Shape {
		t.Fatal("destination frame or mask lost", g)
	}
	for _, p := range [][2]float64{{g.CropLeft, o.CropLeft}, {g.CropTop, o.CropTop}, {g.CropRight, o.CropRight}, {g.CropBottom, o.CropBottom}, {g.X, o.X}, {g.Y, o.Y}, {g.Width, o.Width}, {g.Height, o.Height}, {g.Rotation, o.Rotation}} {
		if math.Abs(p[0]-p[1]) > 1e-5 {
			t.Fatalf("round trip: %+v", g)
		}
	}
	if !bytes.Equal(g.Image, o.Image) {
		t.Fatal("source bytes changed")
	}
}

func TestInvalidPictureDestinationWritesNothing(t *testing.T) {
	for _, inset := range []float64{math.NaN(), math.Inf(1), 1, 2} {
		var output bytes.Buffer
		_, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "image", Width: 100, Height: 100, FillLeft: inset}}}}})
		if err == nil || output.Len() != 0 {
			t.Fatal("invalid destination wrote package", inset, err)
		}
	}
}

func TestPictureDestinationXML(t *testing.T) {
	xml := pictureXML(Object{Width: 200, Height: 100, FillTop: .25, FillBottom: .25}, 2, "rId1", "")
	if !strings.Contains(xml, `<a:stretch><a:fillRect l="0" t="25000" r="0" b="25000"/></a:stretch>`) {
		t.Fatal(xml)
	}
}

func TestPictureOpacityPackageRoundTrip(t *testing.T) {
	for _, opacity := range []float64{0, .375, 1} {
		o := Object{Kind: "image", Width: 200, Height: 100, Image: []byte{137, 80, 78, 71, 13, 10, 26, 10}, ImageMIME: "image/png", ImageOpacity: &opacity}
		var output bytes.Buffer
		if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err != nil {
			t.Fatal(err)
		}
		got, _, err := Read(context.Background(), byteReaderAt(output.Bytes()), int64(output.Len()), Options{})
		if err != nil {
			t.Fatal(err)
		}
		g := got.Slides[0].Objects[0]
		if g.ImageOpacity == nil || *g.ImageOpacity != opacity || !bytes.Equal(g.Image, o.Image) {
			t.Fatal("opacity or source lost", opacity, g)
		}
	}
	if strings.Contains(pictureXML(Object{}, 2, "rId1", ""), "alphaModFix") {
		t.Fatal("implicit opacity changed")
	}
}

func TestInvalidPictureOpacityWritesNothing(t *testing.T) {
	for _, opacity := range []float64{math.NaN(), math.Inf(1), -.1, 1.1} {
		var output bytes.Buffer
		_, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{{Kind: "image", ImageOpacity: &opacity}}}}})
		if err == nil || output.Len() != 0 {
			t.Fatal("invalid opacity wrote package", opacity, err)
		}
	}
}

func TestInvalidPictureCropWritesNothing(t *testing.T) {
	for _, crop := range []float64{math.NaN(), math.Inf(1), 1, 2} {
		var output bytes.Buffer
		_, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{{ID: "bad", Kind: "image", Width: 100, Height: 100, CropLeft: crop}}}}})
		if err == nil || output.Len() != 0 {
			t.Fatal("invalid crop wrote package", crop, err, output.Len())
		}
	}
}

func TestPictureMasksPackageRoundTrip(t *testing.T) {
	for _, shape := range []string{"rect", "ellipse", "roundRect"} {
		o := Object{ID: "mask", Kind: "image", Width: 200, Height: 100, Shape: shape, Image: []byte{137, 80, 78, 71, 13, 10, 26, 10}, ImageMIME: "image/png", CropLeft: .1}
		var output bytes.Buffer
		if _, err := Write(context.Background(), &output, Presentation{Slides: []Slide{{Objects: []Object{o}}}}); err != nil {
			t.Fatal(err)
		}
		got, _, err := Read(context.Background(), byteReaderAt(output.Bytes()), int64(output.Len()), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Slides) != 1 || len(got.Slides[0].Objects) != 1 {
			t.Fatal("missing picture")
		}
		g := got.Slides[0].Objects[0]
		if g.Shape != shape || g.CropLeft != .1 || !bytes.Equal(g.Image, o.Image) {
			t.Fatalf("lost mask/crop/source: %+v", g)
		}
		if shape == "roundRect" && !strings.Contains(pictureXML(o, 2, "rId1", ""), `fmla="val 12000"`) {
			t.Fatal("rounded radius lost")
		}
	}
}
