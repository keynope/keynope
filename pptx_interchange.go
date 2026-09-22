package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"keynope/internal/pptx"
)

const pptxPointsPerSceneUnit = .5

// PPTX conversion is deliberately local. This adapter contains the Keynope
// semantics while internal/pptx only understands the safe PresentationML
// subset; neither layer calls an external presentation application.
func exportPPTXDocument(deck Deck) ([]byte, pptx.Report, error) {
	presentation, report, err := deckToPPTX(deck)
	if err != nil {
		return nil, report, err
	}
	var out bytes.Buffer
	writerReport, err := pptx.Write(context.Background(), &out, presentation)
	report.Warnings = append(report.Warnings, writerReport.Warnings...)
	if err != nil {
		return nil, report, err
	}
	return out.Bytes(), report, nil
}

func deckToPPTX(deck Deck) (pptx.Presentation, pptx.Report, error) {
	deck = cloneDeckForRender(deck)
	cols, rows := authoredRenderSize(245, 56)
	presentation := pptx.Presentation{Width: pptx.SlideWidthEMU, Height: pptx.SlideHeightEMU}
	c64Widths := map[string]float64{}
	var report pptx.Report
	for index := range deck.Slides {
		if deck.Slides[index].TabID != "" {
			report.Warnings = append(report.Warnings, pptx.Warning{Slide: index, Code: "tab-slide-excluded", Message: "Participant-only tab slide was excluded from PowerPoint export."})
			continue
		}
		resolved := standaloneExportSlide(deck.ResolveSlide(index, false), index)
		scene, err := buildStyledSlideScene(deck, resolved, index, cols, rows, false)
		if err != nil {
			return presentation, report, fmt.Errorf("project slide %d: %w", index+1, err)
		}
		slide := pptx.Slide{Notes: ""}
		// A slide base is a normal, editable rectangle. Animated backgrounds and
		// terminal effects cannot be represented as an Office animation; callers
		// get an explicit report rather than a misleading approximation.
		if scene.Background != "" {
			slide.Objects = append(slide.Objects, pptx.Object{ID: fmt.Sprintf("keynope-background-%d", index), Kind: "shape", X: 0, Y: 0, Width: pptx.SceneWidth, Height: pptx.SceneHeight, Fill: scene.Background, Shape: "rect"})
		}
		// PowerPoint's native picture-background package part is optional and
		// inconsistently supported by third-party readers. Export the same
		// non-selectable Keynope layer as the first full-slide picture instead:
		// visually identical, portable, and still behind all slide objects.
		if media := resolved.BackgroundMedia; media != nil && media.AssetID != "" {
			if asset, ok := deck.Assets[media.AssetID]; ok {
				data, mime := asset.Data, asset.MIME
				if asset.Source != nil && len(asset.Source.Data) > 0 {
					data, mime = asset.Source.Data, asset.Source.MIME
				}
				if len(data) > 0 {
					slide.Objects = append(slide.Objects, pptx.Object{ID: fmt.Sprintf("keynope-background-image-%d", index), Kind: "image", X: 0, Y: 0, Width: pptx.SceneWidth, Height: pptx.SceneHeight, Image: data, ImageMIME: mime})
				} else {
					report.Warnings = append(report.Warnings, pptx.Warning{Slide: index, Code: "background-image-unavailable", Message: "Slide background image had no exportable source."})
				}
			}
		}
		if resolved.Effect != "" && resolved.Effect != "none" || resolved.Background != "" && resolved.Background != "none" {
			report.Warnings = append(report.Warnings, pptx.Warning{Slide: index, Code: "animated-background-frozen", Message: "Animated Keynope background/effect was exported as its base colour."})
		}
		for _, diagnostic := range scene.Diagnostics {
			report.Warnings = append(report.Warnings, pptx.Warning{Slide: index, Object: diagnostic.ObjectID, Code: diagnostic.Code, Message: diagnostic.Message})
		}
		for _, object := range scene.Objects {
			collectPPTXC64Width(c64Widths, object.Text)
			collectPPTXC64Width(c64Widths, object.Label)
			converted, warnings := sceneObjectToPPTX(object)
			for i := range warnings {
				warnings[i].Slide = index
				report.Warnings = append(report.Warnings, warnings[i])
			}
			if converted != nil {
				slide.Objects = append(slide.Objects, *converted)
			}
		}
		presentation.Slides = append(presentation.Slides, slide)
	}
	if len(presentation.Slides) == 0 {
		return presentation, report, fmt.Errorf("there are no presentation slides to export")
	}
	if len(c64Widths) > 0 {
		fontData, err := base64.StdEncoding.DecodeString(strings.TrimSpace(trueTypeFontBase64))
		if err != nil {
			return presentation, report, fmt.Errorf("decode bundled C64 font for PowerPoint: %w", err)
		}
		families := make([]string, 0, len(c64Widths))
		for family := range c64Widths {
			families = append(families, family)
		}
		sort.Strings(families)
		for _, family := range families {
			instance, err := pptx.ScaleTrueTypeWidth(fontData, c64Widths[family])
			if err != nil {
				return presentation, report, fmt.Errorf("make embedded C64 width %q: %w", family, err)
			}
			// Each family represents the width rendered by Keynope. The EOT
			// wrapper gives it a Keynope-owned family name, avoiding collisions
			// with a locally installed Yolomancer face.
			presentation.EmbeddedFonts = append(presentation.EmbeddedFonts, pptx.EmbeddedFont{Family: family, Data: instance, ForceEditable: true})
		}
	}
	return presentation, report, nil
}

