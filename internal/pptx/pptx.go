// Package pptx implements a small, bounded subset of PresentationML.
//
// It intentionally does not shell out to Office, LibreOffice or a service. The
// subset is useful on its own: slides, editable text, basic shapes, images,
// links, notes and hidden slides. Callers must render or report anything they
// cannot express using these objects instead of silently dropping it.
package pptx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"math"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	// Keynope's 1920 by 1080 scene maps to a 13⅓ by 7½ inch presentation.
	SlideWidthEMU  int64 = 12192000
	SlideHeightEMU int64 = 6858000
	SceneWidth           = 1920.0
	SceneHeight          = 1080.0
)

type Limits struct {
	MaxInputBytes   int64
	MaxInflatedSize int64
	MaxEntries      int
	MaxSlides       int
	MaxObjects      int
	MaxXMLPart      int64
}

func DefaultLimits() Limits {
	return Limits{MaxInputBytes: 100 << 20, MaxInflatedSize: 300 << 20, MaxEntries: 10000, MaxSlides: 500, MaxObjects: 50000, MaxXMLPart: 16 << 20}
}

type Options struct{ Limits Limits }

type Report struct {
	Warnings []Warning `json:"warnings,omitempty"`
}
type Warning struct {
	Slide       int    `json:"slide"` // Zero-based; -1 denotes a document-wide diagnostic.
	Object      string `json:"object,omitempty"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Feature     string `json:"feature,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Fallback    string `json:"fallback,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

type Presentation struct {
	Theme         *Theme // Optional native palette; imported objects also retain resolved concrete colours/fonts.
	Width         int64
	Height        int64
	Slides        []Slide
	EmbeddedFonts []EmbeddedFont
}

// EmbeddedFont is a font payload included with the presentation. Its text
// remains native PowerPoint text; the payload simply makes the selected face
// available on a receiving machine.
type EmbeddedFont struct {
	Family        string
	Data          []byte
	ForceEditable bool
}
type Slide struct {
	ID              string
	Hidden          bool
	Notes           string
	Background      string // Resolved solid background colour; written as a native slide property.
	BackgroundImage []byte // Resolved picture background, when the source supplies one.
	BackgroundMIME  string
	Objects         []Object
	Timeline        *Timeline
}
type Object struct {
	ID                  string
	Name                string // User-visible nonvisual name; never a reference identity.
	Inherited           bool   // Read-only origin: layout/master IDs cannot be slide timing targets.
	Kind                string // text, shape, image, line or table
	X, Y, Width, Height float64
	Rotation            float64
	Text                string
	FontFace            string
	FontSize            float64 // points
	Color               string  // #RRGGBB
	// Read-time source references; concrete values above/below remain the
	// rendering/export fallbacks until the import adapter can bind a theme.
	ColorThemeSlot      string
	FontThemeRole       string
	FillThemeSlot       string
	StrokeThemeSlot     string
	ColorThemeSource    string
	FontThemeSource     string
	FillThemeSource     string
	StrokeThemeSource   string
	Opacity             float64 // 0 means unspecified unless OpacitySet; otherwise 0..1
	OpacitySet          bool    // Explicit alpha, including zero, from the selected paint.
	Fill                string  // #RRGGBB or empty
	Stroke              string  // #RRGGBB or empty
	StrokeWidth         float64 // points
	LineHead, LineTail  LineEnd
	LinePoints          []LinePoint // Absolute, unrotated route vertices; empty means a straight line.
	LineStart, LineEnd  *Connection
	connectionLoss      bool
	ConnectionSites     []ConnectionSite
	Gradient            *ShapeGradient
	Shadow              *ShapeShadow
	TextEffects         *TextEffects
	Shape               string
	ShapeAdjustments    map[string]int64 // Constant DrawingML adjustment guides.
	CustomGeometry      *CustomGeometry
	hasShapeGeometry    bool
	shapeAdjustmentLoss bool
	shapePaintLoss      bool
	textPaintLoss       bool
	FlipH               bool
	FlipV               bool
	Link                string
	Image               []byte
	ImageMIME           string
	ImageOpacity        *float64 // nil means unchanged source alpha; zero is fully transparent.
	ImageSVG            []byte   // Passive SVG companion; Image remains the raster fallback.
	CropLeft            float64  // Fractional source insets from a:srcRect.
	CropTop             float64
	CropRight           float64
	CropBottom          float64
	FillLeft            float64 // Fractional destination insets from a:fillRect.
	FillTop             float64
	FillRight           float64
	FillBottom          float64
	TextAlign           string // start, center, end or justify
	VerticalAlign       string // top, middle or bottom
	Bold                bool
	Italic              bool
	Paragraphs          []TextParagraph // Native DrawingML runs; Text remains the plain-text compatibility view.
	Table               *Table
	Chart               *Chart
	ChartSource         []byte // Recoverable unsupported chart XML; never executed.
}

// colourResolver holds the presentation theme and the active colour map. It
// deliberately lives inside the reader: callers receive concrete RGB values
// rather than theme tokens that would render differently on the Mac and web.
type colourResolver struct {
	colours              map[string]string
	mapping              map[string]string
	majorFont, minorFont string
	theme                *Theme
	parts                *themePartCache
	textStyles           *node
	colourPart, fontPart string // Independent effective scheme owners; overrides can replace either one.
}

type inheritedShape struct {
	object      Object
	hasGeometry bool
	textSources []*node // Read-only master/layout placeholder cascade.
}

func normaliseLimits(v Limits) Limits {
	d := DefaultLimits()
	if v.MaxInputBytes > 0 {
		d.MaxInputBytes = v.MaxInputBytes
	}
	if v.MaxInflatedSize > 0 {
		d.MaxInflatedSize = v.MaxInflatedSize
	}
	if v.MaxEntries > 0 {
		d.MaxEntries = v.MaxEntries
	}
	if v.MaxSlides > 0 {
		d.MaxSlides = v.MaxSlides
	}
	if v.MaxObjects > 0 {
		d.MaxObjects = v.MaxObjects
	}
	if v.MaxXMLPart > 0 {
		d.MaxXMLPart = v.MaxXMLPart
	}
	return d
}

// Read parses a safe, local, unencrypted PresentationML ZIP package.
func Read(ctx context.Context, source io.ReaderAt, size int64, options Options) (Presentation, Report, error) {
	limits := normaliseLimits(options.Limits)
	if size <= 0 || size > limits.MaxInputBytes {
		return Presentation{}, Report{}, fmt.Errorf("PPTX input exceeds the %d byte limit", limits.MaxInputBytes)
	}
	zr, err := zip.NewReader(source, size)
	if err != nil {
		return Presentation{}, Report{}, fmt.Errorf("invalid PPTX ZIP: %w", err)
	}
	if len(zr.File) > limits.MaxEntries {
		return Presentation{}, Report{}, fmt.Errorf("PPTX has too many entries")
	}
	files := make(map[string]*zip.File, len(zr.File))
	var inflated int64
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return Presentation{}, Report{}, err
		}
		name, ok := cleanPart(f.Name)
		if !ok || name == "" {
			return Presentation{}, Report{}, fmt.Errorf("unsafe PPTX part name")
		}
		if _, duplicate := files[name]; duplicate {
			return Presentation{}, Report{}, fmt.Errorf("duplicate PPTX part %q", name)
		}
		if f.UncompressedSize64 > uint64(limits.MaxInflatedSize) || inflated > limits.MaxInflatedSize-int64(f.UncompressedSize64) {
			return Presentation{}, Report{}, fmt.Errorf("PPTX inflated data exceeds the limit")
		}
		inflated += int64(f.UncompressedSize64)
		files[name] = f
	}
	if _, ok := files["[Content_Types].xml"]; !ok {
		return Presentation{}, Report{}, errors.New("not an Office Open XML package")
	}
	if _, ok := files["ppt/presentation.xml"]; !ok {
		return Presentation{}, Report{}, errors.New("not a PowerPoint presentation")
	}
	presentationXML, err := readPart(files, "ppt/presentation.xml", limits.MaxXMLPart)
	if err != nil {
		return Presentation{}, Report{}, err
	}
	root, err := parseXML(presentationXML, 128)
	if err != nil {
		return Presentation{}, Report{}, fmt.Errorf("read presentation: %w", err)
	}
	if root.Name != "presentation" {
		return Presentation{}, Report{}, errors.New("not a PresentationML presentation")
	}
	result := Presentation{Width: SlideWidthEMU, Height: SlideHeightEMU}
	if sizeNode := findFirst(root, "sldSz"); sizeNode != nil {
		result.Width, _ = parseInt64(attr(sizeNode, "cx"), SlideWidthEMU)
		result.Height, _ = parseInt64(attr(sizeNode, "cy"), SlideHeightEMU)
		if result.Width <= 0 || result.Height <= 0 {
			return Presentation{}, Report{}, errors.New("invalid presentation dimensions")
		}
	}
	rels, err := readRelationships(files, "ppt/presentation.xml", limits.MaxXMLPart)
	if err != nil {
		return Presentation{}, Report{}, err
	}
	list := findFirst(root, "sldIdLst")
	if list == nil {
		return result, Report{}, nil
	}
	resolver, err := readColourResolver(files, rels, limits, root)
	if err != nil {
		return Presentation{}, Report{}, err
	}
	result.Theme = resolver.theme
	var report Report
	for slideIndex, ref := range children(list, "sldId") {
		if slideIndex >= limits.MaxSlides {
			return Presentation{}, Report{}, fmt.Errorf("PPTX has more than %d slides", limits.MaxSlides)
		}
		relID := relationshipID(ref, list, root)
		target, ok := rels[relID]
		if !ok {
			return Presentation{}, Report{}, fmt.Errorf("slide relationship %q is missing", relID)
		}
		slide, warnings, err := readSlide(ctx, files, target, limits, resolver)
		if err != nil {
			return Presentation{}, Report{}, fmt.Errorf("read slide %d: %w", slideIndex+1, err)
		}
		slide.ID = relID
		// Accept the old Keynope reference flag as a legacy fallback; correct
		// PresentationML stores show on the slide root, not on p:sldId.
		slide.Hidden = slide.Hidden || strings.EqualFold(attr(ref, "show"), "0") || strings.EqualFold(attr(ref, "show"), "false")
		for i := range warnings {
			warnings[i].Slide = slideIndex
			report.Warnings = append(report.Warnings, warnings[i])
		}
		result.Slides = append(result.Slides, slide)
	}
	return result, report.Normalized(), nil
}

