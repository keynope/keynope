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
	Slide   int    `json:"slide,omitempty"`
	Object  string `json:"object,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Presentation struct {
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
	Background      string // Resolved solid background colour, when the source supplies one.
	BackgroundImage []byte // Resolved picture background, when the source supplies one.
	BackgroundMIME  string
	Objects         []Object
}
type Object struct {
	ID                  string
	Kind                string // text, shape, image or line
	X, Y, Width, Height float64
	Rotation            float64
	Text                string
	FontFace            string
	FontSize            float64 // points
	Color               string  // #RRGGBB
	Opacity             float64 // 0 means unspecified; otherwise 0..1
	Fill                string  // #RRGGBB or empty
	Stroke              string  // #RRGGBB or empty
	StrokeWidth         float64 // points
	Shape               string
	FlipH               bool
	FlipV               bool
	Link                string
	Image               []byte
	ImageMIME           string
	CropLeft            float64 // Fractional source insets from a:srcRect.
	CropTop             float64
	CropRight           float64
	CropBottom          float64
	TextAlign           string // start, center, end or justify
	VerticalAlign       string // top, middle or bottom
	Bold                bool
	Italic              bool
}

// colourResolver holds the presentation theme and the active colour map. It
// deliberately lives inside the reader: callers receive concrete RGB values
// rather than theme tokens that would render differently on the Mac and web.
type colourResolver struct {
	colours map[string]string
	mapping map[string]string
}

type inheritedShape struct {
	object      Object
	hasGeometry bool
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
	resolver, err := readColourResolver(files, rels, limits)
	if err != nil {
		return Presentation{}, Report{}, err
	}
	var report Report
	for slideIndex, ref := range children(list, "sldId") {
		if slideIndex >= limits.MaxSlides {
			return Presentation{}, Report{}, fmt.Errorf("PPTX has more than %d slides", limits.MaxSlides)
		}
		relID := attr(ref, "id")
		target, ok := rels[relID]
		if !ok {
			return Presentation{}, Report{}, fmt.Errorf("slide relationship %q is missing", relID)
		}
		slide, warnings, err := readSlide(ctx, files, target, limits, resolver)
		if err != nil {
			return Presentation{}, Report{}, fmt.Errorf("read slide %d: %w", slideIndex+1, err)
		}
		slide.ID = attr(ref, "id")
		slide.Hidden = strings.EqualFold(attr(ref, "show"), "0") || strings.EqualFold(attr(ref, "show"), "false")
		for i := range warnings {
			warnings[i].Slide = slideIndex
			report.Warnings = append(report.Warnings, warnings[i])
		}
		result.Slides = append(result.Slides, slide)
	}
	return result, report, nil
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
	objects = append(objects, parentObjects...)
	for _, child := range spTree.Children {
		if err := ctx.Err(); err != nil {
			return Slide{}, nil, err
		}
		var object *Object
		switch child.Name {
		case "sp":
			object = parseShape(child, links, resolver, parents[placeholderKey(child)], false)
		case "pic":
			object = parsePicture(child, rels, files, mediaCache, limits)
		case "cxnSp":
			object = parseConnector(child, resolver)
		case "grpSp":
			warnings = append(warnings, Warning{Code: "group-flattened", Message: "Nested PowerPoint group was flattened during import."})
			objects = append(objects, parseGroup(child, rels, links, files, mediaCache, limits, resolver)...)
		case "graphicFrame":
			warnings = append(warnings, Warning{Code: "unsupported-graphic-frame", Message: "A table, chart or other graphic frame was not imported."})
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
	return Slide{Background: background.Colour, BackgroundImage: background.Image, BackgroundMIME: background.MIME, Objects: objects}, warnings, nil
}

func parseGroup(n *node, rels, links map[string]string, files map[string]*zip.File, cache map[string][]byte, limits Limits, resolver colourResolver) []Object {
	var objects []Object
	for _, child := range n.Children {
		switch child.Name {
		case "sp":
			if o := parseShape(child, links, resolver, nil, false); o != nil {
				objects = append(objects, *o)
			}
		case "pic":
			if o := parsePicture(child, rels, files, cache, limits); o != nil {
				objects = append(objects, *o)
			}
		case "cxnSp":
			if o := parseConnector(child, resolver); o != nil {
				objects = append(objects, *o)
			}
		case "grpSp":
			objects = append(objects, parseGroup(child, rels, links, files, cache, limits, resolver)...)
		}
	}
	return objects
}

func parseShape(n *node, links map[string]string, resolver colourResolver, inherited *inheritedShape, retainEmpty bool) *Object {
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
	}
	// Restrict paint lookup to spPr. A text run can have its own solidFill;
	// treating that run colour as a shape fill changes a text box into a shape.
	spPr := findFirst(n, "spPr")
	fill, opacity := resolvedPaint(findChild(spPr, "solidFill"), resolver)
	stroke, strokeOpacity := "", 0.0
	width := 0.0
	if line := findChild(spPr, "ln"); line != nil {
		stroke, strokeOpacity = resolvedPaint(findFirst(line, "solidFill"), resolver)
		if v, _ := strconv.ParseFloat(attr(line, "w"), 64); v > 0 {
			width = v / 12700
		}
	}
	fontFace, fontSize, color, textOpacity, bold, italic := textProperties(n, resolver)
	if inherited != nil {
		if fontFace == "" {
			fontFace = inherited.object.FontFace
		}
		if fontSize <= 0 {
			fontSize = inherited.object.FontSize
		}
		if color == "" {
			color = inherited.object.Color
		}
		if textOpacity == 0 {
			textOpacity = inherited.object.Opacity
		}
		if !bold {
			bold = inherited.object.Bold
		}
		if !italic {
			italic = inherited.object.Italic
		}
	}
	if text == "" && fill == "" && stroke == "" && !retainEmpty {
		return nil
	}
	kind := "shape"
	if text != "" && fill == "" && stroke == "" {
		kind = "text"
	}
	link := ""
	if hyperlink := findFirst(n, "hlinkClick"); hyperlink != nil {
		link = links[attr(hyperlink, "id")]
	}
	if opacity == 0 {
		opacity = strokeOpacity
	}
	if opacity == 0 {
		opacity = textOpacity
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
	return &Object{ID: id, Kind: kind, X: x, Y: y, Width: w, Height: h, Rotation: rotation, Text: text, FontFace: fontFace, FontSize: fontSize, Color: color, Opacity: opacity, Fill: fill, Stroke: stroke, StrokeWidth: width, Shape: shape, FlipH: flipH, FlipV: flipV, Link: link, TextAlign: align, VerticalAlign: vertical, Bold: bold, Italic: italic}
}

func parsePicture(n *node, rels map[string]string, files map[string]*zip.File, cache map[string][]byte, limits Limits) *Object {
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
	return &Object{ID: id, Kind: "image", X: x, Y: y, Width: w, Height: h, Rotation: rotation, Image: data, ImageMIME: mimeForPart(target), CropLeft: left, CropTop: top, CropRight: right, CropBottom: bottom}
}

func parseConnector(n *node, resolver colourResolver) *Object {
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
	return &Object{ID: id, Kind: "line", X: x, Y: y, Width: w, Height: h, Rotation: r, Stroke: stroke, StrokeWidth: width, Opacity: opacity}
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

func textProperties(n *node, resolver colourResolver) (string, float64, string, float64, bool, bool) {
	// A run generally contains only overrides. The layout's defRPr supplies
	// the actual font, colour and size for ordinary PowerPoint placeholders.
	base := findFirst(n, "defRPr")
	run := findFirst(n, "rPr")
	face, size, colour, opacity, bold, italic := propertiesFromRun(base, resolver)
	if run == nil {
		return face, size, colour, opacity, bold, italic
	}
	rFace, rSize, rColour, rOpacity, rBold, rItalic := propertiesFromRun(run, resolver)
	if rFace != "" {
		face = rFace
	}
	if rSize > 0 {
		size = rSize
	}
	if rColour != "" {
		colour, opacity = rColour, rOpacity
	}
	if attr(run, "b") != "" {
		bold = rBold
	}
	if attr(run, "i") != "" {
		italic = rItalic
	}
	return face, size, colour, opacity, bold, italic
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
		face = attr(latin, "typeface")
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
	layoutPart := relationshipTargetContaining(slideRels, "slideLayouts/")
	if layoutPart == "" {
		return placeholders, fixed, resolvedBackground{}, warnings, nil
	}
	layoutRoot, layoutRels, err := readPartRootAndRelationships(files, layoutPart, limits)
	if err != nil {
		return nil, nil, resolvedBackground{}, nil, err
	}
	masterPart := relationshipTargetContaining(layoutRels, "slideMasters/")
	background := resolvedBackground{}
	if masterPart != "" {
		masterRoot, masterRels, err := readPartRootAndRelationships(files, masterPart, limits)
		if err != nil {
			return nil, nil, resolvedBackground{}, nil, err
		}
		background.Colour = partBackground(masterRoot, resolver)
		background.Image, background.MIME = partBackgroundImage(masterRoot, masterRels, files, limits)
		masterPlaceholders, masterFixed, masterWarnings := parseInheritedShapeTree(masterRoot, masterRels, files, cache, limits, resolver)
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
	layoutPlaceholders, layoutFixed, layoutWarnings := parseInheritedShapeTree(layoutRoot, layoutRels, files, cache, limits, resolver)
	for key, donor := range layoutPlaceholders {
		if base := placeholders[key]; base != nil {
			donor.object = mergeInheritedObject(base.object, donor.object)
			donor.hasGeometry = donor.hasGeometry || base.hasGeometry
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
	for _, target := range rels {
		if strings.Contains(target, segment) {
			return target
		}
	}
	return ""
}

func parseInheritedShapeTree(root *node, rels map[string]string, files map[string]*zip.File, cache map[string][]byte, limits Limits, resolver colourResolver) (map[string]*inheritedShape, []Object, []Warning) {
	placeholders := map[string]*inheritedShape{}
	var fixed []Object
	var warnings []Warning
	tree := findFirst(root, "spTree")
	if tree == nil {
		return placeholders, fixed, warnings
	}
	for _, child := range tree.Children {
		switch child.Name {
		case "sp":
			// A layout/master placeholder frequently carries only geometry and
			// default text properties. Retain that otherwise-empty donor so a
			// slide-local placeholder can inherit its complete appearance.
			object := parseShape(child, nil, resolver, nil, placeholderKey(child) != "")
			if object == nil {
				continue
			}
			if key := placeholderKey(child); key != "" {
				_, _, _, _, _, hasGeometry := transformWithPresence(child)
				placeholders[key] = &inheritedShape{object: *object, hasGeometry: hasGeometry}
				continue
			}
			// Parent templates commonly contain empty authoring placeholders.
			// Only non-placeholder graphics are visible inherited artwork.
			if object.Text == "" || object.Fill != "" || object.Stroke != "" {
				fixed = append(fixed, *object)
			}
		case "pic":
			if object := parsePicture(child, rels, files, cache, limits); object != nil {
				fixed = append(fixed, *object)
			}
		case "cxnSp":
			if object := parseConnector(child, resolver); object != nil {
				fixed = append(fixed, *object)
			}
		case "grpSp":
			warnings = append(warnings, Warning{Code: "inherited-group-flattened", Message: "An inherited PowerPoint group was flattened during import."})
			fixed = append(fixed, parseGroup(child, rels, nil, files, cache, limits, resolver)...)
		}
	}
	return placeholders, fixed, warnings
}

func mergeInheritedObject(base, override Object) Object {
	if override.FontFace == "" {
		override.FontFace = base.FontFace
	}
	if override.FontSize <= 0 {
		override.FontSize = base.FontSize
	}
	if override.Color == "" {
		override.Color, override.Opacity = base.Color, base.Opacity
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
	if len(presentation.Slides) == 0 {
		return Report{}, errors.New("PowerPoint export needs at least one slide")
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
	if err := add("ppt/theme/theme1.xml", themeXML()); err != nil {
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
		xmlPart, rels, warnings := slideXML(slide, assets)
		report.Warnings = append(report.Warnings, warnings...)
		if err := add(fmt.Sprintf("ppt/slides/slide%d.xml", index+1), xmlPart); err != nil {
			return report, err
		}
		if err := add(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", index+1), rels); err != nil {
			return report, err
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
	return report, nil
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
	for i, slide := range p.Slides {
		show := ""
		if slide.Hidden {
			show = ` show="0"`
		}
		fmt.Fprintf(&b, `<p:sldId id="%d" r:id="rId%d"%s/>`, 256+i, i+2, show)
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
	}
	for _, asset := range assets {
		if asset.MIME == "image/gif" {
			b.WriteString(`<Default Extension="gif" ContentType="image/gif"/>`)
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
func themeXML() string {
	// Each format-style list has a schema minimum of three children. Empty
	// containers are accepted by some readers but trigger repair in PowerPoint.
	const fill = `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	const line = `<a:ln w="9525" cap="flat" cmpd="sng" algn="ctr"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/><a:round/><a:headEnd type="none" w="med" len="med"/><a:tailEnd type="none" w="med" len="med"/></a:ln>`
	const effect = `<a:effectStyle><a:effectLst/></a:effectStyle>`
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Keynope"><a:themeElements><a:clrScheme name="Keynope"><a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="1F1F24"/></a:dk2><a:lt2><a:srgbClr val="F4F1E8"/></a:lt2><a:accent1><a:srgbClr val="55AAFF"/></a:accent1><a:accent2><a:srgbClr val="FF55AA"/></a:accent2><a:accent3><a:srgbClr val="55FFAA"/></a:accent3><a:accent4><a:srgbClr val="FFCC55"/></a:accent4><a:accent5><a:srgbClr val="AA55FF"/></a:accent5><a:accent6><a:srgbClr val="55FFFF"/></a:accent6><a:hlink><a:srgbClr val="55AAFF"/></a:hlink><a:folHlink><a:srgbClr val="AA55FF"/></a:folHlink></a:clrScheme><a:fontScheme name="Keynope"><a:majorFont><a:latin typeface="Arial"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="Arial"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme><a:fmtScheme name="Keynope"><a:fillStyleLst>` + fill + fill + fill + `</a:fillStyleLst><a:lnStyleLst>` + line + line + line + `</a:lnStyleLst><a:effectStyleLst>` + effect + effect + effect + `</a:effectStyleLst><a:bgFillStyleLst>` + fill + fill + fill + `</a:bgFillStyleLst></a:fmtScheme></a:themeElements><a:objectDefaults/><a:extraClrSchemeLst/></a:theme>`
}

