package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type nativeEditorSession struct {
	mu            sync.RWMutex
	deck          Deck
	deckPath      string
	current       int
	selected      int
	selection     map[int]bool
	version       int64
	undo          []Deck
	redo          []Deck
	companion     *presenterCompanion
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
	ext := strings.ToLower(filepath.Ext(filepath.Base(header.Filename)))
	if ext == "" || len(ext) > 10 {
		http.Error(w, "unsupported image filename", http.StatusBadRequest)
		return
	}
	name := fmt.Sprintf("keynope-%s%s", newStableID("image"), ext)
	destination := filepath.Join(filepath.Dir(s.deckPath), name)
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		http.Error(w, "could not create image", http.StatusInternalServerError)
		return
	}
	_, copyErr := io.Copy(output, file)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		http.Error(w, "could not store image", http.StatusInternalServerError)
		return
	}
	if err := s.apply(nativeEditorAction{Action: "add-element", Kind: "image"}); err != nil {
		_ = os.Remove(destination)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	state := s.state()
	if state.Selected < 0 {
		http.Error(w, "could not select image", http.StatusInternalServerError)
		return
	}
	element := state.Slides[state.Current].Elements[state.Selected]
	element.Path = destination
	if err := s.apply(nativeEditorAction{Action: "update-element", Element: state.Selected, ElementData: &element}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.state())
}

type nativeEditorState struct {
	Version    int64      `json:"version"`
	Path       string     `json:"path"`
	Current    int        `json:"current"`
	Selected   int        `json:"selected"`
	Selection  []int      `json:"selection"`
	Slides     []Slide    `json:"slides"`
	Resolved   []Slide    `json:"resolved"`
	Masters    MasterDeck `json:"masters"`
	MasterMode bool       `json:"masterMode,omitempty"`
}