func readSlide(ctx context.Context, files map[string]*zip.File, part string, limits Limits, resolver colourResolver) (Slide, []Warning, error) {
	data, err := readPart(files, part, limits.MaxXMLPart)
	if err != nil {
		return Slide{}, nil, err
	}
	root, err := parseXML(data, 128)
	if err != nil {
		return Slide{}, nil, err
	}
	if root.Name != "sld" {
		return Slide{}, nil, errors.New("invalid slide root")
	}
	rels, err := readRelationships(files, part, limits.MaxXMLPart)
	if err != nil {
		return Slide{}, nil, err
	}
	resolver, err = slideColourResolver(files, part, root, rels, limits, resolver)
	if err != nil {
		return Slide{}, nil, err
	}
	links, err := readExternalRelationships(files, part, limits.MaxXMLPart)
	if err != nil {
		return Slide{}, nil, err
	}
	var mediaCache = map[string][]byte{}
	var warnings []Warning
	var objects []Object
	spTree := findFirst(root, "spTree")
	if spTree == nil {
		return Slide{}, nil, errors.New("slide has no shape tree")
	}
	parents, parentObjects, background, parentWarnings, err := resolveInheritedSlideParts(files, part, rels, mediaCache, limits, resolver)
	if err != nil {
		return Slide{}, nil, err
	}
	warnings = append(warnings, parentWarnings...)
	for i := range parentObjects {
		parentObjects[i].Inherited = true
	}
	objects = append(objects, parentObjects...)
	for _, child := range spTree.Children {
		if err := ctx.Err(); err != nil {
			return Slide{}, nil, err
		}
		var object *Object
		switch child.Name {
		case "sp":
			object, err = parseShape(child, links, resolver, parents[placeholderKey(child)], false)
			if err != nil {
				return Slide{}, nil, err
			}
		case "pic":
			object = parsePicture(child, rels, files, mediaCache, limits, resolver)
		case "cxnSp":
			object = parseConnector(child, resolver, links)
		case "grpSp":
			warnings = append(warnings, Warning{Code: "group-flattened", Message: "Nested PowerPoint group was flattened during import."})
			groupObjects, groupWarnings, groupErr := parseGroup(child, rels, links, files, mediaCache, limits, resolver)
			if groupErr != nil {
				return Slide{}, nil, groupErr
			}
			objects = append(objects, groupObjects...)
			warnings = append(warnings, groupWarnings...)
		case "graphicFrame":
			var tableWarnings []Warning
			object, tableWarnings, err = parseGraphicFrame(child, rels, files, mediaCache, limits, resolver)
			if err != nil {
				return Slide{}, nil, err
			}
			warnings = append(warnings, tableWarnings...)
		}
		if object != nil {
			objects = append(objects, *object)
		}
		if len(objects) > limits.MaxObjects {
			return Slide{}, nil, fmt.Errorf("PPTX has more than %d objects", limits.MaxObjects)
		}
	}
	// A slide's own background is the final override over layout/master.
	if colour := partBackground(root, resolver); colour != "" {
		background.Colour = colour
	}
	if image, mime := partBackgroundImage(root, rels, files, limits); len(image) > 0 {
		background.Image, background.MIME = image, mime
	}
	timeline, timingWarnings := readTimeline(ctx, data, root, objects)
	if err := ctx.Err(); err != nil {
		return Slide{}, nil, err
	}
	warnings = append(warnings, timingWarnings...)
	for _, object := range objects {
		if object.connectionLoss {
			warnings = append(warnings, Warning{Object: object.ID, Code: "connector-attachment-unsupported", Message: "Malformed or out-of-bounds connection references were omitted; the line artwork remains."})
		}
		if object.Shape == "custom" && object.CustomGeometry == nil {
			warnings = append(warnings, Warning{Object: object.ID, Code: "custom-shape-geometry-unsupported", Message: "Custom shape contains unsupported formulas, commands or geometry bounds; its native geometry could not be retained."})
		}
		if object.shapePaintLoss {
			warnings = append(warnings, Warning{Object: object.ID, Code: "shape-paint-approximated", Message: "Unsupported gradient or shadow parameters were approximated or omitted."})
		}
		if object.textPaintLoss {
			warnings = append(warnings, Warning{Object: object.ID, Code: "text-effects-approximated", Message: "Unsupported or differing character gradient/outline parameters were approximated or omitted."})
		}
		if object.shapeAdjustmentLoss {
			warnings = append(warnings, Warning{Object: object.ID, Code: "shape-adjustment-formula-unsupported", Message: "Some shape adjustment guides were unsupported, duplicated or outside the supported limits; supported constants were retained and other guides use preset defaults."})
		}
	}
	return Slide{Hidden: attr(root, "show") == "0" || attr(root, "show") == "false", Background: background.Colour, BackgroundImage: background.Image, BackgroundMIME: background.MIME, Objects: objects, Timeline: timeline}, warnings, nil
}

func parseGroup(n *node, rels, links map[string]string, files map[string]*zip.File, cache map[string][]byte, limits Limits, resolver colourResolver) ([]Object, []Warning, error) {
	var objects []Object
	var warnings []Warning
	for _, child := range n.Children {
		switch child.Name {
		case "sp":
			o, err := parseShape(child, links, resolver, nil, false)
			if err != nil {
				return nil, nil, err
			}
			if o != nil {
				objects = append(objects, *o)
			}
		case "pic":
			if o := parsePicture(child, rels, files, cache, limits, resolver); o != nil {
				objects = append(objects, *o)
			}
		case "cxnSp":
			if o := parseConnector(child, resolver, links); o != nil {
				objects = append(objects, *o)
			}
		case "grpSp":
			nested, nestedWarnings, err := parseGroup(child, rels, links, files, cache, limits, resolver)
			if err != nil {
				return nil, nil, err
			}
			objects = append(objects, nested...)
			warnings = append(warnings, nestedWarnings...)
		case "graphicFrame":
			object, tableWarnings, err := parseGraphicFrame(child, rels, files, cache, limits, resolver)
			if err != nil {
				return nil, nil, err
			}
			warnings = append(warnings, tableWarnings...)
			if object != nil {
				objects = append(objects, *object)
			}
		}
	}
	transformWarnings, err := applyGroupTransform(n, objects)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, transformWarnings...)
	return objects, warnings, nil
}

func parseShape(n *node, links map[string]string, resolver colourResolver, inherited *inheritedShape, retainEmpty bool) (*Object, error) {
	id := ""
	if nv := findFirst(n, "cNvPr"); nv != nil {
		id = attr(nv, "id")
	}
	x, y, w, h, rotation, hasGeometry := transformWithPresence(n)
	if !hasGeometry && inherited != nil && inherited.hasGeometry {
		x, y, w, h, rotation = inherited.object.X, inherited.object.Y, inherited.object.Width, inherited.object.Height, inherited.object.Rotation
	}
	text := strings.Join(paragraphText(n), "\n")
	shape := "rect"
	if g := findFirst(n, "prstGeom"); g != nil && attr(g, "prst") != "" {
		shape = attr(g, "prst")
	} else if findFirst(n, "custGeom") != nil {
		shape = "custom"
	} else if findFirst(n, "custGeom") == nil && inherited != nil && inherited.object.Shape != "" {
		shape = inherited.object.Shape
	}
	// Restrict paint lookup to spPr. A text run can have its own solidFill;
	// treating that run colour as a shape fill changes a text box into a shape.
	spPr := findFirst(n, "spPr")
	fill, opacity := resolvedPaint(findChild(spPr, "solidFill"), resolver)
	fillThemeSlot := resolvedThemeColourSlot(findChild(spPr, "solidFill"), resolver)
	fillThemeSource := themeReferenceSource(fillThemeSlot, resolver.colourPart)
	opacitySet := findFirst(findChild(spPr, "solidFill"), "alpha") != nil
	gradient, gradientColor, gradientAlpha, gradientAlphaSet, gradientLoss := parseShapeGradient(spPr, resolver)
	if gradientColor != "" {
		fill, opacity, opacitySet = gradientColor, gradientAlpha, gradientAlphaSet
		fillThemeSlot = ""
		fillThemeSource = ""
	}
	shadow, shadowLoss := parseShapeShadow(spPr, resolver)
	if inherited != nil {
		parent := inherited.object
		if !hasExplicitShapeFill(spPr) {
			fill, opacity, opacitySet = parent.Fill, parent.Opacity, parent.OpacitySet
			fillThemeSlot = parent.FillThemeSlot
			fillThemeSource = parent.FillThemeSource
			if parent.Gradient != nil {
				value := *parent.Gradient
				gradient = &value
			}
			gradientLoss = parent.shapePaintLoss
		}
		if findChild(spPr, "effectLst") == nil && findChild(spPr, "effectDag") == nil {
			if parent.Shadow != nil {
				shadow = cloneShapeShadow(parent.Shadow)
			}
			shadowLoss = parent.shapePaintLoss
		}
	}
	stroke, strokeOpacity := "", 0.0
	strokeOpacitySet := false
	width := 0.0
	if line := findChild(spPr, "ln"); line != nil {
		stroke, strokeOpacity = resolvedPaint(findFirst(line, "solidFill"), resolver)
		strokeOpacitySet = findFirst(findFirst(line, "solidFill"), "alpha") != nil
		// DrawingML's absent line-width default is 0.75 points. An explicit
		// zero remains zero rather than becoming an arbitrary Keynope outline.
		if attr(line, "w") == "" && stroke != "" {
			width = .75
		} else if v, err := strconv.ParseFloat(attr(line, "w"), 64); err == nil && v >= 0 {
			width = v / 12700
		}
	}
	textStyle, textOpacity := textPropertyRun(n, resolver, inherited)
	fontFace, fontSize, color, bold, italic := textStyle.FontFace, textStyle.FontSize, textStyle.Color, textStyle.Bold, textStyle.Italic
	textPaint := findFirst(n, "defRPr")
	if run := findFirst(n, "rPr"); run != nil {
		if c, _ := resolvedPaint(findChild(run, "solidFill"), resolver); c != "" {
			textPaint = run
		}
	}
	textOpacitySet := findFirst(findChild(textPaint, "solidFill"), "alpha") != nil
	if inherited != nil {
		if fontFace == "" {
			fontFace = inherited.object.FontFace
			textStyle.FontThemeRole = inherited.object.FontThemeRole
			textStyle.FontThemeSource = inherited.object.FontThemeSource
		}
		if fontSize <= 0 {
			fontSize = inherited.object.FontSize
		}
		if color == "" {
			color = inherited.object.Color
			if !hasExplicitShapeFill(textPaint) {
				textStyle.ColorThemeSlot = inherited.object.ColorThemeSlot
				textStyle.ColorThemeSource = inherited.object.ColorThemeSource
			}
			textOpacity = inherited.object.Opacity
			textOpacitySet = inherited.object.OpacitySet
		}
		if !bold && len(inherited.textSources) == 0 {
			bold = inherited.object.Bold
		}
		if !italic && len(inherited.textSources) == 0 {
			italic = inherited.object.Italic
		}
	}
	// Our editable no-fill shapes can have neither a border nor label. Honour
	// DrawingML's explicit shape/textbox distinction before legacy heuristics.
	markedShape := false
	switch attr(findFirst(n, "cNvSpPr"), "txBox") {
	case "0", "false", "off":
		markedShape = findFirst(n, "prstGeom") != nil || findFirst(n, "custGeom") != nil
	}
	if text == "" && fill == "" && stroke == "" && !retainEmpty && !markedShape {
		return nil, nil
	}
	kind := "shape"
	if !markedShape && text != "" && fill == "" && stroke == "" && shape != "custom" {
		kind = "text"
	}
	link := ""
	if hyperlink := findFirst(n, "hlinkClick"); hyperlink != nil {
		link = links[attr(hyperlink, "id")]
	}
	if fill == "" {
		opacity, opacitySet = strokeOpacity, strokeOpacitySet
		if stroke == "" {
			opacity, opacitySet = textOpacity, textOpacitySet
		}
	}
	align, vertical := textAlignment(n)
	if inherited != nil {
		if align == "" {
			align = inherited.object.TextAlign
		}
		if vertical == "" {
			vertical = inherited.object.VerticalAlign
		}
	}
	flipH, flipV := false, false
	if xfrm := findFirst(n, "xfrm"); xfrm != nil {
		flipH, flipV = drawingMLBool(attr(xfrm, "flipH")), drawingMLBool(attr(xfrm, "flipV"))
	}
	object := &Object{ID: id, Kind: kind, X: x, Y: y, Width: w, Height: h, Rotation: rotation, Text: text, FontFace: fontFace, FontSize: fontSize, Color: color, Opacity: opacity, Fill: fill, Stroke: stroke, StrokeWidth: width, Shape: shape, FlipH: flipH, FlipV: flipV, Link: link, TextAlign: align, VerticalAlign: vertical, Bold: bold, Italic: italic}
	object.OpacitySet = opacitySet
	object.ColorThemeSlot, object.FontThemeRole = textStyle.ColorThemeSlot, textStyle.FontThemeRole
	object.ColorThemeSource, object.FontThemeSource = textStyle.ColorThemeSource, textStyle.FontThemeSource
	object.FillThemeSlot = fillThemeSlot
	object.FillThemeSource = fillThemeSource
	object.StrokeThemeSlot = resolvedThemeColourSlot(findChild(findChild(spPr, "ln"), "solidFill"), resolver)
	object.StrokeThemeSource = themeReferenceSource(object.StrokeThemeSlot, resolver.colourPart)
	object.Name = attr(findFirst(n, "cNvPr"), "name")
	object.TextEffects, object.textPaintLoss = parseTextEffects(n, resolver)
	object.Gradient, object.Shadow, object.shapePaintLoss = gradient, shadow, gradientLoss || shadowLoss
	object.hasShapeGeometry = findFirst(n, "prstGeom") != nil || findFirst(n, "custGeom") != nil
	object.ShapeAdjustments, object.shapeAdjustmentLoss = parseShapeAdjustments(findFirst(n, "prstGeom"))
	if custom := findFirst(n, "custGeom"); custom != nil {
		object.CustomGeometry, _ = parseCustomGeometry(custom)
		object.ConnectionSites = parseConnectionSites(custom)
		object.ShapeAdjustments, object.shapeAdjustmentLoss = parseShapeAdjustments(custom)
	}
	if !object.hasShapeGeometry && inherited != nil {
		object.hasShapeGeometry = inherited.object.hasShapeGeometry
		object.shapeAdjustmentLoss = inherited.object.shapeAdjustmentLoss
		object.CustomGeometry = cloneCustomGeometry(inherited.object.CustomGeometry)
		object.ConnectionSites = append([]ConnectionSite(nil), inherited.object.ConnectionSites...)
	}
	if findFirst(n, "prstGeom") == nil && findFirst(n, "custGeom") == nil && inherited != nil && len(inherited.object.ShapeAdjustments) > 0 {
		object.ShapeAdjustments = make(map[string]int64, len(inherited.object.ShapeAdjustments))
		for name, value := range inherited.object.ShapeAdjustments {
			object.ShapeAdjustments[name] = value
		}
	}
	var err error
	object.Paragraphs, err = parseTextParagraphs(n, resolver, inherited)
	if err != nil {
		return nil, err
	}
	if len(object.Paragraphs) > 0 {
		object.Text = paragraphPlainText(object.Paragraphs)
		if object.Paragraphs[0].Align != "" {
			object.TextAlign = object.Paragraphs[0].Align
		}
		// Keep the paragraph's default separate from a first-word override.
		// Runs carry their effective style independently, including explicit Off.
		if defaults := findChild(findChild(findChild(findChild(n, "txBody"), "p"), "pPr"), "defRPr"); defaults != nil {
			f, s, c, _, b, i := propertiesFromRun(defaults, resolver)
			if f != "" {
				object.FontFace = f
				object.FontThemeRole = latinThemeRole(attr(findChild(defaults, "latin"), "typeface"), resolver)
				object.FontThemeSource = themeReferenceSource(object.FontThemeRole, resolver.fontPart)
			}
			if s > 0 {
				object.FontSize = s
			}
			if c != "" {
				object.Color = c
				object.ColorThemeSlot = resolvedThemeColourSlot(findChild(defaults, "solidFill"), resolver)
				object.ColorThemeSource = themeReferenceSource(object.ColorThemeSlot, resolver.colourPart)
			}
			if attr(defaults, "b") != "" {
				object.Bold = b
			}
			if attr(defaults, "i") != "" {
				object.Italic = i
			}
		}
	}
	if err := validateObjectText(*object); err != nil {
		return nil, err
	}
	return object, nil
}