func slideXML(slide Slide, assets map[string]mediaPart) (string, string, []Warning) {
	var b strings.Builder
	var rels strings.Builder
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>`)
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld>` + baseTree())
	var warnings []Warning
	nextID := 2
	nextRel := 2
	for _, o := range slide.Objects {
		if o.Width <= 0 || o.Height <= 0 {
			warnings = append(warnings, Warning{Object: o.ID, Code: "invalid-bounds", Message: "Object with empty bounds was skipped."})
			continue
		}
		switch o.Kind {
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
			b.WriteString(pictureXML(o, nextID, rid))
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
		nextID++
	}
	b.WriteString(`</p:spTree></p:cSld></p:sld>`)
	rels.WriteString(`</Relationships>`)
	return b.String(), rels.String(), warnings
}
func shapeXML(o Object, id int, linkID string) string {
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
	fmt.Fprintf(&b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s">%s</p:cNvPr><p:cNvSpPr txBox="%d"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm%s><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="%s"><a:avLst/></a:prstGeom>`, id, escapeAttr(o.ID), link, boolNumber(o.Kind == "text"), rot, x, y, w, h, escapeAttr(shape))
	if o.Fill != "" {
		b.WriteString(solidFill(o.Fill))
	} else {
		b.WriteString(`<a:noFill/>`)
	}
	if o.Stroke != "" {
		fmt.Fprintf(&b, `<a:ln w="%d">%s</a:ln>`, int64(maxFloat(o.StrokeWidth, 1)*12700), solidFill(o.Stroke))
	}
	b.WriteString(`</p:spPr>`)
	if o.Text != "" || o.Kind == "text" {
		b.WriteString(textBody(o))
	}
	b.WriteString(`</p:sp>`)
	return b.String()
}
func pictureXML(o Object, id int, rid string) string {
	x, y, w, h := emu(o.X), emu(o.Y), emu(o.Width), emu(o.Height)
	rot := ""
	if o.Rotation != 0 {
		rot = ` rot="` + strconv.FormatInt(int64(o.Rotation*60000), 10) + `"`
	}
	return fmt.Sprintf(`<p:pic><p:nvPicPr><p:cNvPr id="%d" name="%s"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr><a:xfrm%s><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`, id, escapeAttr(o.ID), rid, rot, x, y, w, h)
}
func textBody(o Object) string {
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
	for _, line := range strings.Split(o.Text, "\n") {
		b.WriteString(`<a:p>` + paragraphPr + `<a:r><a:rPr lang="en-US" sz="` + strconv.Itoa(int(size*100)) + `">` + solidFill(color) + `<a:latin typeface="` + escapeAttr(face) + `"/></a:rPr><a:t>` + escapeText(line) + `</a:t></a:r></a:p>`)
	}
	b.WriteString(`</p:txBody>`)
	return b.String()
}
func solidFill(color string) string {
	return `<a:solidFill><a:srgbClr val="` + strings.TrimPrefix(strings.ToUpper(color), "#") + `"/></a:solidFill>`
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
		key := xmlLocalName(value[start:i])
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
		attrs[key] = stdhtml.UnescapeString(value[start:i])
		i++
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

func readColourResolver(files map[string]*zip.File, rels map[string]string, limits Limits) (colourResolver, error) {
	resolver := colourResolver{colours: map[string]string{}, mapping: map[string]string{
		"bg1": "lt1", "tx1": "dk1", "bg2": "lt2", "tx2": "dk2",
	}}
	var themePart string
	for name := range files {
		if strings.HasPrefix(name, "ppt/theme/") && strings.HasSuffix(name, ".xml") {
			themePart = name
			break
		}
	}
	if themePart != "" {
		data, err := readPart(files, themePart, limits.MaxXMLPart)
		if err != nil {
			return resolver, err
		}
		root, err := parseXML(data, 128)
		if err != nil {
			return resolver, err
		}
		if scheme := findFirst(root, "clrScheme"); scheme != nil {
			for _, child := range scheme.Children {
				if colour, _ := rawColourNode(firstColourChild(child)); colour != "" {
					resolver.colours[child.Name] = colour
				}
			}
		}
	}
	// A colour map belongs to the master. The first master is sufficient for
	// the initial single-master import model; nested layout overrides are still
	// represented by their explicit RGB values.
	for name := range files {
		if !strings.HasPrefix(name, "ppt/slideMasters/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		data, err := readPart(files, name, limits.MaxXMLPart)
		if err != nil {
			return resolver, err
		}
		root, err := parseXML(data, 128)
		if err != nil {
			return resolver, err
		}
		if mapping := findFirst(root, "clrMap"); mapping != nil {
			for key, value := range mapping.Attr {
				resolver.mapping[key] = value
			}
		}
		break
	}
	_ = rels // retained for the relationship-oriented reader signature.
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
