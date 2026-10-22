package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"keynope/internal/pptx"
)

type pptxRenderedCapture struct {
	Slide    int      `json:"slide"`
	ObjectID string   `json:"objectId"`
	Frames   []string `json:"frames"` // PNG data URIs; timing is owned by the scene.
}
type pptxCaptureRequest struct {
	Owner    string                `json:"owner"`
	Version  int64                 `json:"version"`
	Captures []pptxRenderedCapture `json:"captures"`
}
type pptxCaptureItem struct {
	Width  float64     `json:"width,omitempty"`
	Height float64     `json:"height,omitempty"`
	Slide  int         `json:"slide"`
	Object sceneObject `json:"object"`
	Times  []int64     `json:"times"`
}

func decodePPTXCaptureRequest(data []byte, request *pptxCaptureRequest) error {
	root, err := decodeMetadataJSONObject(data, "owner", "version", "captures")
	if err != nil {
		return err
	}
	if root == nil || root["owner"] == nil || root["version"] == nil || root["captures"] == nil {
		return fmt.Errorf("PowerPoint artwork request needs owner, version and captures")
	}
	var captures []json.RawMessage
	if err := json.Unmarshal(root["captures"], &captures); err != nil {
		return err
	}
	if len(captures) > 10000 {
		return fmt.Errorf("too many PowerPoint artwork captures")
	}
	for _, capture := range captures {
		fields, err := decodeMetadataJSONObject(capture, "slide", "objectId", "frames")
		if err != nil {
			return err
		}
		if fields == nil || fields["frames"] == nil {
			return fmt.Errorf("PowerPoint artwork capture needs frames")
		}
		frames := json.NewDecoder(bytes.NewReader(fields["frames"]))
		if token, err := frames.Token(); err != nil || token != json.Delim('[') {
			return fmt.Errorf("PowerPoint artwork frames must be an array")
		}
		count := 0
		for frames.More() {
			count++
			if count > 2048 {
				return fmt.Errorf("too many PowerPoint artwork frames")
			}
			var source string
			if err := frames.Decode(&source); err != nil {
				return err
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(request)
}

func pptxCaptureKey(slide int, id string) string { return strconv.Itoa(slide) + ":" + id }

func pptxResolvedSlide(deck Deck, index int) Slide {
	resolved := standaloneExportSlide(deck.ResolveSlide(index, false), index)
	if !deck.usesConcreteAppearance() {
		for i, element := range resolved.Elements {
			if element.Kind != "image" {
				continue
			}
			query, _ := url.ParseQuery(element.Query)
			if !validElementStyle(query.Get("element-style")) {
				query.Set("element-style", deck.elementStyle(resolved, element))
				resolved.Elements[i].Query = query.Encode()
			}
		}
	}
	return resolved
}

func pptxRetroNeedsFontCapture(object sceneObject) bool {
	if object.Kind != "image" || object.RetroLines == nil {
		return false
	}
	needs := func(lines []exportLine) bool {
		for _, line := range lines {
			if line.TrueType != nil {
				return true
			}
			if line.Role == "outline" {
				continue
			}
			for _, part := range line.Parts {
				for _, r := range part.Text {
					if _, ok := pptxRetroBlockMask(r); !ok {
						return true
					}
				}
			}
		}
		return false
	}
	if needs(object.RetroLines.Lines) {
		return true
	}
	for _, frame := range object.RetroLines.Frames {
		if needs(frame.Lines) || needs(frame.Update) {
			return true
		}
	}
	return false
}

func pptxModernNeedsPaintCapture(object sceneObject) bool {
	return object.Kind == "image" && (object.Paint.Effects != nil || object.Paint.GradientImage != "" || object.RetroLines == nil && object.Media != nil && (object.Paint.GradientStart != "" || object.Paint.Stroke != ""))
}

func artworkCapturePadding(object sceneObject) float64 {
	if object.Paint.Effects != nil {
		return 64 + math.Ceil(object.Paint.Effects.Padding)
	}
	return 64
}

func pptxImageBackgroundObject(background *sceneBackgroundMedia, index int, page ...sceneRect) sceneObject {
	object := pptxRetroBackgroundObject(background, index, page...)
	media := background.sceneMedia
	media.Fit = background.Fit
	object.Media = &media
	return object
}

func chargePPTXCaptureSources(media *sceneMedia, remaining *int64) error {
	if media == nil {
		return nil
	}
	charge := func(source string) error {
		size := int64(len(source))
		if size > 64<<20 || size > *remaining {
			return fmt.Errorf("PowerPoint artwork source manifest exceeds encoded-media budget; reduce image dimensions/frames or export fewer slides")
		}
		*remaining -= size
		return nil
	}
	if err := charge(media.Source); err != nil {
		return err
	}
	for _, frame := range media.Frames {
		if err := charge(frame.Source); err != nil {
			return err
		}
	}
	return nil
}

func pptxCaptureDimensions(object sceneObject) (int, int, error) {
	b := object.Bounds
	pad := artworkCapturePadding(object)
	w, h := math.Ceil(b.Width+pad*2), math.Ceil(b.Height+pad*2)
	if b.Width <= 0 || b.Height <= 0 || math.IsNaN(w+h+b.X+b.Y+object.Rotation) || math.IsInf(w+h+b.X+b.Y+object.Rotation, 0) || w > 8192 || h > 8192 || w*h > 16_000_000 {
		return 0, 0, fmt.Errorf("invalid or oversized PowerPoint capture dimensions")
	}
	return int(w), int(h), nil
}

func buildPPTXCaptureItems(ctx context.Context, deck Deck) ([]pptxCaptureItem, error) {
	deck = preparePPTXEditorModel(deck)
	items := []pptxCaptureItem{}
	encodedSourcesLeft := int64(96 << 20)
	work := newPPTXExportWork(ctx)
	cols, rows := authoredRenderSize(245, 56)
	for index, slide := range deck.Slides {
		if err := work.check(); err != nil {
			return nil, err
		}
		if slide.TabID != "" {
			continue
		}
		scene, err := buildStyledSceneProjection(deck, pptxResolvedSlide(deck, index), index, cols, rows, false, nil, nil, nil)
		if err != nil {
			return nil, err
		}
		objects := scene.Objects
		if scene.BackgroundMedia != nil {
			objects = append(objects, pptxImageBackgroundObject(scene.BackgroundMedia, index, sceneRect{Width: scene.Width, Height: scene.Height}))
		}
		for _, object := range objects {
			if !pptxRetroNeedsFontCapture(object) && !pptxModernNeedsPaintCapture(object) {
				continue
			}
			w, h, err := pptxCaptureDimensions(object)
			if err != nil {
				return nil, err
			}
			times := []int64{0}
			var delays []int64
			if object.RetroLines != nil {
				for _, frame := range object.RetroLines.Frames {
					delays = append(delays, frame.DelayMS)
				}
			} else {
				for _, frame := range object.Media.Frames {
					delays = append(delays, frame.DelayMS)
				}
			}
			if len(delays) > 1 {
				if len(delays) > 2048 || int64(w)*int64(h)*int64(len(delays)) > 32_000_000 {
					return nil, fmt.Errorf("font-based artwork exceeds animated capture limits; reduce size/frames or change the image to Modern")
				}
				times = nil
				position := int64(0)
				for _, delay := range delays {
					times = append(times, position)
					if delay <= 0 {
						delay = 100
					}
					if delay < 10 || delay > 655350 {
						return nil, fmt.Errorf("font-based animation delay cannot be represented in GIF")
					}
					position += delay
				}
			}
			if err := work.pixels(float64(w), float64(h), len(times)); err != nil {
				return nil, err
			}
			if object.RetroLines != nil {
				object.Media = nil // Never send original sources alongside already sampled rows.
			}
			if err := chargePPTXCaptureSources(object.Media, &encodedSourcesLeft); err != nil {
				return nil, err
			}
			item := pptxCaptureItem{Slide: index, Object: object, Times: times}
			if scene.Width != 1920 || scene.Height != 1080 {
				item.Width, item.Height = scene.Width, scene.Height
			}
			items = append(items, item)
		}
	}
	return items, nil
}

func capturedPPTXRetroImage(object sceneObject, capture pptxRenderedCapture, work *pptxExportWork) (*pptx.Object, []pptx.Warning, error) {
	w, h, err := pptxCaptureDimensions(object)
	if err != nil {
		return nil, nil, err
	}
	count, loop := 1, 0
	var delays []int64
	if object.RetroLines != nil {
		loop = object.RetroLines.LoopCount
		for _, frame := range object.RetroLines.Frames {
			delays = append(delays, frame.DelayMS)
		}
	} else if object.Media != nil {
		loop = object.Media.LoopCount
		for _, frame := range object.Media.Frames {
			delays = append(delays, frame.DelayMS)
		}
	}
	count = max(1, len(delays))
	if len(capture.Frames) != count || count > 2048 || count > 1 && int64(w)*int64(h)*int64(count) > 32_000_000 {
		return nil, nil, fmt.Errorf("PowerPoint capture frame count does not match the scene")
	}
	if err := work.pixels(float64(w), float64(h), count); err != nil {
		return nil, nil, err
	}
	var first []byte
	var encodedBytes int64
	media := &sceneMedia{Width: w, Height: h, LoopCount: loop}
	for i, source := range capture.Frames {
		if err := work.check(); err != nil {
			return nil, nil, err
		}
		encodedBytes += int64(len(source))
		if encodedBytes > 96<<20 || len(source) > 64<<20 {
			return nil, nil, fmt.Errorf("PowerPoint capture exceeds encoded-frame budget")
		}
		data, mime, ok := dataURI(source)
		if !ok || mime != "image/png" {
			return nil, nil, fmt.Errorf("PowerPoint capture must be an embedded PNG")
		}
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != w || config.Height != h {
			return nil, nil, fmt.Errorf("PowerPoint capture dimensions do not match the scene")
		}
		if _, err := png.Decode(bytes.NewReader(data)); err != nil {
			return nil, nil, fmt.Errorf("invalid PowerPoint capture pixels: %w", err)
		}
		if i == 0 {
			first = data
		}
		delay := int64(100)
		if count > 1 {
			delay = delays[i]
			if delay == 0 {
				delay = 100
			}
		}
		media.Frames = append(media.Frames, sceneMediaFrame{Source: source, DelayMS: delay})
	}
	warnings := []pptx.Warning{{Object: object.ID, Code: "retro-font-artwork-rasterized", Message: "Font-dependent Retro image cells were captured by the shared renderer and embedded as picture artwork, not editable PowerPoint text."}}
	if object.RetroLines == nil {
		warnings = []pptx.Warning{{Object: object.ID, Code: "image-paint-rasterized", Message: "Image gradient, outline, framing and source treatment were captured as picture artwork. Placement, rotation, opacity and shadow remain editable; source crop and treatment are baked."}}
	}
	mime := "image/png"
	if count > 1 {
		var losses []pptx.Warning
		first, losses, err = encodePPTXAnimation(media, work)
		if err != nil {
			return nil, nil, err
		}
		mime = "image/gif"
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "retro-image-animation-embedded", Message: "Rendered Retro frames, timing and looping were embedded as GIF; playback depends on the reader."})
		if object.RetroLines == nil {
			warnings[len(warnings)-1] = pptx.Warning{Object: object.ID, Code: "image-animation-embedded", Message: "Rendered image-effect frames, timing and looping were embedded as GIF; playback depends on the reader."}
		}
		for _, loss := range losses {
			loss.Object = object.ID
			warnings = append(warnings, loss)
		}
	}
	picture, err := pptxRetroPicture(object, first, mime, float64(w), float64(h))
	if err == nil && object.RetroLines == nil {
		paint := object.Paint
		paint.GradientStart, paint.GradientEnd, paint.Stroke = "", "", ""
		paint.Effects, paint.GradientImage = nil, ""
		applyPPTXPicturePaint(picture, paint)
	}
	return picture, warnings, err
}

func (s *nativeEditorSession) handlePPTXCaptureManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	s.mu.RLock()
	deck, version, owner := cloneDeckForRender(s.deck), s.version, s.notesSessionID
	s.mu.RUnlock()
	items, err := buildPPTXCaptureItems(r.Context(), deck)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.mu.RLock()
	unchanged := s.version == version && s.notesSessionID == owner
	s.mu.RUnlock()
	if !unchanged {
		http.Error(w, "The deck changed while preparing artwork; export again.", 409)
		return
	}
	var out bytes.Buffer
	if err := json.NewEncoder(&pptxExportWriter{target: &out, work: newPPTXExportWork(r.Context()), left: 64 << 20}).Encode(struct {
		Owner   string            `json:"owner"`
		Version int64             `json:"version"`
		Items   []pptxCaptureItem `json:"items"`
	}{owner, version, items}); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out.Bytes())
}