func parsePicture(n *node, rels map[string]string, files map[string]*zip.File, cache map[string][]byte, limits Limits, resolver colourResolver) *Object {
	id := ""
	if nv := findFirst(n, "cNvPr"); nv != nil {
		id = attr(nv, "id")
	}
	x, y, w, h, rotation := transform(n)
	blip := findFirst(n, "blip")
	if blip == nil {
		return nil
	}
	target, ok := rels[attr(blip, "embed")]
	if !ok {
		return nil
	}
	data, ok := cache[target]
	if !ok {
		var err error
		data, err = readPart(files, target, limits.MaxInflatedSize)
		if err != nil {
			return nil
		}
		cache[target] = data
	}
	left, top, right, bottom := 0.0, 0.0, 0.0, 0.0
	if crop := findFirst(n, "srcRect"); crop != nil {
		left, _ = strconv.ParseFloat(attr(crop, "l"), 64)
		top, _ = strconv.ParseFloat(attr(crop, "t"), 64)
		right, _ = strconv.ParseFloat(attr(crop, "r"), 64)
		bottom, _ = strconv.ParseFloat(attr(crop, "b"), 64)
		left, top, right, bottom = left/100000, top/100000, right/100000, bottom/100000
	}
	object := &Object{ID: id, Kind: "image", X: x, Y: y, Width: w, Height: h, Rotation: rotation, Image: data, ImageMIME: mimeForPart(target), CropLeft: left, CropTop: top, CropRight: right, CropBottom: bottom}
	object.Name = attr(findFirst(n, "cNvPr"), "name")
	object.Shadow, object.shapePaintLoss = parseShapeShadow(findChild(n, "spPr"), resolver)
	if xfrm := findFirst(n, "xfrm"); xfrm != nil {
		object.FlipH = attr(xfrm, "flipH") == "1" || attr(xfrm, "flipH") == "true"
		object.FlipV = attr(xfrm, "flipV") == "1" || attr(xfrm, "flipV") == "true"
	}
	if alpha := findFirst(blip, "alphaModFix"); alpha != nil {
		if value, err := strconv.ParseFloat(attr(alpha, "amt"), 64); err == nil && !math.IsNaN(value) && value >= 0 && value <= 100000 {
			value /= 100000
			object.ImageOpacity = &value
		}
	}
	if fill := findFirst(n, "fillRect"); fill != nil {
		for name, target := range map[string]*float64{"l": &object.FillLeft, "t": &object.FillTop, "r": &object.FillRight, "b": &object.FillBottom} {
			value, _ := strconv.ParseFloat(attr(fill, name), 64)
			*target = value / 100000
		}
	}
	if geometry := findFirst(n, "prstGeom"); geometry != nil {
		object.Shape = attr(geometry, "prst")
	}
	if vector := findFirst(blip, "svgBlip"); vector != nil {
		if part := rels[attr(vector, "embed")]; part != "" && strings.HasSuffix(strings.ToLower(part), ".svg") {
			if svg, err := readPart(files, part, min(limits.MaxInflatedSize, 2<<20)); err == nil {
				object.ImageSVG = svg // The application sanitizer owns SVG validation.
			}
		}
	}
	return object
}

func parseConnector(n *node, resolver colourResolver, linkMaps ...map[string]string) *Object {
	x, y, w, h, r := transform(n)
	id := ""
	if nv := findFirst(n, "cNvPr"); nv != nil {
		id = attr(nv, "id")
	}
	line := findFirst(n, "ln")
	stroke, opacity := resolvedPaint(findFirst(line, "solidFill"), resolver)
	width := 1.0
	if line != nil {
		if v, _ := strconv.ParseFloat(attr(line, "w"), 64); v > 0 {
			width = v / 12700
		}
	}
	xfrm := findFirst(n, "xfrm")
	link := ""
	points := parseLinePoints(n, x, y, w, h)
	start, end := parseConnection(findFirst(n, "stCxn")), parseConnection(findFirst(n, "endCxn"))
	if len(linkMaps) > 0 {
		link = linkMaps[0][attr(findFirst(n, "hlinkClick"), "id")]
	}
	object := &Object{ID: id, Kind: "line", LineStart: start, LineEnd: end, LinePoints: points, X: x, Y: y, Width: w, Height: h, Rotation: r, FlipH: attr(xfrm, "flipH") == "1" || attr(xfrm, "flipH") == "true", FlipV: attr(xfrm, "flipV") == "1" || attr(xfrm, "flipV") == "true", Stroke: stroke, StrokeWidth: width, Opacity: opacity, OpacitySet: findFirst(findFirst(line, "solidFill"), "alpha") != nil, Link: link, LineHead: parseLineEnd(findChild(line, "headEnd")), LineTail: parseLineEnd(findChild(line, "tailEnd"))}
	object.connectionLoss = findFirst(n, "stCxn") != nil && start == nil || findFirst(n, "endCxn") != nil && end == nil
	object.Name = attr(findFirst(n, "cNvPr"), "name")
	object.StrokeThemeSlot = resolvedThemeColourSlot(findChild(line, "solidFill"), resolver)
	object.StrokeThemeSource = themeReferenceSource(object.StrokeThemeSlot, resolver.colourPart)
	return object
}

func transform(n *node) (float64, float64, float64, float64, float64) {
	x, y, w, h, r, _ := transformWithPresence(n)
	return x, y, w, h, r
}

func transformWithPresence(n *node) (float64, float64, float64, float64, float64, bool) {
	xfrm := findFirst(n, "xfrm")
	if xfrm == nil {
		return 0, 0, 0, 0, 0, false
	}
	off, ext := findFirst(xfrm, "off"), findFirst(xfrm, "ext")
	x, _ := strconv.ParseFloat(attr(off, "x"), 64)
	y, _ := strconv.ParseFloat(attr(off, "y"), 64)
	w, _ := strconv.ParseFloat(attr(ext, "cx"), 64)
	h, _ := strconv.ParseFloat(attr(ext, "cy"), 64)
	r, _ := strconv.ParseFloat(attr(xfrm, "rot"), 64)
	return x / 6350, y / 6350, w / 6350, h / 6350, r / 60000, true
}

// placeholderKey follows the PowerPoint matching rule used by this subset:
// type plus idx. A title without an idx is a distinct placeholder from a
// body without one. Empty placeholders are deliberately ignored by callers.
func placeholderKey(n *node) string {
	nvPr := findFirst(n, "nvPr")
	ph := findChild(nvPr, "ph")
	if ph == nil {
		return ""
	}
	kind := attr(ph, "type")
	if kind == "" {
		kind = "body"
	}
	return kind + "|" + attr(ph, "idx")
}

func textProperties(n *node, resolver colourResolver, parents ...*inheritedShape) (string, float64, string, float64, bool, bool) {
	style, opacity := textPropertyRun(n, resolver, parents...)
	return style.FontFace, style.FontSize, style.Color, opacity, style.Bold, style.Italic
}

func textPropertyRun(n *node, resolver colourResolver, parents ...*inheritedShape) (TextRun, float64) {
	// A run generally contains only overrides. The layout's defRPr supplies
	// the actual font, colour and size for ordinary PowerPoint placeholders.
	base := findFirst(n, "defRPr")
	run := findFirst(n, "rPr")
	style, opacity, _ := overlayTextRunProperties(TextRun{}, base, resolver)
	var inherited *inheritedShape
	if len(parents) > 0 {
		inherited = parents[0]
	}
	body := findChild(n, "txBody")
	if resolver.textStyles != nil || findChild(body, "lstStyle") != nil || len(inheritedTextSources(inherited)) > 0 {
		paragraph := findChild(body, "p")
		properties := findChild(paragraph, "pPr")
		level, _ := strconv.Atoi(attr(properties, "lvl"))
		if level < 0 || level > 8 {
			level = 0
		}
		defaults := paragraphRunBase(n, resolver, inherited, level)
		defaults = overlayTextRun(defaults, findChild(properties, "defRPr"), resolver)
		style = defaults
	}
	if style.FontFace == "" {
		style.FontFace = shapeThemeFont(n, resolver)
		if style.FontFace != "" {
			style.FontThemeRole = attr(findChild(findChild(n, "style"), "fontRef"), "idx")
			style.FontThemeSource = themeReferenceSource(style.FontThemeRole, resolver.fontPart)
		}
	}
	if run == nil {
		return style, opacity
	}
	style, rOpacity, hasColour := overlayTextRunProperties(style, run, resolver)
	if hasColour {
		opacity = rOpacity
	}
	return style, opacity
}

func propertiesFromRun(n *node, resolver colourResolver) (string, float64, string, float64, bool, bool) {
	if n == nil {
		return "", 0, "", 0, false, false
	}
	var size float64
	if v, _ := strconv.ParseFloat(attr(n, "sz"), 64); v > 0 {
		size = v / 100
	}
	colour, opacity := resolvedPaint(findChild(n, "solidFill"), resolver)
	face := ""
	if latin := findChild(n, "latin"); latin != nil {
		face = resolver.typeface(attr(latin, "typeface"))
	}
	return face, size, colour, opacity, drawingMLBool(attr(n, "b")), drawingMLBool(attr(n, "i"))
}

func drawingMLBool(value string) bool {
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "on")
}

