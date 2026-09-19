package main

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func TestRepeatedSceneMediaSources(t *testing.T) {
	deck := sceneFixture(t)
	e := deck.Slides[0].Elements[len(deck.Slides[0].Elements)-1]
	a := deck.Assets[e.AssetID]
	a.Source.Frames = []DeckAssetFrame{{Data: a.Source.Data, DelayMS: 3}, {Data: a.Source.Data, DelayMS: 180}}
	deck.Assets[e.AssetID] = a
	other := e
	other.ID = "other"
	other.Query += "&modern-mask=ellipse&tint=%23ff0000"
	deck.Slides[0].Elements = []Element{e, other}
	scene, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	x, y := scene.Objects[0].Media, scene.Objects[1].Media
	if x == y || x.Source != y.Source || !reflect.DeepEqual(x.Frames, y.Frames) {
		t.Fatal("source mismatch or shared settings")
	}
	if x.Mask != "" || y.Mask != "ellipse" || len(x.ColourMatrices) != 0 || len(y.ColourMatrices) == 0 {
		t.Fatal("instance settings lost")
	}
	if x.Frames[0].DelayMS != 10 || x.Frames[1].DelayMS != 180 {
		t.Fatal("frame timing changed")
	}
	y.Frames[0].Source = "changed"
	if x.Frames[0].Source == "changed" {
		t.Fatal("frame records alias across instances")
	}
	a.Source.Data = []byte("new source")
	deck.Assets[e.AssetID] = a
	next, err := buildSlideScene(deck, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	if next.Objects[0].Media.Source != "data:"+a.Source.MIME+";base64,"+base64.StdEncoding.EncodeToString(a.Source.Data) {
		t.Fatal("cache escaped projection lifetime")
	}
}

func TestSceneMediaLegacySource(t *testing.T) {
	a := DeckAsset{MIME: "image/png", Width: 2, Height: 3, Data: []byte{1, 2}, Frames: []DeckAssetFrame{{Data: []byte{3}, DelayMS: 40}}}
	s := sceneMediaSource(a)
	if s.Width != 2 || s.Height != 3 || s.Source != "data:image/png;base64,AQI=" || s.Frames[0].Source != "data:image/png;base64,Aw==" {
		t.Fatal("legacy source changed")
	}
}
