package app

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
	return exportPPTXDocumentContext(context.Background(), deck)
}

func exportPPTXDocumentContext(ctx context.Context, deck Deck) ([]byte, pptx.Report, error) {
	return exportPPTXDocumentWithWork(deck, newPPTXExportWork(ctx))
}

func exportPPTXDocumentWithWork(deck Deck, work *pptxExportWork) ([]byte, pptx.Report, error) {
	presentation, report, err := deckToPPTXWithWork(deck, work)
	if err != nil {
		return nil, report, err
	}
	var out bytes.Buffer
	writerReport, err := pptx.Write(work.ctx, &pptxExportWriter{target: &out, work: work, left: work.packageBytes}, presentation)
	var sourceSlides []int
	for index, slide := range deck.Slides {
		if slide.TabID == "" {
			sourceSlides = append(sourceSlides, index)
		}
	}
	for i := range writerReport.Warnings {
		index := writerReport.Warnings[i].Slide
		if index >= 0 && index < len(sourceSlides) {
			writerReport.Warnings[i].Slide = sourceSlides[index]
		}
	}
	report.Warnings = append(report.Warnings, writerReport.Warnings...)
	report = report.Normalized()
	if err == nil {
		err = work.check()
	}
	if err != nil {
		return nil, report, err
	}
	return out.Bytes(), report, nil
}

func deckToPPTX(deck Deck) (pptx.Presentation, pptx.Report, error) {
	return deckToPPTXWithWork(deck, newPPTXExportWork(context.Background()))
}