func textAlignment(n *node) (string, string) {
	align, vertical := "", ""
	if p := findFirst(n, "pPr"); p != nil {
		switch attr(p, "algn") {
		case "ctr":
			align = "center"
		case "r":
			align = "end"
		case "just", "dist":
			align = "justify"
		case "l":
			align = "start"
		}
	} else if p := findFirst(n, "lvl1pPr"); p != nil {
		// Layout defaults live on lvl1pPr rather than a concrete paragraph.
		switch attr(p, "algn") {
		case "ctr":
			align = "center"
		case "r":
			align = "end"
		case "just", "dist":
			align = "justify"
		case "l":
			align = "start"
		}
	}
	if body := findFirst(n, "bodyPr"); body != nil {
		switch attr(body, "anchor") {
		case "ctr":
			vertical = "middle"
		case "b":
			vertical = "bottom"
		case "t":
			vertical = "top"
		}
	}
	return align, vertical
}

// resolveInheritedSlideParts flattens parent visuals into an imported slide
// and, separately, returns style/geometry donors for placeholders overridden
// by the child slide. Keynope has one master model, while a source file may
// have arbitrary layout/master graphs; flattening here avoids blank or
// zero-sized placeholders without importing PowerPoint's editing chrome.
type resolvedBackground struct {
	Colour string
	Image  []byte
	MIME   string
}

func resolveInheritedSlideParts(files map[string]*zip.File, slidePart string, slideRels map[string]string, cache map[string][]byte, limits Limits, resolver colourResolver) (map[string]*inheritedShape, []Object, resolvedBackground, []Warning, error) {
	placeholders := map[string]*inheritedShape{}
	var fixed []Object
	var warnings []Warning
	layoutPart, err := typedRelationshipTarget(files, slidePart, "slideLayout", limits)
	if err != nil {
		return nil, nil, resolvedBackground{}, nil, err
	}
	if layoutPart == "" {
		layoutPart = relationshipTargetContaining(slideRels, "slideLayouts/")
	}
	if layoutPart == "" {
		return placeholders, fixed, resolvedBackground{}, warnings, nil
	}
	layoutRoot, layoutRels, err := readThemePart(files, layoutPart, limits, resolver.parts)
	if err != nil {
		return nil, nil, resolvedBackground{}, nil, err
	}
	masterPart, err := typedRelationshipTarget(files, layoutPart, "slideMaster", limits)
	if err != nil {
		return nil, nil, resolvedBackground{}, nil, err
	}
	if masterPart == "" {
		masterPart = relationshipTargetContaining(layoutRels, "slideMasters/")
	}
	background := resolvedBackground{}
	if masterPart != "" {
		masterRoot, masterRels, err := readThemePart(files, masterPart, limits, resolver.parts)
		if err != nil {
			return nil, nil, resolvedBackground{}, nil, err
		}
		background.Colour = partBackground(masterRoot, resolver)
		background.Image, background.MIME = partBackgroundImage(masterRoot, masterRels, files, limits)
		masterPlaceholders, masterFixed, masterWarnings, err := parseInheritedShapeTree(masterRoot, masterRels, files, cache, limits, resolver)
		if err != nil {
			return nil, nil, resolvedBackground{}, nil, err
		}
		for key, donor := range masterPlaceholders {
			placeholders[key] = donor
		}
		fixed = append(fixed, masterFixed...)
		warnings = append(warnings, masterWarnings...)
	}
	if layoutBackground := partBackground(layoutRoot, resolver); layoutBackground != "" {
		background.Colour = layoutBackground
	}
	if image, mime := partBackgroundImage(layoutRoot, layoutRels, files, limits); len(image) > 0 {
		background.Image, background.MIME = image, mime
	}
	layoutPlaceholders, layoutFixed, layoutWarnings, err := parseInheritedShapeTree(layoutRoot, layoutRels, files, cache, limits, resolver)
	if err != nil {
		return nil, nil, resolvedBackground{}, nil, err
	}
	for key, donor := range layoutPlaceholders {
		if base := placeholders[key]; base != nil {
			donor.object = mergeInheritedObject(base.object, donor.object)
			donor.hasGeometry = donor.hasGeometry || base.hasGeometry
			donor.textSources = append(append([]*node(nil), base.textSources...), donor.textSources...)
		}
		placeholders[key] = donor
	}
	fixed = append(fixed, layoutFixed...)
	warnings = append(warnings, layoutWarnings...)
	return placeholders, fixed, background, warnings, nil
}

func readPartRootAndRelationships(files map[string]*zip.File, part string, limits Limits) (*node, map[string]string, error) {
	data, err := readPart(files, part, limits.MaxXMLPart)
	if err != nil {
		return nil, nil, err
	}
	root, err := parseXML(data, 128)
	if err != nil {
		return nil, nil, err
	}
	rels, err := readRelationships(files, part, limits.MaxXMLPart)
	if err != nil {
		return nil, nil, err
	}
	return root, rels, nil
}

