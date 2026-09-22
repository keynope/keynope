package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type nativeEditorSession struct {
	sceneResponse *sceneResponseCache
	mu            sync.RWMutex
	deck          Deck
	savedDeck     Deck
	deckPath      string
	untitled      bool
	dirtyOverride bool
	current       int
	selected      int
	selection     map[int]bool
	editingGroup  string
	version       int64
	undo          []Deck
	redo          []Deck
	companion     *presenterCompanion
	timerDeadline time.Time
	masterMode    bool
	currentMaster int
}

func (s *nativeEditorSession) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image", http.StatusBadRequest)
		return
	}
	defer file.Close()
	_ = header
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "could not read image", http.StatusBadRequest)
		return
	}
	id, asset, path, err := embeddedImageAsset(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	element := Element{Kind: "image", Path: path, AssetID: id, Query: "image-style=modern"}
	if err := s.apply(nativeEditorAction{Action: "add-element", Kind: "image", ElementData: &element, AssetData: &asset}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.transportState())
}

type nativeEditorState struct {
	Appearance      *DeckAppearance      `json:"appearance,omitempty"`
	Diagnostics     []documentDiagnostic `json:"diagnostics,omitempty"`
	Tabs            []DeckTab            `json:"tabs"`
	HasActivities   bool                 `json:"hasActivities"`
	HideActivityQR  bool                 `json:"hideActivityQR"`
	CanUndo         bool                 `json:"canUndo"`
	CanRedo         bool                 `json:"canRedo"`
	Version         int64                `json:"version"`
	Path            string               `json:"path"`
	Current         int                  `json:"current"`
	Selected        int                  `json:"selected"`
	EditingGroup    string               `json:"editingGroup,omitempty"`
	Selection       []int                `json:"selection"`
	Slides          []Slide              `json:"slides,omitempty"`
	CurrentSlide    *Slide               `json:"currentSlide,omitempty"`
	Resolved        []Slide              `json:"resolved,omitempty"`
	ResolvedCurrent *Slide               `json:"resolvedCurrent,omitempty"`
	Masters         MasterDeck           `json:"masters"`
	MasterMode      bool                 `json:"masterMode,omitempty"`
	Dirty           bool                 `json:"dirty"`
	Untitled        bool                 `json:"untitled"`
	TimerMode       string               `json:"timerMode,omitempty"`
	TimerEndMS      int64                `json:"timerEndMs,omitempty"`
}

type nativeEditorAction struct {
	Crop                  *sceneCrop            `json:"crop,omitempty"`
	ModernMask            *string               `json:"modernMask,omitempty"`
	SceneRevision         *int64                `json:"sceneRevision,omitempty"`
	SceneMaster           bool                  `json:"sceneMaster,omitempty"`
	ObjectID              string                `json:"objectId,omitempty"`
	ObjectIDs             []string              `json:"objectIds,omitempty"`
	TextRuns              []sceneRun            `json:"textRuns,omitempty"`
	ModernFont            *string               `json:"modernFont,omitempty"`
	ModernLineHeight      *float64              `json:"modernLineHeight,omitempty"`
	ModernParagraphBefore *float64              `json:"modernParagraphBefore,omitempty"`
	ModernParagraphAfter  *float64              `json:"modernParagraphAfter,omitempty"`
	ModernSize            *float64              `json:"modernSize,omitempty"`
	ModernWidth           *float64              `json:"modernWidth,omitempty"`
	ShapeTextBounds       *sceneRect            `json:"shapeTextBounds,omitempty"`
	TextSize              *float64              `json:"textSize,omitempty"`
	TextSizeDelta         *float64              `json:"textSizeDelta,omitempty"`
	TextWidth             *float64              `json:"textWidth,omitempty"`
	TextWidthDelta        *float64              `json:"textWidthDelta,omitempty"`
	TextBold              *bool                 `json:"textBold,omitempty"`
	TextItalic            *bool                 `json:"textItalic,omitempty"`
	TextUnderline         *bool                 `json:"textUnderline,omitempty"`
	LabelPaint            map[string]string     `json:"labelPaint,omitempty"`
	TextAlign             *string               `json:"textAlign,omitempty"`
	TextVertical          *string               `json:"textVertical,omitempty"`
	Tabs                  []DeckTab             `json:"tabs,omitempty"`
	Action                string                `json:"action"`
	Slide                 int                   `json:"slide,omitempty"`
	Page                  int                   `json:"page,omitempty"`
	Value                 int                   `json:"value,omitempty"`
	Cols                  int                   `json:"cols,omitempty"`
	Rows                  int                   `json:"rows,omitempty"`
	BoxWidth              int                   `json:"boxWidth,omitempty"`
	BoxHeight             int                   `json:"boxHeight,omitempty"`
	Element               int                   `json:"element,omitempty"`
	Cursor                int                   `json:"cursor,omitempty"`
	SelectionStart        int                   `json:"selectionStart,omitempty"`
	SelectionEnd          int                   `json:"selectionEnd,omitempty"`
	Kind                  string                `json:"kind,omitempty"`
	Level                 int                   `json:"level,omitempty"`
	Name                  string                `json:"name,omitempty"`
	Path                  string                `json:"path,omitempty"`
	Notes                 string                `json:"notes,omitempty"`
	ElementData           *Element              `json:"elementData,omitempty"`
	ElementIndices        []int                 `json:"elementIndices,omitempty"`
	ElementsData          []Element             `json:"elementsData,omitempty"`
	SlideData             *Slide                `json:"slideData,omitempty"`
	AssetData             *DeckAsset            `json:"assetData,omitempty"`
	EngagementData        *EngagementDefinition `json:"engagementData,omitempty"`
	EngagementResult      *EngagementResult     `json:"engagementResult,omitempty"`
}

type nativeEditorCaret struct {
	Row   int `json:"row"`
	Col   int `json:"col"`
	Cells int `json:"cells"`
}

type nativeEditorInlinePreview struct {
	Pages          []exportPage               `json:"pages"`
	ElementData    *Element                   `json:"elementData,omitempty"`
	Caret          nativeEditorCaret          `json:"caret"`
	SelectionStart *nativeEditorCaret         `json:"selectionStart,omitempty"`
	SelectionEnd   *nativeEditorCaret         `json:"selectionEnd,omitempty"`
	SelectionRows  []nativeEditorSelectionRow `json:"selectionRows,omitempty"`
}

type nativeEditorSelectionRow struct {
	Row   int `json:"row"`
	Col   int `json:"col"`
	Cells int `json:"cells"`
}

type nativeEditorTextFit struct {
	Element Element      `json:"element"`
	Pages   []exportPage `json:"pages"`
}

type nativeEditorEmojiItem struct {
	Emoji    string          `json:"emoji"`
	TrueType *exportTrueType `json:"trueType,omitempty"`
	Name     string          `json:"name"`
	Group    string          `json:"group"`
	Lines    []exportLine    `json:"lines"`
}

type nativeEditorEmojiCatalog struct {
	Groups []string                `json:"groups"`
	Items  []nativeEditorEmojiItem `json:"items"`
}

func (s *nativeEditorSession) handleEmojiTextFonts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	text := r.URL.Query().Get("text")
	if len(text) > 8192 {
		http.Error(w, "emoji request too large", http.StatusRequestEntityTooLarge)
		return
	}
	fonts := map[string]string{}
	for _, token := range splitEmojiText(text) {
		if token.assetKey != "" {
			if data := emojiFontData(token.assetKey); data != "" {
				fonts[token.text] = data
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(fonts)
}

func (s *nativeEditorSession) handleEmojiCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 72
	}
	size := textSizeHeading
	if rawSize := r.URL.Query().Get("size"); rawSize != "" {
		size, _ = strconv.Atoi(rawSize)
	}
	size = max(textSizeMin, min(textSizeMax, size))
	_, scale := textImageSourceAndScale(size)
	height := bitmapTextRowHeight(scale)
	entries := emojiCatalogSearch(r.URL.Query().Get("q"), r.URL.Query().Get("group"), offset, limit)
	items := make([]nativeEditorEmojiItem, 0, len(entries))
	for index, entry := range entries {
		rows := renderEmojiGlyph(entry.assetKey, height)
		lines := make([]exportLine, 0, len(rows))
		width := max(1, maxLineDisplayWidth(rows))
		for row, text := range rows {
			lines = append(lines, exportLine{Row: row, Col: 0, Element: index, Role: "emoji", Parts: exportANSITextParts(text, 0, "#f3efe0", width)})
		}
		preview := exportTrueTypeElement(Element{Kind: "text", Text: entry.Emoji, Query: "render=truetype&ttf-size=100"}, 96, 96)
		items = append(items, nativeEditorEmojiItem{Emoji: entry.Emoji, Name: entry.Name, Group: entry.Group, Lines: lines, TrueType: preview})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_ = json.NewEncoder(w).Encode(nativeEditorEmojiCatalog{Groups: emojiCatalogGroups(), Items: items})
}

func (s *nativeEditorSession) handleActivityQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	value := strings.TrimSpace(r.URL.Query().Get("value"))
	if value == "" || len(value) > 512 {
		http.Error(w, "invalid QR value", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"text": activityQRCodeText(value)})
}

func editorShapeName(name string) string {
	switch name {
	case "circle", "square", "triangle", "diamond":
		return name
	default:
		return "circle"
	}
}

var activeNativeEditor *nativeEditorSession