func collectPPTXC64Width(widths map[string]float64, text *sceneText) {
	if text == nil || text.FontID != "c64" {
		return
	}
	scale := text.WidthScale
	if scale <= 0 {
		scale = 1
	}
	widths[pptxC64FontFamily(scale)] = scale
}

func sceneObjectToPPTX(object sceneObject) (*pptx.Object, []pptx.Warning) {
	warnings := []pptx.Warning{}
	base := pptx.Object{ID: object.ID, X: object.Bounds.X, Y: object.Bounds.Y, Width: object.Bounds.Width, Height: object.Bounds.Height, Rotation: object.Rotation, Link: object.Link}
	if object.RetroLines != nil || object.RetroText != nil || object.RetroLabel != nil {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "retro-artwork-omitted", Message: "Retro sampled artwork needs exact-appearance capture and was not included in editable export."})
		if object.Text == nil && object.Label == nil {
			return nil, warnings
		}
	}
	switch object.Kind {
	case "text":
		if object.Text == nil {
			return nil, warnings
		}
		base.Kind = "text"
		base.Text = sceneTextPlain(object.Text)
		base.FontFace = pptxFontFace(object.Text)
		base.FontSize = pptxTextFontSize(object.Text)
		base.TextAlign = object.Text.Align
		base.VerticalAlign = object.Text.Vertical
		base.Color = object.Paint.Color
		if len(object.Text.Runs) > 1 || len(object.Text.Paragraphs) > 0 {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "rich-text-approximated", Message: "Mixed run styling or lists were exported as a single editable text style."})
		}
		if object.Text.FontID != "c64" && object.Text.WidthScale != 1 {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "font-width-approximated", Message: "Keynope horizontal font scaling is not an Office text metric and was approximated."})
		}
		return &base, warnings
	case "shape":
		base.Kind = "shape"
		base.Shape = pptxShape(object.Shape)
		base.Fill = object.Paint.Color
		base.Stroke = object.Paint.Stroke
		base.StrokeWidth = object.Paint.StrokeWidth
		if object.Label != nil {
			base.Text = sceneTextPlain(object.Label)
			base.FontFace = pptxFontFace(object.Label)
			base.FontSize = pptxTextFontSize(object.Label)
			base.TextAlign = object.Label.Align
			base.VerticalAlign = object.Label.Vertical
			if object.LabelPaint != nil {
				base.Color = object.LabelPaint.Color
			} else {
				base.Color = object.Paint.Color
			}
		}
		if object.Paint.GradientStart != "" || object.Paint.ShadowColor != "" || object.Paint.Opacity != nil {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "shape-effects-approximated", Message: "Gradient, shadow or opacity was exported as a basic editable shape."})
		}
		return &base, warnings
	case "image":
		if object.Media == nil || object.Media.Source == "" {
			return nil, append(warnings, pptx.Warning{Object: object.ID, Code: "image-source-missing", Message: "Image had no embedded source and was omitted."})
		}
		data, mime, ok := dataURI(object.Media.Source)
		if !ok {
			return nil, append(warnings, pptx.Warning{Object: object.ID, Code: "image-source-unsupported", Message: "Image source was not an embedded data URI and was omitted."})
		}
		base.Kind = "image"
		base.Image = data
		base.ImageMIME = mime
		if object.Media.Crop != nil || object.Media.Mask != "" || len(object.Media.ColourMatrices) > 0 || len(object.Media.Frames) > 0 {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "image-treatment-approximated", Message: "Crop, mask, colour treatment or animation was exported as the first embedded image frame."})
		}
		return &base, warnings
	case "connector":
		if len(object.Points) < 2 {
			return nil, append(warnings, pptx.Warning{Object: object.ID, Code: "connector-missing-points", Message: "Connector had no route and was omitted."})
		}
		first, last := object.Points[0], object.Points[len(object.Points)-1]
		base.Kind = "line"
		base.X, base.Y = first.X, first.Y
		base.Width, base.Height = last.X-first.X, last.Y-first.Y
		base.Stroke = object.Paint.Stroke
		base.StrokeWidth = object.Paint.StrokeWidth
		if len(object.Points) > 2 {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "connector-route-approximated", Message: "Elbow connector route was exported as a straight editable line."})
		}
		return &base, warnings
	default:
		return nil, append(warnings, pptx.Warning{Object: object.ID, Code: "unsupported-object", Message: "Object is not supported by editable PowerPoint export."})
	}
}