func deckToPPTXWithWork(deck Deck, work *pptxExportWork) (pptx.Presentation, pptx.Report, error) {
	if err := work.check(); err != nil {
		return pptx.Presentation{}, pptx.Report{}, err
	}
	deck = preparePPTXEditorModel(deck)
	if err := deck.PageSize.Validate(); err != nil {
		return pptx.Presentation{}, pptx.Report{}, err
	}
	cols, rows := authoredRenderSize(245, 56)
	width, height := deck.pageDimensions()
	presentation := pptx.Presentation{Width: int64(math.Round(width * 6350)), Height: int64(math.Round(height * 6350))}
	c64Widths := map[string]float64{}
	usedTypefaces := map[string]DeckTypeface{}
	if err := deck.Theme.Validate(); err != nil {
		return pptx.Presentation{}, pptx.Report{}, err
	}
	presentation.Theme = nativePPTXTheme(deck.Theme, c64Widths, deck.Typefaces)
	colourCache := &pptxImageColourCache{}
	var report pptx.Report
	for index := range deck.Slides {
		work.slide = index
		if err := work.check(); err != nil {
			return presentation, report, err
		}
		if deck.Slides[index].TabID != "" {
			report.Warnings = append(report.Warnings, pptx.Warning{Slide: index, Code: "tab-slide-excluded", Message: "Participant-only tab slide was excluded from PowerPoint export."})
			continue
		}
		resolved := pptxResolvedSlide(deck, index)
		var attachments *sceneAttachmentGeometry
		var attachmentTarget **sceneAttachmentGeometry
		for _, element := range resolved.Elements {
			if element.Kind == "connector" {
				attachmentTarget = &attachments
				break
			}
		}
		scene, err := buildStyledSceneProjection(deck, resolved, index, cols, rows, false, nil, nil, attachmentTarget)
		if err != nil {
			return presentation, report, fmt.Errorf("project slide %d: %w", index+1, err)
		}
		slide := pptx.Slide{Notes: ""}
		if scene.Build != nil {
			slide.Timeline = &pptx.Timeline{Transition: pptx.Transition{Effect: scene.Build.Transition.Effect, Direction: scene.Build.Transition.Direction, DurationMS: scene.Build.Transition.DurationMS}}
			for _, step := range scene.Build.Steps {
				slide.Timeline.Steps = append(slide.Timeline.Steps, pptx.BuildStep{TargetIDs: append([]string(nil), step.TargetIDs...), Effect: step.Effect, Direction: step.Direction, DurationMS: step.DurationMS, Advance: step.Advance, DelayMS: step.DelayMS})
			}
		}
		// Keep the solid slide base a background property, not a selectable shape
		// which can obscure content or become ordinary artwork after reimport.
		slide.Background = scene.Background
		// PowerPoint's native picture-background package part is optional and
		// inconsistently supported by third-party readers. Export the same
		// non-selectable Keynope layer as the first full-slide picture instead:
		// visually identical, portable, and still behind all slide objects.
		if projected := scene.BackgroundMedia; projected != nil && (projected.RetroLines != nil || work.captures != nil && pptxModernNeedsPaintCapture(pptxImageBackgroundObject(projected, index, sceneRect{Width: scene.Width, Height: scene.Height}))) {
			object := pptxImageBackgroundObject(projected, index, sceneRect{Width: scene.Width, Height: scene.Height})
			picture, warnings := sceneObjectToPPTX(object, work)
			for i := range warnings {
				warnings[i].Slide = index
			}
			report.Warnings = append(report.Warnings, warnings...)
			if picture != nil {
				slide.Objects = append(slide.Objects, *picture)
			}
		} else if media := resolved.BackgroundMedia; media != nil && media.AssetID != "" {
			if asset, ok := deck.Assets[media.AssetID]; ok {
				data, mime := asset.Data, asset.MIME
				if asset.Source != nil && len(asset.Source.Data) > 0 {
					data, mime = asset.Source.Data, asset.Source.MIME
				}
				if len(data) == 0 && scene.BackgroundMedia != nil {
					data, mime, _ = dataURI(scene.BackgroundMedia.Source)
				}
				if len(data) > 0 {
					background := pptx.Object{ID: fmt.Sprintf("keynope-background-image-%d", index), Kind: "image", X: 0, Y: 0, Width: scene.Width, Height: scene.Height, Image: data, ImageMIME: mime}
					if projected := scene.BackgroundMedia; projected != nil {
						framing := projected.sceneMedia
						framing.Fit = projected.Fit
						applyPPTXImageFraming(&background, &framing)
						applyPPTXImageMask(&background, framing.Mask)
						opacity := projected.Opacity
						background.ImageOpacity = &opacity
						if projected.Paint != nil {
							warnings := applyPPTXPicturePaint(&background, *projected.Paint)
							for i := range warnings {
								warnings[i].Slide = index
							}
							report.Warnings = append(report.Warnings, warnings...)
						}
					}
					if asset.Vector != nil {
						background.ImageSVG = append([]byte(nil), asset.Vector.Data...)
					}
					if scene.BackgroundMedia != nil {
						report.Warnings = append(report.Warnings, exportPPTXMediaTreatment(&background, &scene.BackgroundMedia.sceneMedia, index, colourCache, work)...)
					}
					slide.Objects = append(slide.Objects, background)
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
			if err := work.check(); err != nil {
				return presentation, report, err
			}
			collectPPTXC64Width(c64Widths, object.Text)
			collectPPTXC64Width(c64Widths, object.Label)
			if object.Chart != nil {
				for _, label := range object.Chart.Labels {
					collectPPTXC64Width(c64Widths, label.Text)
				}
			}
			if object.Table != nil {
				for _, cell := range object.Table.Cells {
					collectPPTXC64Width(c64Widths, cell.Text)
				}
			}
			_, capturedModern := work.captures[pptxCaptureKey(index, object.ID)]
			capturedModern = capturedModern && pptxModernNeedsPaintCapture(object)
			converted, warnings := sceneObjectToPPTX(object, work)
			if converted != nil && converted.Kind == "image" && converted.ImageMIME == svgMediaType {
				for _, authored := range resolved.Elements {
					if authored.ID != object.ID {
						continue
					}
					if asset, ok := deck.Assets[authored.AssetID]; ok && asset.Vector != nil && asset.Source != nil {
						converted.ImageSVG = append([]byte(nil), asset.Vector.Data...)
						converted.Image, converted.ImageMIME = asset.Source.Data, asset.Source.MIME
					}
				}
				if converted.ImageMIME == svgMediaType {
					converted = nil
					warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "svg-fallback-unavailable", Message: "Vector image had no validated PNG fallback and was omitted."})
				}
			}
			if converted != nil && converted.Kind == "image" && object.RetroLines == nil && object.RetroText == nil && !capturedModern {
				warnings = append(warnings, exportPPTXMediaTreatment(converted, object.Media, index, colourCache, work)...)
			}
			for i := range warnings {
				warnings[i].Slide = index
				report.Warnings = append(report.Warnings, warnings[i])
			}
			if converted != nil {
				slide.Objects = append(slide.Objects, *converted)
			}
		}
		for _, warning := range exportPPTXConnections(&slide, resolved, scene, attachments, cols, rows) {
			warning.Slide = index
			report.Warnings = append(report.Warnings, warning)
		}
		if err := work.check(); err != nil {
			return presentation, report, err
		}
		for id, face := range scene.Typefaces {
			usedTypefaces[id] = face
		}
		presentation.Slides = append(presentation.Slides, slide)
	}
	if len(presentation.Slides) == 0 {
		return presentation, report, fmt.Errorf("there are no presentation slides to export")
	}
	if len(work.captures) != 0 {
		return presentation, report, fmt.Errorf("unexpected PowerPoint artwork capture target; prepare export again")
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
			if err := work.check(); err != nil {
				return presentation, report, err
			}
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
	ids := make([]string, 0, len(usedTypefaces))
	for id := range usedTypefaces {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		face := usedTypefaces[id]
		if face.LocalOnly || face.referenceOnly() || len(sfntTable(face.Data, "glyf")) == 0 {
			report.Warnings = append(report.Warnings, pptx.Warning{Code: "font-not-embedded", Message: face.Name + " is referenced by name, but cannot be embedded in editable PowerPoint output. Install it on the destination computer."})
			continue
		}
		presentation.EmbeddedFonts = append(presentation.EmbeddedFonts, pptx.EmbeddedFont{Family: face.PostScriptName, Data: face.Data})
		if !editableTypefaceEmbedding(face.Data) {
			report.Warnings = append(report.Warnings, pptx.Warning{Code: "font-embedding-restricted", Message: face.Name + " was embedded with its original permission flags. PowerPoint or other readers may restrict editing or substitute this font."})
		}
	}
	return presentation, report.Normalized(), nil
}

func collectPPTXC64Width(widths map[string]float64, text *sceneText) {
	if text == nil {
		return
	}
	scale := text.WidthScale
	if scale <= 0 {
		scale = 1
	}
	if text.FontID == "c64" {
		widths[pptxC64FontFamily(scale)] = scale
	}
	collect := func(runs []sceneRun) {
		for _, run := range runs {
			if run.FontID == "c64" {
				widths[pptxC64FontFamily(scale)] = scale
			}
		}
	}
	collect(text.Runs)
	for _, paragraph := range text.Paragraphs {
		collect(paragraph.Runs)
	}
}

func sceneObjectToPPTX(object sceneObject, work ...*pptxExportWork) (*pptx.Object, []pptx.Warning) {
	warnings := []pptx.Warning{}
	if object.Kind == "video" {
		object.Kind = "image"
		object.Video = nil
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "video-poster-only", Message: "Embedded video was exported as its still poster. Use Keynope or HTML for playback."})
	}
	if object.Kind == "shape" && object.Paint.StrokePosition != "" {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "stroke-position-approximated", Message: "Inside/outside stroke alignment uses a centred editable PowerPoint stroke; HTML and PDF preserve the original outline."})
	}
	if object.Kind != "image" && (object.Paint.Effects != nil || object.Paint.GradientImage != "") {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "object-effects-omitted", Message: "Object remains editable; additional colour, glow, blur or non-linear gradient effects require HTML or PDF for exact appearance."})
	}
	if object.Kind == "image" && (object.Paint.Effects != nil || object.Paint.GradientImage != "") {
		state := firstPPTXWork(work)
		if state == nil || state.captures == nil {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "object-effects-omitted", Message: "Image effects were not captured for this export. Export through the editor for rendered image treatments, or use PDF/HTML."})
			// The direct/headless fallback has the legacy 64-pixel raster border,
			// not the larger native capture border. Do not misposition that image.
			object.Paint.Effects, object.Paint.GradientImage = nil, ""
		}
	}
	if pptxModernNeedsPaintCapture(object) {
		if state := firstPPTXWork(work); state != nil && state.captures != nil {
			capture, ok := state.captures[pptxCaptureKey(state.slide, object.ID)]
			if !ok {
				state.err = fmt.Errorf("required PowerPoint image-effect capture is missing for %s; prepare export again", object.ID)
				return nil, nil
			}
			delete(state.captures, pptxCaptureKey(state.slide, object.ID))
			picture, notes, err := capturedPPTXRetroImage(object, capture, state)
			if err != nil {
				state.err = err
				return nil, nil
			}
			return picture, notes
		}
	}
	if object.Kind == "image" && object.RetroLines != nil {
		if state := firstPPTXWork(work); state != nil && state.captures != nil {
			if capture, ok := state.captures[pptxCaptureKey(state.slide, object.ID)]; ok {
				delete(state.captures, pptxCaptureKey(state.slide, object.ID))
				picture, notes, err := capturedPPTXRetroImage(object, capture, state)
				if err != nil {
					state.err = err
					return nil, nil
				}
				return picture, notes
			}
			if pptxRetroNeedsFontCapture(object) {
				state.err = fmt.Errorf("required PowerPoint artwork capture is missing for %s; prepare export again", object.ID)
				return nil, nil
			}
		}
		picture, report, err := rasterPPTXRetroImage(object, firstPPTXWork(work))
		if err == nil {
			return picture, append(warnings, report...)
		}
		return nil, []pptx.Warning{{Object: object.ID, Code: "retro-image-capture-unavailable", Message: "Rendered Retro artwork could not be captured and was omitted: " + err.Error() + ". Use exact-appearance PDF/HTML or change this image to Modern."}}
	}
	base := pptx.Object{ID: object.ID, X: object.Bounds.X, Y: object.Bounds.Y, Width: object.Bounds.Width, Height: object.Bounds.Height, Rotation: object.Rotation, Link: object.Link}
	base.Name = object.Name
	if object.Paint.Opacity != nil {
		base.Opacity, base.OpacitySet = *object.Paint.Opacity, true
	}
	if object.RetroLines != nil || object.RetroText != nil || object.RetroLabel != nil {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "retro-artwork-omitted", Message: "Retro sampled artwork needs exact-appearance capture and was not included in editable export."})
		if object.Text == nil && object.Label == nil {
			return nil, warnings
		}
	}
	switch object.Kind {
	case "chart":
		if object.Chart == nil {
			return nil, append(warnings, pptx.Warning{Object: object.ID, Code: "chart-data-missing", Message: "Chart has no projected data and was omitted."})
		}
		base.Kind = "chart"
		base.Chart = sceneChartToPPTX(object.Chart)
		for _, label := range object.Chart.Labels {
			if label.Text != nil && label.Text.FontID != "c64" && math.Abs(label.Text.WidthScale-1) > 1e-9 {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "chart-font-width-approximated", Message: "Chart labels remain editable; horizontal scaling of non-C64 fonts is approximated in Office."})
				break
			}
		}
		if chartHasColorTokens(object.Chart.Data) {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "chart-theme-colours-frozen", Message: "Chart colour roles were exported as their current editable RGB colours."})
		}
		if object.Chart.Data.Options.FontRole != "" {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "chart-theme-font-frozen", Message: "The chart font role was exported as its current editable font family."})
		}
		if object.Paint.Opacity != nil || object.Paint.GradientStart != "" || object.Paint.ShadowColor != "" {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "chart-effects-approximated", Message: "Chart-level effects were omitted; chart data remains editable."})
		}
		return &base, warnings
	case "table":
		if object.Table == nil {
			return nil, append(warnings, pptx.Warning{Object: object.ID, Code: "table-data-missing", Message: "Table has no projected cell data and was omitted."})
		}
		base.Kind = "table"
		var varying bool
		base.Table, varying = sceneTableToPPTXSegments(object.Table)
		for _, cell := range object.Table.Cells {
			if cell.Text.FontID != "c64" && math.Abs(cell.Text.WidthScale-1) > 1e-9 {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "table-font-width-approximated", Message: "Table text remains editable; horizontal scaling of non-C64 fonts is approximated in Office."})
				break
			}
		}
		for _, cell := range object.Table.Cells {
			if hasWordColorTokens(cell.Text) {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "word-theme-colours-frozen", Message: "Table word theme roles were exported as their current editable RGB colours."})
				break
			}
		}
		for _, cell := range object.Table.Cells {
			if hasWordFontRoles(cell.Text) {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "word-theme-fonts-frozen", Message: "Table word font roles were exported as their current editable font families."})
				break
			}
		}
		if varying {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "table-border-segments-reader-dependent", Message: "Merged perimeter segments are preserved in the editable table. Some PowerPoint readers may display continuation-cell borders differently; check their appearance in your target application."})
		}
		if object.Paint.Opacity != nil || object.Paint.GradientStart != "" || object.Paint.ShadowColor != "" {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "table-effects-approximated", Message: "Table-level effects were omitted; cells remain editable."})
		}
		return &base, warnings
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
		base.TextEffects = pptxTextEffects(object.Paint, 1)
		base.Paragraphs = sceneTextToPPTXParagraphs(object.Text)
		if hasWordFontRoles(object.Text) {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "word-theme-fonts-frozen", Message: "Word font roles were exported as their current editable font families."})
		}
		if hasWordColorTokens(object.Text) {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "word-theme-colours-frozen", Message: "Word theme roles were exported as their current editable RGB colours."})
		}
		warnings = append(warnings, normalizePPTXTextSizes(&base)...)
		if object.Text.Role == "code" {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "code-container-omitted", Message: "Code remains editable text with its word styles; the code panel background and code-specific editing role were omitted."})
		}
		if object.Text.SeeThrough {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "text-effects-approximated", Message: "Text remains editable with its word styles and character paint; see-through treatment was omitted."})
		}
		if object.Paint.GradientStart != "" {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "text-gradient-reader-dependent", Message: "Text gradient is retained as editable character paint; gradient continuity across runs and paragraphs may differ in other viewers."})
		}
		if object.Text.FontID != "c64" && object.Text.WidthScale != 1 {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "font-width-approximated", Message: "Keynope horizontal font scaling is not an Office text metric and was approximated."})
		}
		return &base, warnings
	case "shape":
		base.Kind = "shape"
		base.Shape = pptxShape(object.Shape)
		adjustments, mapped := exportShapeAdjustments(object)
		base.ShapeAdjustments = adjustments
		if !mapped {
			if geometry, ok := customPPTXGeometry(object.ShapeGeometry); ok {
				base.CustomGeometry, base.Shape = geometry, "custom"
				base.ShapeAdjustments = customPPTXShapeMarkers(object.ShapeGeometry.Spec)
				mapped = true
			}
		}
		if object.ShapeGeometry != nil && len(object.ShapeGeometry.Spec.Adjustments) > 0 && !mapped {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "shape-adjustments-approximated", Message: "Shape remains editable; its custom adjustment parameters use the PowerPoint preset defaults."})
		} else if !mapped && (object.Shape == "pill" || object.Shape == "cloud") {
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "shape-geometry-approximated", Message: "Shape remains editable; the PowerPoint preset outline differs from Keynope's geometry."})
		}
		base.Fill = object.Paint.Color
		if object.Paint.NoFill {
			base.Fill = ""
		}
		if !object.Paint.NoFill && object.Paint.GradientStart != "" {
			base.Gradient = &pptx.ShapeGradient{Start: object.Paint.GradientStart, End: object.Paint.GradientEnd, Direction: object.Paint.GradientDirection}
		}
		if object.Paint.ShadowColor != "" {
			base.Shadow = &pptx.ShapeShadow{Color: object.Paint.ShadowColor, X: object.Paint.ShadowX, Y: object.Paint.ShadowY, Blur: object.Paint.ShadowBlur}
		}
		base.Stroke = object.Paint.Stroke
		// Scene pixels and PPTX points use the same 2:1 conversion as bounds.
		base.StrokeWidth = object.Paint.StrokeWidth / 2
		if object.Paint.StrokeWidth <= 0 {
			// Width zero is explicit Off in Keynope, not Office's hairline.
			base.Stroke = ""
		}
		if base.CustomGeometry != nil && object.Shape == "database" && base.Stroke == "" {
			base.CustomGeometry.Paths[0].NoStroke = true
			base.Stroke, base.StrokeWidth = "#202833", .75
		}
		if object.Label != nil {
			base.Text = sceneTextPlain(object.Label)
			base.FontFace = pptxFontFace(object.Label)
			base.FontSize = pptxTextFontSize(object.Label)
			base.TextAlign = object.Label.Align
			base.VerticalAlign = object.Label.Vertical
			base.Paragraphs = sceneTextToPPTXParagraphs(object.Label)
			if hasWordColorTokens(object.Label) {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "word-theme-colours-frozen", Message: "Label word theme roles were exported as their current editable RGB colours."})
			}
			if hasWordFontRoles(object.Label) {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "word-theme-fonts-frozen", Message: "Label word font roles were exported as their current editable font families."})
			}
			warnings = append(warnings, normalizePPTXTextSizes(&base)...)
			// Defaults remain distinct from editable word overrides.
			runs := append([]sceneRun(nil), object.Label.Runs...)
			for _, paragraph := range object.Label.Paragraphs {
				runs = append(runs, paragraph.Runs...)
			}
			bold, italic, visible := true, true, false
			for _, run := range runs {
				if run.Text == "" {
					continue
				}
				visible = true
				bold = bold && run.Bold
				italic = italic && run.Italic
			}
			base.Bold, base.Italic = visible && bold, visible && italic
			if object.Label.FontID != "c64" && object.Label.WidthScale != 1 {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "shape-label-width-approximated", Message: "Shape label remains editable; horizontal font scaling is approximated in Office."})
			}
			if object.LabelPaint != nil {
				base.Color = object.LabelPaint.Color
				alpha := 1.0
				if object.Paint.Opacity != nil {
					alpha = *object.Paint.Opacity
				}
				base.TextEffects = pptxTextEffects(*object.LabelPaint, alpha)
				if object.LabelPaint.GradientStart != "" {
					warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "text-gradient-reader-dependent", Message: "Shape-label gradient is retained as editable character paint; gradient continuity across runs and paragraphs may differ in other viewers."})
				}
			} else {
				base.Color = object.Paint.Color
			}
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
		warnings = append(warnings, applyPPTXPicturePaint(&base, object.Paint)...)
		if object.Paint.Opacity != nil {
			opacity := *object.Paint.Opacity
			base.ImageOpacity = &opacity
		}
		applyPPTXImageFraming(&base, object.Media)
		applyPPTXImageMask(&base, object.Media.Mask)
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
		if base.Stroke == "" {
			// Native/HTML connectors fall back to their resolved foreground.
			base.Stroke = object.Paint.Color
		}
		base.StrokeWidth = object.Paint.StrokeWidth / 2
		if len(object.Points) > 2 {
			for _, point := range object.Points {
				base.LinePoints = append(base.LinePoints, pptx.LinePoint{X: point.X, Y: point.Y})
			}
		}
		if object.Arrows != "" && object.Arrows != "none" {
			size := "med"
			ratio := object.ArrowWidth / math.Max(1, object.Paint.StrokeWidth)
			if ratio < 3 {
				size = "sm"
			} else if ratio > 5 {
				size = "lg"
			}
			end := pptx.LineEnd{Type: "triangle", Width: size, Length: size}
			if object.Arrows == "start" || object.Arrows == "both" {
				base.LineHead = end
			}
			if object.Arrows == "end" || object.Arrows == "both" {
				base.LineTail = end
			}
			warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "connector-arrow-size-approximated", Message: "Arrowheads remain editable; their dimensions use the closest small, medium or large Office size."})
		}
		return &base, warnings
	default:
		return nil, append(warnings, pptx.Warning{Object: object.ID, Code: "unsupported-object", Message: "Object is not supported by editable PowerPoint export."})
	}
}

