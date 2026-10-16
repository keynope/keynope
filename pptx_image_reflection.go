package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/gif"
	"image/png"

	"keynope/internal/pptx"
)

// Reflect decoded pixels rather than adding a second display-only transform.
// Crop and destination insets are mirrored too, preserving their native frame.
func reflectPPTXImage(object *pptx.Object) error {
	if !object.FlipH && !object.FlipV {
		return nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(object.Image))
	if err != nil {
		return err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16_000_000 {
		return fmt.Errorf("reflected image exceeds pixel limit")
	}
	point := func(x, y int) (int, int) {
		if object.FlipH {
			x = config.Width - 1 - x
		}
		if object.FlipV {
			y = config.Height - 1 - y
		}
		return x, y
	}
	var encoded bytes.Buffer
	if format == "gif" {
		if err := validatePPTXReflectionGIF(object.Image); err != nil {
			return err
		}
		animation, err := gif.DecodeAll(bytes.NewReader(object.Image))
		if err != nil {
			return err
		}
		pixels := int64(0)
		for _, frame := range animation.Image {
			pixels += int64(frame.Rect.Dx()) * int64(frame.Rect.Dy())
		}
		if len(animation.Image) > 1000 || pixels > 64_000_000 {
			return fmt.Errorf("reflected animation exceeds frame budget")
		}
		for i, frame := range animation.Image {
			bounds := frame.Bounds()
			x0, y0 := point(bounds.Min.X, bounds.Min.Y)
			x1, y1 := point(bounds.Max.X-1, bounds.Max.Y-1)
			next := image.NewPaletted(image.Rect(min(x0, x1), min(y0, y1), max(x0, x1)+1, max(y0, y1)+1), frame.Palette)
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					nx, ny := point(x, y)
					next.SetColorIndex(nx, ny, frame.ColorIndexAt(x, y))
				}
			}
			animation.Image[i] = next
		}
		if err := gif.EncodeAll(&encoded, animation); err != nil {
			return err
		}
		object.ImageMIME = "image/gif"
	} else {
		source, _, err := image.Decode(bytes.NewReader(object.Image))
		if err != nil {
			return err
		}
		next := image.NewNRGBA(image.Rect(0, 0, config.Width, config.Height))
		for y := 0; y < config.Height; y++ {
			for x := 0; x < config.Width; x++ {
				nx, ny := point(x, y)
				next.Set(nx, ny, source.At(source.Bounds().Min.X+x, source.Bounds().Min.Y+y))
			}
		}
		if err := png.Encode(&encoded, next); err != nil {
			return err
		}
		object.ImageMIME = "image/png"
	}
	object.Image = encoded.Bytes()
	object.ImageSVG = nil
	if object.FlipH {
		object.CropLeft, object.CropRight = object.CropRight, object.CropLeft
		object.FillLeft, object.FillRight = object.FillRight, object.FillLeft
	}
	if object.FlipV {
		object.CropTop, object.CropBottom = object.CropBottom, object.CropTop
		object.FillTop, object.FillBottom = object.FillBottom, object.FillTop
	}
	object.FlipH, object.FlipV = false, false
	return nil
}

// Check aggregate frame allocation before DecodeAll, not after decompression.
func validatePPTXReflectionGIF(data []byte) error {
	bad := fmt.Errorf("invalid or oversized reflected GIF")
	if len(data) < 13 {
		return bad
	}
	i := 13
	if data[10]&0x80 != 0 {
		i += 3 * (1 << ((data[10] & 7) + 1))
	}
	skip := func() bool {
		for i < len(data) {
			n := int(data[i])
			i++
			if n == 0 {
				return true
			}
			if n > len(data)-i {
				return false
			}
			i += n
		}
		return false
	}
	frames, pixels := 0, int64(0)
	for i < len(data) {
		tag := data[i]
		i++
		switch tag {
		case 0x3b:
			return nil
		case 0x21:
			if i >= len(data) {
				return bad
			}
			i++
			if !skip() {
				return bad
			}
		case 0x2c:
			if len(data)-i < 9 {
				return bad
			}
			w, h := int64(binary.LittleEndian.Uint16(data[i+4:i+6])), int64(binary.LittleEndian.Uint16(data[i+6:i+8]))
			frames++
			pixels += w * h
			if frames > 1000 || pixels > 64_000_000 {
				return bad
			}
			packed := data[i+8]
			i += 9
			if packed&0x80 != 0 {
				i += 3 * (1 << ((packed & 7) + 1))
			}
			if i >= len(data) {
				return bad
			}
			i++
			if !skip() {
				return bad
			}
		default:
			return bad
		}
	}
	return bad
}