func relationshipTargetContaining(rels map[string]string, segment string) string {
	var matches []string
	for _, target := range rels {
		if strings.Contains(target, segment) {
			matches = append(matches, target)
		}
	}
	sort.Strings(matches)
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

func parseInheritedShapeTree(root *node, rels map[string]string, files map[string]*zip.File, cache map[string][]byte, limits Limits, resolver colourResolver) (map[string]*inheritedShape, []Object, []Warning, error) {
	placeholders := map[string]*inheritedShape{}
	var fixed []Object
	var warnings []Warning
	tree := findFirst(root, "spTree")
	if tree == nil {
		return placeholders, fixed, warnings, nil
	}
	for _, child := range tree.Children {
		switch child.Name {
		case "sp":
			// A layout/master placeholder frequently carries only geometry and
			// default text properties. Retain that otherwise-empty donor so a
			// slide-local placeholder can inherit its complete appearance.
			object, err := parseShape(child, nil, resolver, nil, placeholderKey(child) != "")
			if err != nil {
				return nil, nil, nil, err
			}
			if object == nil {
				continue
			}
			if key := placeholderKey(child); key != "" {
				_, _, _, _, _, hasGeometry := transformWithPresence(child)
				placeholders[key] = &inheritedShape{object: *object, hasGeometry: hasGeometry, textSources: []*node{child}}
				continue
			}
			// Parent templates commonly contain empty authoring placeholders.
			// Only non-placeholder graphics are visible inherited artwork.
			if object.Text == "" || object.Fill != "" || object.Stroke != "" {
				fixed = append(fixed, *object)
			}
		case "pic":
			if object := parsePicture(child, rels, files, cache, limits, resolver); object != nil {
				fixed = append(fixed, *object)
			}
		case "cxnSp":
			if object := parseConnector(child, resolver); object != nil {
				fixed = append(fixed, *object)
			}
		case "grpSp":
			warnings = append(warnings, Warning{Code: "inherited-group-flattened", Message: "An inherited PowerPoint group was flattened during import."})
			objects, groupWarnings, err := parseGroup(child, rels, nil, files, cache, limits, resolver)
			if err != nil {
				return nil, nil, nil, err
			}
			fixed = append(fixed, objects...)
			warnings = append(warnings, groupWarnings...)
		case "graphicFrame":
			object, tableWarnings, err := parseGraphicFrame(child, rels, files, cache, limits, resolver)
			if err != nil {
				return nil, nil, nil, err
			}
			warnings = append(warnings, tableWarnings...)
			if object != nil {
				fixed = append(fixed, *object)
			}
		}
	}
	return placeholders, fixed, warnings, nil
}

func mergeInheritedObject(base, override Object) Object {
	if !override.hasShapeGeometry && base.hasShapeGeometry {
		override.Shape = base.Shape
		override.hasShapeGeometry = true
		override.shapeAdjustmentLoss = base.shapeAdjustmentLoss
		override.CustomGeometry = cloneCustomGeometry(base.CustomGeometry)
		override.ConnectionSites = append([]ConnectionSite(nil), base.ConnectionSites...)
		override.ShapeAdjustments = nil
		if len(base.ShapeAdjustments) > 0 {
			override.ShapeAdjustments = make(map[string]int64, len(base.ShapeAdjustments))
			for name, value := range base.ShapeAdjustments {
				override.ShapeAdjustments[name] = value
			}
		}
	}
	if override.FontFace == "" {
		override.FontFace = base.FontFace
	}
	if override.FontSize <= 0 {
		override.FontSize = base.FontSize
	}
	if override.Color == "" {
		override.Color = base.Color
		if !override.OpacitySet && override.Opacity == 0 && override.Fill == "" && override.Stroke == "" {
			override.Opacity, override.OpacitySet = base.Opacity, base.OpacitySet
		}
	}
	if override.TextAlign == "" {
		override.TextAlign = base.TextAlign
	}
	if override.VerticalAlign == "" {
		override.VerticalAlign = base.VerticalAlign
	}
	if !override.Bold {
		override.Bold = base.Bold
	}
	if !override.Italic {
		override.Italic = base.Italic
	}
	return override
}

func partBackground(root *node, resolver colourResolver) string {
	bg := findFirst(root, "bg")
	if bg == nil {
		return ""
	}
	if bgPr := findChild(bg, "bgPr"); bgPr != nil {
		colour, _ := resolvedPaint(findChild(bgPr, "solidFill"), resolver)
		return colour
	}
	if bgRef := findChild(bg, "bgRef"); bgRef != nil {
		colour, _ := resolvedColourNode(findFirst(bgRef, "schemeClr"), resolver)
		return colour
	}
	return ""
}

// partBackgroundImage reads PresentationML's <p:bgPr><a:blipFill> form. A
// picture background is distinct from a picture on the slide: it is a slide
// property, inherited through layout/master and always painted behind objects.
func partBackgroundImage(root *node, rels map[string]string, files map[string]*zip.File, limits Limits) ([]byte, string) {
	bg := findFirst(root, "bg")
	if bg == nil {
		return nil, ""
	}
	bgPr := findChild(bg, "bgPr")
	if bgPr == nil {
		return nil, ""
	}
	blip := findFirst(findChild(bgPr, "blipFill"), "blip")
	if blip == nil {
		return nil, ""
	}
	target := rels[attr(blip, "embed")]
	if target == "" {
		return nil, ""
	}
	// Media is accounted for by the package's global inflated-size limit. Keep
	// an individual picture background bounded by the input limit as well.
	limit := limits.MaxInputBytes
	if limit <= 0 {
		limit = 100 << 20
	}
	data, err := readPart(files, target, limit)
	if err != nil {
		return nil, ""
	}
	return data, mimeForPart(target)
}
func paragraphText(n *node) []string {
	body := findFirst(n, "txBody")
	if body == nil {
		return nil
	}
	var out []string
	for _, p := range children(body, "p") {
		var b strings.Builder
		for _, t := range descendants(p, "t") {
			b.WriteString(t.Text)
		}
		out = append(out, b.String())
	}
	return out
}

// Write emits a self-contained, macro-free Transitional PresentationML package.
func Write(ctx context.Context, target io.Writer, presentation Presentation) (Report, error) {
	if err := validateTheme(presentation.Theme); err != nil {
		return Report{}, err
	}
	if len(presentation.Slides) == 0 {
		return Report{}, errors.New("PowerPoint export needs at least one slide")
	}
	// Reject malformed semantic payloads before writing even a partial ZIP.
	for _, slide := range presentation.Slides {
		if slide.Background != "" {
			if _, _, _, ok := colourRGB(slide.Background); !ok {
				return Report{}, fmt.Errorf("invalid slide background colour")
			}
		}
		for _, object := range slide.Objects {
			// All object writers convert these values to integer DrawingML
			// coordinates. Reject invalid frames before creating a ZIP, not after
			// float-to-integer conversion has hidden a NaN/overflow as an integer.
			for _, value := range []float64{object.X, object.Y, object.Width, object.Height} {
				if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1e9 {
					return Report{}, fmt.Errorf("invalid object frame bounds")
				}
			}
			// Connector dimensions are signed endpoint deltas; connectorXML
			// normalizes them into positive extents plus flip flags.
			if object.Kind != "line" && (object.Width < 0 || object.Height < 0) {
				return Report{}, fmt.Errorf("object frame dimensions cannot be negative")
			}
			// ST_Angle is a signed 32-bit integer in 1/60000 degree units.
			angle := object.Rotation * 60000
			if math.IsNaN(angle) || math.IsInf(angle, 0) || angle < math.MinInt32 || angle > math.MaxInt32 {
				return Report{}, fmt.Errorf("object rotation exceeds DrawingML angle bounds")
			}
			if !validObjectDisplayName(object.Name) {
				return Report{}, fmt.Errorf("object names must be one line and at most 128 characters")
			}
			if err := validateConnections(object); err != nil {
				return Report{}, err
			}
			if object.CustomGeometry != nil && object.Kind != "shape" && object.Kind != "text" {
				return Report{}, fmt.Errorf("custom geometry requires a shape")
			}
			if err := validateCustomGeometry(object.CustomGeometry); err != nil {
				return Report{}, err
			}
			if err := validateTextEffects(object.TextEffects); err != nil {
				return Report{}, err
			}
			if err := validateShapePaint(object); err != nil {
				return Report{}, err
			}
			if object.Kind == "line" {
				if err := validateLinePoints(object.LinePoints); err != nil {
					return Report{}, err
				}
				if !validLineEnd(object.LineHead) || !validLineEnd(object.LineTail) {
					return Report{}, fmt.Errorf("invalid line arrowhead")
				}
				for _, value := range []float64{object.X, object.Y, object.Width, object.Height, object.Rotation, object.StrokeWidth} {
					if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 1e9 {
						return Report{}, fmt.Errorf("invalid line geometry")
					}
				}
				if object.StrokeWidth < 0 {
					return Report{}, fmt.Errorf("invalid line width")
				}
			}
			if math.IsNaN(object.Opacity) || math.IsInf(object.Opacity, 0) || object.Opacity < 0 || object.Opacity > 1 {
				return Report{}, fmt.Errorf("invalid object opacity")
			}
			if err := validateShapeAdjustments(object.ShapeAdjustments); err != nil {
				return Report{}, err
			}
			if object.Kind == "image" {
				if opacity := object.ImageOpacity; opacity != nil && (math.IsNaN(*opacity) || math.IsInf(*opacity, 0) || *opacity < 0 || *opacity > 1) {
					return Report{}, fmt.Errorf("invalid image opacity")
				}
				for _, inset := range []float64{object.CropLeft, object.CropTop, object.CropRight, object.CropBottom} {
					if math.IsNaN(inset) || math.IsInf(inset, 0) || math.Abs(inset) > 100 {
						return Report{}, fmt.Errorf("invalid image crop")
					}
				}
				if object.CropLeft+object.CropRight >= 1 || object.CropTop+object.CropBottom >= 1 {
					return Report{}, fmt.Errorf("image crop must retain visible source")
				}
				for _, inset := range []float64{object.FillLeft, object.FillTop, object.FillRight, object.FillBottom} {
					if math.IsNaN(inset) || math.IsInf(inset, 0) || math.Abs(inset) > 100 {
						return Report{}, fmt.Errorf("invalid image destination inset")
					}
				}
				if object.FillLeft+object.FillRight >= 1 || object.FillTop+object.FillBottom >= 1 {
					return Report{}, fmt.Errorf("image destination must retain visible area")
				}
			}
			if err := validateObjectText(object); err != nil {
				return Report{}, err
			}
			if err := ctx.Err(); err != nil {
				return Report{}, err
			}
			if object.Kind == "chart" {
				if err := validateChart(object.Chart); err != nil {
					return Report{}, err
				}
				for _, v := range []float64{object.X, object.Y, object.Width, object.Height, object.Rotation} {
					if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e8 {
						return Report{}, fmt.Errorf("invalid chart frame bounds")
					}
				}
				if object.Width <= 0 || object.Height <= 0 {
					return Report{}, fmt.Errorf("chart frame needs positive dimensions")
				}
			}
			if object.Kind == "table" {
				if err := validateTable(object.Table); err != nil {
					return Report{}, err
				}
				for _, v := range []float64{object.X, object.Y, object.Width, object.Height, object.Rotation} {
					if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e8 {
						return Report{}, fmt.Errorf("invalid table frame bounds")
					}
				}
				if object.Width <= 0 || object.Height <= 0 {
					return Report{}, fmt.Errorf("table frame needs positive dimensions")
				}
				for _, tracks := range [][]int64{tableEMUTracks(object.Table.Columns, object.Width), tableEMUTracks(object.Table.Rows, object.Height)} {
					for _, v := range tracks {
						if v <= 0 {
							return Report{}, fmt.Errorf("table tracks are too small for PowerPoint precision")
						}
					}
				}
			}
		}
	}
	if presentation.Width <= 0 {
		presentation.Width = SlideWidthEMU
	}
	if presentation.Height <= 0 {
		presentation.Height = SlideHeightEMU
	}
	presentation.EmbeddedFonts = usableEmbeddedFonts(presentation.EmbeddedFonts)
	var err error
	presentation.EmbeddedFonts, err = embeddedOpenTypeFonts(presentation.EmbeddedFonts)
	if err != nil {
		return Report{}, err
	}
	zw := zip.NewWriter(target)
	add := func(name, content string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, content)
		return err
	}
	assets := map[string]mediaPart{}
	var report Report
	for slideIndex, slide := range presentation.Slides {
		for objectIndex, object := range slide.Objects {
			if object.Kind != "image" || len(object.Image) == 0 {
				continue
			}
			key := base64.RawURLEncoding.EncodeToString(object.Image)
			if _, ok := assets[key]; !ok {
				assets[key] = mediaPart{Part: fmt.Sprintf("ppt/media/image%d%s", len(assets)+1, extensionForMIME(object.ImageMIME)), Data: object.Image, MIME: object.ImageMIME}
			}
			if len(object.ImageSVG) > 0 {
				vectorKey := "svg:" + base64.RawURLEncoding.EncodeToString(object.ImageSVG)
				if _, ok := assets[vectorKey]; !ok {
					assets[vectorKey] = mediaPart{Part: fmt.Sprintf("ppt/media/image%d.svg", len(assets)+1), Data: object.ImageSVG, MIME: "image/svg+xml"}
				}
			}
			_ = objectIndex
		}
		_ = slideIndex
	}
	if err := add("[Content_Types].xml", contentTypes(presentation, assets)); err != nil {
		return report, err
	}
	if err := add("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`); err != nil {
		return report, err
	}
	if err := add("docProps/core.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:creator>Keynope</dc:creator><dc:title>Keynope Presentation</dc:title><dcterms:created xsi:type="dcterms:W3CDTF">1980-01-01T00:00:00Z</dcterms:created></cp:coreProperties>`); err != nil {
		return report, err
	}
	if err := add("docProps/app.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>Keynope</Application><PresentationFormat>Custom</PresentationFormat><Slides>`+strconv.Itoa(len(presentation.Slides))+`</Slides></Properties>`); err != nil {
		return report, err
	}
	if err := add("ppt/presentation.xml", presentationXML(presentation)); err != nil {
		return report, err
	}
	if err := add("ppt/_rels/presentation.xml.rels", presentationRels(presentation)); err != nil {
		return report, err
	}
	if err := add("ppt/theme/theme1.xml", themeXML(presentation.Theme)); err != nil {
		return report, err
	}
	if err := add("ppt/slideMasters/slideMaster1.xml", masterXML()); err != nil {
		return report, err
	}
	if err := add("ppt/slideMasters/_rels/slideMaster1.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/></Relationships>`); err != nil {
		return report, err
	}
	if err := add("ppt/slideLayouts/slideLayout1.xml", layoutXML()); err != nil {
		return report, err
	}
	if err := add("ppt/slideLayouts/_rels/slideLayout1.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/></Relationships>`); err != nil {
		return report, err
	}
	for index, slide := range presentation.Slides {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		xmlPart, rels, warnings := slideXML(slide, assets, index)
		for i := range warnings {
			warnings[i].Slide = index
		}
		report.Warnings = append(report.Warnings, warnings...)
		if err := add(fmt.Sprintf("ppt/slides/slide%d.xml", index+1), xmlPart); err != nil {
			return report, err
		}
		if err := add(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", index+1), rels); err != nil {
			return report, err
		}
		for objectIndex, object := range slide.Objects {
			if object.Kind != "chart" {
				continue
			}
			name := chartPartName(index, objectIndex)
			workbook, err := chartWorkbook(object.Chart)
			if err != nil {
				return report, err
			}
			for _, part := range [][2]string{{"ppt/charts/" + name + ".xml", chartXML(object.Chart)}, {"ppt/charts/_rels/" + name + ".xml.rels", chartRelationships(name)}, {"ppt/embeddings/" + name + ".xlsx", string(workbook)}} {
				if err := add(part[0], part[1]); err != nil {
					return report, err
				}
			}
		}
	}
	keys := make([]string, 0, len(assets))
	for key := range assets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		asset := assets[key]
		h := &zip.FileHeader{Name: asset.Part, Method: zip.Deflate, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return report, err
		}
		if _, err = w.Write(asset.Data); err != nil {
			return report, err
		}
	}
	for index, embedded := range presentation.EmbeddedFonts {
		h := &zip.FileHeader{Name: fmt.Sprintf("ppt/fonts/font%d.fntdata", index+1), Method: zip.Deflate, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return report, err
		}
		if _, err = w.Write(embedded.Data); err != nil {
			return report, err
		}
	}
	if err := zw.Close(); err != nil {
		return report, err
	}
	return report.Normalized(), nil
}

func usableEmbeddedFonts(input []EmbeddedFont) []EmbeddedFont {
	fonts := make([]EmbeddedFont, 0, len(input))
	seen := map[string]bool{}
	for _, font := range input {
		font.Family = strings.TrimSpace(font.Family)
		if font.Family == "" || len(font.Data) == 0 || seen[font.Family] {
			continue
		}
		seen[font.Family] = true
		fonts = append(fonts, font)
	}
	return fonts
}

// embeddedOpenTypeFonts turns ordinary sfnt/TrueType data into the EOT font
// parts PowerPoint expects under application/x-fontdata. The payload stays
// uncompressed (flags = 0): it is larger, but deterministic, portable and
// avoids a proprietary compression dependency.
func embeddedOpenTypeFonts(input []EmbeddedFont) ([]EmbeddedFont, error) {
	fonts := make([]EmbeddedFont, 0, len(input))
	for _, font := range input {
		data, err := embeddedOpenType(font.Family, font.Data, font.ForceEditable)
		if err != nil {
			return nil, fmt.Errorf("prepare embedded font %q: %w", font.Family, err)
		}
		font.Data = data
		fonts = append(fonts, font)
	}
	return fonts, nil
}