func applyPPTXImageMask(object *pptx.Object, mask string) {
	switch mask {
	case "ellipse":
		object.Shape = "ellipse"
	case "rounded":
		object.Shape = "roundRect"
	}
}

func pptxTextEffects(paint scenePaint, parentAlpha float64) *pptx.TextEffects {
	if paint.Stroke == "" && paint.ShadowColor == "" && paint.Opacity == nil && paint.GradientStart == "" {
		return nil
	}
	effects := &pptx.TextEffects{Stroke: paint.Stroke, StrokeWidth: paint.StrokeWidth / 2}
	if paint.GradientStart != "" {
		effects.Gradient = &pptx.ShapeGradient{Start: paint.GradientStart, End: paint.GradientEnd, Direction: paint.GradientDirection}
	}
	if paint.Opacity != nil {
		value := *paint.Opacity * parentAlpha
		effects.Opacity = &value
	}
	if paint.ShadowColor != "" {
		effects.Shadow = &pptx.ShapeShadow{Color: paint.ShadowColor, X: paint.ShadowX, Y: paint.ShadowY, Blur: paint.ShadowBlur}
	}
	return effects
}

// Keep source pixels editable. Cover folds its centred trim into srcRect;
// contain insets the destination fill, preserving the full mask and frame.
func applyPPTXImageFraming(object *pptx.Object, media *sceneMedia) {
	c := sceneCrop{}
	if media.Crop != nil {
		c = *media.Crop
	}
	fit := media.Fit
	if fit == "" && media.Crop != nil {
		fit = "cover"
	}
	w, h := float64(media.Width)*(1-c.Left-c.Right), float64(media.Height)*(1-c.Top-c.Bottom)
	if w > 0 && h > 0 && object.Width > 0 && object.Height > 0 {
		if fit == "contain" {
			scale := math.Min(object.Width/w, object.Height/h)
			nw, nh := w*scale, h*scale
			object.FillLeft = (object.Width - nw) / (2 * object.Width)
			object.FillRight = object.FillLeft
			object.FillTop = (object.Height - nh) / (2 * object.Height)
			object.FillBottom = object.FillTop
		} else if fit == "cover" {
			scale := math.Max(object.Width/w, object.Height/h)
			dx := (w - object.Width/scale) / 2 / float64(media.Width)
			dy := (h - object.Height/scale) / 2 / float64(media.Height)
			c.Left += dx
			c.Right += dx
			c.Top += dy
			c.Bottom += dy
		}
	}
	object.CropLeft, object.CropTop, object.CropRight, object.CropBottom = c.Left, c.Top, c.Right, c.Bottom
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
	if face, ok := text.TypefaceMetrics[text.FontID]; ok {
		return face.PostScriptName
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
	if d, ok := shapeDefinitionFor(shape); ok {
		return d.Preset
	}
	return "rect"
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
	return importPPTXPresentation(presentation, report)
}

func importPPTXPresentation(presentation pptx.Presentation, report pptx.Report) (Deck, pptx.Report, error) {
	if len(presentation.Slides) == 0 {
		return Deck{}, report, fmt.Errorf("PowerPoint presentation has no slides")
	}
	masters := defaultMasterDeck()
	// A PowerPoint deck already owns its scene chrome. Do not introduce the
	// New-document master’s terminal page number into every imported slide.
	masters.Base.Slide.PageNumber = pageNumberHide
	masters.Base.Slide.Elements = nil
	deck := Deck{Assets: map[string]DeckAsset{}, Masters: masters}
	width, height := float64(presentation.Width)/6350, float64(presentation.Height)/6350
	size := &DeckPageSize{Version: 1, Width: int(math.Round(width)), Height: int(math.Round(height))}
	if err := size.Validate(); err != nil {
		return Deck{}, report, fmt.Errorf("unsupported PowerPoint page size: %w", err)
	}
	if size.Width != 1920 || size.Height != 1080 {
		deck.PageSize = size
	}
	canvas := sceneRect{Width: float64(size.Width), Height: float64(size.Height)}
	if math.Abs(width-canvas.Width) > .000001 || math.Abs(height-canvas.Height) > .000001 {
		report.Warnings = append(report.Warnings, pptx.Warning{Code: "page-size-rounded", Message: "PowerPoint page dimensions were rounded to the nearest physical design pixel."})
	}
	themeBinding := newPPTXThemeBinding(presentation.Theme)
	chartRecoveryBytes := 0
	fit, offsetX, offsetY := 1.0, 0.0, 0.0
	for slideIndex, sourceSlide := range presentation.Slides {
		slide := Slide{LayoutID: "blank", Elements: []Element{}}
		timingIDs := map[string]string{}
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
		importedIndices := map[int]int{}
		for objectIndex, sourceObject := range sourceSlide.Objects {
			importID := fmt.Sprintf("pptx-s%d-o%d", slideIndex+1, objectIndex+1)
			element, warnings, asset, err := pptxObjectToElementOnCanvas(sourceObject, fit, offsetX, offsetY, canvas, themeBinding)
			element = themeBinding.element(sourceObject, element)
			for i := range warnings {
				warnings[i].Slide = slideIndex
				warnings[i].Object = importID
				report.Warnings = append(report.Warnings, warnings[i])
			}
			if err != nil {
				return Deck{}, report, fmt.Errorf("import slide %d: %w", slideIndex+1, err)
			}
			if asset != nil {
				deck.Assets[element.AssetID] = *asset
			}
			if element.Kind != "" {
				element.ID = importID
				if name := strings.TrimSpace(sourceObject.Name); name != "" && validObjectName(name) {
					element.Query = setQueryValue(element.Query, "object-name", name)
				} else if name != "" {
					report.Warnings = append(report.Warnings, pptx.Warning{Slide: slideIndex, Object: importID, Code: "object-name-omitted", Message: "Object name was omitted because it exceeds 128 characters or contains control/line separator characters. The object remains editable."})
				}
				if sourceObject.Kind == "chart" {
					chart, chartWarnings, err := pptxChartToData(sourceObject.Chart)
					if err != nil {
						return Deck{}, report, fmt.Errorf("import chart on slide %d: %w", slideIndex+1, err)
					}
					chart, err = themeBinding.chart(deck, sourceObject.Chart, &element, chart)
					if err != nil {
						return Deck{}, report, fmt.Errorf("import chart defaults on slide %d: %w", slideIndex+1, err)
					}
					if deck.Objects == nil {
						deck.Objects = &DeckObjects{Version: 1}
					}
					if deck.Objects.Charts == nil {
						deck.Objects.Charts = map[string]ChartData{}
					}
					element.ObjectID = fmt.Sprintf("pptx-chart-s%d-o%d", slideIndex+1, objectIndex+1)
					deck.Objects.Charts[element.ObjectID] = chart
					for _, warning := range chartWarnings {
						warning.Slide = slideIndex
						warning.Object = element.ObjectID
						report.Warnings = append(report.Warnings, warning)
					}
				}
				if sourceObject.Kind == "table" {
					table, tableWarnings, err := pptxTableToData(sourceObject.Table, fit, themeBinding)
					if err != nil {
						return Deck{}, report, fmt.Errorf("import table on slide %d: %w", slideIndex+1, err)
					}
					for i := range tableWarnings {
						tableWarnings[i].Slide = slideIndex
						tableWarnings[i].Object = element.ID
					}
					report.Warnings = append(report.Warnings, tableWarnings...)
					if deck.Objects == nil {
						deck.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{}}
					}
					if deck.Objects.Tables == nil {
						deck.Objects.Tables = map[string]TableData{}
					}
					element.ObjectID = fmt.Sprintf("pptx-table-s%d-o%d", slideIndex+1, objectIndex+1)
					deck.Objects.Tables[element.ObjectID] = table
				}
				// PowerPoint's cNvPr IDs are scoped to an individual shape tree;
				// layouts/masters may therefore repeat one inside a single resolved
				// slide and every slide begins its own sequence. Keynope scene views
				// retain DOM nodes by ID, so import IDs must be document-unique.
				// Without this, thumbnails can fail and a previous slide's retained
				// object can visibly replace a different slide after reordering.
				element.ID = importID
				if !sourceObject.Inherited {
					if _, duplicate := timingIDs[sourceObject.ID]; duplicate {
						timingIDs[sourceObject.ID] = ""
					} else {
						timingIDs[sourceObject.ID] = element.ID
					}
				}
				if len(sourceObject.ChartSource) > 0 {
					if chartRecoveryBytes+len(sourceObject.ChartSource) > 4<<20 {
						report.Warnings = append(report.Warnings, pptx.Warning{Slide: slideIndex, Object: element.ID, Code: "chart-source-budget-exceeded", Message: "Unsupported chart recovery data exceeds the deck's 4 MiB limit and was not retained."})
					} else {
						chartRecoveryBytes += len(sourceObject.ChartSource)
						if deck.Objects == nil {
							deck.Objects = &DeckObjects{Version: 1}
						}
						if deck.Objects.Extra == nil {
							deck.Objects.Extra = &jsonExtensions{}
						}
						payload, _ := json.Marshal(map[string]string{"source": "PowerPoint chart XML", "base64": base64.StdEncoding.EncodeToString(sourceObject.ChartSource)})
						(*deck.Objects.Extra)["pptxChartSource-"+element.ID] = payload
					}
				}
				// Markdown is spatially normalised on save, while PowerPoint's
				// shape tree order is its paint order. Preserve the latter
				// explicitly so imported chrome never rises above slide content.
				element.Query = setQueryValue(element.Query, "z-index", strconv.Itoa(objectIndex))
				importedIndices[objectIndex] = len(slide.Elements)
				slide.Elements = append(slide.Elements, element)
			}
		}
		restored, connectionWarnings := importPPTXConnections(sourceSlide, &slide, importedIndices, fit, offsetX, offsetY, canvas)
		report.Warnings = removeRestoredLineWarnings(report.Warnings, restored, slideIndex)
		for _, warning := range connectionWarnings {
			warning.Slide = slideIndex
			report.Warnings = append(report.Warnings, warning)
		}
		if sourceSlide.Notes != "" {
			slide.Notes = sourceSlide.Notes
		}
		if sourceSlide.Timeline != nil {
			slide.ID = fmt.Sprintf("pptx-slide-%d", slideIndex+1)
			timeline := sourceSlide.Timeline
			build := SlideBuild{Version: 1, Transition: SlideTransition{Effect: timeline.Transition.Effect, Direction: timeline.Transition.Direction, DurationMS: timeline.Transition.DurationMS}}
			for _, step := range timeline.Steps {
				targets := []string{}
				for _, id := range step.TargetIDs {
					if target := timingIDs[id]; target != "" {
						targets = append(targets, target)
					} else {
						report.Warnings = append(report.Warnings, pptx.Warning{Slide: slideIndex, Object: id, Code: "build-target-omitted", Message: "Entrance target was not imported as an editable object; its build target was omitted."})
					}
				}
				if len(targets) > 0 {
					build.Steps = append(build.Steps, BuildStep{TargetIDs: targets, Effect: step.Effect, Direction: step.Direction, DurationMS: step.DurationMS, Advance: step.Advance, DelayMS: step.DelayMS})
				}
			}
			if len(build.Steps) > 0 || build.Transition.Effect != "cut" && build.Transition.Effect != "" {
				if err := build.Validate(); err != nil {
					return Deck{}, report, fmt.Errorf("invalid imported timeline: %w", err)
				}
				if deck.Objects == nil {
					deck.Objects = &DeckObjects{Version: 1}
				}
				if deck.Objects.Builds == nil {
					deck.Objects.Builds = map[string]SlideBuild{}
				}
				deck.Objects.Builds[slide.ID] = build
			}
		}
		if sourceSlide.Hidden {
			report.Warnings = append(report.Warnings, pptx.Warning{Slide: slideIndex, Code: "hidden-slide-visible", Message: "PowerPoint hidden slide was imported as an ordinary editable Keynope slide."})
		}
		deck.Slides = append(deck.Slides, slide)
	}
	ensureNativeEditorElementIDs(&deck)
	canonicalizeConcreteAppearance(&deck)
	standardizeDeckText(&deck)
	if themeBinding != nil {
		deck.Theme = themeBinding.theme.Clone()
	}
	return deck, report.Normalized(), nil
}

