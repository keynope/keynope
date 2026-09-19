package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAnimationSourceRetentionAndProjection(t *testing.T) {
	palette := color.Palette{color.Transparent, color.White, color.RGBA{255, 0, 0, 255}}
	first := image.NewPaletted(image.Rect(0, 0, 768, 8), palette)
	second := image.NewPaletted(image.Rect(100, 0, 110, 8), palette)
	for i := range first.Pix {
		first.Pix[i] = 1
	}
	for i := range second.Pix {
		second.Pix[i] = 2
	}
	animation := &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{7, 19}, LoopCount: 2, Disposal: []byte{gif.DisposalNone, gif.DisposalNone}, Config: image.Config{ColorModel: palette, Width: 768, Height: 8}}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, animation); err != nil {
		t.Fatal(err)
	}
	oversized := append([]byte(nil), data.Bytes()...)
	binary.LittleEndian.PutUint16(oversized[6:8], 6000)
	binary.LittleEndian.PutUint16(oversized[8:10], 3000)
	if err := preflightGIFFrames(oversized); err == nil {
		t.Fatal("expanded animation limit not checked before decode")
	}
	if err := preflightGIFFrames(data.Bytes()[:20]); err == nil {
		t.Fatal("truncated GIF accepted")
	}
	id, asset, path, err := embeddedImageAsset(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if asset.Width != 384 || asset.Source == nil || asset.Source.Width != 768 || len(asset.Source.Frames) != 2 {
		t.Fatal("missing independent full-resolution animation")
	}
	for i, frame := range asset.Source.Frames {
		if frame.DelayMS != asset.Frames[i].DelayMS {
			t.Fatal("source/derivative timing differs")
		}
		img, err := png.Decode(bytes.NewReader(frame.Data))
		if err != nil || img.Bounds().Dx() != 768 {
			t.Fatal("source frame invalid")
		}
		if i == 1 {
			r, g, b, _ := img.At(105, 2).RGBA()
			if r != 65535 || g != 0 || b != 0 {
				t.Fatal("delta frame was not composited")
			}
			r, g, b, _ = img.At(500, 2).RGBA()
			if r != 65535 || g != 65535 || b != 65535 {
				t.Fatal("previous frame pixels lost")
			}
		}
	}
	deck := Deck{Slides: []Slide{{Elements: []Element{{ID: "animation", Kind: "image", AssetID: id, Path: path, Query: "top=1&left=1&width=100&height=10"}}}}, Assets: map[string]DeckAsset{id: asset}}
	copy := cloneDeck(deck)
	copy.Assets[id].Source.Frames[0].Data[0] ^= 1
	if bytes.Equal(copy.Assets[id].Source.Frames[0].Data, asset.Source.Frames[0].Data) {
		t.Fatal("source frame clone aliases")
	}
	scene, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(scene.Objects) != 1 || scene.Objects[0].Media.Width != 768 || len(scene.Objects[0].Media.Frames) != 2 || scene.Objects[0].Media.LoopCount != 2 {
		t.Fatal("Modern source projection lost timing or resolution")
	}
	retro := cloneDeck(deck)
	retro.Slides[0].Elements[0].Query += "&element-style=retro&glyph=blocks"
	retroScene, err := buildSlideScene(retro, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if len(retroScene.Objects) != 1 || retroScene.Objects[0].RetroLines == nil {
		t.Fatal("missing Retro animation")
	}
	track := retroScene.Objects[0].RetroLines
	if len(track.Frames) != 2 || track.Frames[0].DelayMS != 70 || track.Frames[1].DelayMS != 190 || track.LoopCount != 2 {
		t.Fatalf("Retro frame timing changed: %+v", track.Frames)
	}
	if !track.Frames[0].Full || len(track.Frames[0].Lines) == 0 {
		t.Fatal("Retro animation does not start with a complete frame")
	}
	filename := filepath.Join(t.TempDir(), "Animation.md")
	saved, err := serializeDeck(filename, deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData(filename, saved)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Assets[id].Source, asset.Source) {
		t.Fatal("save/reopen lost animation source")
	}
	alternate := cloneDeck(deck).Assets[id]
	alternate.Source.Frames[0].Data[0] ^= 1
	other, _, _, err := makeAnimatedDeckAsset(alternate)
	if err != nil || other == id {
		t.Fatal("animation source omitted from asset identity")
	}
}