// embeddedOpenType emits the version-2 EMBEDDEDFONT structure used by modern
// PowerPoint. An empty root string deliberately leaves the freely
// distributable bundled C64 font unrestricted. The sfnt data is uncompressed:
// that is larger, but avoids a proprietary compression dependency.
func embeddedOpenType(family string, source []byte, forceEditable bool) ([]byte, error) {
	// Never alter the source font stored by the caller. A presentation gets its
	// own copy, whose embedding rights can be made explicit for a font Keynope
	// itself distributes.
	sfnt := append([]byte(nil), source...)
	if len(sfnt) < 12 || len(sfnt) > math.MaxUint32 {
		return nil, errors.New("invalid TrueType/OpenType data")
	}
	tables, err := sfntTables(sfnt)
	if err != nil {
		return nil, err
	}
	metrics := eotMetrics{weight: 400, charset: 1}
	if table, ok := tables["OS/2"]; ok {
		if forceEditable && len(table) >= 10 {
			// 0x0008 is the OpenType "editable embedding" permission. This is
			// valid for Keynope's own freely distributable C64 typeface and
			// stops PowerPoint opening the exported deck read-only.
			binary.BigEndian.PutUint16(table[8:10], 0x0008)
		}
		if len(table) >= 6 {
			metrics.weight = uint32(binary.BigEndian.Uint16(table[4:6]))
		}
		if len(table) >= 42 {
			copy(metrics.panose[:], table[32:42])
		}
		if len(table) >= 58 {
			for index := range metrics.unicodeRange {
				metrics.unicodeRange[index] = binary.BigEndian.Uint32(table[42+index*4 : 46+index*4])
			}
		}
		if len(table) >= 64 && binary.BigEndian.Uint16(table[62:64])&1 != 0 {
			metrics.italic = 1
		}
		if len(table) >= 86 {
			metrics.codePageRange[0] = binary.BigEndian.Uint32(table[78:82])
			metrics.codePageRange[1] = binary.BigEndian.Uint32(table[82:86])
		}
		if len(table) >= 10 {
			metrics.fsType = binary.BigEndian.Uint16(table[8:10])
		}
	}
	if table, ok := tables["head"]; ok && len(table) >= 12 {
		metrics.checksumAdjustment = binary.BigEndian.Uint32(table[8:12])
	}
	names := eotFontNames{family: family, style: "Regular", version: "Version 1.0", full: family}
	if parsed, ok := sfntFontNames(tables["name"]); ok {
		if names.family == "" && parsed.family != "" {
			names.family = parsed.family
		}
		if parsed.style != "" {
			names.style = parsed.style
		}
		if parsed.version != "" {
			names.version = parsed.version
		}
		if parsed.full != "" {
			names.full = parsed.full
		}
	}
	if names.family == "" {
		return nil, errors.New("font has no family name")
	}
	if family != "" {
		names.full = family + " " + names.style
	}
	stringsUTF16 := [][]byte{utf16LE(names.family), utf16LE(names.style), utf16LE(names.version), utf16LE(names.full)}
	for _, value := range stringsUTF16 {
		if len(value) > math.MaxUint16 {
			return nil, errors.New("font name is too long")
		}
	}
	// The fixed v2 header is 82 bytes, followed by four length-prefixed UTF-16
	// strings, then the required Padding5 and empty RootStringSize fields.
	total := 86 + len(sfnt)
	for _, value := range stringsUTF16 {
		total += 4 + len(value)
	}
	if total > math.MaxUint32 {
		return nil, errors.New("embedded font is too large")
	}
	output := make([]byte, total)
	put32 := func(offset int, value uint32) { binary.LittleEndian.PutUint32(output[offset:offset+4], value) }
	put16 := func(offset int, value uint16) { binary.LittleEndian.PutUint16(output[offset:offset+2], value) }
	put32(0, uint32(total))
	put32(4, uint32(len(sfnt)))
	put32(8, 0x00020001)
	// Flags remain zero: sfnt data is neither subsetted nor compressed.
	copy(output[16:26], metrics.panose[:])
	output[26] = metrics.charset
	output[27] = metrics.italic
	put32(28, metrics.weight)
	put16(32, metrics.fsType)
	put16(34, 0x504C)
	for index, value := range metrics.unicodeRange {
		put32(36+index*4, value)
	}
	put32(52, metrics.codePageRange[0])
	put32(56, metrics.codePageRange[1])
	put32(60, metrics.checksumAdjustment)
	// Reserved1..4 (64..79) and Padding1 (80..81) are already zero.
	offset := 82
	for _, value := range stringsUTF16 {
		put16(offset, uint16(len(value)))
		offset += 2
		copy(output[offset:offset+len(value)], value)
		offset += len(value)
		// Padding2..4 is already zero.
		offset += 2
	}
	// Padding5 and RootStringSize are already zero at offset..offset+3.
	offset += 4
	copy(output[offset:], sfnt)
	return output, nil
}

type eotMetrics struct {
	panose             [10]byte
	charset, italic    byte
	weight             uint32
	fsType             uint16
	unicodeRange       [4]uint32
	codePageRange      [2]uint32
	checksumAdjustment uint32
}

type eotFontNames struct{ family, style, version, full string }

func sfntTables(data []byte) (map[string][]byte, error) {
	count := int(binary.BigEndian.Uint16(data[4:6]))
	if count < 1 || 12+count*16 > len(data) {
		return nil, errors.New("invalid sfnt table directory")
	}
	tables := make(map[string][]byte, count)
	for index := 0; index < count; index++ {
		offset := 12 + index*16
		name := string(data[offset : offset+4])
		start := int(binary.BigEndian.Uint32(data[offset+8 : offset+12]))
		length := int(binary.BigEndian.Uint32(data[offset+12 : offset+16]))
		if start < 0 || length < 0 || start > len(data) || length > len(data)-start {
			return nil, errors.New("invalid sfnt table bounds")
		}
		tables[name] = data[start : start+length]
	}
	return tables, nil
}

func sfntFontNames(table []byte) (eotFontNames, bool) {
	if len(table) < 6 {
		return eotFontNames{}, false
	}
	count := int(binary.BigEndian.Uint16(table[2:4]))
	stringsOffset := int(binary.BigEndian.Uint16(table[4:6]))
	if count < 1 || 6+count*12 > len(table) || stringsOffset > len(table) {
		return eotFontNames{}, false
	}
	type candidate struct {
		text string
		rank int
	}
	values := map[uint16]candidate{}
	for index := 0; index < count; index++ {
		offset := 6 + index*12
		platform, language := binary.BigEndian.Uint16(table[offset:offset+2]), binary.BigEndian.Uint16(table[offset+4:offset+6])
		nameID, length, start := binary.BigEndian.Uint16(table[offset+6:offset+8]), int(binary.BigEndian.Uint16(table[offset+8:offset+10])), int(binary.BigEndian.Uint16(table[offset+10:offset+12]))
		if nameID != 1 && nameID != 2 && nameID != 4 && nameID != 5 || start > len(table)-stringsOffset || length > len(table)-stringsOffset-start {
			continue
		}
		rank := 4
		switch {
		case platform == 3 && language == 0x0409:
			rank = 0
		case platform == 3:
			rank = 1
		case platform == 0:
			rank = 2
		case platform == 1:
			rank = 3
		default:
			continue
		}
		bytes := table[stringsOffset+start : stringsOffset+start+length]
		text := ""
		if platform == 1 {
			text = string(bytes) // The bundled font uses ASCII names.
		} else if len(bytes)%2 == 0 {
			codeUnits := make([]uint16, len(bytes)/2)
			for unit := range codeUnits {
				codeUnits[unit] = binary.BigEndian.Uint16(bytes[unit*2 : unit*2+2])
			}
			text = string(utf16.Decode(codeUnits))
		}
		if text != "" {
			if existing, ok := values[nameID]; !ok || rank < existing.rank {
				values[nameID] = candidate{text: text, rank: rank}
			}
		}
	}
	return eotFontNames{family: values[1].text, style: values[2].text, full: values[4].text, version: values[5].text}, true
}

func utf16LE(value string) []byte {
	units := utf16.Encode([]rune(value))
	output := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(output[index*2:index*2+2], unit)
	}
	return output
}

type mediaPart struct {
	Part string
	Data []byte
	MIME string
}