func pptxObjectToElement(object pptx.Object, scale, offsetX, offsetY float64, bindings ...*pptxThemeBinding) (Element, []pptx.Warning, *DeckAsset, error) {
	return pptxObjectToElementOnCanvas(object, scale, offsetX, offsetY, sceneRect{Width: 1920, Height: 1080}, bindings...)
}

func pptxObjectToElementOnCanvas(object pptx.Object, scale, offsetX, offsetY float64, canvas sceneRect, bindings ...*pptxThemeBinding) (Element, []pptx.Warning, *DeckAsset, error) {
	warnings := []pptx.Warning{}
	binding := pptxBinding(bindings)
	x, y, w, h := object.X*scale+offsetX, object.Y*scale+offsetY, object.Width*scale, object.Height*scale
	if object.Kind == "line" || (object.Kind == "shape" && strings.EqualFold(object.Shape, "line")) {
		return pptxLineToElement(object, x, y, w, h, scale, canvas)
	}
	if w <= 0 || h <= 0 {
		return Element{}, warnings, nil, nil
	}
	query := pptxSceneBoundsQuery(x, y, w, h, canvas)
	if object.Rotation != 0 {
		query.Set("object-rotation", decimal(object.Rotation))
	}
	if object.OpacitySet || object.Opacity > 0 && object.Opacity < 1 {
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
	case "chart":
		if object.Chart == nil {
			return Element{}, warnings, nil, fmt.Errorf("missing PowerPoint chart payload")
		}
		if object.Chart.Color != "" {
			query.Set("fg", object.Chart.Color)
		}
		if object.Chart.FontFace != "" {
			query.Set("modern-font", importFont(object.Chart.FontFace))
		}
		return Element{ID: id, Kind: "chart", Text: "[chart]", Query: query.Encode()}, warnings, nil, nil
	case "table":
		if object.Table == nil {
			return Element{}, warnings, nil, fmt.Errorf("missing PowerPoint table payload")
		}
		return Element{ID: id, Kind: "table", Text: "[table]", Query: query.Encode()}, warnings, nil, nil
	case "text":
		warnings = append(warnings, applyPPTXTextEffects(query, object, scale, false, canvas)...)
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
		element, runWarnings, err := importPPTXTextRuns(object, Element{ID: id, Kind: "text", Text: object.Text, Query: query.Encode()}, scale, binding)
		return element, append(warnings, runWarnings...), nil, err
	case "shape":
		query.Set("shape", importShape(object.Shape))
		if object.Shape == "custom" {
			if !restoreCustomPPTXShape(object, query) {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "custom-shape-geometry-approximated", Message: "Foreign or edited freeform shape is outside Keynope's editable catalogue and uses a rectangle; its custom path was not interpreted as a catalogue shape."})
			} else if query.Get("shape") == "database" && object.CustomGeometry.Paths[0].NoStroke {
				object.Stroke, object.StrokeWidth = "", 0
			}
		} else {
			warnings = append(warnings, importShapeAdjustments(object, query)...)
		}
		if object.Fill != "" {
			query.Set("fg", object.Fill)
		} else if object.Gradient == nil {
			query.Set("shape-fill", "none")
		}
		if g := object.Gradient; g != nil {
			query.Set("gradient-start", g.Start)
			query.Set("gradient-end", g.End)
			query.Set("gradient-dir", g.Direction)
		}
		if shadow := object.Shadow; shadow != nil {
			if shadow.Opacity != nil && math.Abs(*shadow.Opacity-object.Opacity) > 1e-6 {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "shape-shadow-opacity-approximated", Message: "Independent shadow opacity follows the containing shape in Keynope."})
			}
			sx, sy := canvas.Width/245.0, canvas.Height/56.0
			x, y := clampInt(int(math.Round(shadow.X*scale/sx)), -4, 4), clampInt(int(math.Round(shadow.Y*scale/sy)), -4, 4)
			mode := "solid"
			blur := 0.0
			if shadow.Blur > 0 {
				mode = "soft"
				blur = 8
			}
			if x == 0 && y == 0 {
				x, y = 1, 1
			}
			query.Set("shadow", mode)
			query.Set("shadow-color", shadow.Color)
			query.Set("shadow-x", strconv.Itoa(x))
			query.Set("shadow-y", strconv.Itoa(y))
			if math.Abs(float64(x)*sx-shadow.X*scale) > .001 || math.Abs(float64(y)*sy-shadow.Y*scale) > .001 || math.Abs(blur-shadow.Blur*scale) > .001 {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "shape-shadow-approximated", Message: "Shadow colour is retained; offset and blur use the closest supported Keynope shadow settings."})
			}
		}
		if object.Stroke != "" {
			query.Set("shape-stroke", object.Stroke)
			width := object.StrokeWidth * 2 * scale
			if width > 64 {
				width = 64
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "shape-stroke-width-clamped", Message: "Shape stroke exceeded the supported 64 scene-pixel width and was clamped."})
			}
			query.Set("shape-stroke-width", decimal(width))
		} else {
			query.Set("shape-stroke", "none")
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
			// Character alpha is composed in DrawingML; recover the independent
			// label multiplier rather than applying the shape alpha twice.
			warnings = append(warnings, applyPPTXTextEffects(labelQuery, object, scale, true, canvas)...)
			if object.Bold || object.Italic {
				if encoded, err := encodeModernRuns(object.Text, "", "text", []sceneRun{{Text: object.Text, Bold: object.Bold, Italic: object.Italic, Color: object.Color}}); err == nil && encoded != "" {
					labelQuery.Set("modern-runs", encoded)
				}
			}
			label, runWarnings, err := importPPTXTextRuns(object, Element{Kind: "text", Text: object.Text, Query: labelQuery.Encode()}, scale, binding)
			warnings = append(warnings, runWarnings...)
			if err != nil {
				return Element{}, warnings, nil, err
			}
			encoded, err := json.Marshal(shapeLabelData{Text: label.Text, Query: label.Query})
			if err != nil || len(encoded) > 65536 {
				return Element{}, warnings, nil, fmt.Errorf("PowerPoint shape label exceeds supported storage bounds")
			}
			query.Set("shape-label", base64.StdEncoding.EncodeToString(encoded))
		}
		return Element{ID: id, Kind: "shape", Text: "[shape:" + importShape(object.Shape) + "]", Query: query.Encode()}, warnings, nil, nil
	case "image":
		var assetID, pathValue string
		warnings = append(warnings, importPPTXPictureShadow(object, query, scale, canvas)...)
		if object.ImageOpacity != nil {
			query.Set("modern-opacity", decimal(*object.ImageOpacity))
		}
		switch object.Shape {
		case "ellipse":
			query.Set("modern-mask", "ellipse")
		case "roundRect":
			query.Set("modern-mask", "rounded")
		}
		var asset DeckAsset
		var err error
		if object.FlipH || object.FlipV {
			vector := len(object.ImageSVG) > 0
			if err := reflectPPTXImage(&object); err != nil {
				return Element{}, warnings, nil, fmt.Errorf("could not reflect imported image: %w", err)
			}
			if vector {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "reflected-svg-rasterized", Message: "The reflected SVG was imported using its raster fallback."})
			}
		}
		if len(object.ImageSVG) > 0 {
			assetID, asset, pathValue, err = embeddedSVGAsset(context.Background(), object.ImageSVG)
			if err != nil {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "svg-source-refused", Message: "PowerPoint SVG source was unsafe or unsupported; its PNG fallback was imported."})
			}
		}
		if len(object.ImageSVG) == 0 || err != nil {
			assetID, asset, pathValue, err = embeddedImageAsset(object.Image)
		}
		if err != nil {
			return Element{}, warnings, nil, err
		}
		query.Set("image-style", "modern")
		// Images otherwise retain the sampled legacy image dimensions during
		// layout. Imported PowerPoint picture bounds are explicit scene bounds.
		query.Set("stretch", "1")
		if object.FillLeft != 0 || object.FillTop != 0 || object.FillRight != 0 || object.FillBottom != 0 {
			width, height := asset.Width, asset.Height
			if asset.Source != nil {
				width, height = asset.Source.Width, asset.Source.Height
			}
			if asset.Vector != nil {
				width, height = asset.Vector.Width, asset.Vector.Height
			}
			if pptxPictureUsesContain(object, width, height) {
				query.Set("image-fit", "contain")
			} else {
				warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "image-destination-approximated", Message: "Custom image destination insets could not be represented; the image uses the full frame."})
			}
		}
		if object.CropLeft != 0 || object.CropTop != 0 || object.CropRight != 0 || object.CropBottom != 0 {
			query.Set("modern-crop", strings.Join([]string{decimal(object.CropLeft), decimal(object.CropTop), decimal(object.CropRight), decimal(object.CropBottom)}, ","))
		}
		return Element{ID: id, Kind: "image", AssetID: assetID, Path: pathValue, Query: query.Encode()}, warnings, &asset, nil
	default:
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "unsupported-object", Message: "PowerPoint object was not imported."})
		return Element{}, warnings, nil, nil
	}
}

