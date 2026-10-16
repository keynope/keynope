package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Scene is a render-only projection, not a second persisted copy of the deck.
// It deliberately cannot hold notes, workshop answers, identities or archives.
// Legacy grid units map independently to the two physical slide axes.
type sceneRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type sceneRun struct {
	Text       string  `json:"text"`
	Bold       bool    `json:"bold,omitempty"`
	BoldOff    bool    `json:"boldOff,omitempty"` // Explicit normal weight overrides inherited header emphasis.
	Italic     bool    `json:"italic,omitempty"`
	Underline  bool    `json:"underline,omitempty"`
	Color      string  `json:"color,omitempty"`
	ColorToken string  `json:"colorToken,omitempty"` // Semantic role; Color is the portable RGB fallback.
	FontID     string  `json:"fontId,omitempty"`
	FontRole   string  `json:"fontRole,omitempty"` // Theme family role; FontID is a portable fallback.
	FontSize   float64 `json:"fontSize,omitempty"`
}

type sceneText struct {
	ReadOnly        bool                    `json:"readOnly,omitempty"`
	TypefaceMetrics map[string]DeckTypeface `json:"-"`
	FontRoles       map[string]string       `json:"fontRoles,omitempty"`   // Resolved used word font roles; never authored.
	ColorTokens     map[string]string       `json:"colorTokens,omitempty"` // Resolved, used word roles only; never authored.
	SeeThrough      bool                    `json:"seeThrough,omitempty"`
	EmojiTint       string                  `json:"emojiTint,omitempty"`
	EmojiWidthScale float64                 `json:"emojiWidthScale,omitempty"`
	EmojiFonts      map[string]string       `json:"emojiFonts,omitempty"`
	ParagraphBefore float64                 `json:"paragraphBefore"`
	ParagraphAfter  float64                 `json:"paragraphAfter"`
	LineHeight      float64                 `json:"lineHeight"`
	FontID          string                  `json:"fontId,omitempty"`
	WidthScale      float64                 `json:"widthScale"`
	Runs            []sceneRun              `json:"runs,omitempty"`
	Role            string                  `json:"role"`
	Level           int                     `json:"level,omitempty"`
	Size            float64                 `json:"size"`
	Family          string                  `json:"family"`
	Align           string                  `json:"align"`
	Vertical        string                  `json:"vertical"`
	Paragraphs      []sceneParagraph        `json:"paragraphs,omitempty"`
}

type sceneParagraph struct {
	Runs   []sceneRun `json:"runs"`
	Bullet bool       `json:"bullet,omitempty"`
}

type scenePaint struct {
	NoFill            bool     `json:"noFill,omitempty"` // Shape fill is explicitly off; colour remains available for style editing.
	Opacity           *float64 `json:"opacity,omitempty"`
	Color             string   `json:"color"`
	GradientStart     string   `json:"gradientStart,omitempty"`
	GradientEnd       string   `json:"gradientEnd,omitempty"`
	GradientDirection string   `json:"gradientDirection,omitempty"`
	Stroke            string   `json:"stroke,omitempty"`
	StrokeWidth       float64  `json:"strokeWidth"` // Zero is explicit Off, not the renderer's fallback width.
	ShadowColor       string   `json:"shadowColor,omitempty"`
	ShadowX           float64  `json:"shadowX,omitempty"`
	ShadowY           float64  `json:"shadowY,omitempty"`
	ShadowBlur        float64  `json:"shadowBlur,omitempty"`
}

type sceneMedia struct {
	Fit            string            `json:"fit,omitempty"`
	Sharpness      float64           `json:"sharpness,omitempty"`
	SharpnessAfter int               `json:"sharpnessAfter,omitempty"` // Number of colour passes before the spatial pass; tint follows it.
	Alt            string            `json:"alt,omitempty"`
	Decorative     bool              `json:"decorative,omitempty"`
	Mask           string            `json:"mask,omitempty"`
	Crop           *sceneCrop        `json:"crop,omitempty"`
	ColourMatrices [][]float64       `json:"colourMatrices,omitempty"`
	Source         string            `json:"source,omitempty"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	Frames         []sceneMediaFrame `json:"frames,omitempty"`
	LoopCount      int               `json:"loopCount,omitempty"`
}

// SVG sRGB matrices mirror adjustRGB's contrast -> saturation -> brightness
// order. Tint is a separate pass so adjusted channels are clamped first, just
// as in the Retro pipeline. Neither pass changes alpha or source pixels.
func sceneImageColourMatrices(query string) [][]float64 {
	opts := parseImageASCIIOptions(query)
	var matrices [][]float64
	if opts.brightness != 1 || opts.contrast != 1 || opts.saturation != 1 {
		matrix := make([]float64, 20)
		weights := []float64{.299, .587, .114}
		for row := 0; row < 3; row++ {
			for column := 0; column < 3; column++ {
				value := (1 - opts.saturation) * weights[column]
				if row == column {
					value += opts.saturation
				}
				matrix[row*5+column] = value * opts.contrast * opts.brightness
			}
			matrix[row*5+4] = (128.0 / 255) * (1 - opts.contrast) * opts.brightness
		}
		matrix[18] = 1
		matrices = append(matrices, matrix)
	}
	if opts.tint != nil {
		matrix := make([]float64, 20)
		for row, value := range []uint8{opts.tint.r, opts.tint.g, opts.tint.b} {
			for column, weight := range []float64{.299, .587, .114} {
				matrix[row*5+column] = float64(value) / 255 * weight
			}
		}
		matrix[18] = 1
		matrices = append(matrices, matrix)
	}
	return matrices
}

type sceneMediaFrame struct {
	Source  string `json:"source"`
	DelayMS int64  `json:"delayMs"`
}

type sceneObject struct {
	Name             string              `json:"name,omitempty"`
	RetroStroke      []sceneRect         `json:"retroStroke,omitempty"`
	Chart            *sceneChart         `json:"chart,omitempty"`
	Table            *sceneTable         `json:"table,omitempty"`
	SampleOffset     *connectorPoint     `json:"sampleOffset,omitempty"` // Translate retained legacy artwork with its continuous bounds.
	SampleScale      *connectorPoint     `json:"sampleScale,omitempty"`  // Map the sampled shape body onto continuous authored bounds.
	TextCapabilities []string            `json:"textCapabilities,omitempty"`
	Style            string              `json:"style,omitempty"`
	RetroText        *sceneRetroText     `json:"retroText,omitempty"`
	RetroLines       *sceneRetroLines    `json:"retroLines,omitempty"`
	EditRuns         []sceneRun          `json:"editRuns,omitempty"` // Editor-only list source with continuation indentation.
	Editable         bool                `json:"editable,omitempty"`
	ID               string              `json:"id"`
	Kind             string              `json:"kind"`
	Group            string              `json:"group,omitempty"`
	Bounds           sceneRect           `json:"bounds"`
	Rotation         float64             `json:"rotation,omitempty"`
	Paint            scenePaint          `json:"paint"`
	Text             *sceneText          `json:"text,omitempty"`
	Shape            string              `json:"shape,omitempty"`
	ShapeGeometry    *sceneShapeGeometry `json:"shapeGeometry,omitempty"`
	Label            *sceneText          `json:"label,omitempty"`
	LabelPaint       *scenePaint         `json:"labelPaint,omitempty"`
	LabelStyle       string              `json:"labelStyle,omitempty"`
	RetroLabel       *sceneRetroText     `json:"retroLabel,omitempty"`
	Media            *sceneMedia         `json:"media,omitempty"`
	Points           []connectorPoint    `json:"points,omitempty"`
	Arrows           string              `json:"arrows,omitempty"`
	ArrowWidth       float64             `json:"arrowWidth,omitempty"`
	Link             string              `json:"link,omitempty"`
}

// Reuse the original text painter, including sampled glyph treatments, rather
// than approximating Retro text with the proportional Modern layout engine.
type sceneRetroText struct {
	Cols int        `json:"cols"`
	Rows int        `json:"rows"`
	Line exportLine `json:"line"`
}

type sceneRetroLines struct {
	CanvasWidth     float64              `json:"canvasWidth,omitempty"`
	CanvasHeight    float64              `json:"canvasHeight,omitempty"`
	ContentOpacity  *float64             `json:"contentOpacity,omitempty"`
	BackdropOpacity *float64             `json:"backdropOpacity,omitempty"`
	Frames          []exportContentFrame `json:"frames,omitempty"`
	LoopCount       int                  `json:"loopCount,omitempty"`
	Alt             string               `json:"alt,omitempty"`
	Decorative      bool                 `json:"decorative,omitempty"`
	ShapeOpacity    *float64             `json:"shapeOpacity,omitempty"`
	Cols            int                  `json:"cols"`
	Rows            int                  `json:"rows"`
	Lines           []exportLine         `json:"lines"`
}

func retroObjectLines(deck Deck, slide Slide, lines []Line, index, cols, rows int) []exportLine {
	var own []Line
	for _, line := range lines {
		if line.Element == index {
			own = append(own, line)
		}
	}
	slide.ThemeColors = deck.themeColors("retro")
	if index >= 0 && index < len(slide.Elements) && elementTransparent(slide.Elements[index]) {
		slide.Elements = append([]Element(nil), slide.Elements...)
		q, _ := url.ParseQuery(slide.Elements[index].Query)
		q.Del("transparent")
		slide.Elements[index].Query = q.Encode()
	}
	return exportLines(own, slide, cols, rows, len(deck.Slides))
}

func sceneLinesByElement(lines []Line) map[int][]Line {
	indexed := make(map[int][]Line)
	for _, line := range lines {
		indexed[line.Element] = append(indexed[line.Element], line)
	}
	return indexed
}

func applyRetroOpacity(lines *sceneRetroLines, element Element) {
	if !elementTransparent(element) {
		return
	}
	opacity := .5
	if element.Kind == "code" {
		lines.BackdropOpacity = &opacity
	} else {
		lines.ContentOpacity = &opacity
	}
}

// A single object's cycle must not be truncated to the multi-animation LCM
// limit used by whole-slide HTML export. Preserve every imported frame delay.
func retroObjectFrames(slide Slide, frames []DeckAssetFrame, cols, rows, slideCount int) []exportContentFrame {
	key, cacheable := sceneAnimationKey(slide, frames, cols, rows, slideCount)
	if cacheable {
		if cached, ok := sharedSceneAnimations.get(key); ok {
			return cached
		}
	}
	result := renderRetroObjectFrames(slide, frames, cols, rows, slideCount)
	if cacheable {
		sharedSceneAnimations.put(key, result)
	}
	return result
}

func renderRetroObjectFrames(slide Slide, frames []DeckAssetFrame, cols, rows, slideCount int) []exportContentFrame {
	exportImageAnimationMu.Lock()
	defer exportImageAnimationMu.Unlock()
	previous := exportImageAnimationPosition
	defer func() { exportImageAnimationPosition = previous }()
	var position time.Duration
	var result []exportContentFrame
	var last []exportLine
	for index, frame := range frames {
		exportImageAnimationPosition = &position
		// Scene callers already supply coordinates in this sampling grid. The
		// terminal display path fits/scales again against the authored deck and
		// can shrink every animated frame relative to the still scene geometry.
		lines := exportLines(layout(slide, cols, rows), slide, cols, rows, slideCount)
		delay := frame.DelayMS
		if delay <= 0 {
			delay = 100
		}
		if index == 0 {
			result = append(result, exportContentFrame{Full: true, Lines: lines, DelayMS: delay})
		} else {
			clear, update := exportLineDelta(last, lines)
			result = append(result, exportContentFrame{Clear: clear, Update: update, DelayMS: delay})
		}
		last = lines
		position += time.Duration(delay) * time.Millisecond
	}
	return result
}

type sceneDiagnostic struct {
	ObjectID string `json:"objectId,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type slideScene struct {
	Typefaces       map[string]DeckTypeface `json:"typefaces,omitempty"`
	SlideID         string                  `json:"slideId,omitempty"`
	Build           *SlideBuild             `json:"build,omitempty"`
	DefaultStyle    string                  `json:"defaultStyle,omitempty"`
	Master          bool                    `json:"master,omitempty"`     // Editor target scope, never audience state.
	Conversion      *sceneConversionReport  `json:"conversion,omitempty"` // Editor-only, on explicit request.
	Revision        *int64                  `json:"revision,omitempty"`   // Editor endpoint only; not audience render data.
	SlideIndex      int                     `json:"slideIndex,omitempty"`
	Version         int                     `json:"version"`
	Width           float64                 `json:"width"`
	Height          float64                 `json:"height"`
	Background      string                  `json:"background"`
	BackgroundMedia *sceneBackgroundMedia   `json:"backgroundMedia,omitempty"`
	Objects         []sceneObject           `json:"objects"`
	Diagnostics     []sceneDiagnostic       `json:"diagnostics"`
}