type nativeEditorAction struct {
	Action       string    `json:"action"`
	Slide        int       `json:"slide,omitempty"`
	Page         int       `json:"page,omitempty"`
	Value        int       `json:"value,omitempty"`
	Cols         int       `json:"cols,omitempty"`
	Rows         int       `json:"rows,omitempty"`
	BoxWidth     int       `json:"boxWidth,omitempty"`
	BoxHeight    int       `json:"boxHeight,omitempty"`
	Element      int       `json:"element,omitempty"`
	Kind         string    `json:"kind,omitempty"`
	Level        int       `json:"level,omitempty"`
	Name         string    `json:"name,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	ElementData  *Element  `json:"elementData,omitempty"`
	ElementsData []Element `json:"elementsData,omitempty"`
	SlideData    *Slide    `json:"slideData,omitempty"`
}

type nativeEditorTextFit struct {
	Element Element      `json:"element"`
	Pages   []exportPage `json:"pages"`
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
	deck := cloneDeck(s.deck)
	current := s.current
	masterMode, currentMaster := s.masterMode, s.currentMaster
	s.mu.RUnlock()
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
		measure.Query = removeImageQueryKeys(measure.Query, "top", "bottom", "left", "right", "left_pct", "right_pct", "row_delta", "align", "width", "height")
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
		preview = deck.ResolvedSlides()[current]
		slideIndex, slideCount = current, len(deck.Slides)
	}
	w.Header().Set("Content-Type", "application/json")
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
		for _, key := range []string{"top", "bottom", "row_delta"} {
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
	deck := cloneDeck(s.deck)
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
		deck.Slides[current].Elements[action.Element] = normalized
		preview = deck.ResolvedSlides()[current]
		normalized = normalizedTextKindPlacement(preview, normalized, action.Element, cols, rows, action.Page)
		deck.Slides[current].Elements[action.Element] = normalized
		preview = deck.ResolvedSlides()[current]
		slideIndex, slideCount = current, len(deck.Slides)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(nativeEditorTextFit{Element: normalized, Pages: exportSlidePages(preview, slideIndex, slideCount, cols, rows)})
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
	deck := cloneDeck(s.deck)
	current := s.current
	masterMode, currentMaster := s.masterMode, s.currentMaster
	s.mu.RUnlock()
	if masterMode {
		if action.ElementData == nil || currentMaster < 0 || currentMaster > len(deck.Masters.Layouts) {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		target := masterSlideAt(&deck, currentMaster)
		if action.Element < 0 || action.Element >= len(target.Elements) {
			http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
			return
		}
		target.Elements[action.Element] = *action.ElementData
		cols, rows := action.Cols, action.Rows
		if cols <= 0 || rows <= 0 {
			cols, rows = authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
		}
		pages := exportSlidePages(masterViewPreview(deck.Masters, currentMaster), currentMaster, len(deck.Masters.Layouts)+1, cols, rows)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pages)
		return
	}
	if action.ElementData == nil || current < 0 || current >= len(deck.Slides) || action.Element < 0 || action.Element >= len(deck.Slides[current].Elements) {
		http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
		return
	}
	deck.Slides[current].Elements[action.Element] = *action.ElementData
	resolved := deck.ResolvedSlides()
	cols, rows := action.Cols, action.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
	}
	pages := exportSlidePages(resolved[current], current, len(resolved), cols, rows)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pages)
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
	deck := cloneDeck(s.deck)
	masterMode, currentMaster := s.masterMode, s.currentMaster
	s.mu.RUnlock()
	if !masterMode || currentMaster < 0 || currentMaster > len(deck.Masters.Layouts) {
		http.Error(w, errInvalidEditorAction.Error(), http.StatusBadRequest)
		return
	}
	pages := exportSlidePages(masterViewPreview(deck.Masters, currentMaster), currentMaster, len(deck.Masters.Layouts)+1, cols, rows)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pages)
}

func newNativeEditorSession(deckPath string, deck Deck) *nativeEditorSession {
	return &nativeEditorSession{deck: deck, deckPath: deckPath, selected: -1, selection: map[int]bool{}, version: 1}
}

func (s *nativeEditorSession) state() nativeEditorState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	selection := make([]int, 0, len(s.selection))
	for index := range s.selection {
		selection = append(selection, index)
	}
	sort.Ints(selection)
	slides, resolved, current := cloneSlides(s.deck.Slides), s.deck.ResolvedSlides(), s.current
	if s.masterMode {
		slides = []Slide{cloneSlide(s.deck.Masters.Base.Slide)}
		resolved = []Slide{masterViewPreview(s.deck.Masters, 0)}
		for index, layout := range s.deck.Masters.Layouts {
			slides = append(slides, cloneSlide(layout.Slide))
			resolved = append(resolved, masterViewPreview(s.deck.Masters, index+1))
		}
		current = min(s.currentMaster, len(slides)-1)
	}
	return nativeEditorState{
		Version: s.version, Path: s.deckPath, Current: current, Selected: s.selected, MasterMode: s.masterMode,
		Selection: selection, Slides: slides, Resolved: resolved, Masters: s.deck.Masters,
	}
}

func (s *nativeEditorSession) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.state())
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
	_ = json.NewEncoder(w).Encode(s.state())
}

func (s *nativeEditorSession) apply(action nativeEditorAction) error {
	if action.Action == "toggle-master-mode" || s.masterMode {
		return s.applyMaster(action)
	}
	s.mu.Lock()
	changed := false
	presenterPage := 0
	before := Deck{}
	if action.Action != "select-slide" && action.Action != "select-element" {
		before = cloneDeck(s.deck)
	}
	slideCount := len(s.deck.Slides)
	switch action.Action {
	case "select-slide":
		if action.Slide < 0 || action.Slide >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.current, s.selected = action.Slide, -1
		s.selection = map[int]bool{}
	case "select-element":
		if action.Element < -1 || s.current < 0 || s.current >= slideCount || action.Element >= len(s.deck.Slides[s.current].Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if action.Name == "toggle" && action.Element >= 0 {
			if s.selection[action.Element] {
				delete(s.selection, action.Element)
			} else {
				s.selection[action.Element] = true
			}
			s.selected = action.Element
		} else {
			s.selected = action.Element
			s.selection = map[int]bool{}
			if action.Element >= 0 {
				s.selection[action.Element] = true
			}
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
	case "stop-timer":
	case "previous-slide":
		s.current = max(0, s.current-1)
		s.selected = -1
		s.selection = map[int]bool{}
	case "next-slide":
		s.current = min(slideCount-1, s.current+1)
		s.selected = -1
		s.selection = map[int]bool{}
	case "add-slide":
		insert := min(slideCount, s.current+1)
		s.deck.Slides = append(s.deck.Slides, Slide{})
		copy(s.deck.Slides[insert+1:], s.deck.Slides[insert:])
		s.deck.Slides[insert] = Slide{}
		s.current, s.selected, changed = insert, -1, true
		s.selection = map[int]bool{}
	case "clone-slide":
		if slideCount == 0 {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		insert := s.current + 1
		s.deck.Slides = append(s.deck.Slides, Slide{})
		copy(s.deck.Slides[insert+1:], s.deck.Slides[insert:])
		s.deck.Slides[insert] = cloneSlide(s.deck.Slides[s.current])
		s.current, s.selected, changed = insert, -1, true
		s.selection = map[int]bool{}
	case "delete-slide":
		if slideCount <= 1 {
			s.deck.Slides = []Slide{placeholderSlide()}
			s.current = 0
		} else {
			s.deck.Slides = append(s.deck.Slides[:s.current], s.deck.Slides[s.current+1:]...)
			s.current = min(s.current, len(s.deck.Slides)-1)
		}
		s.selected, changed = -1, true
		s.selection = map[int]bool{}
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
		case "bullet":
			element.Text = "List item"
		case "code":
			element.Text = "code"
		case "shape":
			element = newShapeElement(editorShapeName(action.Name))
		case "image":
			element.Path = "image.png"
		default:
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.deck.Slides[s.current].Elements = append(s.deck.Slides[s.current].Elements, element)
		s.selected, changed = len(s.deck.Slides[s.current].Elements)-1, true
		s.selection = map[int]bool{s.selected: true}
	case "duplicate-element":
		if s.current < 0 || s.current >= slideCount || action.Element < 0 || action.Element >= len(s.deck.Slides[s.current].Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		duplicate := s.deck.Slides[s.current].Elements[action.Element]
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
		for _, source := range action.ElementsData {
			element := source
			element.Kind = strings.TrimSpace(element.Kind)
			if element.Kind == "" {
				continue
			}
			element.ID = newStableID(element.Kind)
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
		original := s.deck.Slides[s.current].Elements[action.Element]
		if updated.ID == "" {
			updated.ID = original.ID
		}
		updated.Kind = strings.TrimSpace(updated.Kind)
		if updated.Kind == "" {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		s.deck.Slides[s.current].Elements[action.Element] = updated
		s.selected, changed = action.Element, true
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
		elements := s.deck.Slides[s.current].Elements
		target := action.Element
		if action.Kind == "back" {
			target = 0
		}
		if action.Kind == "backward" {
			target = max(0, action.Element-1)
		}
		if action.Kind == "forward" {
			target = min(len(elements)-1, action.Element+1)
		}
		if action.Kind == "front" {
			target = len(elements) - 1
		}
		if target != action.Element {
			element := elements[action.Element]
			elements = append(elements[:action.Element], elements[action.Element+1:]...)
			elements = append(elements, Element{})
			copy(elements[target+1:], elements[target:])
			elements[target] = element
			s.deck.Slides[s.current].Elements = elements
			s.selected, changed = target, true
		}
	case "update-slide":
		if action.SlideData == nil || s.current < 0 || s.current >= slideCount {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		updated := *action.SlideData
		updated.FG = nativeEditorColorCode(updated.FG, false)
		updated.BG = nativeEditorColorCode(updated.BG, true)
		updated.HeaderFG = nativeEditorColorCode(updated.HeaderFG, false)
		updated.Elements = s.deck.Slides[s.current].Elements
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
			s.deck.Masters.Base.Slide = cloneSlide(*action.SlideData)
			if strings.TrimSpace(action.Name) != "" {
				s.deck.Masters.Base.Name = strings.TrimSpace(action.Name)
			}
		} else {
			index := s.deck.Masters.LayoutIndex(action.Kind)
			if index < 0 {
				s.mu.Unlock()
				return errInvalidEditorAction
			}
			s.deck.Masters.Layouts[index].Slide = cloneSlide(*action.SlideData)
			if strings.TrimSpace(action.Name) != "" {
				s.deck.Masters.Layouts[index].Name = strings.TrimSpace(action.Name)
			}
		}
		s.deck.Masters.Normalize()
		changed = true
	case "delete-layout":
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
	case "export":
		resolved := s.deck.ResolvedSlides()
		width, height := authoredRenderSize(80, 25)
		s.mu.Unlock()
		return exportHTML(s.deckPath, resolved, width, height)
	case "undo":
		s.selected = -1
		s.selection = map[int]bool{}
		if len(s.undo) > 0 {
			s.redo = append(s.redo, cloneDeck(s.deck))
			s.deck = s.undo[len(s.undo)-1]
			s.undo = s.undo[:len(s.undo)-1]
			s.current = min(s.current, len(s.deck.Slides)-1)
			changed = true
		}
	case "redo":
		s.selected = -1
		s.selection = map[int]bool{}
		if len(s.redo) > 0 {
			s.undo = append(s.undo, cloneDeck(s.deck))
			s.deck = s.redo[len(s.redo)-1]
			s.redo = s.redo[:len(s.redo)-1]
			s.current = min(s.current, len(s.deck.Slides)-1)
			changed = true
		}
	default:
		s.mu.Unlock()
		return errInvalidEditorAction
	}
	if changed && action.Action != "undo" && action.Action != "redo" {
		s.undo = append(s.undo, before)
		if len(s.undo) > 100 {
			s.undo = s.undo[len(s.undo)-100:]
		}
		s.redo = nil
	}
	s.version++
	deck := cloneDeck(s.deck)
	current := s.current
	companion := s.companion
	if changed {
		_ = saveDeck(s.deckPath, s.deck)
	}
	s.mu.Unlock()
	if companion != nil {
		resolved := deck.ResolvedSlides()
		if changed {
			switch nativeEditorRefreshScope(action.Action) {
			case "slide":
				companion.RefreshActiveSlideAsync(resolved, authoredTerminalWidth, authoredTerminalHeight)
			case "deck":
				companion.RefreshAllAsync(s.deckPath, resolved, authoredTerminalWidth, authoredTerminalHeight)
			}
		}
		_, _, presenting := companion.Status()
		companion.Update(current, presenterPage, presenting, nil)
		if action.Action == "start-timer" {
			companion.StartTimer(time.Duration(action.Value) * time.Second)
		} else if action.Action == "stop-timer" {
			companion.StopTimer()
		}
	}
	return nil
}

func nativeEditorRefreshScope(action string) string {
	switch action {
	case "add-element", "duplicate-element", "paste-elements", "update-element", "delete-element", "delete-selection", "move-element", "update-slide", "set-layout":
		return "slide"
	case "update-slide-notes":
		return ""
	default:
		return "deck"
	}
}

func (s *nativeEditorSession) applyMaster(action nativeEditorAction) error {
	s.mu.Lock()
	s.deck.EnsureDefaultMasters()
	if action.Action == "toggle-master-mode" {
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
	before := cloneDeck(s.deck)
	changed := false
	masterCount := len(s.deck.Masters.Layouts) + 1
	s.currentMaster = max(0, min(s.currentMaster, masterCount-1))
	target := masterSlideAt(&s.deck, s.currentMaster)
	switch action.Action {
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
		s.deck.Masters.Layouts = append(s.deck.Masters.Layouts, clone)
		s.currentMaster, s.selected, changed = len(s.deck.Masters.Layouts), -1, true
		s.selection = map[int]bool{}
	case "delete-slide", "delete-layout":
		if s.currentMaster <= 0 || s.currentMaster-1 >= len(s.deck.Masters.Layouts) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		id := s.deck.Masters.Layouts[s.currentMaster-1].ID
		s.deck.Masters.Layouts = append(s.deck.Masters.Layouts[:s.currentMaster-1], s.deck.Masters.Layouts[s.currentMaster:]...)
		for index := range s.deck.Slides {
			if s.deck.Slides[index].LayoutID == id {
				s.deck.Slides[index].LayoutID = ""
			}
		}
		s.currentMaster = min(s.currentMaster, len(s.deck.Masters.Layouts))
		s.selected, s.selection, changed = -1, map[int]bool{}, true
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
		case "bullet":
			element.Text = "List item"
		case "code":
			element.Text = "code"
		case "shape":
			element = newShapeElement(editorShapeName(action.Name))
		case "image":
			element.Path = "image.png"
		default:
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		target.Elements = append(target.Elements, element)
		s.selected, changed = len(target.Elements)-1, true
		s.selection = map[int]bool{s.selected: true}
	case "select-element":
		if action.Element < -1 || action.Element >= len(target.Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		if action.Name == "toggle" && action.Element >= 0 {
			if s.selection[action.Element] {
				delete(s.selection, action.Element)
			} else {
				s.selection[action.Element] = true
			}
			s.selected = action.Element
		} else {
			s.selected = action.Element
			s.selection = map[int]bool{}
			if action.Element >= 0 {
				s.selection[action.Element] = true
			}
		}
	case "update-element":
		if action.ElementData == nil || action.Element < 0 || action.Element >= len(target.Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		updated := *action.ElementData
		if updated.ID == "" {
			updated.ID = target.Elements[action.Element].ID
		}
		target.Elements[action.Element] = updated
		s.selected, changed = action.Element, true
	case "duplicate-element":
		if action.Element < 0 || action.Element >= len(target.Elements) {
			s.mu.Unlock()
			return errInvalidEditorAction
		}
		duplicate := target.Elements[action.Element]
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
		for _, source := range action.ElementsData {
			element := source
			element.Kind = strings.TrimSpace(element.Kind)
			if element.Kind == "" {
				continue
			}
			element.ID = newStableID(element.Kind)
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
		destination := action.Element
		switch action.Kind {
		case "back":
			destination = 0
		case "backward":
			destination = max(0, action.Element-1)
		case "forward":
			destination = min(len(target.Elements)-1, action.Element+1)
		case "front":
			destination = len(target.Elements) - 1
		}
		if destination != action.Element {
			element := target.Elements[action.Element]
			target.Elements = append(target.Elements[:action.Element], target.Elements[action.Element+1:]...)
			target.Elements = append(target.Elements, Element{})
			copy(target.Elements[destination+1:], target.Elements[destination:])
			target.Elements[destination] = element
			s.selected, changed = destination, true
		}
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
			s.deck.Masters.Layouts[s.currentMaster-1].Name = name
		}
		changed = true
	case "update-slide-notes":
		index := max(0, min(action.Slide, masterCount-1))
		masterSlideAt(&s.deck, index).Notes = action.Notes
		changed = true
	case "undo":
		s.selected = -1
		s.selection = map[int]bool{}
		if len(s.undo) > 0 {
			s.redo = append(s.redo, cloneDeck(s.deck))
			s.deck = s.undo[len(s.undo)-1]
			s.undo = s.undo[:len(s.undo)-1]
			s.currentMaster = min(s.currentMaster, len(s.deck.Masters.Layouts))
			changed = true
		}
	case "redo":
		s.selected = -1
		s.selection = map[int]bool{}
		if len(s.redo) > 0 {
			s.undo = append(s.undo, cloneDeck(s.deck))
			s.deck = s.redo[len(s.redo)-1]
			s.redo = s.redo[:len(s.redo)-1]
			s.currentMaster = min(s.currentMaster, len(s.deck.Masters.Layouts))
			changed = true
		}
	default:
		s.mu.Unlock()
		return errInvalidEditorAction
	}
	if changed && action.Action != "undo" && action.Action != "redo" {
		s.undo = append(s.undo, before)
		if len(s.undo) > 100 {
			s.undo = s.undo[len(s.undo)-100:]
		}
		s.redo = nil
	}
	s.deck.Masters.Normalize()
	s.version++
	deck := cloneDeck(s.deck)
	companion := s.companion
	if changed {
		_ = saveDeck(s.deckPath, s.deck)
	}
	s.mu.Unlock()
	if changed && companion != nil {
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