func pptxPictureUsesContain(object pptx.Object, width, height int) bool {
	if width <= 0 || height <= 0 || object.Width <= 0 || object.Height <= 0 {
		return false
	}
	expected := pptx.Object{Width: object.Width, Height: object.Height}
	applyPPTXImageFraming(&expected, &sceneMedia{Width: width, Height: height, Fit: "contain", Crop: &sceneCrop{Left: object.CropLeft, Top: object.CropTop, Right: object.CropRight, Bottom: object.CropBottom}})
	for _, pair := range [][2]float64{{object.FillLeft, expected.FillLeft}, {object.FillRight, expected.FillRight}, {object.FillTop, expected.FillTop}, {object.FillBottom, expected.FillBottom}} {
		if math.IsNaN(pair[0]) || math.Abs(pair[0]-pair[1]) > .00002 {
			return false
		}
	}
	return true
}

// pptxLineToElement retains PowerPoint line and connector artwork as a thin,
// rotated modern rectangle. Keynope connectors need attachable shape ports,
// which arbitrary imported lines do not have. This visual form is editable,
// preserves the source paint order, and is substantially closer than a blank
// rectangular outline or a missing connector.
func pptxLineToElement(object pptx.Object, x, y, w, h, scale float64, canvas ...sceneRect) (Element, []pptx.Warning, *DeckAsset, error) {
	var warnings []pptx.Warning
	if len(object.LinePoints) > 2 {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "line-route-approximated", Message: "Imported custom polyline was approximated as a straight editable shape; its bends are not yet supported on import."})
	}
	if object.LineHead.Type != "" && object.LineHead.Type != "none" || object.LineTail.Type != "" && object.LineTail.Type != "none" {
		warnings = append(warnings, pptx.Warning{Object: object.ID, Code: "line-arrowheads-omitted", Message: "Imported line remains editable as a shape; Office arrowheads are not represented by this shape."})
	}
	if object.Stroke == "" {
		return Element{}, []pptx.Warning{{Object: object.ID, Code: "line-no-stroke", Message: "A PowerPoint line without a stroke was omitted."}}, nil, nil
	}
	length := math.Hypot(w, h)
	if length <= 0 {
		return Element{}, nil, nil, nil
	}
	thickness := math.Max(1, object.StrokeWidth*2*scale)
	angle := math.Atan2(h, w) * 180 / math.Pi
	if object.FlipH != object.FlipV {
		angle = -angle
	}
	angle += object.Rotation
	query := pptxSceneBoundsQuery(x+w/2-length/2, y+h/2-thickness/2, length, thickness, canvas...)
	query.Set("shape", "square")
	query.Set("fg", object.Stroke)
	query.Set("object-rotation", decimal(angle))
	if object.Link != "" {
		query.Set("link", object.Link)
	}
	if object.OpacitySet || object.Opacity > 0 && object.Opacity < 1 {
		query.Set("modern-opacity", decimal(object.Opacity))
	}
	id := "pptx-" + object.ID
	if object.ID == "" {
		id = newStableID("pptx-line")
	}
	return Element{ID: id, Kind: "shape", Text: "[shape:square]", Query: query.Encode()}, warnings, nil, nil
}

