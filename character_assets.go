package main

import (
	"encoding/json"
	"fmt"
)

//go:generate go run ./tools/generate_character_assets.go

// The compact character artwork is generated into character_assets_data.go.
// Keynope never opens the design-time items.json file at runtime. Live
// Introduction rooms receive the compiled rows and positions in their
// definition and never fetch character assets from disk or the website.

type introductionAsset struct {
	ID                   string            `json:"id"`
	Rows                 []string          `json:"rows"`
	X                    int               `json:"x"`
	Y                    int               `json:"y"`
	Width                int               `json:"width"`
	Height               int               `json:"height"`
	ColorRoles           map[string]string `json:"colorRoles,omitempty"`
	BackgroundColorRoles map[string]string `json:"backgroundColorRoles,omitempty"`
}

type introductionCategory struct {
	ID           string              `json:"id"`
	Label        string              `json:"label"`
	Layer        int                 `json:"layer"`
	DefaultColor string              `json:"defaultColor"`
	Optional     bool                `json:"optional,omitempty"`
	Assets       []introductionAsset `json:"assets"`
}

type introductionAssetSet struct {
	Width      int                    `json:"width"`
	Height     int                    `json:"height"`
	Palette    []string               `json:"palette"`
	Categories []introductionCategory `json:"categories"`
}

var introductionPalette = []string{
	"#3a241b", "#6b422c", "#8f5f45", "#b97955",
	"#d9977b", "#f0c7a5", "#ffffff", "#a64032",
	"#d9b46f", "#f2cf4a", "#d9792b", "#3f6fb6",
	"#744b9e", "#3f7d4e", "#454b55", "#b9c0c8",
}

var introductionAssets = loadIntroductionAssets()
var introductionAssetsJSON = marshalIntroductionAssets(introductionAssets)

func loadIntroductionAssets() introductionAssetSet {
	type sourceShape struct {
		Category             string            `json:"category"`
		Asset                string            `json:"asset"`
		X                    int               `json:"x"`
		Y                    int               `json:"y"`
		Width                int               `json:"width"`
		Height               int               `json:"height"`
		ASCIIPixels          []string          `json:"asciiPixels"`
		ColorRoles           map[string]string `json:"colorRoles,omitempty"`
		BackgroundColorRoles map[string]string `json:"backgroundColorRoles,omitempty"`
	}
	type sourceDocument struct {
		Grid struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"grid"`
		Shapes []sourceShape `json:"shapes"`
	}
	var source sourceDocument
	if err := json.Unmarshal([]byte(introductionAssetLayoutJSON), &source); err != nil {
		panic(fmt.Sprintf("decode compiled Introduction assets: %v", err))
	}

	type categorySpec struct {
		id, label, defaultColor string
		layer                   int
		optional                bool
	}
	specs := []categorySpec{
		{id: "head", label: "Face", layer: 0, defaultColor: "#d9977b"},
		{id: "hair", label: "Hair", layer: 4, optional: true, defaultColor: "#6b422c"},
		{id: "facial_hair", label: "Facial hair", layer: 1, optional: true, defaultColor: "#6b422c"},
		{id: "nose", label: "Nose", layer: 2, defaultColor: "#8f5f45"},
		{id: "eyebrows", label: "Eyebrows", layer: 2, defaultColor: "#6b422c"},
		{id: "eyes", label: "Eyes", layer: 2, defaultColor: "#8f5f45"},
		{id: "mouth", label: "Mouth", layer: 2, defaultColor: "#a64032"},
		{id: "glasses", label: "Glasses", layer: 3, optional: true, defaultColor: "#454b55"},
	}
	set := introductionAssetSet{Width: source.Grid.Width, Height: source.Grid.Height, Palette: append([]string(nil), introductionPalette...)}
	for _, spec := range specs {
		category := introductionCategory{ID: spec.id, Label: spec.label, Layer: spec.layer, DefaultColor: spec.defaultColor, Optional: spec.optional}
		for _, shape := range source.Shapes {
			if shape.Category != spec.id {
				continue
			}
			if shape.Width < 1 || shape.Height < 1 || len(shape.ASCIIPixels) != shape.Height || shape.X < 0 || shape.Y < 0 || shape.X+shape.Width > set.Width || shape.Y+shape.Height > set.Height {
				panic(fmt.Sprintf("compiled Introduction asset %s/%s has invalid bounds", shape.Category, shape.Asset))
			}
			category.Assets = append(category.Assets, introductionAsset{
				ID:                   shape.Asset,
				Rows:                 append([]string(nil), shape.ASCIIPixels...),
				X:                    shape.X,
				Y:                    shape.Y,
				Width:                shape.Width,
				Height:               shape.Height,
				ColorRoles:           cloneStringMap(shape.ColorRoles),
				BackgroundColorRoles: cloneStringMap(shape.BackgroundColorRoles),
			})
		}
		if len(category.Assets) != 16 {
			panic(fmt.Sprintf("compiled Introduction category %s has %d assets, want 16", spec.id, len(category.Assets)))
		}
		set.Categories = append(set.Categories, category)
	}
	return set
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func marshalIntroductionAssets(assets introductionAssetSet) string {
	data, err := json.Marshal(assets)
	if err != nil {
		panic(fmt.Sprintf("encode embedded Introduction assets: %v", err))
	}
	return string(data)
}