func sceneTextPlain(text *sceneText) string {
	if len(text.Paragraphs) > 0 {
		parts := make([]string, 0, len(text.Paragraphs))
		for _, paragraph := range text.Paragraphs {
			var b strings.Builder
			for _, run := range paragraph.Runs {
				b.WriteString(run.Text)
			}
			parts = append(parts, b.String())
		}
		return strings.Join(parts, "\n")
	}
	var b strings.Builder
	for _, run := range text.Runs {
		b.WriteString(run.Text)
	}
	return b.String()
}
func pptxFontFace(text *sceneText) string {
	if text == nil {
		return "Arial"
	}
	switch text.FontID {
	case "c64":
		return pptxC64FontFamily(text.WidthScale)
	case "mono":
		return "Courier New"
	default:
		return "Arial"
	}
}

// pptxTextFontSize maps a Keynope scene size to points. C64 width is embodied
// in a matching embedded font instance, so height and point size stay exactly
// as authored instead of being reduced to compensate for a narrow glyph run.
func pptxTextFontSize(text *sceneText) float64 {
	if text == nil {
		return 18
	}
	// The PPTX scene maps one Keynope scene unit to half a point.
	size := text.Size * pptxPointsPerSceneUnit
	if size <= 0 {
		size = 18
	}
	return size
}

func pptxC64FontFamily(scale float64) string {
	if scale <= 0 {
		scale = 1
	}
	width := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(scale*100, 'f', 3, 64), "0"), ".")
	if width == "100" {
		return "Keynope C64"
	}
	return "Keynope C64 W" + width
}
func pptxShape(shape string) string {
	switch shape {
	case "circle", "ellipse":
		return "ellipse"
	case "diamond", "triangle", "roundRect", "roundrect":
		return shape
	default:
		return "rect"
	}
}
func dataURI(source string) ([]byte, string, bool) {
	const prefix = "data:"
	if !strings.HasPrefix(source, prefix) {
		return nil, "", false
	}
	comma := strings.IndexByte(source, ',')
	if comma < 0 {
		return nil, "", false
	}
	header := source[len(prefix):comma]
	if !strings.HasSuffix(header, ";base64") {
		return nil, "", false
	}
	mime := strings.TrimSuffix(header, ";base64")
	data, err := base64.StdEncoding.DecodeString(source[comma+1:])
	if err != nil || len(data) == 0 {
		return nil, "", false
	}
	return data, mime, true
}