// pptxSceneBoundsQuery maps the physical PowerPoint scene to Keynope's
// authored 245×56 grid (1920×1080 when no canvas is supplied).
// Placement intentionally uses integer anchors plus
// sub-cell offsets: imagePlacement parses anchors as integers, whereas width
// and height are allowed to remain continuous for the scene compositor.
// Writing a fractional `left` or `top` made imported objects silently lose
// their anchor and collapse into normal document flow.
func pptxSceneBoundsQuery(x, y, width, height float64, canvas ...sceneRect) url.Values {
	const cols, rows = 245.0, 56.0
	canvasWidth, canvasHeight := pptxCanvasDimensions(canvas)
	x = x * cols / canvasWidth
	y = y * rows / canvasHeight
	width = math.Max(.5, width*cols/canvasWidth)
	height = math.Max(.5, height*rows/canvasHeight)
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
	// Keep importing the fixed flowchart presets used by existing Office
	// documents. Keynope exports their adjustable equivalent presets instead.
	switch strings.ToLower(shape) {
	case "flowchartinputoutput":
		return "data"
	case "flowchartpreparation":
		return "preparation"
	case "flowchartterminator":
		return "terminator"
	}
	for _, d := range shapeDefinitions {
		if strings.EqualFold(shape, d.Preset) {
			return d.ID
		}
	}
	if d, ok := shapeDefinitionFor(shape); ok {
		return d.ID
	}
	return "square"
}

func pptxSuggestedName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if strings.TrimSpace(base) == "" {
		base = "Untitled"
	}
	return base + ".md"
}