func (s *nativeEditorSession) handleFitText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action nativeEditorAction
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&action); err != nil || action.ElementData == nil || action.BoxWidth < 1 || action.BoxHeight < 1 {
		http.Error(w, "invalid text fit preview", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	deck := cloneDeckForRender(s.deck)
	current := s.current
	masterMode, currentMaster := s.masterMode, s.currentMaster
	s.mu.RUnlock()
	if r.Context().Err() != nil {
		return
	}
	cols, rows := action.Cols, action.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
	}
	target := action.ElementData
	if !isEditableElement(*target) {
		http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
		return
	}
	best := *target
	bestFound := false
	for size := textSizeMin; size <= textSizeMax; size++ {
		candidate := *target
		applyTextSize(&candidate, size)
		measure := candidate
		measure.Query = removeImageQueryKeys(measure.Query, "top", "bottom", "left", "right", "left_pct", "right_pct", "row_delta", "align", "valign", "width", "height")
		measure.Query = setImageQueryInt(measure.Query, "top", 0)
		measure.Query = setImageQueryInt(measure.Query, "left", 0)
		pages := exportSlidePages(Slide{Elements: []Element{measure}}, 0, 1, cols, rows)
		width, height, ok := renderedExportElementSize(pages, 0)
		if ok && width <= action.BoxWidth && height <= action.BoxHeight {
			best, bestFound = candidate, true
		}
	}
	if !bestFound {
		applyTextSize(&best, textSizeMin)
	}
	var preview Slide
	var slideIndex, slideCount int
	if masterMode {
		if currentMaster < 0 || currentMaster > len(deck.Masters.Layouts) {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		master := masterSlideAt(&deck, currentMaster)
		if action.Element < 0 || action.Element >= len(master.Elements) {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		master.Elements[action.Element] = best
		preview = masterViewPreview(deck.Masters, currentMaster)
		slideIndex, slideCount = currentMaster, len(deck.Masters.Layouts)+1
	} else {
		if current < 0 || current >= len(deck.Slides) || action.Element < 0 || action.Element >= len(deck.Slides[current].Elements) {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		deck.Slides[current].Elements[action.Element] = best
		preview = deck.slideRenderPreview(current, cols, rows)
		slideIndex, slideCount = current, len(deck.Slides)
	}
	w.Header().Set("Content-Type", "application/json")
	if masterMode {
		preview = deck.masterRenderPreview(currentMaster, cols, rows)
	}
	_ = json.NewEncoder(w).Encode(nativeEditorTextFit{Element: best, Pages: exportSlidePages(preview, slideIndex, slideCount, cols, rows)})
}

func renderedExportElementSize(pages []exportPage, element int) (int, int, bool) {
	maxWidth, maxHeight, found := 0, 0, false
	for _, page := range pages {
		minX, minY, maxX, maxY := 0, 0, 0, 0
		pageFound := false
		for _, line := range page.Lines {
			if line.Element != element {
				continue
			}
			for _, part := range line.Parts {
				width := displayWidth(stripANSI(part.Text))
				if !pageFound {
					minX, minY, maxX, maxY = part.Col, line.Row, part.Col+width, line.Row+1
					pageFound = true
				} else {
					minX, minY = min(minX, part.Col), min(minY, line.Row)
					maxX, maxY = max(maxX, part.Col+width), max(maxY, line.Row+1)
				}
			}
		}
		if pageFound {
			found = true
			maxWidth, maxHeight = max(maxWidth, maxX-minX), max(maxHeight, maxY-minY)
		}
	}
	return maxWidth, maxHeight, found
}

func normalizedTextLeftQuery(query string, left, width int) string {
	values, _ := url.ParseQuery(query)
	for _, key := range []string{"align", "left", "right", "left_pct", "right_pct"} {
		values.Del(key)
	}
	values.Set("left_pct", fmt.Sprintf("%.6f", clampFloat(float64(max(0, left))/float64(max(1, width-1)), 0, 1)))
	return values.Encode()
}

func normalizedTextKindPlacement(preview Slide, target Element, fallbackIndex, width, height, page int) Element {
	if isTrueType(target) {
		return target
	}
	index := -1
	if target.ID != "" {
		for candidate := range preview.Elements {
			if preview.Elements[candidate].ID == target.ID {
				index = candidate
				break
			}
		}
	}
	if index < 0 && fallbackIndex >= 0 && fallbackIndex < len(preview.Elements) {
		index = fallbackIndex
	}
	if index < 0 {
		return target
	}
	targetValues, _ := url.ParseQuery(target.Query)
	placement := parseImagePlacement(target.Query)
	intrinsicRows := renderElementRows(target, width)
	intrinsicWidth := max(1, maxLineDisplayWidth(intrinsicRows))
	if target.Kind == "heading" && !rendersAsTextImage(target) {
		scale := 1
		if target.Level == 1 {
			scale = 2
		}
		intrinsicWidth = max(intrinsicWidth, min(width, styledSpansWidth(parseMarkdownStyledSpans(target.Text), 8*scale, 10*scale)))
	}
	if placement.hasHorizontalOffset() {
		desiredLeft := placementLeftCol(placement, width, intrinsicWidth)
		fittedLeft := clampBlockCol(desiredLeft, width, intrinsicWidth)
		if fittedLeft != desiredLeft {
			query := normalizedTextLeftQuery(targetValues.Encode(), fittedLeft, width)
			targetValues, _ = url.ParseQuery(query)
			preview.Elements[index].Query = normalizedTextLeftQuery(preview.Elements[index].Query, fittedLeft, width)
		}
	}
	top, bottom, left, right, ok := elementFullBox(preview, index, width, height)
	if !ok {
		return target
	}
	pageTop := max(0, page) * max(1, height)
	pageBottom := pageTop + max(1, height) - 1
	if top < pageTop || bottom > pageBottom {
		blockHeight := max(1, bottom-top+1)
		top = max(pageTop, min(top, pageBottom-blockHeight+1))
		for _, key := range []string{"top", "bottom", "row_delta", "valign"} {
			targetValues.Del(key)
		}
		targetValues.Set("top", strconv.Itoa(top))
	}
	if left < 0 || right >= width {
		blockWidth := max(1, right-left+1)
		left = clampBlockCol(left, width, blockWidth)
		query := normalizedTextLeftQuery(targetValues.Encode(), left, width)
		targetValues, _ = url.ParseQuery(query)
	}
	target.Query = targetValues.Encode()
	return target
}

func (s *nativeEditorSession) handleNormalizeTextKind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action nativeEditorAction
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&action); err != nil || action.ElementData == nil || !isEditableElement(*action.ElementData) {
		http.Error(w, "invalid text kind normalization", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	deck := cloneDeckForRender(s.deck)
	current := s.current
	masterMode, currentMaster := s.masterMode, s.currentMaster
	s.mu.RUnlock()
	cols, rows := action.Cols, action.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
	}
	normalized := *action.ElementData
	if normalized.ID == "" {
		normalized.ID = newStableID("slide-element")
	}
	var preview Slide
	var slideIndex, slideCount int
	if masterMode {
		if currentMaster < 0 || currentMaster > len(deck.Masters.Layouts) {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		target := masterSlideAt(&deck, currentMaster)
		if action.Element < 0 || action.Element >= len(target.Elements) {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		normalized = preserveRunStyles(target.Elements[action.Element], normalized)
		target.Elements[action.Element] = normalized
		preview = masterViewPreview(deck.Masters, currentMaster)
		normalized = normalizedTextKindPlacement(preview, normalized, action.Element, cols, rows, action.Page)
		target.Elements[action.Element] = normalized
		preview = masterViewPreview(deck.Masters, currentMaster)
		slideIndex, slideCount = currentMaster, len(deck.Masters.Layouts)+1
	} else {
		if current < 0 || current >= len(deck.Slides) || action.Element < 0 || action.Element >= len(deck.Slides[current].Elements) {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		normalized = preserveRunStyles(deck.Slides[current].Elements[action.Element], normalized)
		deck.Slides[current].Elements[action.Element] = normalized
		preview = deck.slideRenderPreview(current, cols, rows)
		normalized = normalizedTextKindPlacement(preview, normalized, action.Element, cols, rows, action.Page)
		deck.Slides[current].Elements[action.Element] = normalized
		preview = deck.slideRenderPreview(current, cols, rows)
		slideIndex, slideCount = current, len(deck.Slides)
	}
	w.Header().Set("Content-Type", "application/json")
	if masterMode {
		preview = deck.masterRenderPreview(currentMaster, cols, rows)
	}
	_ = json.NewEncoder(w).Encode(nativeEditorTextFit{Element: normalized, Pages: exportSlidePages(preview, slideIndex, slideCount, cols, rows)})
}

func convertedTextKindElement(source Element, kind string, level int) Element {
	converted := source
	values, _ := url.ParseQuery(source.Query)
	wasHeading := source.Kind == "heading"
	colour := values.Get("fg")
	if wasHeading {
		colour = values.Get("header")
		if colour == "" {
			colour = values.Get("fg")
		}
	} else if colour == "" {
		colour = values.Get("header")
	}
	for _, key := range []string{"render", "source", "scale", "text-size", "ttf-size", "ttf-weight", "ttf-width"} {
		if isTrueType(source) && (key == "render" || key == "ttf-size" || key == "ttf-weight" || key == "ttf-width") {
			continue
		}
		values.Del(key)
	}
	if kind == "heading" {
		values.Del("fg")
		if isTrueType(source) {
			size := 386
			if level == 2 {
				size = 193
			}
			values.Set("ttf-size", strconv.Itoa(size))
		}
		if colour != "" {
			values.Set("header", colour)
		}
	} else {
		values.Del("header")
		if isTrueType(source) && kind == "text" {
			values.Set("ttf-size", "97")
		}
		if colour != "" {
			values.Set("fg", colour)
		}
	}
	converted.Kind = kind
	converted.Level = 0
	if kind == "heading" {
		converted.Level = level
	}
	converted.Query = values.Encode()
	return converted
}

func convertNativeEditorTextKind(slide *Slide, index int, kind string, level, width, height int) (int, error) {
	if slide == nil || index < 0 || index >= len(slide.Elements) || !isEditableElement(slide.Elements[index]) {
		return -1, errInvalidEditorAction
	}
	if kind != "heading" && kind != "text" && kind != "bullet" && kind != "code" {
		return -1, errInvalidEditorAction
	}
	if kind == "heading" && level != 1 && level != 2 {
		return -1, errInvalidEditorAction
	}
	width, height = max(1, width), max(1, height)
	source := slide.Elements[index]
	richRuns := exportedRichRuns(source)
	blockSource := source.Kind == "bullet" || source.Kind == "code"
	split := blockSource && (kind == "heading" || kind == "text" || kind == source.Kind)
	if !split {
		converted := convertedTextKindElement(source, kind, level)
		if converted.Kind == "bullet" {
			converted.Text = normalizeBulletText(converted.Text)
		}
		if richRuns != nil {
			converted = withConvertedRuns(converted, richRuns)
		}
		slide.Elements[index] = converted
		return index, nil
	}

	targetKind, targetLevel := kind, level
	if kind == source.Kind {
		targetKind, targetLevel = "text", 0
	}
	top, _, left, _, found := elementFullBox(*slide, index, width, height)
	if !found {
		top, left = 0, 0
	}
	rawLines := strings.Split(strings.ReplaceAll(source.Text, "\r\n", "\n"), "\n")
	richLines := splitStyledLines(richRuns, source.Kind == "bullet")
	converted := make([]Element, 0, len(rawLines))
	cursor := 0
	for lineIndex, rawLine := range rawLines {
		rowOffset, colOffset := renderedOffsetForCursor(source, width, cursor)
		rowOffset -= max(0, editGlyphHeight(source)-1)
		line := rawLine
		if source.Kind == "bullet" {
			line = strings.TrimSpace(line)
		} else {
			leading := len([]rune(line)) - len([]rune(strings.TrimLeft(line, " \t")))
			colOffset += leading * editGlyphWidth(source)
			line = strings.TrimLeft(line, " \t")
		}
		if richRuns != nil && lineIndex < len(richLines) && len(richLines[lineIndex]) == 0 {
			line = ""
		}
		if line != "" {
			element := convertedTextKindElement(source, targetKind, targetLevel)
			element.Text = line
			if richRuns != nil && lineIndex < len(richLines) {
				element = withConvertedRuns(element, richLines[lineIndex])
			}
			if len(converted) > 0 {
				element.ID = newStableID(targetKind)
				element.SlotID, element.MasterSlotID, element.PlaceholderRole = "", "", ""
				element.Placeholder = false
			}
			query := removeImageQueryKeys(element.Query, "align", "left", "right", "left_pct", "right_pct", "top", "bottom", "row_delta", "valign")
			query = setImageQueryInt(query, "top", max(0, top+rowOffset))
			query = setPlacementHorizontalPct(query, max(0, left+colOffset), max(0, left+colOffset), width)
			element.Query = query
			converted = append(converted, element)
		}
		cursor += len([]rune(rawLine)) + 1
	}
	if len(converted) == 0 {
		element := convertedTextKindElement(source, targetKind, targetLevel)
		element.Text = ""
		if richRuns != nil {
			element = withConvertedRuns(element, nil)
		}
		converted = append(converted, element)
	}
	elements := make([]Element, 0, len(slide.Elements)-1+len(converted))
	elements = append(elements, slide.Elements[:index]...)
	elements = append(elements, converted...)
	elements = append(elements, slide.Elements[index+1:]...)
	slide.Elements = elements
	return index, nil
}

func convertNativeEditorTextSelection(slide *Slide, selection map[int]bool, kind string, level, width, height int) (int, map[int]bool, error) {
	if slide == nil {
		return -1, nil, errInvalidEditorAction
	}
	indices := make([]int, 0, len(selection))
	for index := range selection {
		if index >= 0 && index < len(slide.Elements) && isEditableElement(slide.Elements[index]) {
			indices = append(indices, index)
		}
	}
	if len(indices) == 0 {
		return -1, nil, errInvalidEditorAction
	}
	for index := range slide.Elements {
		if slide.Elements[index].ID == "" {
			slide.Elements[index].ID = newStableID("slide-element")
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indices)))
	selectedIDs := map[string]bool{}
	for index := range selection {
		if index >= 0 && index < len(slide.Elements) {
			selectedIDs[slide.Elements[index].ID] = true
		}
	}
	for _, index := range indices {
		before := len(slide.Elements)
		blockSource := slide.Elements[index].Kind == "bullet" || slide.Elements[index].Kind == "code"
		if _, err := convertNativeEditorTextKind(slide, index, kind, level, width, height); err != nil {
			return -1, nil, err
		}
		count := 1 + len(slide.Elements) - before
		if count == 1 && !blockSource {
			slide.Elements[index] = normalizedTextKindPlacement(*slide, slide.Elements[index], index, max(1, width), max(1, height), 0)
		}
		for offset := 0; offset < count; offset++ {
			selectedIDs[slide.Elements[index+offset].ID] = true
		}
	}
	remapped := map[int]bool{}
	selected := -1
	for index, element := range slide.Elements {
		if selectedIDs[element.ID] {
			remapped[index] = true
			if selected < 0 {
				selected = index
			}
		}
	}
	if selected < 0 {
		return -1, nil, errInvalidEditorAction
	}
	return selected, remapped, nil
}

func (s *nativeEditorSession) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action nativeEditorAction
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&action); err != nil {
		http.Error(w, "invalid editor preview", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	deck := cloneDeckForRender(s.deck)
	current := s.current
	masterMode, currentMaster := s.masterMode, s.currentMaster
	s.mu.RUnlock()
	if r.Context().Err() != nil {
		return
	}
	if masterMode {
		if action.ElementData == nil || currentMaster < 0 || currentMaster > len(deck.Masters.Layouts) {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		target := masterSlideAt(&deck, currentMaster)
		elementIndex := nativeEditorElementIndex(target.Elements, action.Element, action.ElementData.ID)
		if elementIndex < 0 {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		target.Elements[elementIndex] = standardTextElement(preserveRunStyles(target.Elements[elementIndex], *action.ElementData))
		action.Element = elementIndex
		frozenImage := action.Name == "frozen-image" && target.Elements[elementIndex].Kind == "image"
		if frozenImage {
			target.Elements[elementIndex].Query = setQueryValue(target.Elements[elementIndex].Query, "keynope_freeze", "1")
		}
		cols, rows := action.Cols, action.Rows
		if cols <= 0 || rows <= 0 {
			cols, rows = authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
		}
		preview := deck.masterRenderPreview(currentMaster, cols, rows)
		// Mutation previews are still editor surfaces. Keep the same authoring
		// annotations as the committed workspace so an empty shape does not lose
		// its editable label draft (and force a retained-owner remount) while it
		// is being resized or moved.
		if preview.ModernScene != nil {
			annotateSceneEditing(preview.ModernScene, deck, target.Elements)
		}
		if r.Context().Err() != nil {
			return
		}
		var pages []exportPage
		if frozenImage {
			pages = exportSlidePagesFrozen(preview, currentMaster, len(deck.Masters.Layouts)+1, cols, rows)
		} else {
			pages = exportSlidePages(preview, currentMaster, len(deck.Masters.Layouts)+1, cols, rows)
		}
		w.Header().Set("Content-Type", "application/json")
		writeNativeEditorPreview(w, action, preview, pages, cols, rows)
		return
	}
	if action.ElementData == nil || current < 0 || current >= len(deck.Slides) {
		http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
		return
	}
	elementIndex := nativeEditorElementIndex(deck.Slides[current].Elements, action.Element, action.ElementData.ID)
	if elementIndex < 0 {
		http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
		return
	}
	deck.Slides[current].Elements[elementIndex] = standardTextElement(preserveRunStyles(deck.Slides[current].Elements[elementIndex], *action.ElementData))
	action.Element = elementIndex
	frozenImage := action.Name == "frozen-image" && deck.Slides[current].Elements[elementIndex].Kind == "image"
	if frozenImage {
		deck.Slides[current].Elements[elementIndex].Query = setQueryValue(deck.Slides[current].Elements[elementIndex].Query, "keynope_freeze", "1")
	}
	cols, rows := action.Cols, action.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
	}
	preview := deck.slideRenderPreview(current, cols, rows)
	if preview.ModernScene != nil {
		annotateSceneEditing(preview.ModernScene, deck, deck.Slides[current].Elements)
	}
	if r.Context().Err() != nil {
		return
	}
	var pages []exportPage
	if frozenImage {
		pages = exportSlidePagesFrozen(preview, current, len(deck.Slides), cols, rows)
	} else {
		pages = exportSlidePages(preview, current, len(deck.Slides), cols, rows)
	}
	w.Header().Set("Content-Type", "application/json")
	writeNativeEditorPreview(w, action, preview, pages, cols, rows)
}

func writeNativeEditorPreview(w http.ResponseWriter, action nativeEditorAction, preview Slide, pages []exportPage, cols, rows int) {
	if action.Name != "inline-edit" || action.ElementData == nil {
		_ = json.NewEncoder(w).Encode(pages)
		return
	}
	elementIndex := action.Element
	for index := range preview.Elements {
		candidate := preview.Elements[index]
		if action.ElementData.ID != "" && candidate.ID == action.ElementData.ID || action.ElementData.ID == "" && nativeEditorElementSignature(candidate) == nativeEditorElementSignature(*action.ElementData) {
			elementIndex = index
			break
		}
	}
	visualPreview := preview
	caret := editorCaretForElement(visualPreview, elementIndex, action.Cursor, cols, rows, action.Page)
	response := nativeEditorInlinePreview{Pages: pages, Caret: caret}
	if action.ElementData.Kind == "shape" {
		fitted := *action.ElementData
		fitShapeLabel(&fitted, cols, rows)
		response.ElementData = &fitted
	}
	if action.SelectionStart != action.SelectionEnd {
		start := editorCaretForElement(visualPreview, elementIndex, min(action.SelectionStart, action.SelectionEnd), cols, rows, action.Page)
		end := editorCaretForElement(visualPreview, elementIndex, max(action.SelectionStart, action.SelectionEnd), cols, rows, action.Page)
		response.SelectionStart = &start
		response.SelectionEnd = &end
		response.SelectionRows = editorSelectionRows(visualPreview, elementIndex, start, end, cols, rows, action.Page)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func editorSelectionRows(slide Slide, elementIndex int, start, end nativeEditorCaret, cols, rows, page int) []nativeEditorSelectionRow {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) || end.Row < start.Row {
		return nil
	}
	bounds := map[int][2]int{}
	for _, line := range displayLines(slide, cols, rows, page) {
		if line.Element != elementIndex || line.Role == "outline" {
			continue
		}
		lineEnd := line.Col + displayWidth(stripANSI(line.Text))
		if current, ok := bounds[line.Row]; ok {
			bounds[line.Row] = [2]int{min(current[0], line.Col), max(current[1], lineEnd)}
		} else {
			bounds[line.Row] = [2]int{line.Col, lineEnd}
		}
	}
	glyphHeight := max(1, editGlyphHeight(slide.Elements[elementIndex]))
	startTop := max(0, start.Row-glyphHeight+1)
	endTop := max(0, end.Row-glyphHeight+1)
	selection := make([]nativeEditorSelectionRow, 0, end.Row-startTop+1)
	for row := startTop; row <= end.Row; row++ {
		rowBounds, ok := bounds[row]
		if !ok {
			continue
		}
		from, to := rowBounds[0], rowBounds[1]
		if start.Row == end.Row {
			from, to = start.Col, end.Col
		} else {
			if row <= start.Row {
				from = start.Col
			}
			if row >= endTop {
				to = end.Col
			}
		}
		from = max(0, min(from, cols))
		to = max(from, min(to, cols))
		if to > from {
			selection = append(selection, nativeEditorSelectionRow{Row: row, Col: from, Cells: to - from})
		}
	}
	return selection
}

func nativeEditorElementSignature(element Element) string {
	return strings.Join([]string{element.Kind, strconv.Itoa(element.Level), element.Text, element.Path, element.Query, element.ID, element.MasterSlotID}, "\x1f")
}

func editorCaretForElement(slide Slide, elementIndex, cursor, cols, rows, page int) nativeEditorCaret {
	if elementIndex < 0 || elementIndex >= len(slide.Elements) {
		return nativeEditorCaret{Cells: 1}
	}
	element := slide.Elements[elementIndex]
	renderWidth := constrainedElementWidth(element, cols)
	placement := parseImagePlacement(element.Query)
	if placement.left != nil || placement.leftPct != nil {
		anchor := placementLeftCol(placement, cols, 1)
		renderWidth = min(renderWidth, max(1, cols-anchor))
	}
	localRow, localCol := renderedOffsetForCursor(element, renderWidth, cursor)
	cells := editGlyphWidth(element)
	if element.Kind == "bullet" {
		localRow, localCol, cells = bulletCaretMetrics(element, renderWidth, cursor)
	}
	if emojiRunes, ok := emojiRuneLengthAtCursor(element.Text, cursor); ok {
		endRow, endCol := renderedOffsetForCursor(element, renderWidth, cursor+emojiRunes)
		if endRow == localRow && endCol > localCol {
			cells = endCol - localCol
		} else {
			cells = cells*2 + 2
		}
	}
	lines := displayLines(slide, cols, rows, page)
	elementLines := make([]Line, 0)
	for _, line := range lines {
		if line.Element == elementIndex && line.Role != "outline" {
			elementLines = append(elementLines, line)
		}
	}
	if len(elementLines) == 0 {
		return nativeEditorCaret{Cells: max(1, cells)}
	}
	localRow = max(0, min(localRow, len(elementLines)-1))
	line := elementLines[localRow]
	return nativeEditorCaret{
		Row:   max(0, min(rows-1, line.Row)),
		Col:   max(0, min(cols-1, line.Col+localCol)),
		Cells: max(1, cells),
	}
}

func (s *nativeEditorSession) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols <= 0 || rows <= 0 {
		cols, rows = authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
	}
	s.mu.RLock()
	deck := cloneDeckForRender(s.deck)
	masterMode, currentMaster := s.masterMode, s.currentMaster
	revision := s.version
	s.mu.RUnlock()
	if raw := r.URL.Query().Get("revision"); raw != "" {
		requested, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || requested != revision {
			http.Error(w, "workspace changed", http.StatusConflict)
			return
		}
	}
	if raw := r.URL.Query().Get("master"); raw != "" {
		requested, err := strconv.Atoi(raw)
		if err != nil {
			http.Error(w, "invalid master", http.StatusBadRequest)
			return
		}
		currentMaster = requested
	}
	if !masterMode || currentMaster < 0 || currentMaster > len(deck.Masters.Layouts) {
		http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
		return
	}
	pages, err := editorWorkspacePages(deck, true, currentMaster, cols, rows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(pages)
}

func newNativeEditorSession(deckPath string, deck Deck, options ...bool) *nativeEditorSession {
	isUntitled := len(options) > 0 && options[0]
	dirtyOverride := len(options) > 1 && options[1]
	deck = cloneDeck(deck)
	// The editor has one object model. Resolve legacy deck/style inheritance
	// once into concrete font and image properties before establishing the
	// saved baseline; merely opening an older deck therefore stays clean.
	canonicalizeConcreteAppearance(&deck)
	dirtyOverride = ensureUniqueEngagementIDs(&deck) || dirtyOverride
	syncSlideTabs(&deck)
	deck.EnsureDefaultMasters()
	standardizeDeckText(&deck)
	ensureNativeEditorElementIDs(&deck)
	return &nativeEditorSession{
		deck: deck, savedDeck: cloneDeck(deck), deckPath: deckPath, untitled: isUntitled, dirtyOverride: dirtyOverride,
		selected: -1, selection: map[int]bool{}, version: 1,
	}
}

func ensureNativeEditorElementIDs(deck *Deck) {
	if deck == nil {
		return
	}
	ensureSlide := func(slide *Slide) {
		for index := range slide.Elements {
			if slide.Elements[index].ID == "" {
				slide.Elements[index].ID = newStableID("slide-element")
			}
		}
	}
	for index := range deck.Slides {
		ensureSlide(&deck.Slides[index])
	}
	ensureSlide(&deck.Masters.Base.Slide)
	for index := range deck.Masters.Layouts {
		ensureSlide(&deck.Masters.Layouts[index].Slide)
	}
}

func nativeEditorElementIndex(elements []Element, requested int, id string) int {
	if id != "" {
		if requested >= 0 && requested < len(elements) && elements[requested].ID == id {
			return requested
		}
		for index := range elements {
			if elements[index].ID == id {
				return index
			}
		}
		return -1
	}
	if requested >= 0 && requested < len(elements) {
		return requested
	}
	return -1
}

func (s *nativeEditorSession) dirtyLocked() bool {
	return s.untitled || s.dirtyOverride || !reflect.DeepEqual(s.deck, s.savedDeck)
}

func (s *nativeEditorSession) state() nativeEditorState {
	return s.stateForClient(false, false)
}

// transportState keeps the authored deck available to the editor while only
// resolving the slide it can currently interact with. Resolving every slide on
// every pointer/key action made large decks pay an O(deck) cost for an O(1)
// edit. state retains the complete resolved view for internal callers and
// focused tests; browser/native-webview responses use this narrower contract.
func (s *nativeEditorSession) transportState() nativeEditorState {
	return s.stateForClient(true, false)
}

// actionTransportState sends a current-slide delta for commands that cannot
// change the authored slide collection. Structural/history commands retain a
// complete recovery response. Master editing always transports all masters.
func (s *nativeEditorSession) actionTransportState(action string) nativeEditorState {
	return s.stateForClient(true, !nativeEditorActionRequiresFullSlides(action))
}

func nativeEditorActionRequiresFullSlides(action string) bool {
	switch action {
	case "toggle-master-mode", "add-slide", "clone-slide", "reorder-slide", "delete-slide",
		"set-tabs", "delete-layout", "undo", "redo", "set-engagement-result":
		return true
	}
	return false
}

func (s *nativeEditorSession) stateForClient(currentOnly, currentSlideOnly bool) nativeEditorState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	selection := make([]int, 0, len(s.selection))
	for index := range s.selection {
		selection = append(selection, index)
	}
	sort.Ints(selection)
	var slides, resolved []Slide
	var currentSlide *Slide
	current := s.current
	if s.masterMode {
		// Master navigation and mutations can change the layout catalogue and
		// every inheriting slide. Keep its response self-contained.
		currentSlideOnly = false
		slides = []Slide{cloneSlide(s.deck.Masters.Base.Slide)}
		for _, layout := range s.deck.Masters.Layouts {
			slides = append(slides, cloneSlide(layout.Slide))
		}
		current = min(s.currentMaster, len(slides)-1)
		if !currentOnly {
			resolved = make([]Slide, len(slides))
			for index := range resolved {
				resolved[index] = masterViewPreview(s.deck.Masters, index)
			}
		}
	} else {
		if currentSlideOnly {
			if current >= 0 && current < len(s.deck.Slides) {
				slide := cloneSlide(s.deck.Slides[current])
				currentSlide = &slide
			}
		} else {
			slides = cloneSlides(s.deck.Slides)
		}
		if !currentOnly {
			resolved = make([]Slide, len(s.deck.Slides))
			for index := range resolved {
				// State exposes authored/resolved metadata only. ModernScene is
				// excluded from JSON; generating every slide here wastes layout,
				// image conversion and allocations on selection-only requests.
				resolved[index] = s.deck.ResolveSlide(index, false)
			}
		}
	}
	var resolvedCurrent *Slide
	if currentOnly && current >= 0 && ((!s.masterMode && current < len(s.deck.Slides)) || (s.masterMode && current < len(slides))) {
		var slide Slide
		if s.masterMode {
			slide = masterViewPreview(s.deck.Masters, current)
		} else {
			slide = s.deck.ResolveSlide(current, false)
		}
		resolvedCurrent = &slide
	}
	timerMode, timerEndMS := "", int64(0)
	if !s.timerDeadline.IsZero() {
		timerMode, timerEndMS = "running", s.timerDeadline.UnixMilli()
	}
	return nativeEditorState{
		Appearance:  s.deck.Appearance.Clone(),
		Diagnostics: append([]documentDiagnostic(nil), s.deck.Diagnostics...),
		Tabs:        append([]DeckTab(nil), s.deck.Tabs...), HasActivities: deckHasActivities(s.deck), HideActivityQR: s.deck.HideActivityQR,
		CanUndo: len(s.undo) > 0, CanRedo: len(s.redo) > 0,
		Version: s.version, Path: s.deckPath, Current: current, Selected: s.selected, MasterMode: s.masterMode,
		Selection: selection, EditingGroup: s.editingGroup, Slides: slides, CurrentSlide: currentSlide, Resolved: resolved, ResolvedCurrent: resolvedCurrent, Masters: s.deck.Masters,
		Dirty: s.dirtyLocked(), Untitled: s.untitled, TimerMode: timerMode, TimerEndMS: timerEndMS,
	}
}

type nativeEditorDocumentRequest struct {
	Path string `json:"path"`
}

type nativeEditorDocument struct {
	Content string `json:"content"`
	Version int64  `json:"version"`
}

type nativeEditorExportRequest struct {
	Path     string `json:"path"`
	Existing string `json:"existing,omitempty"`
}

// nativeEditorPPTXExport is deliberately base64 JSON. The editor transport is
// JSON in both the local HTTP shell and WASM; the browser decodes this payload
// directly into a Blob, so ZIP bytes are never coerced through a UTF-8 string.
type nativeEditorPPTXExport struct {
	Content string      `json:"content"`
	Report  interface{} `json:"report,omitempty"`
}

type nativeEditorPPTXImport struct {
	Content string      `json:"content"`
	Name    string      `json:"name"`
	Report  interface{} `json:"report,omitempty"`
}

func (s *nativeEditorSession) handleDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request nativeEditorDocumentRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil || strings.TrimSpace(request.Path) == "" {
		http.Error(w, "invalid save destination", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	deck, version := cloneDeck(s.deck), s.version
	s.mu.RUnlock()
	data, err := serializeDeck(request.Path, deck)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(nativeEditorDocument{Content: string(data), Version: version})
}

func (s *nativeEditorSession) handleExportDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request nativeEditorExportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20)).Decode(&request); err != nil || strings.TrimSpace(request.Path) == "" {
		http.Error(w, "invalid export destination", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	deck, sourcePath := cloneDeck(s.deck), s.deckPath
	s.mu.RUnlock()
	width, height := authoredRenderSize(80, 25)
	content, err := exportHTMLDocument(sourcePath, deck.ResolvedSlides(), width, height, preservedExportHeadFromHTML(request.Existing), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"content": content})
}

func (s *nativeEditorSession) handleExportPPTX(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	deck := cloneDeckForRender(s.deck)
	s.mu.RUnlock()
	content, report, err := exportPPTXDocument(deck)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(nativeEditorPPTXExport{Content: base64.StdEncoding.EncodeToString(content), Report: report})
}

// handleImportPPTX only prepares a candidate document. It intentionally does
// not alter this session: callers must complete their usual dirty-document
// guard before replacing the active deck with the returned Markdown.
func (s *nativeEditorSession) handleImportPPTX(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	content, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read PowerPoint presentation", http.StatusBadRequest)
		return
	}
	name := r.Header.Get("X-Keynope-Filename")
	if name == "" {
		name = "Imported.pptx"
	}
	deck, report, err := importPPTXDocument(content, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	markdown, err := serializeDeck(pptxSuggestedName(name), deck)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(markdown) == 0 || bytes.Equal(markdown, []byte("\n")) {
		http.Error(w, "PowerPoint import produced an empty presentation", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(nativeEditorPPTXImport{Content: string(markdown), Name: pptxSuggestedName(name), Report: report})
}

func (s *nativeEditorSession) confirmSaved(path string, version int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version != s.version {
		return false
	}
	s.deckPath = path
	s.savedDeck = cloneDeck(s.deck)
	s.untitled = false
	s.dirtyOverride = false
	s.version++
	return true
}

func (s *nativeEditorSession) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64); err == nil {
		s.mu.RLock()
		unchanged := version == s.version
		s.mu.RUnlock()
		if unchanged {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	_ = json.NewEncoder(w).Encode(s.transportState())
}

func (s *nativeEditorSession) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action nativeEditorAction
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&action); err != nil {
		http.Error(w, "invalid editor action", http.StatusBadRequest)
		return
	}
	if err := s.apply(action); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.actionTransportState(action.Action))
}

// These commands change session/view state only. Unknown commands deliberately
// retain the conservative history snapshot so new mutations remain undoable.
func nativeEditorViewAction(action string) bool {
	switch action {
	case "select-slide", "select-element", "select-elements", "navigate-presentation", "previous-slide", "next-slide", "enter-group", "exit-group", "start-timer", "stop-timer":
		return true
	}
	return false
}

func (s *nativeEditorSession) apply(action nativeEditorAction) error {
	if action.Action == "set-engagement-result" {
		return s.applyEngagementResult(action)
	}
	if action.Action == "confirm-save" {
		if strings.TrimSpace(action.Path) == "" || !s.confirmSaved(action.Path, int64(action.Value)) {
			return errInvalidEditorAction
		}
		return nil
	}
	if action.Action == "upsert-font" || action.Action == "delete-font" || action.Action == "delete-library-font" {
		return errInvalidEditorAction
	}
	if action.Action == "toggle-master-mode" || s.masterMode {
		return s.applyMaster(action)
	}
	s.mu.Lock()
	changed := false
	presenterPage := 0
	if s.companion != nil {
		presenterSlide, page := s.companion.Position()
		if presenterSlide == s.current {
			presenterPage = page
		}
	}
	before := Deck{}
	if !nativeEditorViewAction(action.Action) {
		before = cloneDeck(s.deck)
	}
	slideCount := len(s.deck.Slides)
	if s.current >= 0 && s.current < slideCount {
		if err := checkObjectLocks(s.deck.Slides[s.current], action, s.selection, s.editingGroup); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	switch action.Action {
	case "select-slide", "previous-slide", "next-slide", "navigate-presentation", "add-slide", "clone-slide", "delete-slide", "add-element":
		s.editingGroup = ""
	}
	switch action.Action {
	case "set-text-width", "set-text-size", "set-modern-text-style":
		if action.SceneMaster || action.SceneRevision == nil || *action.SceneRevision != s.version {
			s.mu.Unlock()
			return fmt.Errorf("the document changed; reselect text before formatting")
		}
		var err error
		if action.Action == "set-text-width" {
			changed, err = applyTextWidthCommand(&s.deck, action, s.editingGroup)
		} else if action.Action == "set-text-size" {
			changed, err = applyTextSizeCommand(&s.deck, action, s.editingGroup)
		} else {
			changed, err = applyModernTextStyle(&s.deck, action, s.editingGroup)
		}
		if err != nil {
			s.mu.Unlock()
			return err
		}
	case "set-scene-crop":
		if action.SceneMaster || action.SceneRevision == nil || *action.SceneRevision != s.version {
			s.mu.Unlock()
			return fmt.Errorf("the document changed; reopen crop before applying")
		}
		var err error
		changed, err = applySceneCrop(&s.deck, action)
		if err != nil {
			s.mu.Unlock()
			return err
		}
	case "set-object-lock":
		if s.current >= 0 && s.current < slideCount {
			changed = setObjectLocks(&s.deck.Slides[s.current], s.selection, s.selected, action.Value == 1)
		}
	case "set-scene-text":
		if action.SceneMaster || action.SceneRevision == nil || *action.SceneRevision != s.version {
			s.mu.Unlock()
			return fmt.Errorf("the document changed while this preview was open; reopen it before editing")
		}
		var err error
		changed, err = applySceneText(&s.deck, action)
		if err != nil {
			s.mu.Unlock()
			return err
		}
	case "set-theme":
		appearance, err := s.deck.withTheme(action.Name)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		changed = !reflect.DeepEqual(s.deck.Appearance, appearance)
		s.deck.Appearance = appearance
	case "move-object-layer":
		if s.current < 0 || s.current >= len(s.deck.Slides) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		var err error
		if action.Name != "" {
			changed, err = moveObjectStack(&s.deck.Slides[s.current], action.ObjectID, action.Kind, s.editingGroup, action.Name)
		} else {
			changed, err = moveObjectLayer(&s.deck.Slides[s.current], action.ObjectID, action.Value, s.editingGroup)
		}
		if err != nil {
			s.mu.Unlock()
			return err
		}
	case "group-elements", "ungroup-elements", "enter-group", "exit-group":
		if s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		var err error
		changed, err = s.applyGroupAction(&s.deck.Slides[s.current], action)
		if err != nil {
			s.mu.Unlock()
			return err
		}
	case "toggle-slide-tab":
		if s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if s.deck.Slides[s.current].TabID != "" {
			s.deck.Slides[s.current].TabID = ""
		} else {
			if len(s.deck.Tabs) >= 20 {
				s.mu.Unlock()
				return fmt.Errorf("use at most 20 participant tabs")
			}
			s.deck.Slides[s.current].TabID = newStableID("tab")
		}
		s.selected, s.selection, presenterPage, changed = -1, map[int]bool{}, 0, true
	case "reorder-slide-tab":
		if action.Slide < 0 || action.Slide >= slideCount || s.deck.Slides[action.Slide].TabID == "" {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		var slots []int
		from := -1
		for i, tab := range s.deck.Tabs {
			if tab.SlideTab {
				if tab.ID == s.deck.Slides[action.Slide].TabID {
					from = len(slots)
				}
				slots = append(slots, i)
			}
		}
		if from < 0 || action.Value < 0 || action.Value >= len(slots) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		moved := s.deck.Tabs[slots[from]]
		for i := from; i < action.Value; i++ {
			s.deck.Tabs[slots[i]] = s.deck.Tabs[slots[i+1]]
		}
		for i := from; i > action.Value; i-- {
			s.deck.Tabs[slots[i]] = s.deck.Tabs[slots[i-1]]
		}
		s.deck.Tabs[slots[action.Value]] = moved
		s.current, s.selected, s.selection, changed = action.Slide, -1, map[int]bool{}, from != action.Value
	case "set-activity-qr":
		if s.masterMode || !deckHasActivities(s.deck) || (action.Value != 0 && action.Value != 1) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		hide := action.Value == 0
		changed = s.deck.HideActivityQR != hide
		s.deck.HideActivityQR = hide
	case "set-tabs":
		if !deckHasActivities(s.deck) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if err := validateDeckTabs(action.Tabs); err != nil {
			s.mu.Unlock()
			return err
		}
		for _, tab := range action.Tabs {
			if tab.Page > len(s.deck.Slides) {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
		}
		// Removing a managed tab in Settings also returns its slide to Slides.
		for i := range s.deck.Slides {
			id := s.deck.Slides[i].TabID
			if id == "" {
				continue
			}
			keep := false
			for _, tab := range action.Tabs {
				if tab.ID == id && tab.SlideTab && tab.Page == i+1 && tab.URL == "" {
					keep = true
				}
			}
			if !keep {
				s.deck.Slides[i].TabID = ""
			}
		}
		for index := range action.Tabs {
			if action.Tabs[index].Extra != nil {
				continue
			}
			for _, previous := range s.deck.Tabs {
				if previous.ID == action.Tabs[index].ID {
					action.Tabs[index].Extra = cloneJSONExtensions(previous.Extra)
					break
				}
			}
		}
		s.deck.Tabs = append([]DeckTab(nil), action.Tabs...)
		changed = true
	case "select-slide":
		if action.Slide < 0 || action.Slide >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.current, s.selected = action.Slide, -1
		presenterPage = 0
		s.selection = map[int]bool{}
	case "select-element":
		if s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		elementIndex := action.Element
		if action.ElementData != nil && action.ElementData.ID != "" {
			elementIndex = nativeEditorElementIndex(s.deck.Slides[s.current].Elements, action.Element, action.ElementData.ID)
		}
		if elementIndex < -1 || elementIndex >= len(s.deck.Slides[s.current].Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.selectGroupElement(s.deck.Slides[s.current].Elements, elementIndex, action.Name == "toggle")
	case "select-elements":
		if s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if err := s.selectGroupElements(s.deck.Slides[s.current].Elements, action); err != nil {
			s.mu.Unlock()
			return err
		}
	case "navigate-presentation":
		if action.Slide < 0 || action.Slide >= slideCount || action.Page < 0 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.current, s.selected = action.Slide, -1
		s.selection = map[int]bool{}
		presenterPage = action.Page
	case "start-timer":
		if action.Value <= 0 || action.Value > 24*60*60 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.timerDeadline = time.Now().Add(time.Duration(action.Value) * time.Second)
	case "stop-timer":
		s.timerDeadline = time.Time{}
	case "previous-slide":
		s.current = nextPresentationSlide(s.deck.Slides, s.current, -1)
		s.selected = -1
		s.selection = map[int]bool{}
		presenterPage = 0
	case "next-slide":
		s.current = nextPresentationSlide(s.deck.Slides, s.current, 1)
		s.selected = -1
		s.selection = map[int]bool{}
		presenterPage = 0
	case "add-slide":
		cols, rows := authoredRenderSize(245, 56)
		preset, err := slidePreset(action.Name, cols, rows)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		insert := min(slideCount, s.current+1)
		remapTabPages(&s.deck, func(i int) int {
			if i >= insert {
				return i + 1
			}
			return i
		})
		s.deck.Slides = append(s.deck.Slides, Slide{})
		copy(s.deck.Slides[insert+1:], s.deck.Slides[insert:])
		s.deck.Slides[insert] = preset
		s.current, s.selected, changed = insert, -1, true
		s.selection = map[int]bool{}
	case "clone-slide":
		if slideCount == 0 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		insert := s.current + 1
		remapTabPages(&s.deck, func(i int) int {
			if i >= insert {
				return i + 1
			}
			return i
		})
		s.deck.Slides = append(s.deck.Slides, Slide{})
		copy(s.deck.Slides[insert+1:], s.deck.Slides[insert:])
		s.deck.Slides[insert] = cloneSlide(s.deck.Slides[s.current])
		if activity := s.deck.Slides[insert].Engagement; activity != nil {
			activity.ID, activity.Code = "", ""
			if err := ensureActivityID(activity); err != nil {
				s.deck = before
				s.mu.Unlock()
				return err
			}
			s.deck.Slides[insert].EngagementResult = nil
		}
		s.current, s.selected, changed = insert, -1, true
		s.selection = map[int]bool{}
	case "reorder-slide":
		if action.Slide < 0 || action.Slide >= slideCount || action.Value < 0 || action.Value >= slideCount || s.deck.Slides[action.Slide].TabID != "" || s.deck.Slides[action.Value].TabID != "" {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if action.Slide != action.Value {
			remapTabPages(&s.deck, func(i int) int {
				if i == action.Slide {
					return action.Value
				}
				if action.Slide < i && i <= action.Value {
					return i - 1
				}
				if action.Value <= i && i < action.Slide {
					return i + 1
				}
				return i
			})
			slide := s.deck.Slides[action.Slide]
			s.deck.Slides = append(s.deck.Slides[:action.Slide], s.deck.Slides[action.Slide+1:]...)
			s.deck.Slides = append(s.deck.Slides, Slide{})
			copy(s.deck.Slides[action.Value+1:], s.deck.Slides[action.Value:])
			s.deck.Slides[action.Value] = slide
			changed = true
		}
		s.current, s.selected = action.Value, -1
		s.selection = map[int]bool{}
		presenterPage = 0
	case "delete-slide":
		remapTabPages(&s.deck, func(i int) int {
			if i == s.current {
				return -1
			}
			if i > s.current {
				return i - 1
			}
			return i
		})
		if slideCount <= 1 {
			s.deck.Slides = []Slide{placeholderSlide()}
			s.current = 0
		} else {
			s.deck.Slides = append(s.deck.Slides[:s.current], s.deck.Slides[s.current+1:]...)
			s.current = min(s.current, len(s.deck.Slides)-1)
		}
		s.selected, changed = -1, true
		s.selection = map[int]bool{}
	case "set-engagement":
		if s.current < 0 || s.current >= slideCount || action.EngagementData == nil {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		definition, err := normalizeEngagement(*action.EngagementData)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		if previous := s.deck.Slides[s.current].Engagement; previous != nil && previous.ID == definition.ID && previous.Kind == definition.Kind {
			if definition.Extra == nil {
				definition.Extra = cloneJSONExtensions(previous.Extra)
			}
			for index := range definition.Questions {
				if definition.Questions[index].Extra == nil && index < len(previous.Questions) {
					definition.Questions[index].Extra = cloneJSONExtensions(previous.Questions[index].Extra)
				}
			}
			for index := range definition.Prerequisites {
				if definition.Prerequisites[index].Extra == nil && index < len(previous.Prerequisites) {
					definition.Prerequisites[index].Extra = cloneJSONExtensions(previous.Prerequisites[index].Extra)
				}
			}
		}
		for i, slide := range s.deck.Slides {
			if i != s.current && slide.Engagement != nil && slide.Engagement.ID == definition.ID {
				definition.ID = newStableID("activity")
				break
			}
		}
		s.deck.Slides[s.current].Engagement = &definition
		if previous := before.Slides[s.current].Engagement; previous == nil || previous.ID != definition.ID || previous.Kind != definition.Kind {
			s.deck.Slides[s.current].EngagementResult = nil
		}
		if definition.Kind == "onboarding" && before.Slides[s.current].Engagement != nil && before.Slides[s.current].Engagement.Code != definition.Code {
			for i := range s.deck.Slides {
				s.deck.Slides[i].EngagementResult = nil
			}
		}
		changed = true
	case "remove-engagement":
		if s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if s.deck.Slides[s.current].Engagement != nil {
			s.deck.Slides[s.current].Engagement = nil
			s.deck.Slides[s.current].EngagementResult = nil
			changed = true
		}
	case "add-element":
		if s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		element := Element{Kind: action.Kind, ID: newStableID(action.Kind)}
		switch action.Kind {
		case "heading":
			element.Level, element.Text = 1, "Title"
			if action.Level == 2 {
				element.Level, element.Text = 2, "Subtitle"
			}
		case "text":
			element.Text = "Text"
			if action.ElementData != nil && action.ElementData.Kind == "text" && action.ElementData.Text != "" {
				element = *action.ElementData
				element.ID = newStableID("text")
			}
		case "bullet":
			element.Text = "List item"
		case "code":
			element.Text = "code"
		case "shape":
			element = newShapeElement(editorShapeName(action.Name))
		case "connector":
			if action.ElementData == nil || !validShapeConnector(*action.ElementData, s.deck.Slides[s.current]) {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
			element = *action.ElementData
			element.ID = newStableID("connector")
		case "image":
			if action.ElementData != nil && action.ElementData.Kind == "image" && action.ElementData.Path != "" {
				element = *action.ElementData
				element.ID = newStableID("image")
			} else {
				element.Path = "image.png"
			}
		default:
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		element = standardTextElement(element)
		if s.deck.usesConcreteAppearance() {
			q, _ := url.ParseQuery(element.Query)
			switch element.Kind {
			case "heading", "text", "bullet", "code", "text-image", "page-number":
				if !q.Has("modern-font") {
					q.Set("modern-font", "c64")
				}
			case "image":
				if !q.Has("image-style") {
					q.Set("image-style", "modern")
				}
			}
			element.Query = q.Encode()
		}
		s.selected = insertElementAfter(&s.deck.Slides[s.current], s.selected, element)
		if element.AssetID != "" && action.AssetData != nil {
			if s.deck.Assets == nil {
				s.deck.Assets = map[string]DeckAsset{}
			}
			s.deck.Assets[element.AssetID] = *action.AssetData
		}
		if element.Kind == "image" && action.ElementData != nil {
			initializeInsertedImagePlacement(&s.deck.Slides[s.current], s.selected)
		}
		changed = true
		s.selection = map[int]bool{s.selected: true}
	case "duplicate-element":
		if s.current < 0 || s.current >= slideCount || action.Element < 0 || action.Element >= len(s.deck.Slides[s.current].Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		duplicate := s.deck.Slides[s.current].Elements[action.Element]
		duplicate.Query = removeImageQueryKeys(duplicate.Query, "group")
		duplicate.ID = newStableID(duplicate.Kind)
		insert := action.Element + 1
		elements := s.deck.Slides[s.current].Elements
		elements = append(elements, Element{})
		copy(elements[insert+1:], elements[insert:])
		elements[insert] = duplicate
		s.deck.Slides[s.current].Elements = elements
		s.selected, s.selection, changed = insert, map[int]bool{insert: true}, true
	case "paste-elements":
		if s.current < 0 || s.current >= slideCount || len(action.ElementsData) == 0 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		freshPastedGroups(action.ElementsData)
		freshPastedConnectorIDs(action.ElementsData)
		for _, source := range action.ElementsData {
			element := source
			element.Kind = strings.TrimSpace(element.Kind)
			if element.Kind == "" {
				continue
			}
			s.deck.Slides[s.current].Elements = append(s.deck.Slides[s.current].Elements, element)
			s.selected = len(s.deck.Slides[s.current].Elements) - 1
			changed = true
		}
		if !changed {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.selection = map[int]bool{s.selected: true}
	case "update-element":
		if action.ElementData == nil || s.current < 0 || s.current >= slideCount || action.Element < 0 || action.Element >= len(s.deck.Slides[s.current].Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		updated := *action.ElementData
		elementIndex := nativeEditorElementIndex(s.deck.Slides[s.current].Elements, action.Element, updated.ID)
		if elementIndex < 0 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		original := s.deck.Slides[s.current].Elements[elementIndex]
		if updated.ID == "" {
			updated.ID = original.ID
		}
		if original.ID != "" && updated.ID != original.ID {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		updated.Kind = strings.TrimSpace(updated.Kind)
		if updated.Kind == "" {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.deck.Slides[s.current].Elements[elementIndex] = preserveRunStyles(original, updated)
		if elementIndex != action.Element && s.selection[action.Element] {
			delete(s.selection, action.Element)
			s.selection[elementIndex] = true
		}
		s.selected, changed = elementIndex, true
	case "update-elements":
		if s.current < 0 || s.current >= slideCount || len(action.ElementIndices) == 0 || len(action.ElementIndices) != len(action.ElementsData) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if err := resolveEditorBatch(s.deck.Slides[s.current].Elements, &action); err != nil {
			s.mu.Unlock()
			return err
		}
		for dataIndex, elementIndex := range action.ElementIndices {
			if elementIndex < 0 || elementIndex >= len(s.deck.Slides[s.current].Elements) {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
			updated := action.ElementsData[dataIndex]
			if updated.ID == "" {
				updated.ID = s.deck.Slides[s.current].Elements[elementIndex].ID
			}
			updated.Kind = strings.TrimSpace(updated.Kind)
			if updated.Kind == "" {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
			s.deck.Slides[s.current].Elements[elementIndex] = preserveRunStyles(s.deck.Slides[s.current].Elements[elementIndex], updated)
		}
		changed = true
	case "convert-text-kind":
		if s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		selected, err := convertNativeEditorTextKind(&s.deck.Slides[s.current], action.Element, action.Kind, action.Level, action.Cols, action.Rows)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.selected, s.selection, changed = selected, map[int]bool{selected: true}, true
	case "convert-selected-text-kind":
		if s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		selected, selection, err := convertNativeEditorTextSelection(&s.deck.Slides[s.current], s.selection, action.Kind, action.Level, action.Cols, action.Rows)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.selected, s.selection, changed = selected, selection, true
	case "delete-element":
		if s.current < 0 || s.current >= slideCount || action.Element < 0 || action.Element >= len(s.deck.Slides[s.current].Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		elements := s.deck.Slides[s.current].Elements
		s.deck.Slides[s.current].Elements = append(elements[:action.Element], elements[action.Element+1:]...)
		s.selected, changed = -1, true
		s.selection = map[int]bool{}
	case "delete-selection":
		if s.current < 0 || s.current >= slideCount || len(s.selection) == 0 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		indices := make([]int, 0, len(s.selection))
		for index := range s.selection {
			if index >= 0 && index < len(s.deck.Slides[s.current].Elements) {
				indices = append(indices, index)
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(indices)))
		for _, index := range indices {
			elements := s.deck.Slides[s.current].Elements
			s.deck.Slides[s.current].Elements = append(elements[:index], elements[index+1:]...)
		}
		s.selected, s.selection, changed = -1, map[int]bool{}, true
	case "move-element":
		if s.current < 0 || s.current >= slideCount || action.Element < 0 || action.Element >= len(s.deck.Slides[s.current].Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		var err error
		changed, err = moveObjectStack(&s.deck.Slides[s.current], s.deck.Slides[s.current].Elements[action.Element].ID, action.Kind, s.editingGroup)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.selected = action.Element
	case "update-slide":
		if action.SlideData == nil || s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		updated := *action.SlideData
		updated.Extra = cloneJSONExtensions(s.deck.Slides[s.current].Extra)
		updated.FG = nativeEditorColorCode(updated.FG, false)
		updated.BG = nativeEditorColorCode(updated.BG, true)
		updated.HeaderFG = nativeEditorColorCode(updated.HeaderFG, false)
		updated.Elements = s.deck.Slides[s.current].Elements
		updated.TabID = s.deck.Slides[s.current].TabID
		updated.Engagement = s.deck.Slides[s.current].Engagement
		updated.EngagementResult = s.deck.Slides[s.current].EngagementResult
		s.deck.Slides[s.current] = updated
		changed = true
	case "update-slide-notes":
		if action.Slide < 0 || action.Slide >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if s.deck.Slides[action.Slide].Notes != action.Notes {
			s.deck.Slides[action.Slide].Notes = action.Notes
			changed = true
		}
	case "set-layout":
		if s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.deck.EnsureDefaultMasters()
		if !s.deck.RebindSlideLayout(s.current, action.Kind) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.selected, changed = -1, true
	case "add-layout":
		s.deck.EnsureDefaultMasters()
		id := newStableID("layout")
		name := strings.TrimSpace(action.Name)
		if name == "" {
			name = "New Layout"
		}
		s.deck.Masters.Layouts = append(s.deck.Masters.Layouts, MasterLayout{ID: id, Name: name, Slide: Slide{}})
		changed = true
	case "update-layout":
		if action.SlideData == nil {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.deck.EnsureDefaultMasters()
		if action.Kind == "base" {
			updated := cloneSlide(*action.SlideData)
			if updated.Extra == nil {
				updated.Extra = cloneJSONExtensions(s.deck.Masters.Base.Slide.Extra)
			}
			s.deck.Masters.Base.Slide = updated
			if strings.TrimSpace(action.Name) != "" {
				s.deck.Masters.Base.Name = strings.TrimSpace(action.Name)
			}
		} else {
			index := s.deck.Masters.LayoutIndex(action.Kind)
			if index < 0 {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
			updated := cloneSlide(*action.SlideData)
			if updated.Extra == nil {
				updated.Extra = cloneJSONExtensions(s.deck.Masters.Layouts[index].Slide.Extra)
			}
			if action.Kind == activityLayoutID {
				for _, required := range s.deck.Masters.Layouts[index].Slide.Elements {
					if protectedActivityElement(required) {
						found := false
						for _, element := range updated.Elements {
							found = found || element.PlaceholderRole == required.PlaceholderRole
						}
						if !found {
							updated.Elements = append(updated.Elements, required)
						}
					}
				}
			}
			s.deck.Masters.Layouts[index].Slide = updated
			if action.Kind != activityLayoutID && strings.TrimSpace(action.Name) != "" {
				s.deck.Masters.Layouts[index].Name = strings.TrimSpace(action.Name)
			}
		}
		s.deck.Masters.Normalize()
		changed = true
	case "delete-layout":
		if action.Kind == activityLayoutID {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		index := s.deck.Masters.LayoutIndex(action.Kind)
		if index < 0 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.deck.Masters.Layouts = append(s.deck.Masters.Layouts[:index], s.deck.Masters.Layouts[index+1:]...)
		for slideIndex := range s.deck.Slides {
			if s.deck.Slides[slideIndex].LayoutID == action.Kind {
				s.deck.Slides[slideIndex].LayoutID = ""
			}
		}
		changed = true
	case "undo":
		if len(s.undo) > 0 {
			previous := s.historyElements()
			s.redo = append(s.redo, cloneDeck(s.deck))
			s.deck = s.undo[len(s.undo)-1]
			s.undo = s.undo[:len(s.undo)-1]
			s.current = min(s.current, len(s.deck.Slides)-1)
			s.retainHistorySelection(previous, s.historyElements())
			changed = true
		}
	case "redo":
		if len(s.redo) > 0 {
			previous := s.historyElements()
			s.undo = append(s.undo, cloneDeck(s.deck))
			s.deck = s.redo[len(s.redo)-1]
			s.redo = s.redo[:len(s.redo)-1]
			s.current = min(s.current, len(s.deck.Slides)-1)
			s.retainHistorySelection(previous, s.historyElements())
			changed = true
		}
	default:
		s.mu.Unlock()
		return errInvalidEditorAction
	}
	// Appearance edits do not edit content. History restores complete snapshots;
	// re-fitting or reordering them would make undo cease to be an exact restore.
	if changed && s.current >= 0 && s.current < len(before.Slides) && s.current < len(s.deck.Slides) {
		preserveInsertedStack(before.Slides[s.current], &s.deck.Slides[s.current], action)
	}
	normalizeMutation := changed && action.Action != "set-theme" && action.Action != "set-object-lock" && action.Action != "move-element" && action.Action != "move-object-layer" && action.Action != "set-scene-text" && action.Action != "set-scene-crop" && action.Action != "undo" && action.Action != "redo"
	normalizeMutation = normalizeMutation && action.Action != "set-modern-text-style" && action.Action != "set-text-size" && action.Action != "set-text-width"
	if normalizeMutation {
		syncSlideTabs(&s.deck)
		standardizeDeckText(&s.deck)
	}
	if normalizeMutation && s.current >= 0 && s.current < len(s.deck.Slides) {
		fitSlideShapeLabels(&s.deck.Slides[s.current], authoredTerminalWidth, authoredTerminalHeight)
		pruneSingletonGroups(&s.deck.Slides[s.current])
		s.expandSelectedGroups(s.deck.Slides[s.current].Elements)
		remapNativeEditorSelection(canonicalizeSlideElementOrder(&s.deck.Slides[s.current], authoredTerminalWidth, authoredTerminalHeight), &s.selected, &s.selection)
	}
	if changed && action.Action != "undo" && action.Action != "redo" {
		s.undo = append(s.undo, before)
		if len(s.undo) > 100 {
			s.undo = s.undo[len(s.undo)-100:]
		}
		s.redo = nil
	}
	if !changed {
		s.retainSceneForViewAction(action.Action)
	}
	s.version++
	current := s.current
	companion := s.companion
	deckPath := s.deckPath
	var deck Deck
	refreshScope, refreshSlide := nativeEditorRefreshPlan(action.Action, before, s.deck, current, action.Slide)
	if changed && companion != nil && refreshScope != "" {
		deck = cloneDeckForRender(s.deck)
	}
	s.mu.Unlock()
	if companion != nil {
		if changed {
			switch refreshScope {
			case "slide":
				companion.RefreshDeckSlideAsync(deckPath, deck, refreshSlide, authoredTerminalWidth, authoredTerminalHeight)
			case "deck":
				companion.RefreshAllAsync(deckPath, deck.ResolvedSlides(), authoredTerminalWidth, authoredTerminalHeight)
			}
		}
		presenting := companion.presentationEnabled()
		companion.Update(current, presenterPage, presenting, nil)
		if action.Action == "start-timer" {
			companion.StartTimer(time.Duration(action.Value) * time.Second)
		} else if action.Action == "stop-timer" {
			companion.StopTimer()
		}
	}
	return nil
}

func remapNativeEditorSelection(oldToNew []int, selected *int, selection *map[int]bool) {
	if len(oldToNew) == 0 {
		return
	}
	if selected != nil && *selected >= 0 && *selected < len(oldToNew) {
		*selected = oldToNew[*selected]
	}
	if selection == nil {
		return
	}
	remapped := map[int]bool{}
	for oldIndex := range *selection {
		if oldIndex >= 0 && oldIndex < len(oldToNew) {
			remapped[oldToNew[oldIndex]] = true
		}
	}
	*selection = remapped
}

func nativeEditorRefreshScope(action string) string {
	switch action {
	case "set-appearance-profile", "set-style-default", "set-element-style", "set-appearance-mode":
		// Retired appearance commands are rejected by the unified editor and
		// must never trigger a presenter rebuild as a side effect.
		return ""
	case "set-text-width", "set-text-size", "set-modern-text-style", "set-scene-text", "set-scene-crop", "set-object-lock", "move-object-layer", "group-elements", "ungroup-elements":
		return "slide"
	case "add-element", "toggle-page-number", "duplicate-element", "paste-elements", "update-element", "update-elements", "convert-text-kind", "convert-selected-text-kind", "delete-element", "delete-selection", "move-element", "update-slide", "set-layout", "set-engagement", "remove-engagement":
		return "slide"
	case "update-slide-notes":
		return ""
	default:
		return "deck"
	}
}

// Presenter refreshes follow the document change, not merely the command
// label. Undo/Redo can restore a structural edit and must then rebuild the
// deck, but the overwhelmingly common case restores one authored slide. Keep
// every unaffected presenter page alive in that case.
func nativeEditorRefreshPlan(action string, before, after Deck, current, requestedSlide int) (string, int) {
	target := current
	if action == "set-text-width" || action == "set-text-size" || action == "set-modern-text-style" || action == "set-scene-text" || action == "set-scene-crop" {
		target = requestedSlide
	}
	scope := nativeEditorRefreshScope(action)
	if action != "undo" && action != "redo" {
		return scope, target
	}
	if !nativeEditorSharedDeckEqual(before, after) || len(before.Slides) != len(after.Slides) {
		return "deck", target
	}
	changed := -1
	for index := range after.Slides {
		if reflect.DeepEqual(before.Slides[index], after.Slides[index]) {
			continue
		}
		if changed >= 0 {
			return "deck", target
		}
		changed = index
	}
	if changed >= 0 {
		return "slide", changed
	}
	return "", target
}

func nativeEditorSharedDeckEqual(a, b Deck) bool {
	a.Slides = nil
	b.Slides = nil
	return reflect.DeepEqual(a, b)
}

// Master-list housekeeping changes the editor chrome, not any resolved slide.
// Keep the presenter alive for those operations; mutations to authored master
// content and structural removals remain conservative full-deck refreshes.
func nativeEditorMasterRefreshScope(action string) string {
	switch action {
	case "add-slide", "add-layout", "clone-slide", "rename-master", "reorder-master", "update-slide-notes":
		return ""
	default:
		return "deck"
	}
}

func (s *nativeEditorSession) applyMaster(action nativeEditorAction) error {
	s.mu.Lock()
	s.deck.EnsureDefaultMasters()
	if action.Action == "toggle-master-mode" {
		s.editingGroup = ""
		s.masterMode = !s.masterMode
		s.selected = -1
		s.selection = map[int]bool{}
		if s.masterMode {
			s.currentMaster = min(s.currentMaster, len(s.deck.Masters.Layouts))
		}
		s.version++
		s.mu.Unlock()
		return nil
	}
	if !s.masterMode {
		s.mu.Unlock()
		return errInvalidEditorAction
	}
	before := Deck{}
	if !nativeEditorViewAction(action.Action) {
		before = cloneDeck(s.deck)
	}
	changed := false
	masterCount := len(s.deck.Masters.Layouts) + 1
	s.currentMaster = max(0, min(s.currentMaster, masterCount-1))
	target := masterSlideAt(&s.deck, s.currentMaster)
	if err := checkObjectLocks(*target, action, s.selection, s.editingGroup); err != nil {
		s.mu.Unlock()
		return err
	}
	activityMaster := s.currentMaster > 0 && s.deck.Masters.Layouts[s.currentMaster-1].ID == activityLayoutID
	if action.Action == "set-text-width" || action.Action == "set-text-size" || action.Action == "set-modern-text-style" || action.Action == "set-scene-text" || action.Action == "set-scene-crop" {
		if !action.SceneMaster || action.SceneRevision == nil || *action.SceneRevision != s.version || action.Slide != s.currentMaster {
			s.mu.Unlock()
			return fmt.Errorf("master changed; reopen the editor")
		}
		// Reuse validated mutations against an isolated authored master, not its
		// inherited preview and not the normal slide at the same numeric index.
		edit := s.deck
		edit.Slides = []Slide{cloneSlide(*target)}
		command := action
		command.Slide = 0
		var err error
		if action.Action == "set-text-width" {
			changed, err = applyTextWidthCommand(&edit, command, s.editingGroup)
		} else if action.Action == "set-text-size" {
			changed, err = applyTextSizeCommand(&edit, command, s.editingGroup)
		} else if action.Action == "set-modern-text-style" {
			changed, err = applyModernTextStyle(&edit, command, s.editingGroup)
		} else if action.Action == "set-scene-text" {
			changed, err = applySceneText(&edit, command)
		} else {
			changed, err = applySceneCrop(&edit, command)
		}
		if err != nil {
			s.mu.Unlock()
			return err
		}
		if changed {
			*target = edit.Slides[0]
			s.undo = append(s.undo, before)
			if len(s.undo) > 100 {
				s.undo = s.undo[len(s.undo)-100:]
			}
			s.redo = nil
			s.version++
		}
		updated, companion := cloneDeck(s.deck), s.companion
		s.mu.Unlock()
		if changed && companion != nil {
			companion.RefreshAllAsync(s.deckPath, updated.ResolvedSlides(), authoredTerminalWidth, authoredTerminalHeight)
		}
		return nil
	}
	protectedIndex := func(index int) bool {
		return activityMaster && index >= 0 && index < len(target.Elements) && protectedActivityElement(target.Elements[index])
	}
	if action.Action == "set-object-lock" {
		changed = setObjectLocks(target, s.selection, s.selected, action.Value == 1)
		if changed {
			s.undo = append(s.undo, before)
			if len(s.undo) > 100 {
				s.undo = s.undo[len(s.undo)-100:]
			}
			s.redo = nil
			s.version++
		}
		s.mu.Unlock()
		return nil
	}
	switch action.Action {
	case "select-slide", "previous-slide", "next-slide", "add-slide", "clone-slide", "delete-slide", "add-element":
		s.editingGroup = ""
	}
	switch action.Action {
	case "move-object-layer":
		var err error
		if action.Name != "" {
			changed, err = moveObjectStack(target, action.ObjectID, action.Kind, s.editingGroup, action.Name)
		} else {
			changed, err = moveObjectLayer(target, action.ObjectID, action.Value, s.editingGroup)
		}
		if err != nil {
			s.mu.Unlock()
			return err
		}
	case "group-elements", "ungroup-elements", "enter-group", "exit-group":
		var err error
		changed, err = s.applyGroupAction(target, action)
		if err != nil {
			s.mu.Unlock()
			return err
		}
	case "select-slide":
		if action.Slide < 0 || action.Slide >= masterCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.currentMaster, s.selected = action.Slide, -1
		s.selection = map[int]bool{}
	case "previous-slide", "next-slide":
		if action.Action == "previous-slide" {
			s.currentMaster = max(0, s.currentMaster-1)
		} else {
			s.currentMaster = min(masterCount-1, s.currentMaster+1)
		}
		s.selected = -1
		s.selection = map[int]bool{}
	case "add-slide", "add-layout":
		name := strings.TrimSpace(action.Name)
		if name == "" {
			name = "New Master"
		}
		id := newStableID("layout")
		s.deck.Masters.Layouts = append(s.deck.Masters.Layouts, MasterLayout{ID: id, Name: name, Slide: Slide{}})
		s.currentMaster, s.selected, changed = len(s.deck.Masters.Layouts), -1, true
		s.selection = map[int]bool{}
	case "clone-slide":
		var source MasterLayout
		if s.currentMaster == 0 {
			source = MasterLayout{ID: "base", Name: "Base Master", Slide: cloneSlide(s.deck.Masters.Base.Slide)}
		} else {
			source = s.deck.Masters.Layouts[s.currentMaster-1]
		}
		clone := cloneMasterLayoutFresh(source)
		insert := s.currentMaster
		s.deck.Masters.Layouts = append(s.deck.Masters.Layouts, MasterLayout{})
		copy(s.deck.Masters.Layouts[insert+1:], s.deck.Masters.Layouts[insert:])
		s.deck.Masters.Layouts[insert] = clone
		s.currentMaster, s.selected, changed = insert+1, -1, true
		s.selection = map[int]bool{}
	case "reorder-master":
		if action.Slide <= 0 || action.Slide >= masterCount || action.Value <= 0 || action.Value >= masterCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if action.Slide != action.Value {
			source, destination := action.Slide-1, action.Value-1
			layout := s.deck.Masters.Layouts[source]
			s.deck.Masters.Layouts = append(s.deck.Masters.Layouts[:source], s.deck.Masters.Layouts[source+1:]...)
			s.deck.Masters.Layouts = append(s.deck.Masters.Layouts, MasterLayout{})
			copy(s.deck.Masters.Layouts[destination+1:], s.deck.Masters.Layouts[destination:])
			s.deck.Masters.Layouts[destination] = layout
			s.currentMaster, changed = action.Value, true
		}
		s.selected = -1
		s.selection = map[int]bool{}
	case "delete-slide", "delete-layout":
		if s.currentMaster <= 0 || s.currentMaster-1 >= len(s.deck.Masters.Layouts) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		id := s.deck.Masters.Layouts[s.currentMaster-1].ID
		if id == activityLayoutID {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.deck.Masters.Layouts = append(s.deck.Masters.Layouts[:s.currentMaster-1], s.deck.Masters.Layouts[s.currentMaster:]...)
		for index := range s.deck.Slides {
			if s.deck.Slides[index].LayoutID == id {
				s.deck.Slides[index].LayoutID = ""
			}
		}
		s.currentMaster = min(s.currentMaster, len(s.deck.Masters.Layouts))
		s.selected, s.selection, changed = -1, map[int]bool{}, true
	case "toggle-page-number":
		removePageNumberElements(target)
		if s.currentMaster == 0 {
			if target.PageNumber == pageNumberShow {
				target.PageNumber = pageNumberHide
			} else {
				target.PageNumber = pageNumberShow
			}
		} else {
			target.PageNumber = nextPageNumberOverride(target.PageNumber)
		}
		if target.PageNumber == pageNumberShow {
			ensurePageNumberElement(target, "master")
		}
		s.selected = -1
		s.selection = map[int]bool{}
		changed = true
	case "add-element":
		element := Element{Kind: action.Kind, ID: newStableID(action.Kind)}
		switch action.Kind {
		case "heading":
			element.Level, element.Text = 1, "Title"
			if action.Level == 2 {
				element.Level, element.Text = 2, "Subtitle"
			}
		case "text":
			element.Text = "Text"
			if action.ElementData != nil && action.ElementData.Kind == "text" && action.ElementData.Text != "" {
				element = *action.ElementData
				element.ID = newStableID("text")
			}
		case "bullet":
			element.Text = "List item"
		case "code":
			element.Text = "code"
		case "shape":
			element = newShapeElement(editorShapeName(action.Name))
		case "connector":
			if action.ElementData == nil || !validShapeConnector(*action.ElementData, *target) {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
			element = *action.ElementData
			element.ID = newStableID("connector")
		case "image":
			if action.ElementData != nil && action.ElementData.Kind == "image" && action.ElementData.Path != "" {
				element = *action.ElementData
				element.ID = newStableID("image")
			} else {
				element.Path = "image.png"
			}
		default:
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		element = standardTextElement(element)
		if s.deck.usesConcreteAppearance() {
			q, _ := url.ParseQuery(element.Query)
			switch element.Kind {
			case "heading", "text", "bullet", "code", "text-image", "page-number":
				if !q.Has("modern-font") {
					q.Set("modern-font", "c64")
				}
			case "image":
				if !q.Has("image-style") {
					q.Set("image-style", "modern")
				}
			}
			element.Query = q.Encode()
		}
		s.selected = insertElementAfter(target, s.selected, element)
		if element.AssetID != "" && action.AssetData != nil {
			if s.deck.Assets == nil {
				s.deck.Assets = map[string]DeckAsset{}
			}
			s.deck.Assets[element.AssetID] = *action.AssetData
		}
		if element.Kind == "image" && action.ElementData != nil {
			initializeInsertedImagePlacement(target, s.selected)
		}
		changed = true
		s.selection = map[int]bool{s.selected: true}
	case "select-element":
		elementIndex := action.Element
		if action.ElementData != nil && action.ElementData.ID != "" {
			elementIndex = nativeEditorElementIndex(target.Elements, action.Element, action.ElementData.ID)
		}
		if elementIndex < -1 || elementIndex >= len(target.Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.selectGroupElement(target.Elements, elementIndex, action.Name == "toggle")
	case "select-elements":
		if err := s.selectGroupElements(target.Elements, action); err != nil {
			s.mu.Unlock()
			return err
		}
	case "update-element":
		if action.ElementData == nil || action.Element < 0 || action.Element >= len(target.Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		updated := *action.ElementData
		elementIndex := nativeEditorElementIndex(target.Elements, action.Element, updated.ID)
		if elementIndex < 0 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if updated.ID == "" {
			updated.ID = target.Elements[elementIndex].ID
		}
		if protectedIndex(elementIndex) {
			original := target.Elements[elementIndex]
			updated.Kind, updated.Level, updated.Text = original.Kind, original.Level, original.Text
			updated.ID, updated.SlotID, updated.PlaceholderRole = original.ID, original.SlotID, original.PlaceholderRole
		}
		target.Elements[elementIndex] = preserveRunStyles(target.Elements[elementIndex], updated)
		if elementIndex != action.Element && s.selection[action.Element] {
			delete(s.selection, action.Element)
			s.selection[elementIndex] = true
		}
		s.selected, changed = elementIndex, true
	case "update-elements":
		if len(action.ElementIndices) == 0 || len(action.ElementIndices) != len(action.ElementsData) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if err := resolveEditorBatch(target.Elements, &action); err != nil {
			s.mu.Unlock()
			return err
		}
		for dataIndex, elementIndex := range action.ElementIndices {
			if elementIndex < 0 || elementIndex >= len(target.Elements) {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
			updated := action.ElementsData[dataIndex]
			if protectedIndex(elementIndex) {
				original := target.Elements[elementIndex]
				updated.Kind, updated.Level, updated.Text = original.Kind, original.Level, original.Text
				updated.ID, updated.SlotID, updated.PlaceholderRole = original.ID, original.SlotID, original.PlaceholderRole
			}
			if updated.ID == "" {
				updated.ID = target.Elements[elementIndex].ID
			}
			if strings.TrimSpace(updated.Kind) == "" {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
			target.Elements[elementIndex] = preserveRunStyles(target.Elements[elementIndex], updated)
		}
		changed = true
	case "convert-text-kind":
		if protectedIndex(action.Element) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		selected, err := convertNativeEditorTextKind(target, action.Element, action.Kind, action.Level, action.Cols, action.Rows)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.selected, s.selection, changed = selected, map[int]bool{selected: true}, true
	case "convert-selected-text-kind":
		for index := range s.selection {
			if protectedIndex(index) {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
		}
		selected, selection, err := convertNativeEditorTextSelection(target, s.selection, action.Kind, action.Level, action.Cols, action.Rows)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.selected, s.selection, changed = selected, selection, true
	case "duplicate-element":
		if action.Element < 0 || action.Element >= len(target.Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if protectedIndex(action.Element) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		duplicate := target.Elements[action.Element]
		duplicate.Query = removeImageQueryKeys(duplicate.Query, "group")
		duplicate.ID = newStableID(duplicate.Kind)
		insert := action.Element + 1
		target.Elements = append(target.Elements, Element{})
		copy(target.Elements[insert+1:], target.Elements[insert:])
		target.Elements[insert] = duplicate
		s.selected, s.selection, changed = insert, map[int]bool{insert: true}, true
	case "paste-elements":
		if len(action.ElementsData) == 0 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		freshPastedGroups(action.ElementsData)
		freshPastedConnectorIDs(action.ElementsData)
		for _, source := range action.ElementsData {
			element := source
			element.Kind = strings.TrimSpace(element.Kind)
			if element.Kind == "" {
				continue
			}
			target.Elements = append(target.Elements, element)
			s.selected = len(target.Elements) - 1
			changed = true
		}
		if !changed {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.selection = map[int]bool{s.selected: true}
	case "delete-element", "delete-selection":
		indices := []int{action.Element}
		if action.Action == "delete-selection" {
			indices = indices[:0]
			for index := range s.selection {
				indices = append(indices, index)
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(indices)))
		for _, index := range indices {
			if index >= 0 && index < len(target.Elements) {
				if protectedIndex(index) {
					continue
				}
				target.Elements = append(target.Elements[:index], target.Elements[index+1:]...)
				changed = true
			}
		}
		s.selected, s.selection = -1, map[int]bool{}
	case "move-element":
		if action.Element < 0 || action.Element >= len(target.Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		var err error
		changed, err = moveObjectStack(target, target.Elements[action.Element].ID, action.Kind, s.editingGroup)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		s.selected = action.Element
	case "update-slide":
		if action.SlideData == nil {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		updated := cloneSlide(*action.SlideData)
		updated.Elements = target.Elements
		updated.FG = nativeEditorColorCode(updated.FG, false)
		updated.BG = nativeEditorColorCode(updated.BG, true)
		updated.HeaderFG = nativeEditorColorCode(updated.HeaderFG, false)
		*target, changed = updated, true
	case "rename-master":
		name := strings.TrimSpace(action.Name)
		if name == "" {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if s.currentMaster == 0 {
			s.deck.Masters.Base.Name = name
		} else {
			if activityMaster {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
			s.deck.Masters.Layouts[s.currentMaster-1].Name = name
		}
		changed = true
	case "update-slide-notes":
		index := max(0, min(action.Slide, masterCount-1))
		masterSlideAt(&s.deck, index).Notes = action.Notes
		changed = true
	case "undo":
		if len(s.undo) > 0 {
			previous := s.historyElements()
			s.redo = append(s.redo, cloneDeck(s.deck))
			s.deck = s.undo[len(s.undo)-1]
			s.undo = s.undo[:len(s.undo)-1]
			s.currentMaster = min(s.currentMaster, len(s.deck.Masters.Layouts))
			s.retainHistorySelection(previous, s.historyElements())
			changed = true
		}
	case "redo":
		if len(s.redo) > 0 {
			previous := s.historyElements()
			s.undo = append(s.undo, cloneDeck(s.deck))
			s.deck = s.redo[len(s.redo)-1]
			s.redo = s.redo[:len(s.redo)-1]
			s.currentMaster = min(s.currentMaster, len(s.deck.Masters.Layouts))
			s.retainHistorySelection(previous, s.historyElements())
			changed = true
		}
	default:
		s.mu.Unlock()
		return errInvalidEditorAction
	}
	if changed && s.currentMaster >= 0 && s.currentMaster <= len(before.Masters.Layouts) && s.currentMaster <= len(s.deck.Masters.Layouts) {
		preserveInsertedStack(*masterSlideAt(&before, s.currentMaster), masterSlideAt(&s.deck, s.currentMaster), action)
	}
	if changed && action.Action != "move-element" && action.Action != "move-object-layer" && action.Action != "undo" && action.Action != "redo" {
		standardizeDeckText(&s.deck)
		target = masterSlideAt(&s.deck, s.currentMaster)
		fitSlideShapeLabels(target, authoredTerminalWidth, authoredTerminalHeight)
		pruneSingletonGroups(target)
		s.expandSelectedGroups(target.Elements)
		remapNativeEditorSelection(canonicalizeSlideElementOrder(target, authoredTerminalWidth, authoredTerminalHeight), &s.selected, &s.selection)
	}
	if changed && action.Action != "undo" && action.Action != "redo" {
		s.undo = append(s.undo, before)
		if len(s.undo) > 100 {
			s.undo = s.undo[len(s.undo)-100:]
		}
		s.redo = nil
	}
	if changed && action.Action != "undo" && action.Action != "redo" {
		s.deck.Masters.Normalize()
	}
	if !changed {
		s.retainSceneForViewAction(action.Action)
	}
	s.version++
	companion := s.companion
	var deck Deck
	refreshScope := nativeEditorMasterRefreshScope(action.Action)
	if changed && companion != nil && refreshScope != "" {
		deck = cloneDeckForRender(s.deck)
	}
	s.mu.Unlock()
	if changed && companion != nil && refreshScope == "deck" {
		companion.RefreshAllAsync(s.deckPath, deck.ResolvedSlides(), authoredTerminalWidth, authoredTerminalHeight)
	}
	return nil
}

func nativeEditorColorCode(value string, background bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "#") || strings.HasPrefix(value, "rgb(") {
		code := cssColourToFG(value, value)
		if background && strings.HasPrefix(code, "38;") {
			code = "48;" + strings.TrimPrefix(code, "38;")
		}
		return code
	}
	return value
}

type editorActionError string

func (e editorActionError) Error() string { return string(e) }

const errInvalidEditorAction = editorActionError("invalid editor action")