// importPPTXDocument creates an isolated candidate deck. It does not register
// assets, touch recents or replace the active editor; the caller does that only
// after the usual dirty-document guard has accepted the candidate.
func importPPTXDocument(source []byte, sourcePath string) (Deck, pptx.Report, error) {
	presentation, report, err := pptx.Read(context.Background(), bytes.NewReader(source), int64(len(source)), pptx.Options{})
	if err != nil {
		return Deck{}, report, err
	}
	if len(presentation.Slides) == 0 {
		return Deck{}, report, fmt.Errorf("PowerPoint presentation has no slides")
	}
	masters := defaultMasterDeck()
	// A PowerPoint deck already owns its scene chrome. Do not introduce the
	// New-document master’s terminal page number into every imported slide.
	masters.Base.Slide.PageNumber = pageNumberHide
	masters.Base.Slide.Elements = nil
	deck := Deck{Assets: map[string]DeckAsset{}, Masters: masters}
	fit := math.Min(float64(pptx.SlideWidthEMU)/float64(presentation.Width), float64(pptx.SlideHeightEMU)/float64(presentation.Height))
	offsetX := (pptx.SceneWidth - float64(presentation.Width)/6350*fit) / 2
	offsetY := (pptx.SceneHeight - float64(presentation.Height)/6350*fit) / 2
	for slideIndex, sourceSlide := range presentation.Slides {
		slide := Slide{LayoutID: "blank", Elements: []Element{}}
		if sourceSlide.Background != "" {
			// Slide colours are stored as ANSI-compatible values. Passing a raw
			// #RRGGBB through to the scene's ANSI colour resolver falls back to
			// Keynope's cream default, which is especially destructive for modern
			// PowerPoint templates with a dark full-slide background.
			if background, ok := ansiBG(sourceSlide.Background); ok {
				slide.BG, slide.BGSet = background, true
			} else {
				report.Warnings = append(report.Warnings, pptx.Warning{Slide: slideIndex, Code: "background-colour-unreadable", Message: "PowerPoint background colour was not recognised; Keynope kept its default background."})
			}
		}
		if len(sourceSlide.BackgroundImage) > 0 {
			assetID, asset, _, err := embeddedImageAsset(sourceSlide.BackgroundImage)
			if err != nil {
				report.Warnings = append(report.Warnings, pptx.Warning{Slide: slideIndex, Code: "background-image-unreadable", Message: "PowerPoint picture background could not be imported: " + err.Error()})
			} else {
				deck.Assets[assetID] = asset
				slide.BackgroundMedia = &slideBackgroundMedia{AssetID: assetID, Fit: "cover", Opacity: 1}
				slide.BackgroundMediaSet = true
			}
		}
		for objectIndex, sourceObject := range sourceSlide.Objects {
			element, warnings, asset, err := pptxObjectToElement(sourceObject, fit, offsetX, offsetY)
			for i := range warnings {
				warnings[i].Slide = slideIndex
				report.Warnings = append(report.Warnings, warnings[i])
			}
			if err != nil {
				return Deck{}, report, fmt.Errorf("import slide %d: %w", slideIndex+1, err)
			}
			if asset != nil {
				deck.Assets[element.AssetID] = *asset
			}
			if element.Kind != "" {
				// PowerPoint's cNvPr IDs are scoped to an individual shape tree;
				// layouts/masters may therefore repeat one inside a single resolved
				// slide and every slide begins its own sequence. Keynope scene views
				// retain DOM nodes by ID, so import IDs must be document-unique.
				// Without this, thumbnails can fail and a previous slide's retained
				// object can visibly replace a different slide after reordering.
				element.ID = fmt.Sprintf("pptx-s%d-o%d", slideIndex+1, objectIndex+1)
				// Markdown is spatially normalised on save, while PowerPoint's
				// shape tree order is its paint order. Preserve the latter
				// explicitly so imported chrome never rises above slide content.
				element.Query = setQueryValue(element.Query, "z-index", strconv.Itoa(objectIndex))
				slide.Elements = append(slide.Elements, element)
			}
		}
		if sourceSlide.Notes != "" {
			slide.Notes = sourceSlide.Notes
		}
		if sourceSlide.Hidden {
			report.Warnings = append(report.Warnings, pptx.Warning{Slide: slideIndex, Code: "hidden-slide-visible", Message: "PowerPoint hidden slide was imported as an ordinary editable Keynope slide."})
		}
		deck.Slides = append(deck.Slides, slide)
	}
	ensureNativeEditorElementIDs(&deck)
	canonicalizeConcreteAppearance(&deck)
	standardizeDeckText(&deck)
	return deck, report, nil
}

