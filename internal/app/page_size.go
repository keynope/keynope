package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
)

// Physical design pixels, independent of the Markdown text grid. Nil retains
// the established widescreen canvas without adding metadata to old documents.
type DeckPageSize struct {
	Version int `json:"version"`
	Width   int `json:"width"`
	Height  int `json:"height"`
}

func (deck Deck) pageDimensions() (float64, float64) {
	if deck.PageSize == nil {
		return 1920, 1080
	}
	return float64(deck.PageSize.Width), float64(deck.PageSize.Height)
}

func (deck Deck) physicalCanvas() sceneRect {
	width, height := deck.pageDimensions()
	return sceneRect{Width: width, Height: height}
}

func (slide Slide) physicalCanvas() sceneRect {
	if slide.canvasWidth > 0 && slide.canvasHeight > 0 {
		return sceneRect{Width: slide.canvasWidth, Height: slide.canvasHeight}
	}
	return sceneRect{Width: 1920, Height: 1080}
}

// Optional canvas arguments keep the established single-object converter API
// compatible while a full presentation supplies its physical page dimensions.
func pptxCanvasDimensions(canvas []sceneRect) (float64, float64) {
	if len(canvas) > 0 {
		return canvas[0].Width, canvas[0].Height
	}
	return 1920, 1080
}

func setRetroPageDimensions(retro *sceneRetroLines, width, height float64) {
	if retro != nil && (width != 1920 || height != 1080) {
		retro.CanvasWidth, retro.CanvasHeight = width, height
	}
}

func (size *DeckPageSize) UnmarshalJSON(data []byte) error {
	if _, err := decodeMetadataJSONObject(data, "version", "width", "height"); err != nil {
		return err
	}
	type stored DeckPageSize
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*size = DeckPageSize(value)
	return nil
}

func (size *DeckPageSize) Validate() error {
	if size == nil {
		return nil
	}
	if size.Version != 1 || size.Width < 320 || size.Height < 320 || size.Width > 7680 || size.Height > 7680 || int64(size.Width)*int64(size.Height) > 33_177_600 || size.Width > 8*size.Height || size.Height > 8*size.Width {
		return fmt.Errorf("page size needs version 1, dimensions 320–7680, at most 33,177,600 pixels and aspect ratio between 1:8 and 8:1")
	}
	return nil
}

func (size *DeckPageSize) Clone() *DeckPageSize {
	if size == nil {
		return nil
	}
	copy := *size
	return &copy
}

var pageSizeMetadata = scalarSetting("page size", "keynope-page-size", regexp.MustCompile(`<!--\s*keynope-page-size\s+version=([0-9]+)\s+width=([0-9]+)\s+height=([0-9]+)\s*-->[\t ]*\r?\n?`))

func decodeDeckPageSize(text string) (*DeckPageSize, string, error) {
	match, remaining, err := pageSizeMetadata.read(text)
	if err != nil || match == nil {
		return nil, text, err
	}
	values := [3]int{}
	for i := range values {
		values[i], err = strconv.Atoi(match[i+1])
		if err != nil {
			return nil, text, fmt.Errorf("page size contains an unrepresentable integer")
		}
	}
	size := &DeckPageSize{Version: values[0], Width: values[1], Height: values[2]}
	if err := size.Validate(); err != nil {
		return nil, text, err
	}
	return size, remaining, nil
}

func encodeDeckPageSize(size *DeckPageSize) (string, error) {
	if err := size.Validate(); err != nil {
		return "", err
	}
	if size == nil {
		return "", nil
	}
	return fmt.Sprintf("<!-- keynope-page-size version=1 width=%d height=%d -->", size.Width, size.Height), nil
}