func presentationXML(p Presentation) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`)
	if len(p.EmbeddedFonts) > 0 {
		b.WriteString(` embedTrueTypeFonts="1" saveSubsetFonts="1"`)
	}
	b.WriteString(`><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>`)
	for i := range p.Slides {
		fmt.Fprintf(&b, `<p:sldId id="%d" r:id="rId%d"/>`, 256+i, i+2)
	}
	fmt.Fprintf(&b, `</p:sldIdLst><p:sldSz cx="%d" cy="%d" type="screen16x9"/><p:notesSz cx="6858000" cy="9144000"/>`, p.Width, p.Height)
	if len(p.EmbeddedFonts) > 0 {
		b.WriteString(`<p:embeddedFontLst>`)
		for index, font := range p.EmbeddedFonts {
			fmt.Fprintf(&b, `<p:embeddedFont><p:font typeface="%s" pitchFamily="34" charset="0"/><p:regular r:id="rId%d"/></p:embeddedFont>`, escapeAttr(font.Family), len(p.Slides)+index+2)
		}
		b.WriteString(`</p:embeddedFontLst>`)
	}
	b.WriteString(`</p:presentation>`)
	return b.String()
}
func presentationRels(p Presentation) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>`)
	for i := range p.Slides {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, i+2, i+1)
	}
	for index := range p.EmbeddedFonts {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/font" Target="fonts/font%d.fntdata"/>`, len(p.Slides)+index+2, index+1)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}
func contentTypes(p Presentation, assets map[string]mediaPart) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Default Extension="jpg" ContentType="image/jpeg"/><Default Extension="jpeg" ContentType="image/jpeg"/><Default Extension="fntdata" ContentType="application/x-fontdata"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/><Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/><Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/><Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>`)
	for i := range p.Slides {
		fmt.Fprintf(&b, `<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, i+1)
		for j, object := range p.Slides[i].Objects {
			if object.Kind == "chart" {
				name := chartPartName(i, j)
				fmt.Fprintf(&b, `<Override PartName="/ppt/charts/%s.xml" ContentType="application/vnd.openxmlformats-officedocument.drawingml.chart+xml"/><Override PartName="/ppt/embeddings/%s.xlsx" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"/>`, name, name)
			}
		}
	}
	seen := map[string]bool{}
	for _, asset := range assets {
		if (asset.MIME == "image/gif" || asset.MIME == "image/svg+xml") && !seen[asset.MIME] {
			seen[asset.MIME] = true
			fmt.Fprintf(&b, `<Default Extension="%s" ContentType="%s"/>`, strings.TrimPrefix(extensionForMIME(asset.MIME), "."), asset.MIME)
		}
	}
	b.WriteString(`</Types>`)
	return b.String()
}
func baseTree() string {
	return `<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
}
func masterXML() string {
	// clrMap is mandatory in a slide master. Omitting it (and emitting empty
	// style-list containers in the theme) makes PowerPoint repair the package.
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sldMaster xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld name="Keynope">` + baseTree() + `</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`
}
func layoutXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sldLayout xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" type="blank" preserve="1"><p:cSld name="Blank">` + baseTree() + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`
}
func themeXML(themes ...*Theme) string {
	var theme *Theme
	if len(themes) > 0 {
		theme = themes[0]
	}
	// Each format-style list has a schema minimum of three children. Empty
	// containers are accepted by some readers but trigger repair in PowerPoint.
	const fill = `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	const line = `<a:ln w="9525" cap="flat" cmpd="sng" algn="ctr"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/><a:round/><a:headEnd type="none" w="med" len="med"/><a:tailEnd type="none" w="med" len="med"/></a:ln>`
	const effect = `<a:effectStyle><a:effectLst/></a:effectStyle>`
	return themeHeader(theme) + `<a:fmtScheme name="Keynope"><a:fillStyleLst>` + fill + fill + fill + `</a:fillStyleLst><a:lnStyleLst>` + line + line + line + `</a:lnStyleLst><a:effectStyleLst>` + effect + effect + effect + `</a:effectStyleLst><a:bgFillStyleLst>` + fill + fill + fill + `</a:bgFillStyleLst></a:fmtScheme></a:themeElements><a:objectDefaults/><a:extraClrSchemeLst/></a:theme>`
}

func slideXML(slide Slide, assets map[string]mediaPart, slideIndexes ...int) (string, string, []Warning) {
	slideIndex := 0
	if len(slideIndexes) > 0 {
		slideIndex = slideIndexes[0]
	}
	var b strings.Builder
	var rels strings.Builder
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>`)
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:p14="http://schemas.microsoft.com/office/powerpoint/2010/main" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" mc:Ignorable="p14"`)
	if slide.Hidden {
		b.WriteString(` show="0"`)
	}
	b.WriteString(`><p:cSld>`)
	if slide.Background != "" {
		b.WriteString(`<p:bg><p:bgPr>` + solidFill(slide.Background) + `<a:effectLst/></p:bgPr></p:bg>`)
	}
	b.WriteString(baseTree())
	var warnings []Warning
	shapeIDs := map[string]int{}
	connectionIDs, connectionCounts := map[string]int{}, map[string]int{}
	connectionID := 2
	for _, object := range slide.Objects {
		if !hasExportBounds(object) {
			continue
		}
		if object.Kind == "image" {
			if _, exists := assets[base64.RawURLEncoding.EncodeToString(object.Image)]; !exists {
				continue
			}
		}
		if _, duplicate := connectionIDs[object.ID]; duplicate {
			connectionIDs[object.ID] = 0
		} else {
			connectionIDs[object.ID] = connectionID
		}
		connectionCounts[object.ID] = len(object.ConnectionSites)
		connectionID++
	}
	nextID := 2
	nextRel := 2
	for objectIndex, o := range slide.Objects {
		if o.Kind == "line" && len(o.LinePoints) > 1 {
			_, o.X, o.Y, o.Width, o.Height = routedLineGeometry(o.LinePoints)
		}
		if o.Kind != "line" && (o.Width <= 0 || o.Height <= 0) || o.Kind == "line" && o.Width == 0 && o.Height == 0 {
			warnings = append(warnings, Warning{Object: o.ID, Code: "invalid-bounds", Message: "Object with empty bounds was skipped."})
			continue
		}
		if o.Kind == "line" {
			var loss []Warning
			o, loss = resolveSlideConnections(o, connectionIDs, connectionCounts)
			warnings = append(warnings, loss...)
		}
		switch o.Kind {
		case "chart":
			rid := fmt.Sprintf("rId%d", nextRel)
			nextRel++
			fmt.Fprintf(&rels, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart" Target="../charts/%s.xml"/>`, rid, chartPartName(slideIndex, objectIndex))
			b.WriteString(chartFrameXML(o, nextID, rid))
			if o.Link != "" {
				warnings = append(warnings, Warning{Object: o.ID, Code: "chart-link-omitted", Message: "Chart-level hyperlinks were not exported."})
			}
		case "table":
			b.WriteString(tableXML(o, nextID, ""))
			if o.Link != "" {
				warnings = append(warnings, Warning{Object: o.ID, Code: "table-link-omitted", Message: "Table-level hyperlinks were not exported."})
			}
		case "image":
			key := base64.RawURLEncoding.EncodeToString(o.Image)
			asset, ok := assets[key]
			if !ok {
				warnings = append(warnings, Warning{Object: o.ID, Code: "image-missing", Message: "Image had no embedded bytes and was skipped."})
				continue
			}
			rid := fmt.Sprintf("rId%d", nextRel)
			nextRel++
			fmt.Fprintf(&rels, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/%s"/>`, rid, path.Base(asset.Part))
			vectorRID := ""
			if len(o.ImageSVG) > 0 {
				if vector, ok := assets["svg:"+base64.RawURLEncoding.EncodeToString(o.ImageSVG)]; ok {
					vectorRID = fmt.Sprintf("rId%d", nextRel)
					nextRel++
					fmt.Fprintf(&rels, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/%s"/>`, vectorRID, path.Base(vector.Part))
				}
			}
			b.WriteString(pictureXML(o, nextID, rid, vectorRID))
		default:
			linkID := ""
			if o.Link != "" {
				if safeExternalLink(o.Link) {
					linkID = fmt.Sprintf("rId%d", nextRel)
					nextRel++
					fmt.Fprintf(&rels, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`, linkID, escapeAttr(o.Link))
				} else {
					warnings = append(warnings, Warning{Object: o.ID, Code: "link-omitted", Message: "Only ordinary http, https and mailto links can be exported to PowerPoint."})
				}
			}
			b.WriteString(shapeXML(o, nextID, linkID))
		}
		if _, duplicate := shapeIDs[o.ID]; duplicate {
			shapeIDs[o.ID] = 0
		} else {
			shapeIDs[o.ID] = nextID
		}
		nextID++
	}
	b.WriteString(`</p:spTree></p:cSld>`)
	timing, timingWarnings := timelineXML(slide.Timeline, shapeIDs, slide.Objects)
	b.WriteString(timing)
	warnings = append(warnings, timingWarnings...)
	b.WriteString(`</p:sld>`)
	rels.WriteString(`</Relationships>`)
	return b.String(), rels.String(), warnings
}
func shapeXML(o Object, id int, linkID string) string {
	if o.Kind == "line" {
		return connectorXML(o, id, linkID)
	}
	x, y, w, h := emu(o.X), emu(o.Y), emu(o.Width), emu(o.Height)
	rot := ""
	if o.Rotation != 0 {
		rot = ` rot="` + strconv.FormatInt(int64(o.Rotation*60000), 10) + `"`
	}
	shape := o.Shape
	if shape == "" {
		shape = "rect"
	}
	var b strings.Builder
	link := ""
	if linkID != "" {
		link = `<a:hlinkClick r:id="` + linkID + `"/>`
	}
	fmt.Fprintf(&b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s">%s</p:cNvPr><p:cNvSpPr txBox="%d"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm%s><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`, id, escapeAttr(objectDisplayName(o)), link, boolNumber(o.Kind == "text"), rot, x, y, w, h)
	if o.CustomGeometry != nil {
		b.WriteString(customGeometryXML(o.CustomGeometry, o.ShapeAdjustments, o.ConnectionSites))
	} else {
		fmt.Fprintf(&b, `<a:prstGeom prst="%s">%s</a:prstGeom>`, escapeAttr(shape), shapeAdjustmentXML(o.ShapeAdjustments))
	}
	if o.Gradient != nil {
		b.WriteString(gradientFillXML(o))
	} else if o.Fill != "" {
		b.WriteString(objectSolidFill(o.Fill, o))
	} else {
		b.WriteString(`<a:noFill/>`)
	}
	if o.Stroke != "" {
		fmt.Fprintf(&b, `<a:ln w="%d">%s<a:round/></a:ln>`, int64(maxFloat(o.StrokeWidth, 0)*12700), objectSolidFill(o.Stroke, o))
	} else {
		b.WriteString(`<a:ln><a:noFill/></a:ln>`)
	}
	b.WriteString(shadowEffectXML(o))
	b.WriteString(`</p:spPr>`)
	if o.Text != "" || o.Kind == "text" {
		b.WriteString(textBody(o))
	}
	b.WriteString(`</p:sp>`)
	return b.String()
}
func pictureXML(o Object, id int, rid string, vectorRID string) string {
	shape, adjustments := "rect", "<a:avLst/>"
	if o.Shape == "ellipse" {
		shape = "ellipse"
	}
	if o.Shape == "roundRect" {
		shape = "roundRect"
		adjustments = `<a:avLst><a:gd name="adj" fmla="val 12000"/></a:avLst>`
	}
	x, y, w, h := emu(o.X), emu(o.Y), emu(o.Width), emu(o.Height)
	rot := ""
	if o.Rotation != 0 {
		rot = ` rot="` + strconv.FormatInt(int64(o.Rotation*60000), 10) + `"`
	}
	vector := ""
	crop := ""
	alpha := ""
	if o.ImageOpacity != nil {
		alpha = fmt.Sprintf(`<a:alphaModFix amt="%d"/>`, int64(math.Round(*o.ImageOpacity*100000)))
	}
	fill := "<a:fillRect/>"
	if o.FillLeft != 0 || o.FillTop != 0 || o.FillRight != 0 || o.FillBottom != 0 {
		fill = fmt.Sprintf(`<a:fillRect l="%d" t="%d" r="%d" b="%d"/>`, int64(math.Round(o.FillLeft*100000)), int64(math.Round(o.FillTop*100000)), int64(math.Round(o.FillRight*100000)), int64(math.Round(o.FillBottom*100000)))
	}
	if o.CropLeft != 0 || o.CropTop != 0 || o.CropRight != 0 || o.CropBottom != 0 {
		crop = fmt.Sprintf(`<a:srcRect l="%d" t="%d" r="%d" b="%d"/>`, int64(math.Round(o.CropLeft*100000)), int64(math.Round(o.CropTop*100000)), int64(math.Round(o.CropRight*100000)), int64(math.Round(o.CropBottom*100000)))
	}
	if vectorRID != "" {
		vector = `<a:extLst><a:ext uri="{96DAC541-7B7A-43D3-8B79-37D633B846F1}"><asvg:svgBlip xmlns:asvg="http://schemas.microsoft.com/office/drawing/2016/SVG/main" r:embed="` + escapeAttr(vectorRID) + `"/></a:ext></a:extLst>`
	}
	return fmt.Sprintf(`<p:pic><p:nvPicPr><p:cNvPr id="%d" name="%s"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="%s">%s</a:blip>%s<a:stretch>%s</a:stretch></p:blipFill><p:spPr><a:xfrm%s><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="%s">%s</a:prstGeom>%s</p:spPr></p:pic>`, id, escapeAttr(objectDisplayName(o)), rid, alpha+vector, crop, fill, rot, x, y, w, h, shape, adjustments, shadowEffectXML(o))
}
func legacyTextBody(o Object) string {
	face := o.FontFace
	if face == "" {
		face = "Arial"
	}
	size := o.FontSize
	if size <= 0 {
		size = 18
	}
	color := o.Color
	if color == "" {
		color = "#000000"
	}
	var b strings.Builder
	// Office supplies roughly 0.1in insets when they are omitted. Keynope's
	// text bounds are already the authored usable area, so retaining those
	// implicit margins clips text that precisely fits on the Keynope canvas.
	bodyPr := `<a:bodyPr wrap="square" lIns="0" rIns="0" tIns="0" bIns="0"`
	switch o.VerticalAlign {
	case "middle":
		bodyPr += ` anchor="ctr"`
	case "bottom":
		bodyPr += ` anchor="b"`
	default:
		bodyPr += ` anchor="t"`
	}
	bodyPr += `/>`
	paragraphPr := ""
	switch o.TextAlign {
	case "center":
		paragraphPr = `<a:pPr algn="ctr"/>`
	case "end", "right":
		paragraphPr = `<a:pPr algn="r"/>`
	case "justify":
		paragraphPr = `<a:pPr algn="just"/>`
	default:
		paragraphPr = `<a:pPr algn="l"/>`
	}
	b.WriteString(`<p:txBody>` + bodyPr + `<a:lstStyle/>`)
	emphasis := ""
	if o.Bold {
		emphasis += ` b="1"`
	}
	if o.Italic {
		emphasis += ` i="1"`
	}
	for _, line := range strings.Split(o.Text, "\n") {
		b.WriteString(`<a:p>` + paragraphPr + `<a:r><a:rPr lang="en-US" sz="` + strconv.Itoa(int(size*100)) + `"` + emphasis + `>` + textRunPaintXML(color, o) + `<a:latin typeface="` + escapeAttr(face) + `"/></a:rPr><a:t>` + escapeText(line) + `</a:t></a:r></a:p>`)
	}
	b.WriteString(`</p:txBody>`)
	return b.String()
}
func solidFill(color string) string {
	return `<a:solidFill><a:srgbClr val="` + strings.TrimPrefix(strings.ToUpper(color), "#") + `"/></a:solidFill>`
}

// DrawingML has no shared object alpha for editable shapes. Apply the same
// alpha to each paint, including run colour overrides, exactly once.
func objectSolidFill(color string, object Object) string {
	if !object.OpacitySet && object.Opacity == 0 {
		return solidFill(color)
	}
	return fmt.Sprintf(`<a:solidFill><a:srgbClr val="%s"><a:alpha val="%d"/></a:srgbClr></a:solidFill>`, strings.TrimPrefix(strings.ToUpper(color), "#"), int64(math.Round(object.Opacity*100000)))
}
func emu(v float64) int64 { return int64(v*6350 + .5) }
func boolNumber(v bool) int {
	if v {
		return 1
	}
	return 0
}
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func escapeText(v string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(v)
}
func escapeAttr(v string) string { return escapeText(v) }
func extensionForMIME(m string) string {
	switch strings.ToLower(m) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".png"
	}
}
func mimeForPart(v string) string {
	switch strings.ToLower(path.Ext(v)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

type node struct {
	Name     string
	Attr     map[string]string
	Text     string
	Children []*node
}

func parseXML(data []byte, maxDepth int) (*node, error) {
	var stack []*node
	var root *node
	for offset := 0; offset < len(data); {
		start := bytes.IndexByte(data[offset:], '<')
		if start < 0 {
			if len(stack) > 0 {
				stack[len(stack)-1].Text += stdhtml.UnescapeString(string(data[offset:]))
			}
			break
		}
		start += offset
		if start > offset && len(stack) > 0 {
			stack[len(stack)-1].Text += stdhtml.UnescapeString(string(data[offset:start]))
		}
		if bytes.HasPrefix(data[start:], []byte("<!--")) {
			end := bytes.Index(data[start+4:], []byte("-->"))
			if end < 0 {
				return nil, errors.New("unterminated XML comment")
			}
			offset = start + 4 + end + 3
			continue
		}
		if bytes.HasPrefix(data[start:], []byte("<?")) {
			end := bytes.Index(data[start+2:], []byte("?>"))
			if end < 0 {
				return nil, errors.New("unterminated XML processing instruction")
			}
			offset = start + 2 + end + 2
			continue
		}
		if bytes.HasPrefix(data[start:], []byte("<![CDATA[")) {
			end := bytes.Index(data[start+9:], []byte("]]>"))
			if end < 0 {
				return nil, errors.New("unterminated CDATA")
			}
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(data[start+9 : start+9+end])
			}
			offset = start + 9 + end + 3
			continue
		}
		if bytes.HasPrefix(data[start:], []byte("<!")) {
			return nil, errors.New("DTD/directives are not allowed")
		}
		end, err := xmlTagEnd(data, start+1)
		if err != nil {
			return nil, err
		}
		inside := strings.TrimSpace(string(data[start+1 : end]))
		offset = end + 1
		if inside == "" {
			return nil, errors.New("empty XML tag")
		}
		if inside[0] == '/' {
			if len(stack) == 0 || xmlLocalName(strings.TrimSpace(inside[1:])) != stack[len(stack)-1].Name {
				return nil, errors.New("unexpected XML end")
			}
			stack = stack[:len(stack)-1]
			continue
		}
		selfClosing := strings.HasSuffix(inside, "/")
		if selfClosing {
			inside = strings.TrimSpace(strings.TrimSuffix(inside, "/"))
		}
		name, attrs, err := xmlTag(inside)
		if err != nil {
			return nil, err
		}
		if len(stack) >= maxDepth {
			return nil, errors.New("XML nesting exceeds limit")
		}
		n := &node{Name: name, Attr: attrs}
		if len(stack) > 0 {
			stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, n)
		} else if root == nil {
			root = n
		} else {
			return nil, errors.New("multiple XML roots")
		}
		if !selfClosing {
			stack = append(stack, n)
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, errors.New("incomplete XML")
	}
	return root, nil
}

func xmlTagEnd(data []byte, start int) (int, error) {
	quote := byte(0)
	for i := start; i < len(data); i++ {
		c := data[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '>' {
			return i, nil
		}
	}
	return 0, errors.New("unterminated XML tag")
}
func xmlTag(value string) (string, map[string]string, error) {
	i := 0
	for i < len(value) && !isXMLSpace(value[i]) {
		i++
	}
	name := xmlLocalName(value[:i])
	if name == "" {
		return "", nil, errors.New("invalid XML tag")
	}
	attrs := map[string]string{}
	for i < len(value) {
		for i < len(value) && isXMLSpace(value[i]) {
			i++
		}
		if i >= len(value) {
			break
		}
		start := i
		for i < len(value) && !isXMLSpace(value[i]) && value[i] != '=' {
			i++
		}
		rawKey := value[start:i]
		key := xmlLocalName(rawKey)
		for i < len(value) && isXMLSpace(value[i]) {
			i++
		}
		if i >= len(value) || value[i] != '=' || key == "" {
			return "", nil, errors.New("invalid XML attribute")
		}
		i++
		for i < len(value) && isXMLSpace(value[i]) {
			i++
		}
		if i >= len(value) || (value[i] != '\'' && value[i] != '"') {
			return "", nil, errors.New("XML attribute must be quoted")
		}
		quote := value[i]
		i++
		start = i
		for i < len(value) && value[i] != quote {
			i++
		}
		if i >= len(value) {
			return "", nil, errors.New("unterminated XML attribute")
		}
		if _, duplicate := attrs[rawKey]; duplicate {
			return "", nil, errors.New("duplicate XML attribute")
		}
		attrs[rawKey] = stdhtml.UnescapeString(value[start:i])
		i++
	}
	for key, value := range attrs {
		local := xmlLocalName(key)
		if _, exists := attrs[local]; !exists {
			attrs[local] = value
		}
	}
	return name, attrs, nil
}
func isXMLSpace(v byte) bool { return v == ' ' || v == '\t' || v == '\r' || v == '\n' }
func xmlLocalName(v string) string {
	if index := strings.LastIndexByte(v, ':'); index >= 0 {
		v = v[index+1:]
	}
	return v
}
func attr(n *node, key string) string {
	if n == nil {
		return ""
	}
	return n.Attr[key]
}
func children(n *node, name string) []*node {
	if n == nil {
		return nil
	}
	var out []*node
	for _, c := range n.Children {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}
func findChild(n *node, name string) *node {
	if n == nil {
		return nil
	}
	for _, child := range n.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}
func findFirst(n *node, name string) *node {
	if n == nil {
		return nil
	}
	if n.Name == name {
		return n
	}
	for _, c := range n.Children {
		if r := findFirst(c, name); r != nil {
			return r
		}
	}
	return nil
}
func descendants(n *node, name string) []*node {
	if n == nil {
		return nil
	}
	var out []*node
	for _, c := range n.Children {
		if c.Name == name {
			out = append(out, c)
		}
		out = append(out, descendants(c, name)...)
	}
	return out
}
func colorFrom(n *node) string {
	if n == nil {
		return ""
	}
	if c := findFirst(n, "srgbClr"); c != nil {
		v := strings.TrimPrefix(attr(c, "val"), "#")
		if len(v) == 6 {
			return "#" + strings.ToUpper(v)
		}
	}
	return ""
}

func readColourResolver(files map[string]*zip.File, rels map[string]string, limits Limits, root *node) (colourResolver, error) {
	resolver := defaultColourResolver()
	resolver.parts = &themePartCache{entries: make(map[string]themePart)}
	master := ""
	list := findFirst(root, "sldMasterIdLst")
	declared := children(list, "sldMasterId")
	if len(declared) > 0 {
		master = rels[relationshipID(declared[0], list, root)]
		if master == "" {
			return resolver, errors.New("presentation master relationship is missing")
		}
	} else {
		// Legacy minimal packages can omit master relationships. Their fallback
		// is deterministic, never the first entry encountered in a Go map.
		masters := sortedXMLParts(files, "ppt/slideMasters/")
		if len(masters) > 0 {
			master = masters[0]
		}
		themes := sortedXMLParts(files, "ppt/theme/")
		if len(themes) > 0 {
			var err error
			resolver, err = themeColourResolver(files, themes[0], limits, resolver.parts)
			if err != nil {
				return resolver, err
			}
		}
	}
	if master != "" {
		return masterColourResolver(files, master, limits, resolver)
	}
	return resolver, nil
}

func firstColourChild(n *node) *node {
	if n == nil {
		return nil
	}
	for _, child := range n.Children {
		switch child.Name {
		case "srgbClr", "schemeClr", "sysClr":
			return child
		}
	}
	return nil
}

func resolvedPaint(n *node, resolver colourResolver) (string, float64) {
	if n == nil {
		return "", 0
	}
	return resolvedColourNode(firstColourChild(n), resolver)
}

func resolvedColourNode(n *node, resolver colourResolver) (string, float64) {
	colour, alpha := rawColourNode(n)
	if n == nil {
		return "", alpha
	}
	if n.Name == "schemeClr" {
		key := attr(n, "val")
		if mapped := resolver.mapping[key]; mapped != "" {
			key = mapped
		}
		colour = resolver.colours[key]
	}
	if colour == "" {
		return "", alpha
	}
	r, g, b, ok := colourRGB(colour)
	if !ok {
		return colour, alpha
	}
	for _, transform := range n.Children {
		value, _ := strconv.ParseFloat(attr(transform, "val"), 64)
		switch transform.Name {
		case "tint":
			factor := clampUnit(value / 100000)
			r = mixColour(r, 255, factor)
			g = mixColour(g, 255, factor)
			b = mixColour(b, 255, factor)
		case "shade":
			factor := clampUnit(value / 100000)
			r, g, b = r*factor, g*factor, b*factor
		case "lumMod":
			factor := value / 100000
			r, g, b = clamp255(r*factor), clamp255(g*factor), clamp255(b*factor)
		case "lumOff":
			offset := 255 * value / 100000
			r, g, b = clamp255(r+offset), clamp255(g+offset), clamp255(b+offset)
		case "alpha":
			alpha = clampUnit(value / 100000)
		}
	}
	return fmt.Sprintf("#%02X%02X%02X", int(math.Round(r)), int(math.Round(g)), int(math.Round(b))), alpha
}

func rawColourNode(n *node) (string, float64) {
	if n == nil {
		return "", 0
	}
	alpha := 0.0
	if n.Name == "srgbClr" {
		v := strings.TrimPrefix(attr(n, "val"), "#")
		if len(v) == 6 {
			return "#" + strings.ToUpper(v), alpha
		}
	}
	if n.Name == "sysClr" {
		v := strings.TrimPrefix(attr(n, "lastClr"), "#")
		if len(v) == 6 {
			return "#" + strings.ToUpper(v), alpha
		}
	}
	return "", alpha
}

func colourRGB(value string) (float64, float64, float64, bool) {
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return 0, 0, 0, false
	}
	n, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return float64((n >> 16) & 0xff), float64((n >> 8) & 0xff), float64(n & 0xff), true
}
func clampUnit(v float64) float64       { return math.Max(0, math.Min(1, v)) }
func clamp255(v float64) float64        { return math.Max(0, math.Min(255, v)) }
func mixColour(a, b, t float64) float64 { return a + (b-a)*t }
func cleanPart(name string) (string, bool) {
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", false
	}
	clean := path.Clean(name)
	if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "//") {
		return "", false
	}
	return clean, true
}
func readPart(files map[string]*zip.File, name string, limit int64) ([]byte, error) {
	clean, ok := cleanPart(name)
	if !ok {
		return nil, errors.New("unsafe part target")
	}
	f, ok := files[clean]
	if !ok {
		return nil, fmt.Errorf("missing part %q", clean)
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("part %q exceeds size limit", clean)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("part %q exceeds size limit", clean)
	}
	return data, nil
}
func readRelationships(files map[string]*zip.File, owner string, limit int64) (map[string]string, error) {
	dir, base := path.Dir(owner), path.Base(owner)
	relsPart := path.Join(dir, "_rels", base+".rels")
	f, ok := files[relsPart]
	if !ok {
		return map[string]string{}, nil
	}
	data, err := readPart(files, relsPart, limit)
	if err != nil {
		return nil, err
	}
	root, err := parseXML(data, 128)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range children(root, "Relationship") {
		if strings.EqualFold(attr(r, "TargetMode"), "External") {
			continue
		}
		target, ok := resolvePart(owner, attr(r, "Target"))
		if !ok {
			return nil, errors.New("unsafe relationship target")
		}
		out[attr(r, "Id")] = target
	}
	_ = f
	return out, nil
}
func readExternalRelationships(files map[string]*zip.File, owner string, limit int64) (map[string]string, error) {
	dir, base := path.Dir(owner), path.Base(owner)
	relsPart := path.Join(dir, "_rels", base+".rels")
	if _, ok := files[relsPart]; !ok {
		return map[string]string{}, nil
	}
	data, err := readPart(files, relsPart, limit)
	if err != nil {
		return nil, err
	}
	root, err := parseXML(data, 128)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range children(root, "Relationship") {
		if !strings.EqualFold(attr(r, "TargetMode"), "External") {
			continue
		}
		target := attr(r, "Target")
		if safeExternalLink(target) {
			out[attr(r, "Id")] = target
		}
	}
	return out, nil
}
func safeExternalLink(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || strings.ContainsAny(value, "\r\n") {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	default:
		return false
	}
}
func resolvePart(owner, target string) (string, bool) {
	if target == "" {
		return "", false
	}
	resolved := path.Clean(path.Join(path.Dir(owner), target))
	if strings.HasPrefix(resolved, "../") || resolved == "." {
		return "", false
	}
	return resolved, true
}
func parseInt64(raw string, fallback int64) (int64, error) {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback, err
	}
	return v, nil
}