func pptxObjectToElement(object pptx.Object, scale, offsetX, offsetY float64) (Element, []pptx.Warning, *DeckAsset, error) {
	warnings := []pptx.Warning{}
	x, y, w, h := object.X*scale+offsetX, object.Y*scale+offsetY, object.Width*scale, object.Height*scale
	if object.Kind == "line" || (object.Kind == "shape" && strings.EqualFold(object.Shape, "line")) {
		return pptxLineToElement(object, x, y, w, h, scale)
	}
	if w <= 0 || h <= 0 {
		return Element{}, warnings, nil, nil
	}
	query := pptxSceneBoundsQuery(x, y, w, h)
	if object.Rotation != 0 {
		query.Set("object-rotation", decimal(object.Rotation))
	}
	if object.Opacity > 0 && object.Opacity < 1 {
		query.Set("modern-opacity", decimal(object.Opacity))
	}
	if object.Link != "" {
		query.Set("link", object.Link)
	}
	id := "pptx-" + object.ID
	if object.ID == "" {
		id = newStableID("pptx")
	}
	switch object.Kind {
	case "text":
		query.Set("text-box", "1")
		query.Set("render", "truetype")
		query.Set("modern-font", importFont(object.FontFace))
		if object.FontSize > 0 {
			// Undo the export's scene-unit-to-point conversion, then apply the
			// uniform source-page fit used for all imported geometry.
			query.Set("modern-size", decimal(object.FontSize*2*scale))
		}
		if object.Color != "" {
			query.Set("fg", object.Color)
		}
		if object.TextAlign != "" {
			query.Set("text-align", object.TextAlign)
		}
		if object.VerticalAlign != "" {
			query.Set("text-valign", object.VerticalAlign)
		}
		if object.Bold || object.Italic {
			if encoded, err := encodeModernRuns(object.Text, "", "text", []sceneRun{{Text: object.Text, Bold: object.Bold, Italic: object.Italic, Color: object.Color}}); err == nil && encoded != "" {
				query.Set("modern-runs", encoded)
			}
		}
		return Element{ID: id, Kind: "text", Text: object.Text, Query: query.Encode()}, warnings, nil, nil
	case "shape":
		query.Set("shape", importShape(object.Shape))
		if object.Fill != "" {
			query.Set("fg", object.Fill)
		}
		if object.Stroke != "" {
			query.Set("outline", "dark")
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "shape-stroke-approximated", Message: "PowerPoint shape stroke was approximated with a Keynope outline."})
		}
		if object.Text != "" {
			labelQuery := url.Values{}
			labelQuery.Set("render", "truetype")
			labelQuery.Set("modern-font", importFont(object.FontFace))
			if object.FontSize > 0 {
				labelQuery.Set("modern-size", decimal(object.FontSize*2*scale))
			}
			if object.Color != "" {
				labelQuery.Set("fg", object.Color)
			}
			if object.TextAlign != "" {
				labelQuery.Set("text-align", object.TextAlign)
			}
			if object.VerticalAlign != "" {
				labelQuery.Set("text-valign", object.VerticalAlign)
			}
			if object.Opacity > 0 && object.Opacity < 1 {
				labelQuery.Set("modern-opacity", decimal(object.Opacity))
			}
			if object.Bold || object.Italic {
				if encoded, err := encodeModernRuns(object.Text, "", "text", []sceneRun{{Text: object.Text, Bold: object.Bold, Italic: object.Italic, Color: object.Color}}); err == nil && encoded != "" {
					labelQuery.Set("modern-runs", encoded)
				}
			}
			if encoded, err := json.Marshal(shapeLabelData{Text: object.Text, Query: labelQuery.Encode()}); err == nil {
				query.Set("shape-label", base64.StdEncoding.EncodeToString(encoded))
			}
		}
		return Element{ID: id, Kind: "shape", Text: "[shape:" + importShape(object.Shape) + "]", Query: query.Encode()}, warnings, nil, nil
	case "image":
		assetID, asset, pathValue, err := embeddedImageAsset(object.Image)
		if err != nil {
			return Element{}, warnings, nil, err
		}
		query.Set("image-style", "modern")
		// Images otherwise retain the sampled legacy image dimensions during
		// layout. Imported PowerPoint picture bounds are explicit scene bounds.
		query.Set("stretch", "1")
		if object.CropLeft != 0 || object.CropTop != 0 || object.CropRight != 0 || object.CropBottom != 0 {
			query.Set("modern-crop", strings.Join([]string{decimal(object.CropLeft), decimal(object.CropTop), decimal(object.CropRight), decimal(object.CropBottom)}, ","))
		}
		return Element{ID: id, Kind: "image", AssetID: assetID, Path: pathValue, Query: query.Encode()}, warnings, &asset, nil
	default:
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "unsupported-object", Message: "PowerPoint object was not imported."})
		return Element{}, warnings, nil, nil
	}
}