// sceneBackgroundMedia is a slide-owned image painted below scene objects.
// It deliberately has no object ID or editing affordances: use a normal image
// for artwork that should participate in selection and z-order.
type sceneBackgroundMedia struct {
	sceneMedia
	Fit        string           `json:"fit,omitempty"`
	Opacity    float64          `json:"opacity"`
	Paint      *scenePaint      `json:"paint,omitempty"`
	RetroLines *sceneRetroLines `json:"retroLines,omitempty"`
}

type sceneConversionIssue struct {
	Slide   int    `json:"slide"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

type sceneConversionReport struct {
	Slides int                    `json:"slides"`
	Issues []sceneConversionIssue `json:"issues"`
}

// Summarise all slides, including tab-only slides and inherited objects. Never
// return source text, media, activity definitions or private result archives.
func buildSceneConversionReport(deck Deck, cols, rows int) (*sceneConversionReport, error) {
	report := &sceneConversionReport{Slides: len(deck.Slides), Issues: []sceneConversionIssue{}}
	for index := range deck.Slides {
		scene, err := buildSlideScene(deck, index, cols, rows)
		if err != nil {
			return nil, err
		}
		seen := map[string]int{}
		for _, issue := range scene.Diagnostics {
			if entry, ok := seen[issue.Code]; ok {
				report.Issues[entry].Count++
				continue
			}
			seen[issue.Code] = len(report.Issues)
			report.Issues = append(report.Issues, sceneConversionIssue{index + 1, issue.Code, issue.Message, 1})
		}
	}
	return report, nil
}

func sceneColor(code string) string {
	css := ansiCSSColour(code)
	var r, g, b int
	if n, err := fmt.Sscanf(css, "rgb(%d,%d,%d)", &r, &g, &b); n == 3 && err == nil {
		return fmt.Sprintf("#%02x%02x%02x", clampInt(r, 0, 255), clampInt(g, 0, 255), clampInt(b, 0, 255))
	}
	return css
}

func scenePaintFor(e Element, slide Slide, sx, sy float64) scenePaint {
	q, err := url.ParseQuery(e.Query)
	return scenePaintFromValues(e, slide, sx, sy, q, err)
}

func scenePaintFromValues(e Element, slide Slide, sx, sy float64, q url.Values, queryErr error) scenePaint {
	color := sceneColor(slideFG(slide))
	if e.Kind == "heading" {
		color = sceneColor(slideHeaderFG(slide))
	}
	if queryErr == nil && validConcreteDefault("default-color", q.Get("default-color")) && (e.Kind == "heading" && slide.HeaderFG == "" || e.Kind != "heading" && slide.FG == "") {
		color = q.Get("default-color")
	}
	if q.Has("fg") || (e.Kind == "heading" && q.Has("header")) {
		if fg := elementFG(e.Query, e.Kind == "heading"); fg != "" {
			color = sceneColor(fg)
		}
	}
	p := scenePaint{Color: color}
	if gradient, ok := textGradientFromValues(q); ok && queryErr == nil {
		p.GradientStart, p.GradientEnd, p.GradientDirection = q.Get("gradient-start"), q.Get("gradient-end"), gradient.direction
	}
	if shadow, ok := textShadowFromValues(q); ok && queryErr == nil {
		p.ShadowColor = fmt.Sprintf("#%02x%02x%02x", shadow.colour.r, shadow.colour.g, shadow.colour.b)
		p.ShadowX, p.ShadowY = float64(shadow.x)*sx, float64(shadow.y)*sy
		if q.Get("shadow") == "soft" {
			p.ShadowBlur = 8
		}
	}
	if q.Get("outline") != "" && q.Get("outline") != "none" {
		p.Stroke, p.StrokeWidth = "#111111", 2
		if q.Get("outline") == "light" {
			p.Stroke = "#ffffff"
		}
	}
	if opacity, err := strconv.ParseFloat(q.Get("modern-opacity"), 64); err == nil && opacity >= 0 && opacity <= 1 {
		p.Opacity = &opacity
	}
	return p
}

// Colour tags become styled runs; punctuation remains literal. Code never
// interprets tags. No source offsets into markup are exposed to the new editor.
// All TrueType C64 typography shares the shaped surface. Retired font/glyph
// metadata must not reactivate sampled text when rendering an unmigrated copy.
func sharedRetroText(e Element) bool {
	return isTrueType(e)
}

func sceneTextFor(e Element) *sceneText {
	q, _ := url.ParseQuery(e.Query)
	return sceneTextFromValues(e, q)
}

func sceneTextFromValues(e Element, q url.Values) *sceneText {
	result := &sceneText{Role: e.Kind, Size: float64(trueTypeSizeWithValues(e, q)) * .5, WidthScale: trueTypeWidthWithValues(q) / 100, Family: "KeynopeModern, sans-serif", Align: "start", Vertical: "top"}
	result.SeeThrough = q.Get("transparent") == "1"
	if tint := q.Get("tint"); len(tint) == 7 && tint[0] == '#' {
		if _, err := strconv.ParseUint(tint[1:], 16, 24); err == nil {
			result.EmojiTint = tint
		}
	}
	if width, err := strconv.ParseFloat(q.Get("modern-width"), 64); err == nil && width >= 1 && width <= 200 {
		result.WidthScale = width / 100
	}
	if e.Kind == "heading" {
		result.Level = e.Level
		if result.Level < 1 || result.Level > 6 {
			result.Level = 1
		}
	}
	result.LineHeight = 1.25
	for key, target := range map[string]*float64{"modern-paragraph-before": &result.ParagraphBefore, "modern-paragraph-after": &result.ParagraphAfter} {
		if value, err := strconv.ParseFloat(q.Get(key), 64); err == nil && value >= 0 && value <= 4 {
			*target = value
		}
	}
	if height, err := strconv.ParseFloat(q.Get("modern-line-height"), 64); err == nil && height >= .5 && height <= 4 {
		result.LineHeight = height
	}
	if e.Kind == "code" {
		result.Family = "KeynopeModernMono, monospace"
	}
	switch q.Get("modern-font") {
	case "sans":
		result.FontID, result.Family = "sans", "KeynopeModern, sans-serif"
	case "mono":
		result.FontID, result.Family = "mono", "KeynopeModernMono, monospace"
	case "c64":
		result.FontID, result.Family = "c64", "KeynopeC64, monospace"
	}
	if font := q.Get("modern-font"); typefaceIDRE.MatchString(font) {
		result.FontID, result.Family = font, typefaceFamily(font)+", KeynopeModern, sans-serif"
	}
	switch textBoxAlignmentFromValues(q) {
	case "center":
		result.Align = "center"
	case "right":
		result.Align = "end"
	case "justify":
		result.Align = "justify"
	}
	switch q.Get("text-valign") {
	case "middle", "bottom":
		result.Vertical = q.Get("text-valign")
	}
	bold := q.Get("ttf-weight") == "bold"
	color, stack := "", []string{}
	text := e.Text
	appendText := func(value string) {
		if value == "" {
			return
		}
		n := len(result.Runs)
		if n > 0 && result.Runs[n-1].Color == color {
			result.Runs[n-1].Text += value
			return
		}
		result.Runs = append(result.Runs, sceneRun{Text: value, Bold: bold, Color: color})
	}
	for len(text) > 0 {
		if e.Kind != "code" && strings.HasPrefix(text, "[color=") && len(text) >= 15 && text[14] == ']' {
			if hex, ok := normalizeHexColour(text[7:14]); ok && strings.Contains(text[15:], "[/color]") {
				stack = append(stack, color)
				color = hex
				text = text[15:]
				continue
			}
		}
		if e.Kind != "code" && strings.HasPrefix(text, "[/color]") && len(stack) > 0 {
			color = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			text = text[8:]
			continue
		}
		// Scan to the next possible tag in a single pass, preserving UTF-8 bytes.
		next := strings.IndexByte(text[1:], '[') + 1
		if next == 0 {
			next = len(text)
		}
		appendText(text[:next])
		text = text[next:]
	}
	if result.Runs == nil {
		result.Runs = []sceneRun{{Text: ""}}
	}
	result.Runs = resolvedModernRuns(q.Get("modern-runs"), e.Text, q.Get("ttf-weight"), result.Runs)
	result.Runs = applyTextEmphasisFlags(q, result.Runs)
	for _, token := range splitEmojiText(e.Text) {
		if token.assetKey != "" {
			if data := emojiFontData(token.assetKey); data != "" {
				if result.EmojiFonts == nil {
					result.EmojiFonts = map[string]string{}
				}
				result.EmojiFonts[token.text] = data
			}
		}
	}
	if e.Kind == "bullet" {
		result.Paragraphs = sceneBulletParagraphs(result.Runs)
		result.Runs = nil
	}
	return result
}

func sceneBulletParagraphs(runs []sceneRun) []sceneParagraph {
	lines := [][]sceneRun{{}}
	for _, run := range runs {
		for i, part := range strings.Split(run.Text, "\n") {
			if i > 0 {
				lines = append(lines, []sceneRun{})
			}
			if part != "" {
				r := run
				r.Text = part
				lines[len(lines)-1] = append(lines[len(lines)-1], r)
			}
		}
	}
	var paragraphs []sceneParagraph
	for _, line := range lines {
		var plain strings.Builder
		for _, run := range line {
			plain.WriteString(run.Text)
		}
		if len(paragraphs) > 0 && strings.HasPrefix(plain.String(), "  ") {
			remaining := 2
			for i := range line {
				n := min(remaining, len(line[i].Text))
				line[i].Text = line[i].Text[n:]
				remaining -= n
			}
			p := &paragraphs[len(paragraphs)-1]
			p.Runs = append(p.Runs, sceneRun{Text: "\n"})
			p.Runs = append(p.Runs, line...)
		} else {
			paragraphs = append(paragraphs, sceneParagraph{Runs: line, Bullet: true})
		}
	}
	return paragraphs
}

func buildSlideScene(deck Deck, index, cols, rows int) (slideScene, error) {
	if index < 0 || index >= len(deck.Slides) {
		return slideScene{}, fmt.Errorf("invalid scene slide")
	}
	return buildResolvedSlideScene(deck, deck.ResolveSlide(index, false), index, cols, rows)
}

func buildMixedSlideScene(deck Deck, index, cols, rows int) (slideScene, error) {
	if index < 0 || index >= len(deck.Slides) {
		return slideScene{}, fmt.Errorf("invalid scene slide")
	}
	return buildStyledSlideScene(deck, deck.ResolveSlide(index, false), index, cols, rows, true)
}

// Master previews are already resolved. Project them directly, without resolving
// the normal slide at the same index or applying master inheritance twice.
func buildResolvedSlideScene(deck Deck, resolved Slide, index, cols, rows int) (slideScene, error) {
	return buildStyledSlideScene(deck, resolved, index, cols, rows, false)
}

func buildStyledSlideScene(deck Deck, resolved Slide, index, cols, rows int, inheritedStyles bool) (slideScene, error) {
	return buildStyledSceneTarget(deck, resolved, index, cols, rows, inheritedStyles, "")
}

// Keep the full layout context (flow, masters and connector anchors), but avoid
// projecting unrelated text, media sources and animation frames for a draft.
// An empty target produces the full scene through the same code path.
func buildStyledSceneTarget(deck Deck, resolved Slide, index, cols, rows int, inheritedStyles bool, target string) (slideScene, error) {
	return buildStyledSceneTargetMeasured(deck, resolved, index, cols, rows, inheritedStyles, target, nil)
}

// Opt-in counters are request-owned and never persisted in deck metadata or
// global mutable state. Tests distinguish semantic reuse from whole-scene work.
type sceneRenderMetrics struct {
	DisableLayoutCache    bool // Request-owned cold-parity control, never authored.
	LayoutBuilds          int
	LayoutCacheHits       int
	ObjectLayoutBuilds    []string
	ObjectLayoutHits      []string
	ObjectArtworkLayouts  []string
	SceneBuilds           int
	TextProjectionBuilds  []string
	TextProjectionHits    []string
	TableProjectionBuilds []string
	TableProjectionHits   []string
}

func buildStyledSceneTargetMeasured(deck Deck, resolved Slide, index, cols, rows int, inheritedStyles bool, target string, metrics *sceneRenderMetrics) (slideScene, error) {
	var targets map[string]bool
	if target != "" {
		targets = map[string]bool{target: true}
	}
	return buildStyledSceneTargetsMeasured(deck, resolved, index, cols, rows, inheritedStyles, targets, metrics)
}

func buildStyledSceneTargetsMeasured(deck Deck, resolved Slide, index, cols, rows int, inheritedStyles bool, targets map[string]bool, metrics *sceneRenderMetrics) (slideScene, error) {
	return buildStyledSceneProjection(deck, resolved, index, cols, rows, inheritedStyles, targets, metrics, nil)
}

func buildStyledSceneProjection(deck Deck, resolved Slide, index, cols, rows int, inheritedStyles bool, targets map[string]bool, metrics *sceneRenderMetrics, attachments **sceneAttachmentGeometry) (slideScene, error) {
	if metrics != nil {
		metrics.SceneBuilds++
	}
	width, height := deck.pageDimensions()
	scene := slideScene{Version: 1, Width: width, Height: height, Objects: []sceneObject{}, Diagnostics: []sceneDiagnostic{}}
	if err := deck.PageSize.Validate(); err != nil {
		return scene, err
	}
	// Sources are immutable within this projection. Repeated instances share
	// encoding work, but never share their mutable per-object media settings.
	mediaSources := make(map[string]sceneMedia)
	if index < 0 || cols <= 0 || rows <= 0 {
		return scene, fmt.Errorf("invalid scene slide or dimensions")
	}
	slide := withTrueTypeDefaults(resolved)
	slide = deck.withThemeTypography(slide)
	slide = deck.applyShapeStrokeDefaults(slide)
	// Scene images rotate as objects, not by resampling their source. Keep
	// local crop/mask geometry stable in both treatments and rotate once.
	imageOrientations := make(map[int]string)
	slide.Elements = append([]Element(nil), slide.Elements...)
	// Legacy layout fits labels before sampling the shape. Resolve those same
	// dimensions once on this owned projection so paths, strokes, label regions
	// and sampled coverage cannot disagree about the expanded frame.
	fitSlideShapeLabels(&slide, cols, rows)
	for i, element := range slide.Elements {
		if element.Kind != "image" {
			continue
		}
		slide.Elements[i].Path = sceneEmbeddedImagePath(deck, element)
		q, _ := url.ParseQuery(element.Query)
		if imageFit(q.Get("image-fit")) != "" || q.Get("stretch") == "1" {
			if w, ok := sceneObjectDimension(q, "width", cols); ok {
				if h, ok := sceneObjectDimension(q, "height", rows); ok && h > 0 {
					q.Set("keynope-fit-aspect", strconv.FormatFloat(w*scene.Width/float64(cols)/(h*scene.Height/float64(rows)), 'f', -1, 64))
				}
			}
		}
		switch orientation := q.Get("orientation"); orientation {
		case "cw", "ccw", "down", "flip":
			imageOrientations[i] = orientation
			q.Del("orientation")
			slide.Elements[i].Query = q.Encode()
		}
		if imageFit(q.Get("image-fit")) != "" || q.Get("stretch") == "1" {
			slide.Elements[i].Query = q.Encode()
		}
	}
	defaultStyle := "modern"
	if inheritedStyles && !deck.usesConcreteAppearance() {
		defaultStyle = deck.slideDefaultStyle(resolved)
	}
	slide.ThemeColors = deck.themeColors(defaultStyle)
	scene.DefaultStyle = defaultStyle
	scene.Background = sceneColor(slideBG(slide))
	if slide.BackgroundMedia != nil && slide.BackgroundMedia.AssetID != "" {
		if asset, ok := deck.Assets[slide.BackgroundMedia.AssetID]; ok {
			source := sceneMediaSource(asset)
			if source.Source != "" {
				fit := slide.BackgroundMedia.Fit
				if fit != "contain" && fit != "stretch" {
					fit = "cover"
				}
				opacity := slide.BackgroundMedia.Opacity
				if opacity < 0 || opacity > 1 || math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity == 0 && !slide.BackgroundMedia.OpacitySet {
					opacity = 1
				}
				source.Decorative = true
				source.ColourMatrices = sceneImageColourMatrices(slide.BackgroundMedia.Query)
				options := parseImageASCIIOptions(slide.BackgroundMedia.Query)
				if options.sharpness != 1 {
					source.Sharpness = options.sharpness
					if options.brightness != 1 || options.contrast != 1 || options.saturation != 1 {
						source.SharpnessAfter = 1
					}
				}
				query, _ := url.ParseQuery(slide.BackgroundMedia.Query)
				// A promoted image must retain its paint treatment too: gradients,
				// outlines and shadows are all valid image effects in the normal
				// canvas renderer. Opacity lives on the background layer so it is
				// never applied twice when the original image is promoted.
				paint := scenePaintFromValues(Element{Kind: "image", Query: slide.BackgroundMedia.Query}, slide, scene.Width/float64(cols), scene.Height/float64(rows), query, nil)
				paint.Opacity = &opacity
				source.Crop = parseModernCrop(query.Get("modern-crop"))
				if mask := query.Get("modern-mask"); mask == "rounded" || mask == "ellipse" {
					source.Mask = mask
				}
				background := &sceneBackgroundMedia{sceneMedia: source, Fit: fit, Opacity: opacity, Paint: &paint}
				if query.Get("image-style") == "retro" {
					background.RetroLines = sceneBackgroundRetroLines(deck, slide, slide.BackgroundMedia.AssetID, asset, slide.BackgroundMedia.Query, cols, rows)
				}
				scene.BackgroundMedia = background
			} else {
				scene.Diagnostics = append(scene.Diagnostics, sceneDiagnostic{Code: "background-media-unavailable", Message: "The slide background image source is unavailable."})
			}
		} else {
			scene.Diagnostics = append(scene.Diagnostics, sceneDiagnostic{Code: "background-media-unavailable", Message: "The slide background image asset is unavailable."})
		}
	}
	if slide.Effect != "" && slide.Effect != "none" || slide.Background != "" && slide.Background != "none" {
		scene.Diagnostics = append(scene.Diagnostics, sceneDiagnostic{Code: "background-counterpart", Message: "This preview uses the slide base colour. Its animated Retro background/effect is retained, not converted yet."})
	}
	sx, sy := scene.Width/float64(cols), scene.Height/float64(rows)
	lines, geometryOnly := sceneGeometryLayout(slide, cols, rows, metrics, deck.usesConcreteAppearance())
	retroSlide := slide
	retroSlide.ThemeColors = deck.themeColors("retro")
	var retroLayout []Line
	// Default Retro decks were doing the same expensive layout twice. The
	// theme is the only difference between these two projections; when equal,
	// reuse the immutable line set instead of rebuilding it for every object.
	if !geometryOnly && (slide.ThemeColors == nil && retroSlide.ThemeColors == nil || slide.ThemeColors != nil && retroSlide.ThemeColors != nil && *slide.ThemeColors == *retroSlide.ThemeColors) {
		retroLayout = lines
	}
	var retroByElement map[int][]Line
	var imageAnchors map[int]int
	objectRows := func(index int) []Line {
		if deck.usesConcreteAppearance() && (metrics == nil || !metrics.DisableLayoutCache) && index >= 0 && index < len(retroSlide.Elements) && retroSlide.Elements[index].Kind == "image" {
			if imageAnchors == nil {
				imageAnchors = make(map[int]int)
				for _, line := range lines {
					if line.Role == "image" {
						if _, exists := imageAnchors[line.Element]; !exists {
							imageAnchors[line.Element] = line.Row
						}
					}
				}
			}
			var anchor *int
			if row, exists := imageAnchors[index]; exists {
				anchor = &row
			}
			if own, valid := sceneIsolatedImageRows(retroSlide, index, anchor, cols, rows); valid {
				if metrics != nil {
					metrics.ObjectArtworkLayouts = append(metrics.ObjectArtworkLayouts, retroSlide.Elements[index].ID)
				}
				return own
			}
		}
		if retroByElement == nil {
			if retroLayout == nil {
				if metrics != nil {
					metrics.LayoutBuilds++
				}
				retroLayout = layout(retroSlide, cols, rows)
			}
			retroByElement = sceneLinesByElement(retroLayout)
		}
		return retroByElement[index]
	}
	type gridBox struct{ top, bottom, left, right int }
	boxes := map[int]gridBox{}
	for _, line := range lines {
		if line.Role == "outline" || line.Role == "shape-label" {
			continue
		}
		w := max(1, displayWidth(stripANSI(line.Text)))
		b, ok := boxes[line.Element]
		if !ok {
			b = gridBox{line.Row, line.Row, line.Col, line.Col + w - 1}
		} else {
			b.top = min(b.top, line.Row)
			b.bottom = max(b.bottom, line.Row)
			b.left = min(b.left, line.Col)
			b.right = max(b.right, line.Col+w-1)
		}
		boxes[line.Element] = b
	}
	routes := map[string]shapeConnector{}
	var ports []shapePort
	if attachments != nil {
		ports = slideShapePorts(slide, lines, cols, rows)
	}
	connectors := slideShapeConnectorsWithPorts(slide, lines, cols, rows, ports)
	if attachments != nil {
		*attachments = &sceneAttachmentGeometry{cols: cols, rows: rows, ports: ports, connectors: connectors}
	}
	for _, route := range connectors {
		routes[route.ID] = route
	}
	warn := func(id, code, message string) {
		scene.Diagnostics = append(scene.Diagnostics, sceneDiagnostic{id, code, message})
	}
	queries := make([]url.Values, len(slide.Elements))
	queryErrors := make([]error, len(slide.Elements))
	for i, element := range slide.Elements {
		queries[i], queryErrors[i] = url.ParseQuery(element.Query)
	}
	indices := slidePaintOrderWithQueries(slide, queries)
	hidden := hiddenSlideObjectsWithQueries(slide, queries)
	for _, i := range indices {
		if hidden[i] {
			continue
		}
		e := slide.Elements[i]
		if len(targets) != 0 && !targets[e.ID] {
			continue
		}
		id := e.ID
		if id == "" {
			id = fmt.Sprintf("legacy-%d-%d", index, i)
			warn(id, "unstable-id", "Legacy object needs a persistent ID before semantic editing.")
		}
		q, queryErr := queries[i], queryErrors[i]
		treatment := q.Get("element-style")
		if deck.usesConcreteAppearance() {
			// Style is concrete in v3: fonts choose typography, only images
			// retain a Retro/Modern visual treatment, and geometry is vector.
			treatment = "modern"
			if e.Kind == "image" && q.Get("image-style") == "retro" {
				treatment = "retro"
			}
		} else if !validElementStyle(treatment) {
			treatment = "modern"
			if inheritedStyles {
				treatment = deck.elementStyle(slide, e)
			}
		}
		objectSlide := slide
		objectSlide.ThemeColors = deck.themeColors(treatment)
		// A Retro style alone does not require sampled layout: ordinary C64
		// text shares the shaped surface. objectRows builds the Retro layout
		// lazily only when a retained-art adapter actually consumes its rows.
		b, ok := boxes[i]
		if !ok && e.Kind != "connector" && e.Kind != "table" && e.Kind != "chart" {
			warn(id, "missing-layout", "Object has no resolved layout; it has not been deleted.")
			continue
		}
		o := sceneObject{ID: id, Kind: e.Kind, Group: q.Get("group"), Bounds: sceneRect{float64(b.left) * sx, float64(b.top) * sy, float64(b.right-b.left+1) * sx, float64(b.bottom-b.top+1) * sy}}
		cacheText := e.ID != "" && queryErr == nil && q.Get("render") == "truetype" && q.Get("font") == "" && q.Get("glyph") == "" && (e.Kind == "text" || e.Kind == "heading" || e.Kind == "bullet" || e.Kind == "code")
		var textKey sceneTextObjectKey
		var cachedText *sceneText
		if cacheText {
			textKey = sceneTextDependencyKey(deck, e, treatment, q)
			if cached, _, ok := sharedSceneTextObjects.get(textKey); ok {
				cachedText = cached.Text
				if metrics != nil {
					metrics.TextProjectionHits = append(metrics.TextProjectionHits, id)
				}
			}
		}
		o.Style = treatment
		o.Name = objectNameFromValues(q)
		o.TextCapabilities = textCapabilities(e, treatment)
		o.Paint = scenePaintFromValues(e, objectSlide, sx, sy, q, queryErr)
		o.Paint = deck.applyThemeObjectPaint(e, objectSlide, q, o.Paint)
		orientation := q.Get("orientation")
		if e.Kind == "image" {
			orientation = imageOrientations[i]
		}
		switch orientation {
		case "cw":
			o.Rotation = 90
		case "ccw":
			o.Rotation = -90
		case "down", "flip": // Toolbar/legacy decks use down; retain the early scene alias.
			o.Rotation = 180
		}
		if queryErr == nil {
			if target, ok := linkTargetFromValues(q, len(deck.Slides)); ok {
				o.Link = target.Value
			}
		}
		switch e.Kind {
		case "chart":
			w, h := 170.0, 36.0
			if v, ok := sceneObjectDimension(q, "width", cols); ok {
				w = v
			}
			if v, ok := sceneObjectDimension(q, "height", rows); ok {
				h = v
			}
			x, y := tableSceneAnchor(q, w, h, cols, rows)
			o.Bounds = sceneRect{x * sx, y * sy, w * sx, h * sy}
			if deck.Objects == nil {
				warn(id, "missing-chart", "Chart data is missing.")
				continue
			}
			data, ok := deck.Objects.Charts[e.ObjectID]
			if !ok {
				warn(id, "missing-chart", "Chart data is missing.")
				continue
			}
			color := o.Paint.Color
			if (deck.Theme != nil || data.Options.DefaultColor != "") && !q.Has("fg") && !q.Has("theme-fg") && objectSlide.FG == "" {
				color = ""
				if data.Options.DefaultColor != "" {
					o.Paint.Color = data.Options.DefaultColor
				}
			}
			var err error
			o.Chart, err = deck.projectChart(data, o.Bounds, color, q.Get("modern-font"))
			if err != nil {
				warn(id, "invalid-chart", err.Error())
				continue
			}
		case "table":
			w, h := 160.0, 30.0
			if value, ok := sceneObjectDimension(q, "width", cols); ok {
				w = value
			}
			if value, ok := sceneObjectDimension(q, "height", rows); ok {
				h = value
			}
			x, y := tableSceneAnchor(q, w, h, cols, rows)
			o.Bounds = sceneRect{x * sx, y * sy, w * sx, h * sy}
			if deck.Objects == nil {
				warn(id, "missing-table", "Table data is missing.")
				continue
			}
			data, exists := deck.Objects.Tables[e.ObjectID]
			if !exists {
				warn(id, "missing-table", "Table data is missing.")
				continue
			}
			var err error
			tableColor := o.Paint.Color
			if (deck.Theme != nil || data.Style.Cell.DefaultColor != "") && !q.Has("fg") && !q.Has("header") && !q.Has("theme-fg") && objectSlide.FG == "" {
				tableColor = ""
				if data.Style.Cell.DefaultColor != "" {
					o.Paint.Color = data.Style.Cell.DefaultColor
				}
			}
			var hit bool
			o.Table, err, hit = sharedSceneTables.project(deck, data, o.Bounds, tableColor)
			if err != nil {
				warn(id, "invalid-table", err.Error())
				continue
			}
			if metrics != nil {
				if hit {
					metrics.TableProjectionHits = append(metrics.TableProjectionHits, id)
				} else {
					metrics.TableProjectionBuilds = append(metrics.TableProjectionBuilds, id)
				}
			}
		case "heading", "text", "bullet", "code", "text-image", "page-number":
			o.Kind = "text"
			o.Text = cachedText
			if o.Text == nil {
				o.Text = deck.sceneTextForStyleFromValues(e, treatment, q)
				if metrics != nil {
					metrics.TextProjectionBuilds = append(metrics.TextProjectionBuilds, id)
				}
			}
			deck.resolveWordColors(o.Text)
			deck.resolveWordFonts(o.Text)
			if e.Kind == "page-number" && o.Text != nil {
				// Automatic page-number boxes must match the resolved theme font,
				// not the C64 occupancy estimate used by legacy layout. Preserve
				// the already-resolved right/bottom edges when the box grows.
				if !q.Has("width") {
					width := math.Max(1, math.Ceil(chartTextWidth(sceneTextPlain(o.Text), o.Text)+1))
					if q.Has("right") || q.Has("right_pct") {
						o.Bounds.X += o.Bounds.Width - width
					}
					o.Bounds.Width = width
				}
				if !q.Has("height") {
					height := math.Max(1, math.Ceil(o.Text.Size*o.Text.LineHeight))
					if q.Has("bottom") {
						o.Bounds.Y += o.Bounds.Height - height
					}
					o.Bounds.Height = height
				}
			}
			sharedText := treatment == "retro" && sharedRetroText(e)
			if !sharedText && (treatment == "retro" || q.Get("font") != "" || q.Get("glyph") != "") {
				if treatment == "retro" && o.RetroText == nil {
					o.RetroLines = &sceneRetroLines{Cols: cols, Rows: rows, Lines: retroObjectLines(deck, retroSlide, objectRows(i), i, cols, rows)}
					applyRetroOpacity(o.RetroLines, e)
					o.Rotation = 0
				}
				if o.RetroText == nil && o.RetroLines == nil {
					warn(id, "art-treatment", "Custom glyph treatment needs an explicit retained-art adapter; this preview shows semantic text.")
				}
			}
		case "shape":
			o.Shape = shapeName(e)
			geometry := shapeGeometry(shapeSpecFromValues(q), shapeDimension(q, "width", 12)*sx/(shapeDimension(q, "height", 6)*sy))
			o.ShapeGeometry = &geometry
			x, y := preciseShapeAnchor(q, o.Bounds.X/sx, o.Bounds.Y/sy, cols, rows)
			anchorDX, anchorDY := x*sx-o.Bounds.X, y*sy-o.Bounds.Y
			o.Bounds.X, o.Bounds.Y = x*sx, y*sy
			if anchorDX != 0 || anchorDY != 0 {
				o.SampleOffset = &connectorPoint{X: anchorDX, Y: anchorDY}
			}
			o.Bounds.X += shapeSubcellOffset(q, "shape-offset-x") * sx
			o.Bounds.Y += shapeSubcellOffset(q, "shape-offset-y") * sy
			o.Bounds.Width = shapeDimension(q, "width", 12) * sx
			o.Bounds.Height = shapeDimension(q, "height", 6) * sy
			if label, ok := shapeLabel(e); ok {
				labelQuery, _ := url.ParseQuery(label.Query)
				o.LabelStyle = labelQuery.Get("element-style")
				if deck.usesConcreteAppearance() {
					o.LabelStyle = "modern"
				} else if !validElementStyle(o.LabelStyle) {
					o.LabelStyle = treatment
				}
				o.Label = deck.sceneTextForStyleFromValues(label, o.LabelStyle, labelQuery)
				deck.resolveWordColors(o.Label)
				deck.resolveWordFonts(o.Label)
				labelSlide := objectSlide
				labelSlide.ThemeColors = deck.themeColors(o.LabelStyle)
				p := scenePaintFor(label, labelSlide, sx, sy)
				p = deck.applyThemeObjectPaint(label, labelSlide, labelQuery, p)
				o.LabelPaint = &p
				if o.LabelStyle == "retro" && !sharedRetroText(label) {
					for _, line := range retroObjectLines(deck, retroSlide, objectRows(i), i, cols, rows) {
						if line.Role == "shape-label" && line.TrueType != nil {
							o.RetroLabel = &sceneRetroText{Cols: cols, Rows: rows, Line: line}
							break
						}
					}
				}
			}
			if treatment == "retro" {
				// Export only this object's already-positioned rows. Exporting
				// the full stack would bake later transparent objects into it.
				var ownLines []Line
				for _, line := range objectRows(i) {
					if line.Element == i && line.Role != "shape-label" {
						if o.Paint.NoFill && line.Role != "outline" {
							continue
						}
						if line.Role == "outline" && (q.Has("shape-stroke") || q.Has("shape-stroke-width")) {
							continue
						}
						ownLines = append(ownLines, line)
					}
				}
				retroSlide := slide
				retroSlide.ThemeColors = deck.themeColors("retro")
				// Retain the full fill coverage and composite at paint time.
				// Otherwise exportLines discards see-through shape rows and
				// bakes their tint into the already rendered neighbouring rows.
				if q.Get("transparent") == "1" {
					retroSlide.Elements = append([]Element(nil), slide.Elements...)
					fillQuery, _ := url.ParseQuery(e.Query)
					fillQuery.Del("transparent")
					retroSlide.Elements[i].Query = fillQuery.Encode()
				}
				o.RetroLines = &sceneRetroLines{Cols: cols, Rows: rows, Lines: exportLines(ownLines, retroSlide, cols, rows, len(deck.Slides))}
				if o.Paint.Stroke != "" && o.Paint.StrokeWidth > 0 && (q.Has("shape-stroke") || q.Has("shape-stroke-width") || !q.Has("outline")) {
					var reduced bool
					o.RetroStroke, reduced = retroShapeStroke(geometry, o.Bounds, o.Paint.StrokeWidth, sx, sy)
					if reduced {
						warn(id, "shape-stroke-raster-reduced", "The very large Retro border uses a coarser block grid to bound rendering work.")
					}
				}
				sx := shapeDimension(q, "width", 12) * 2 / float64(shapeHalfCells(q, "width", 12))
				sy := shapeDimension(q, "height", 6) * 2 / float64(shapeHalfCells(q, "height", 6))
				if sx != 1 || sy != 1 {
					o.SampleScale = &connectorPoint{X: sx, Y: sy}
				}
				if q.Get("transparent") == "1" {
					opacity := .5
					o.RetroLines.ShapeOpacity = &opacity
				}
				o.Rotation = 0
			}
		case "connector":
			route, ok := routes[e.ID]
			if !ok {
				warn(id, "dangling-connector", "Connector endpoint is unavailable.")
				continue
			}
			for _, p := range route.Points {
				o.Points = append(o.Points, connectorPoint{p.X * sx, p.Y * sy})
			}
			o.Arrows = route.Arrows
			o.Paint.StrokeWidth = connectorNumber(q, "connector-width", 1, 8) * sx
			o.ArrowWidth = connectorNumber(q, "connector-arrow-width", 6, 20) * sx
			if treatment != "retro" && len(o.Points) > 0 {
				// Vector route bounds come from the actual continuous vertices,
				// not blank cells carried by the legacy sampled layout box.
				left, right, top, bottom := o.Points[0].X, o.Points[0].X, o.Points[0].Y, o.Points[0].Y
				for _, point := range o.Points {
					left = math.Min(left, point.X)
					right = math.Max(right, point.X)
					top = math.Min(top, point.Y)
					bottom = math.Max(bottom, point.Y)
				}
				padding := o.Paint.StrokeWidth / 2
				if o.Arrows != "" && o.Arrows != "none" {
					padding = math.Max(padding, o.ArrowWidth/2)
				}
				o.Bounds = sceneRect{X: left - padding, Y: top - padding, Width: right - left + 2*padding, Height: bottom - top + 2*padding}
			}
			if treatment == "retro" {
				o.RetroLines = &sceneRetroLines{Cols: cols, Rows: rows, Lines: retroObjectLines(deck, retroSlide, objectRows(i), i, cols, rows), Alt: "Connector"}
			}
		case "image":
			asset, ok := deck.Assets[e.AssetID]
			if !ok {
				warn(id, "missing-source", "Embedded image source is unavailable.")
				o.Kind = "text"
				o.Text = sceneTextFor(Element{Kind: "text", Text: "[IMG]"})
				break
			}
			// Explicit box sizing is independent of the integer sampling grid.
			// Aspect-fit imports keep their natural fitted bounds until resized.
			if q.Get("stretch") == "1" || imageFit(q.Get("image-fit")) != "" {
				old := o.Bounds
				if w, ok := sceneObjectDimension(q, "width", cols); ok {
					o.Bounds.Width = w * sx
				}
				if h, ok := sceneObjectDimension(q, "height", rows); ok {
					o.Bounds.Height = h * sy
				}
				x, y := preciseTextAnchor(q, o.Bounds.Width/sx, o.Bounds.Height/sy, old.X/sx, old.Y/sy, cols, rows)
				o.Bounds.X, o.Bounds.Y = x*sx, y*sy
				if treatment == "retro" && old.Width > 0 && old.Height > 0 {
					if old.Width != o.Bounds.Width || old.Height != o.Bounds.Height {
						o.SampleScale = &connectorPoint{X: o.Bounds.Width / old.Width, Y: o.Bounds.Height / old.Height}
					}
					if old.X != o.Bounds.X || old.Y != o.Bounds.Y {
						o.SampleOffset = &connectorPoint{X: o.Bounds.X - old.X, Y: o.Bounds.Y - old.Y}
					}
				}
			}
			if treatment == "retro" {
				o.RetroLines = &sceneRetroLines{Cols: cols, Rows: rows, Lines: retroObjectLines(deck, retroSlide, objectRows(i), i, cols, rows), Alt: q.Get("alt-text"), Decorative: q.Get("image-decorative") == "1"}
				applyRetroOpacity(o.RetroLines, e)
				if len(asset.Frames) > 1 {
					isolated := slide
					isolated.ThemeColors = deck.themeColors("retro")
					position, _ := url.ParseQuery(e.Query)
					position.Del("transparent")
					for _, key := range []string{"bottom", "left_pct", "right", "right_pct", "row_delta", "align", "valign"} {
						position.Del(key)
					}
					position.Set("top", strconv.Itoa(b.top))
					position.Set("left", strconv.Itoa(b.left))
					animated := e
					animated.Query = position.Encode()
					isolated.Elements = []Element{animated}
					o.RetroLines.Frames = retroObjectFrames(isolated, asset.Frames, cols, rows, len(deck.Slides))
					o.RetroLines.LoopCount = asset.LoopCount
					if len(o.RetroLines.Frames) > 0 {
						o.RetroLines.Lines = o.RetroLines.Frames[0].Lines
					}
				}
			}
			media := &sceneMedia{Width: asset.Width, Height: asset.Height, LoopCount: asset.LoopCount, ColourMatrices: sceneImageColourMatrices(e.Query)}
			options := parseImageASCIIOptions(e.Query)
			if options.sharpness != 1 {
				media.Sharpness = options.sharpness
				if options.brightness != 1 || options.contrast != 1 || options.saturation != 1 {
					media.SharpnessAfter = 1
				}
			}
			media.Alt = q.Get("alt-text")
			media.Decorative = q.Get("image-decorative") == "1"
			media.Crop = parseModernCrop(q.Get("modern-crop"))
			media.Fit = imageFit(q.Get("image-fit"))
			if mask := q.Get("modern-mask"); mask == "rounded" || mask == "ellipse" {
				media.Mask = mask
			}
			source, cached := mediaSources[e.AssetID]
			if !cached {
				source = sceneMediaSource(asset)
				mediaSources[e.AssetID] = source
			}
			media.Source, media.Width, media.Height = source.Source, source.Width, source.Height
			media.Frames = append([]sceneMediaFrame(nil), source.Frames...)
			if asset.Source == nil {
				warn(id, "limited-source", "Only the legacy reduced image/frame source is available.")
			}
			o.Media = media
		default:
			warn(id, "unsupported-object", "This preview cannot yet draw object kind "+e.Kind+".")
			continue
		}
		if o.Kind == "text" && o.RetroLines == nil && o.RetroText == nil {
			naturalWidth := e.Kind != "page-number" && o.Text != nil && !q.Has("width")
			if naturalWidth {
				var natural sceneRect
				if cacheText {
					natural, _ = sharedSceneTextObjects.naturalSize(textKey, o.Text)
				} else {
					natural.Width, natural.Height = sceneTextNaturalSize(o.Text)
				}
				o.Bounds = naturalSceneTextBounds(o.Bounds, natural, q)
			}
			if width, ok := sceneObjectDimension(q, "width", cols); ok {
				o.Bounds.Width = width * sx
			}
			if height, ok := sceneObjectDimension(q, "height", rows); ok {
				o.Bounds.Height = height * sy
			}
			autoHeight := q.Has("width") && !q.Has("height") && o.Text != nil && e.Kind != "page-number"
			if autoHeight {
				if cacheText {
					o.Bounds.Height, _ = sharedSceneTextObjects.wrappedHeight(textKey, o.Text, o.Bounds.Width)
				} else {
					o.Bounds.Height = sceneTextWrappedHeight(o.Text, o.Bounds.Width)
				}
			}
			x, y := preciseTextAnchorForFrame(q, o.Bounds.Width/sx, o.Bounds.Height/sy, o.Bounds.X/sx, o.Bounds.Y/sy, cols, rows, naturalWidth || autoHeight)
			o.Bounds.X, o.Bounds.Y = x*sx, y*sy
			// Emoji rows have real square artwork, not an invisible text margin.
			// Old authored frames can be shorter than their line box; include its
			// full height while preserving the painted top/centre/bottom anchor.
			if q.Has("height") && sceneOnlyEmojiText(o.Text) {
				height := sceneTextWrappedHeight(o.Text, o.Bounds.Width)
				if height > o.Bounds.Height {
					switch o.Text.Vertical {
					case "middle":
						o.Bounds.Y += (o.Bounds.Height - height) / 2
					case "bottom":
						o.Bounds.Y += o.Bounds.Height - height
					}
					o.Bounds.Height = height
				}
			}
		}
		if treatment == "retro" && o.RetroLines == nil && o.RetroText == nil && !(o.Kind == "text" && sharedRetroText(e)) {
			warn(id, "retro-adapter-pending", "This Retro treatment is not supported in the shared scene yet; this preview uses a Modern fallback without changing the authored object.")
		}
		if q.Get("transparent") == "1" && o.RetroLines == nil && o.RetroText == nil && o.Text == nil {
			warn(id, "retro-blending", "Retro see-through is retained in the deck; it is not standard alpha opacity.")
		}
		if o.Bounds.Y+o.Bounds.Height > scene.Height || o.Bounds.X+o.Bounds.Width > scene.Width {
			warn(id, "bounds-overflow", "Object extends beyond this slide; its authored bounds have not been changed.")
		}
		if angle, err := strconv.ParseFloat(q.Get("object-rotation"), 64); err == nil && !math.IsNaN(angle) && !math.IsInf(angle, 0) && o.Kind != "connector" {
			o.Rotation = math.Mod(angle, 360)
		}
		if o.Kind != "connector" {
			dx, dy := objectPlacementOffset(q, "x")*sx, objectPlacementOffset(q, "y")*sy
			o.Bounds.X += dx
			o.Bounds.Y += dy
			if (dx != 0 || dy != 0) && (o.RetroText != nil || o.RetroLines != nil || o.RetroLabel != nil) {
				if o.SampleOffset == nil {
					o.SampleOffset = &connectorPoint{}
				}
				o.SampleOffset.X += dx
				o.SampleOffset.Y += dy
			}
		}
		setRetroPageDimensions(o.RetroLines, scene.Width, scene.Height)
		scene.Objects = append(scene.Objects, o)
		if cacheText && cachedText == nil {
			sharedSceneTextObjects.put(textKey, sceneObject{Kind: "text", Text: o.Text}, nil)
		}
	}
	if slide.Engagement != nil {
		warn("", "activity-preview", "Activity remains attached to this slide; this preview does not start it or expose its private data.")
	}
	scene.SlideID = resolved.ID
	scene.Build = sceneSlideBuild(deck, resolved.ID, scene.Objects)
	scene.Typefaces = sceneTypefaceResources(scene, deck.Typefaces)
	markPreviewOnlyText(&scene)
	return scene, nil
}

func sceneMediaSource(asset DeckAsset) sceneMedia {
	media := sceneMedia{Width: asset.Width, Height: asset.Height, LoopCount: asset.LoopCount}
	uri := func(mime string, data []byte) string {
		return sharedSceneSources.uri(mime, data)
	}
	if asset.Source != nil && len(asset.Source.Data) > 0 {
		media.Source = uri(asset.Source.MIME, asset.Source.Data)
		media.Width, media.Height = asset.Source.Width, asset.Source.Height
	} else if len(asset.Data) > 0 {
		media.Source = uri(asset.MIME, asset.Data)
	}
	if vector := asset.Vector; vector != nil {
		media.Source = uri(svgMediaType, vector.Data)
		media.Width, media.Height = vector.Width, vector.Height
	}
	frames := asset.Frames
	if asset.Source != nil && len(asset.Source.Frames) > 0 {
		frames = asset.Source.Frames
		media.Width, media.Height = asset.Source.Width, asset.Source.Height
	}
	for _, frame := range frames {
		delay := frame.DelayMS
		if delay < 10 {
			delay = 10
		}
		media.Frames = append(media.Frames, sceneMediaFrame{uri("image/png", frame.Data), delay})
	}
	if media.Source == "" && len(media.Frames) > 0 {
		// Animated assets may contain frames only. A first-frame source is
		// required by backgrounds, still fallbacks and interchange consumers.
		media.Source = media.Frames[0].Source
	}
	return media
}

// sceneBackgroundRetroLines samples a background using the same renderer as
// an ordinary Retro image. A background has no authored placement of its own,
// so it deliberately fills the complete slide while retaining all treatment
// keys (glyph, sampling, brightness, tint, sharpness and alpha threshold).
func sceneBackgroundRetroLines(deck Deck, slide Slide, assetID string, asset DeckAsset, query string, cols, rows int) *sceneRetroLines {
	if assetID == "" || cols <= 0 || rows <= 0 {
		return nil
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		values = url.Values{}
	}
	values.Set("image-style", "retro")
	values.Set("left", "0")
	values.Set("top", "0")
	values.Set("width", strconv.Itoa(cols))
	values.Set("height", strconv.Itoa(rows))
	values.Set("stretch", "1")
	fit := "cover"
	if slide.BackgroundMedia != nil && imageFit(slide.BackgroundMedia.Fit) != "" {
		fit = slide.BackgroundMedia.Fit
	}
	values.Set("image-fit", fit)
	// Physical page aspect is independent of the sampling grid's cell aspect.
	width, height := deck.pageDimensions()
	values.Set("keynope-fit-aspect", strconv.FormatFloat(width/height, 'f', -1, 64))
	for _, key := range []string{"right", "right_pct", "left_pct", "bottom", "row_delta", "align", "valign", "object-hidden", "object-offset-x", "object-offset-y", "scale"} {
		values.Del(key)
	}
	// Parsed documents materialize these assets already. Registering here also
	// makes a newly constructed in-memory deck render consistently in tests and
	// preview before its first save/load cycle.
	if len(asset.Frames) > 0 {
		registerEmbeddedAnimatedAsset(assetID, asset)
	} else {
		registerEmbeddedStillAsset(assetID, asset)
	}
	isolate := slide
	isolate.ThemeColors = deck.themeColors("retro")
	ext := ".png"
	if len(asset.Frames) > 0 || asset.MIME == "image/gif" {
		ext = ".gif"
	}
	// Use the registered compact source on every platform. Rendering an owned
	// in-memory deck must not depend on a previous disk materialisation pass.
	isolate.Elements = []Element{{Kind: "image", AssetID: assetID, Path: "keynope-asset:" + assetID + ext, Query: values.Encode()}}
	lines := exportLines(layout(isolate, cols, rows), isolate, cols, rows, len(deck.Slides))
	retro := &sceneRetroLines{Cols: cols, Rows: rows, Lines: lines, Decorative: true}
	setRetroPageDimensions(retro, width, height)
	if len(asset.Frames) > 1 {
		retro.Frames = retroObjectFrames(isolate, asset.Frames, cols, rows, len(deck.Slides))
		retro.LoopCount = asset.LoopCount
		if len(retro.Frames) > 0 {
			retro.Lines = retro.Frames[0].Lines
		}
	}
	return retro
}

func (s *nativeEditorSession) handleScene(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	index, err := strconv.Atoi(r.URL.Query().Get("slide"))
	if err != nil {
		http.Error(w, "invalid slide", http.StatusBadRequest)
		return
	}
	// Explicit local profiling bypasses cached responses so these phase timings
	// still describe actual projection work. Normal misses stream their JSON
	// while retaining at most one bounded response for this session.
	profile := r.URL.Query().Get("timing") == "1"
	var phaseStart time.Time
	if profile {
		phaseStart = time.Now()
	}
	markPhase := func(name string) {
		if !profile {
			return
		}
		now := time.Now()
		w.Header().Add("Server-Timing", fmt.Sprintf("%s;dur=%.3f", name, float64(now.Sub(phaseStart))/float64(time.Millisecond)))
		phaseStart = now
	}
	cols, rows := authoredRenderSize(245, 56)
	s.mu.RLock()
	revision := s.version
	masterMode := s.masterMode
	cacheKey := sceneResponseKey{revision: revision, index: index, cols: cols, rows: rows, master: masterMode, styles: r.URL.Query().Get("styles"), report: r.URL.Query().Get("report")}
	if !profile && s.sceneResponse != nil && s.sceneResponse.key == cacheKey {
		cached := s.sceneResponse
		s.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Keynope-Scene-Cache", "hit")
		cached.write(w)
		return
	}
	deck := cloneDeckForScene(s.deck)
	s.mu.RUnlock()
	markPhase("snapshot")
	var scene slideScene
	var authored []Element
	var metrics *sceneRenderMetrics
	if profile {
		metrics = &sceneRenderMetrics{}
	}
	// Document editing must use the same resolved styles as presentation output.
	// Conversion previews remain explicitly available without modifying the deck.
	styleRequest := r.URL.Query().Get("styles")
	resolvedStyles := styleRequest == "resolved" || (styleRequest == "" && r.URL.Query().Get("report") != "deck" && deck.usesElementStyles())
	if masterMode {
		if index < 0 || index > len(deck.Masters.Layouts) {
			http.Error(w, "invalid master", http.StatusBadRequest)
			return
		}
		scene, err = buildStyledSceneTargetMeasured(deck, masterViewPreview(deck.Masters, index), index, cols, rows, resolvedStyles, "", metrics)
		authored = masterSlideAt(&deck, index).Elements
		scene.Master = true
	} else {
		if index < 0 || index >= len(deck.Slides) {
			http.Error(w, "invalid slide", http.StatusBadRequest)
			return
		}
		scene, err = buildStyledSceneTargetMeasured(deck, deck.ResolveSlide(index, false), index, cols, rows, resolvedStyles, "", metrics)
		if err == nil {
			authored = deck.Slides[index].Elements
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if metrics != nil {
		w.Header().Set("X-Keynope-Scene-Builds", strconv.Itoa(metrics.SceneBuilds))
		w.Header().Set("X-Keynope-Layout-Builds", strconv.Itoa(metrics.LayoutBuilds))
		w.Header().Set("X-Keynope-Layout-Cache-Hits", strconv.Itoa(metrics.LayoutCacheHits))
		w.Header().Set("X-Keynope-Object-Layout-Builds", strconv.Itoa(len(metrics.ObjectLayoutBuilds)))
		w.Header().Set("X-Keynope-Object-Layout-Cache-Hits", strconv.Itoa(len(metrics.ObjectLayoutHits)))
		w.Header().Set("X-Keynope-Object-Artwork-Layouts", strconv.Itoa(len(metrics.ObjectArtworkLayouts)))
		w.Header().Set("X-Keynope-Text-Builds", strconv.Itoa(len(metrics.TextProjectionBuilds)))
		w.Header().Set("X-Keynope-Text-Cache-Hits", strconv.Itoa(len(metrics.TextProjectionHits)))
		w.Header().Set("X-Keynope-Table-Builds", strconv.Itoa(len(metrics.TableProjectionBuilds)))
		w.Header().Set("X-Keynope-Table-Cache-Hits", strconv.Itoa(len(metrics.TableProjectionHits)))
	}
	scene.Revision, scene.SlideIndex = &revision, index
	if r.URL.Query().Get("report") == "deck" {
		scene.Conversion, err = buildSceneConversionReport(deck, cols, rows)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	markPhase("projection")
	annotateSceneEditing(&scene, deck, authored)
	markPhase("annotation")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Keynope-Scene-Cache", "miss")
	if profile {
		w.Header().Set("X-Keynope-Scene-Cache", "bypass")
		var encoded bytes.Buffer
		if err := json.NewEncoder(&encoded).Encode(scene); err != nil {
			http.Error(w, "could not encode scene", http.StatusInternalServerError)
			return
		}
		markPhase("encoding")
		_, _ = w.Write(encoded.Bytes())
		return
	}
	var capture sceneResponseCapture
	if err := json.NewEncoder(io.MultiWriter(w, &capture)).Encode(scene); err == nil && !capture.exceeded {
		s.mu.Lock()
		// Never let a slow old projection evict a newer revision's response.
		if s.version == revision && s.masterMode == masterMode {
			s.sceneResponse = newSceneResponseCache(cacheKey, capture.data)
		}
		s.mu.Unlock()
	}
}

func annotateSceneEditing(scene *slideScene, deck Deck, authored []Element) {
	// Preserve source order even for malformed duplicate IDs, without scanning
	// and parsing every unrelated element for each projected object.
	byID := make(map[string][]Element, len(authored))
	for _, element := range authored {
		if element.ID != "" && !objectLocked(element) && !protectedActivityElement(element) {
			byID[element.ID] = append(byID[element.ID], element)
		}
	}
	for i := range scene.Objects {
		object := &scene.Objects[i]
		for _, element := range byID[object.ID] {
			if element.Kind == "image" && object.Media != nil {
				object.Editable = true
			}
			if element.Kind == "shape" {
				label, _, err := sceneEditableShapeLabel(element)
				object.Editable = err == nil
				if object.Editable && object.Label == nil {
					object.Label = deck.modernSceneText(label)
					object.LabelPaint = &scenePaint{Color: "#ffffff"}
				}
				if object.Editable && (hasWordColorTokens(sceneTextFor(label)) || hasWordFontRoles(sceneTextFor(label))) {
					object.EditRuns = sceneTextFor(label).Runs
				}
			}
			if element.Kind == "table" && object.Table != nil || element.Kind == "chart" && object.Chart != nil {
				object.Editable = true
			}
			if element.Kind == "text" || element.Kind == "heading" || element.Kind == "code" || element.Kind == "bullet" {
				object.Editable = true
				if element.Kind == "bullet" || hasWordColorTokens(sceneTextFor(element)) || hasWordFontRoles(sceneTextFor(element)) {
					element.Kind = "text"
					object.EditRuns = sceneTextFor(element).Runs
				}
			}
		}
	}
}

// Empty shapes have an editor-only label draft. Merely opening the dialog does
// not add metadata. Existing label style data is retained verbatim on commit.
func sceneEditableShapeLabel(owner Element) (Element, *shapeLabelData, error) {
	q, _ := url.ParseQuery(owner.Query)
	data := &shapeLabelData{Query: "render=truetype&text-align=center&text-valign=middle&fg=%23ffffff"}
	if encoded := q.Get("shape-label"); encoded != "" {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) > 65536 {
			return Element{}, nil, fmt.Errorf("invalid shape label")
		}
		if err := json.Unmarshal(raw, data); err != nil {
			return Element{}, nil, err
		}
	}
	values, _ := url.ParseQuery(data.Query)
	values = shapeLabelStyleValues(owner, values)
	values.Set("render", "truetype")
	if !values.Has("text-align") {
		values.Set("text-align", "center")
	}
	if !values.Has("text-valign") {
		values.Set("text-valign", "middle")
	}
	if !values.Has("fg") && !values.Has("theme-fg") {
		values.Set("fg", "#ffffff")
	}
	return Element{Kind: "text", ID: owner.ID, Text: data.Text, Query: values.Encode()}, data, nil
}

// Commit a shaped-text edit against the exact scene revision it started from.
// Resolve by stable identity, never by the current selection or display order.
func withModernFont(query, font string) string {
	values, _ := url.ParseQuery(query)
	if font == "" {
		values.Del("modern-font")
	} else {
		values.Set("modern-font", font)
	}
	return values.Encode()
}

func applySceneText(deck *Deck, action nativeEditorAction) (bool, error) {
	if value := action.ModernWidth; value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < .4167 || *value > 200) {
		return false, fmt.Errorf("font width is outside the supported range")
	}
	for _, value := range []*float64{action.ModernParagraphBefore, action.ModernParagraphAfter} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 4) {
			return false, fmt.Errorf("paragraph spacing must be between 0 and 4")
		}
	}
	if action.ModernSize != nil && (math.IsNaN(*action.ModernSize) || math.IsInf(*action.ModernSize, 0) || (*action.ModernSize != 0 && *action.ModernSize < 1) || *action.ModernSize > 1024) {
		return false, fmt.Errorf("Modern font size must be between 1 and 1024, or zero to inherit")
	}
	if action.ModernLineHeight != nil && (math.IsNaN(*action.ModernLineHeight) || math.IsInf(*action.ModernLineHeight, 0) || *action.ModernLineHeight < .5 || *action.ModernLineHeight > 4) {
		return false, fmt.Errorf("line spacing must be between 0.5 and 4")
	}
	if action.ModernFont != nil && *action.ModernFont != "" && !validTextFont(*action.ModernFont) && *action.ModernFont != "theme" {
		return false, fmt.Errorf("unsupported Modern font")
	}
	if action.Slide < 0 || action.Slide >= len(deck.Slides) || action.ObjectID == "" || len(action.TextRuns) > 10000 {
		return false, errInvalidEditorAction
	}
	if _, err := editableObjectIndices(deck.Slides[action.Slide], []string{action.ObjectID}); err != nil {
		return false, err
	}
	for i, element := range deck.Slides[action.Slide].Elements {
		if element.ID != action.ObjectID {
			continue
		}
		owner := element
		grownQuery := owner.Query
		if action.ShapeTextBounds != nil {
			var err error
			grownQuery, err = shapeTextGrowthQuery(*deck, action, owner)
			if err != nil {
				return false, err
			}
		}
		if objectLocked(element) {
			return false, fmt.Errorf("object is locked; unlock it in Objects before editing")
		}
		var labelData *shapeLabelData
		if element.Kind == "shape" {
			labelOwner := element
			for _, candidate := range deck.ResolveSlide(action.Slide, false).Elements {
				if candidate.ID == element.ID {
					labelOwner = candidate
					break
				}
			}
			label, data, err := sceneEditableShapeLabel(labelOwner)
			if err != nil {
				return false, err
			}
			labelData = data
			element = label
		} else if element.MasterSlotID != "" {
			// Rich-run fingerprints and edit metrics must use the effective
			// master weight, not the child's sparse authored query. Keep owner
			// unchanged so committing does not materialize unrelated defaults.
			for _, candidate := range deck.ResolveSlide(action.Slide, false).Elements {
				if candidate.ID == element.ID {
					element = candidate
					break
				}
			}
		}
		if protectedActivityElement(owner) || (element.Kind != "text" && element.Kind != "heading" && element.Kind != "code" && element.Kind != "bullet") {
			return false, errInvalidEditorAction
		}
		q, _ := url.ParseQuery(element.Query)
		// Legacy conversion previews are explicitly Modern. Mixed documents
		// resolve the active element/label profile, just as their scene does.
		treatment := "modern"
		if deck.usesElementStyles() && !deck.usesConcreteAppearance() {
			treatment = deck.elementStyle(deck.Slides[action.Slide], owner)
		}
		if explicit := q.Get("element-style"); validElementStyle(explicit) {
			treatment = explicit
		}
		sizeKey, widthKey, widthFactor := "modern-size", "modern-width", 1.0
		textStyle := deck.sceneTextForStyle(element, treatment)
		if treatment == "modern" && action.ModernWidth != nil && *action.ModernWidth < 1 {
			return false, fmt.Errorf("Modern font width must be between 1 and 200 percent")
		}
		if treatment == "retro" {
			sizeKey, widthKey, widthFactor = "ttf-size", "ttf-width", .4167
			if action.ModernSize != nil && (*action.ModernSize > trueTypeMaxSize || math.Trunc(*action.ModernSize) != *action.ModernSize) {
				return false, fmt.Errorf("C64 font size must be a whole number between 1 and 512, or zero to inherit")
			}
			if action.ModernWidth != nil && (*action.ModernWidth/widthFactor < 1 || *action.ModernWidth/widthFactor > 200) {
				return false, fmt.Errorf("C64 font width must be between 1 and 200 percent of its baseline")
			}
			if action.ModernFont != nil && *action.ModernFont != "c64" && *action.ModernFont != "" || action.ModernLineHeight != nil && *action.ModernLineHeight != textStyle.LineHeight || action.ModernParagraphBefore != nil && *action.ModernParagraphBefore != 0 || action.ModernParagraphAfter != nil && *action.ModernParagraphAfter != 0 {
				return false, fmt.Errorf("this Retro text treatment does not yet support changing font family or paragraph spacing")
			}
		}
		if deck.usesConcreteAppearance() && concreteTextFont(element.Kind, "modern", q) == "c64" {
			widthFactor = .4167
		}
		var content strings.Builder
		for _, run := range action.TextRuns {
			if run.Text == "" {
				continue
			}
			if run.Color != "" && element.Kind != "code" {
				hex, ok := normalizeHexColour(run.Color)
				if !ok {
					return false, fmt.Errorf("invalid text colour")
				}
				content.WriteString("[color=" + hex + "]")
				content.WriteString(run.Text)
				content.WriteString("[/color]")
			} else {
				content.WriteString(run.Text)
			}
			if content.Len() > 100000 {
				return false, fmt.Errorf("text exceeds editing limit")
			}
		}
		styles, err := encodeModernRuns(content.String(), q.Get("ttf-weight"), element.Kind, action.TextRuns)
		if err != nil {
			return false, err
		}
		stylesChanged := q.Get("modern-runs") != styles
		fontChanged := treatment == "modern" && action.ModernFont != nil && q.Get("modern-font") != *action.ModernFont
		spacingChanged := action.ModernLineHeight != nil && textStyle.LineHeight != *action.ModernLineHeight
		targetWidth := 0.0
		if action.ModernWidth != nil {
			targetFactor := widthFactor
			if treatment == "retro" && !deck.usesConcreteAppearance() {
				targetFactor = 1
			}
			targetWidth = *action.ModernWidth / 100 * targetFactor
		}
		widthChanged := action.ModernWidth != nil && math.Abs(textStyle.WidthScale-targetWidth) > 1e-9
		paragraphChanged := action.ModernParagraphBefore != nil && textStyle.ParagraphBefore != *action.ModernParagraphBefore || action.ModernParagraphAfter != nil && textStyle.ParagraphAfter != *action.ModernParagraphAfter
		sizeChanged := action.ModernSize != nil && ((*action.ModernSize == 0 && q.Has(sizeKey)) || (*action.ModernSize != 0 && textStyle.Size != *action.ModernSize))
		updateStyle := func(query string) string {
			if widthChanged {
				values, _ := url.ParseQuery(query)
				stored := *action.ModernWidth
				if treatment == "retro" && !deck.usesConcreteAppearance() {
					stored /= widthFactor
				}
				values.Set(widthKey, strconv.FormatFloat(stored, 'f', -1, 64))
				query = values.Encode()
			}
			if paragraphChanged {
				values, _ := url.ParseQuery(query)
				for key, value := range map[string]*float64{"modern-paragraph-before": action.ModernParagraphBefore, "modern-paragraph-after": action.ModernParagraphAfter} {
					if value == nil {
						continue
					}
					values.Set(key, strconv.FormatFloat(*value, 'f', -1, 64))
				}
				query = values.Encode()
			}
			if sizeChanged {
				values, _ := url.ParseQuery(query)
				if *action.ModernSize == 0 {
					values.Del(sizeKey)
				} else {
					values.Set(sizeKey, strconv.FormatFloat(*action.ModernSize, 'f', -1, 64))
				}
				query = values.Encode()
			}
			if stylesChanged {
				values, _ := url.ParseQuery(query)
				for _, key := range []string{"text-bold", "text-italic", "text-underline"} {
					values.Del(key)
				}
				if labelData == nil && owner.MasterSlotID != "" {
					setMasterQueryClears(values, []string{"text-bold", "text-italic", "text-underline"}, []string{"text-bold", "text-italic", "text-underline"})
				}
				if styles == "" {
					values.Del("modern-runs")
				} else {
					values.Set("modern-runs", styles)
				}
				query = values.Encode()
			}
			if fontChanged {
				query = withModernFont(query, *action.ModernFont)
			}
			if spacingChanged {
				values, _ := url.ParseQuery(query)
				// The editor sends the chosen numeric value, not an inheritance
				// request. Even the default must override a different master value.
				values.Set("modern-line-height", strconv.FormatFloat(*action.ModernLineHeight, 'f', -1, 64))
				query = values.Encode()
			}
			return query
		}
		if content.String() == element.Text && !fontChanged && !spacingChanged && !paragraphChanged && !stylesChanged && !sizeChanged && !widthChanged && grownQuery == owner.Query {
			return false, nil
		}
		if labelData != nil {
			labelData.Text = content.String()
			// Inline content edits start from the effective label style, including
			// sparse owner overrides. Starting from the older nested payload
			// would silently restore its size/paint when synchronising overrides.
			labelData.Query = updateStyle(element.Query)
			if stylesChanged {
				// The inline editor can create mixed emphasis. Its new rich runs
				// replace whole-label emphasis flags, which must not force every
				// edited character back to the previous uniform formatting.
				ownerValues := mustQuery(grownQuery)
				for _, key := range []string{"ttf-weight", "text-italic", "text-underline"} {
					ownerValues.Del("label-" + key)
				}
				grownQuery = ownerValues.Encode()
			}
			encoded, err := json.Marshal(labelData)
			if err != nil {
				return false, err
			}
			if len(encoded) > 65536 {
				return false, fmt.Errorf("shape label exceeds its storage limit")
			}
			grownQuery = syncExistingLabelOverrides(grownQuery, labelData.Query)
			next := setQueryValue(grownQuery, "shape-label", base64.StdEncoding.EncodeToString(encoded))
			if next == owner.Query {
				return false, nil
			}
			deck.Slides[action.Slide].Elements[i].Query = next
		} else {
			next := updateStyle(owner.Query)
			// Effective inherited runs can differ from their sparse authored
			// representation without producing any actual document change.
			if content.String() == owner.Text && next == owner.Query {
				return false, nil
			}
			// A deliberate frame resize may leave overflow, but committing new
			// content must not strand its extra lines outside the selection box.
			// Measure the effective master typography, never the sparse child.
			if content.String() != element.Text {
				candidate := element
				candidate.Text, candidate.Query = content.String(), updateStyle(element.Query)
				next = growEditedTextFrame(*deck, action.Slide, candidate, treatment, next)
			}
			deck.Slides[action.Slide].Elements[i].Text = content.String()
			deck.Slides[action.Slide].Elements[i].Query = next
		}
		return true, nil
	}
	return false, fmt.Errorf("the edited object no longer exists")
}

// Expand only the bottom of an explicitly bounded text object. Natural frames
// already derive their height from content. This is part of the text transaction
// (including undo/save), not a render-time mutation or a resize of its font.
func growEditedTextFrame(deck Deck, slide int, effective Element, treatment, authored string) string {
	values := mustQuery(effective.Query)
	if !values.Has("height") || !values.Has("width") {
		return authored
	}
	cols, rows := authoredRenderSize(245, 56)
	pageW, pageH := deck.pageDimensions()
	sx, sy := pageW/float64(cols), pageH/float64(rows)
	width, ok := sceneObjectDimension(values, "width", cols)
	if !ok {
		return authored
	}
	height, ok := sceneObjectDimension(values, "height", rows)
	if !ok {
		return authored
	}
	text := deck.sceneTextForStyle(effective, treatment)
	deck.resolveWordFonts(text)
	needed := sceneTextWrappedHeight(text, width*sx)
	if needed <= height*sy+.01 {
		return authored
	}
	// Resolve the old origin before changing height: bottom/centre anchors
	// would otherwise grow upwards, and flow placement can have no top field.
	scene, err := buildMixedSlideScene(deck, slide, cols, rows)
	if err != nil {
		return authored
	}
	for _, object := range scene.Objects {
		if object.ID != effective.ID || object.Kind != "text" || object.RetroLines != nil || object.RetroText != nil {
			continue
		}
		q := mustQuery(authored)
		q.Set("height", strconv.FormatFloat(needed/sy, 'f', -1, 64))
		for _, key := range []string{"bottom", "row_delta", "valign"} {
			q.Del(key)
		}
		y := object.Bounds.Y / sy
		q.Set("top", strconv.FormatFloat(math.Floor(y), 'f', 0, 64))
		q.Del("object-offset-y")
		if offset := y - math.Floor(y); offset > 0 {
			q.Set("object-offset-y", strconv.FormatFloat(offset, 'f', -1, 64))
		}
		if effective.MasterSlotID != "" {
			setMasterQueryClears(q, []string{"bottom", "row_delta", "valign", "object-offset-y"}, []string{"bottom", "row_delta", "valign"})
		}
		return q.Encode()
	}
	return authored
}

// Commit browser-measured label growth with its text. The scene revision is
// checked by the caller; only right/bottom expansion of a shape is permitted.
func shapeTextGrowthQuery(deck Deck, action nativeEditorAction, owner Element) (string, error) {
	b := *action.ShapeTextBounds
	if owner.Kind != "shape" {
		return "", fmt.Errorf("text growth requires a shape")
	}
	for _, value := range []float64{b.X, b.Y, b.Width, b.Height} {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 20000 {
			return "", fmt.Errorf("invalid shape growth bounds")
		}
	}
	cols, rows := authoredRenderSize(245, 56)
	scene, err := buildMixedSlideScene(deck, action.Slide, cols, rows)
	if err != nil {
		return "", err
	}
	for _, object := range scene.Objects {
		if object.ID != owner.ID {
			continue
		}
		old := object.Bounds
		if math.Abs(b.X-old.X) > .001 || math.Abs(b.Y-old.Y) > .001 || b.Width < old.Width-.001 || b.Height < old.Height-.001 || b.Width <= 0 || b.Height <= 0 {
			return "", fmt.Errorf("shape text may grow only the right and bottom edges")
		}
		if b.Width <= old.Width+.001 && b.Height <= old.Height+.001 {
			return owner.Query, nil
		}
		q, _ := url.ParseQuery(owner.Query)
		sx, sy := scene.Width/float64(cols), scene.Height/float64(rows)
		x, y := old.X/sx, old.Y/sy
		for _, key := range []string{"left_pct", "right_pct", "right", "bottom", "align", "valign", "row_delta"} {
			q.Del(key)
		}
		q.Set("left", strconv.Itoa(int(math.Floor(x))))
		q.Set("top", strconv.Itoa(int(math.Floor(y))))
		for axis, value := range map[string]float64{"x": x, "y": y} {
			fraction := value - math.Floor(value)
			half := math.Floor(fraction*2) / 2
			q.Set("shape-offset-"+axis, strconv.FormatFloat(half, 'f', 1, 64))
			q.Del("object-offset-" + axis)
			if remainder := fraction - half; remainder != 0 {
				q.Set("object-offset-"+axis, strconv.FormatFloat(remainder, 'f', -1, 64))
			}
		}
		// The browser has measured continuous physical extents. Do not quantize
		// both dimensions to half-cells: a width-only edit must leave the authored
		// height (including inheritance) untouched, and vice versa.
		if b.Width > old.Width+.001 {
			q.Set("width", strconv.FormatFloat(b.Width/sx, 'f', -1, 64))
		}
		if b.Height > old.Height+.001 {
			q.Set("height", strconv.FormatFloat(b.Height/sy, 'f', -1, 64))
		}
		return q.Encode(), nil
	}
	return "", fmt.Errorf("shape has no measurable scene bounds")
}