// pptxLineToElement retains PowerPoint line and connector artwork as a thin,
// rotated modern rectangle. Keynope connectors need attachable shape ports,
// which arbitrary imported lines do not have. This visual form is editable,
// preserves the source paint order, and is substantially closer than a blank
// rectangular outline or a missing connector.
func pptxLineToElement(object pptx.Object, x, y, w, h, scale float64) (Element, []pptx.Warning, *DeckAsset, error) {
	if object.Stroke == "" {
		return Element{}, []pptx.Warning{{Object: object.ID, Code: "line-no-stroke", Message: "A PowerPoint line without a stroke was omitted."}}, nil, nil
	}
	length := math.Hypot(w, h)
	if length <= 0 {
		return Element{}, nil, nil, nil
	}
	thickness := math.Max(1, object.StrokeWidth*scale)
	angle := math.Atan2(h, w) * 180 / math.Pi
	if object.FlipH != object.FlipV {
		angle = -angle
	}
	angle += object.Rotation
	query := pptxSceneBoundsQuery(x+w/2-length/2, y+h/2-thickness/2, length, thickness)
	query.Set("shape", "square")
	query.Set("fg", object.Stroke)
	query.Set("object-rotation", decimal(angle))
	if object.Opacity > 0 && object.Opacity < 1 {
		query.Set("modern-opacity", decimal(object.Opacity))
	}
	id := "pptx-" + object.ID
	if object.ID == "" {
		id = newStableID("pptx-line")
	}
	return Element{ID: id, Kind: "shape", Text: "[shape:square]", Query: query.Encode()}, nil, nil, nil
}

// pptxSceneBoundsQuery maps a 1920×1080 PowerPoint scene to Keynope's
// authored 245×56 canvas. Placement intentionally uses integer anchors plus
// sub-cell offsets: imagePlacement parses anchors as integers, whereas width
// and height are allowed to remain continuous for the scene compositor.
// Writing a fractional `left` or `top` made imported objects silently lose
// their anchor and collapse into normal document flow.
func pptxSceneBoundsQuery(x, y, width, height float64) url.Values {
	const cols, rows = 245.0, 56.0
	x = math.Max(0, x*cols/pptx.SceneWidth)
	y = math.Max(0, y*rows/pptx.SceneHeight)
	width = math.Max(.5, width*cols/pptx.SceneWidth)
	height = math.Max(.5, height*rows/pptx.SceneHeight)
	left, top := math.Floor(x), math.Floor(y)
	query := url.Values{}
	query.Set("left", strconv.Itoa(int(left)))
	query.Set("top", strconv.Itoa(int(top)))
	query.Set("width", decimal(width))
	query.Set("height", decimal(height))
	if fraction := x - left; fraction > 1e-9 {
		query.Set("object-offset-x", decimal(fraction))
	}
	if fraction := y - top; fraction > 1e-9 {
		query.Set("object-offset-y", decimal(fraction))
	}
	return query
}
func decimal(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
func importFont(face string) string {
	lower := strings.ToLower(face)
	if strings.Contains(lower, "courier") || strings.Contains(lower, "mono") {
		return "mono"
	}
	if strings.Contains(lower, "keynope") || strings.Contains(lower, "c64") {
		return "c64"
	}
	return "sans"
}
func importShape(shape string) string {
	switch strings.ToLower(shape) {
	case "ellipse", "circle":
		return "circle"
	case "diamond":
		return "diamond"
	case "triangle":
		return "triangle"
	default:
		return "square"
	}
}

func pptxSuggestedName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if strings.TrimSpace(base) == "" {
		base = "Untitled"
	}
	return base + ".md"
}
