package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"
)

const placeholderFixed = "fixed"

type masterViewResult struct {
	Quit bool
}

func persistMasterDeck(deckPath string, deck *Deck) bool {
	if deck == nil || !persistDeck(deckPath, *deck) {
		return false
	}
	if activePresenter != nil {
		width, height := terminalAuthoredSize()
		activePresenter.RefreshAllAsync(deckPath, deck.ResolvedSlides(), width, height)
	}
	return true
}

func playLayoutPicker(deck *Deck, currentLayout string, width, height int) (string, bool) {
	if deck == nil {
		return "", false
	}
	deck.EnsureDefaultMasters()
	selected := deck.Masters.LayoutIndex(currentLayout)
	if selected < 0 {
		selected = 0
	}
	result := ""
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalSize()
			preview := masterLayoutPreview(deck.Masters, deck.Masters.Layouts[selected].ID)
			renderer.draw(preview, width, height, 0, frame, func(lines []Line) {
				drawLayoutPickerPanel(deck.Masters, selected, width, height)
			})
		},
		Read: readEffectPickerKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape", "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				result = deck.Masters.Layouts[selected].ID
				return overlayDecision{Disposition: overlayCommit}
			case "up", "left":
				selected = (selected - 1 + len(deck.Masters.Layouts)) % len(deck.Masters.Layouts)
			case "down", "right":
				selected = (selected + 1) % len(deck.Masters.Layouts)
			case "mouse-click":
				if index, ok := layoutPickerIndexAtPoint(event.X, event.Y, selected, len(deck.Masters.Layouts), width, height); ok {
					if index == selected {
						result = deck.Masters.Layouts[selected].ID
						return overlayDecision{Disposition: overlayCommit}
					}
					selected = index
				}
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func masterLayoutPreview(masters MasterDeck, layoutID string) Slide {
	previewDeck := Deck{Masters: masters, Slides: []Slide{{LayoutID: layoutID}}}
	return previewDeck.ResolveSlide(0, true)
}

func drawLayoutPickerPanel(masters MasterDeck, selected, width, height int) {
	panelW := min(max(34, width/3), max(24, width-4))
	itemH := 7
	panelH := min(height-2, max(9, len(masters.Layouts)*itemH+4))
	left := max(0, (width-panelW)/2)
	top := max(0, (height-panelH)/2)
	for row := 0; row < panelH; row++ {
		termPrintf("\033[0;37;40m\033[%d;%dH%s", top+row+1, left+1, strings.Repeat(" ", panelW))
	}
	termPrintf("\033[1;37;40m\033[%d;%dH%s", top+2, left+3, crop("Choose a layout", max(0, panelW-4)))
	visible := max(1, (panelH-4)/itemH)
	scroll := max(0, min(selected, len(masters.Layouts)-visible))
	for slot := 0; slot < visible; slot++ {
		index := scroll + slot
		if index >= len(masters.Layouts) {
			break
		}
		layout := masters.Layouts[index]
		preview := masterLayoutPreview(masters, layout.ID)
		itemY := top + 2 + slot*itemH
		drawSlideNavigatorItem(preview, index, selected == index, left+1, itemY, panelW-2, itemH)
		fg, bg := "37", "40"
		if selected == index {
			fg, bg = "30", "47"
		}
		termPrintf("\033[1;%s;%sm\033[%d;%dH%s", fg, bg, itemY+1, left+3, crop(layout.Name, max(0, panelW-6)))
	}
	termPrintf("\033[0;90;40m\033[%d;%dH%s", top+panelH-1, left+3, crop("arrows select  enter create  esc cancel", max(0, panelW-5)))
}

func layoutPickerIndexAtPoint(x, y, selected, count, width, height int) (int, bool) {
	panelW := min(max(34, width/3), max(24, width-4))
	itemH := 7
	panelH := min(height-2, max(9, count*itemH+4))
	left := max(0, (width-panelW)/2)
	top := max(0, (height-panelH)/2)
	if x < left+1 || x >= left+panelW-1 || y < top+2 || y >= top+panelH-2 {
		return 0, false
	}
	visible := max(1, (panelH-4)/itemH)
	scroll := max(0, min(selected, count-visible))
	index := scroll + (y-(top+2))/itemH
	return index, index >= 0 && index < count
}

func playPlaceholderRolePicker(slide Slide, page, width, height int, current string) (string, bool) {
	roles := []struct {
		Value string
		Title string
	}{
		{placeholderTitle, "Title"},
		{placeholderSubtitle, "Subtitle"},
		{placeholderBody, "Body"},
		{placeholderCode, "Code"},
		{placeholderImage, "Image"},
		{placeholderFixed, "Fixed master element"},
	}
	selected := len(roles) - 1
	for index, role := range roles {
		if role.Value == current {
			selected = index
		}
	}
	titles := make([]string, len(roles))
	for index, role := range roles {
		titles[index] = role.Title
	}
	result := ""
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalSize()
			renderer.draw(slide, width, height, page, frame, func(lines []Line) {
				drawSimpleChoicePanel("Placeholder role", titles, selected, width, height, "arrows select  enter confirm  esc cancel")
			})
		},
		Read: readEffectPickerKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape", "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "enter":
				result = roles[selected].Value
				return overlayDecision{Disposition: overlayCommit}
			case "up", "left":
				selected = (selected - 1 + len(roles)) % len(roles)
			case "down", "right":
				selected = (selected + 1) % len(roles)
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func drawSimpleChoicePanel(title string, choices []string, selected, width, height int, hint string) {
	panelW := min(max(36, width/3), max(24, width-4))
	panelH := min(height-2, len(choices)+5)
	left := max(0, (width-panelW)/2)
	top := max(0, (height-panelH)/2)
	for row := 0; row < panelH; row++ {
		termPrintf("\033[0;37;40m\033[%d;%dH%s", top+row+1, left+1, strings.Repeat(" ", panelW))
	}
	termPrintf("\033[1;37;40m\033[%d;%dH%s", top+2, left+3, crop(title, max(0, panelW-5)))
	for index, choice := range choices {
		fg, bg, prefix := "37", "40", "  "
		if index == selected {
			fg, bg, prefix = "30", "47", "> "
		}
		termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, bg, top+4+index, left+3, padRight(crop(prefix+choice, max(0, panelW-6)), max(0, panelW-6)))
	}
	termPrintf("\033[0;90;40m\033[%d;%dH%s", top+panelH-1, left+3, crop(hint, max(0, panelW-5)))
}

func applyPlaceholderRole(element *Element, role string) {
	if element == nil {
		return
	}
	if role == placeholderFixed || role == "" {
		element.PlaceholderRole = ""
		element.SlotID = ""
		return
	}
	if element.ID == "" {
		element.ID = newStableID("master-element")
	}
	element.SlotID = element.ID
	element.PlaceholderRole = normalizePlaceholderRole(role)
	switch element.PlaceholderRole {
	case placeholderTitle:
		element.Kind, element.Level = "heading", 1
		if strings.TrimSpace(element.Text) == "" {
			element.Text = "Title"
		}
	case placeholderSubtitle:
		element.Kind, element.Level = "heading", 2
		if strings.TrimSpace(element.Text) == "" {
			element.Text = "Subtitle"
		}
	case placeholderCode:
		element.Kind, element.Level = "code", 0
		if strings.TrimSpace(element.Text) == "" {
			element.Text = "Code"
		}
	case placeholderImage:
		element.Kind, element.Level = "image", 0
	case placeholderBody:
		element.Kind, element.Level = "text", 0
		if strings.TrimSpace(element.Text) == "" {
			element.Text = "Body text"
		}
	}
}

func playMasterView(deck *Deck, deckPath string, width, height int, undoState *EditState) (result masterViewResult) {
	if deck == nil {
		return masterViewResult{}
	}
	beforeSession := cloneDeck(*deck)
	defer func() {
		commitFullDeckSnapshot(undoState, beforeSession, *deck, 0, 0)
	}()
	deck.EnsureDefaultMasters()
	deck.Masters.Normalize()
	selected := 0
	renderer := &liveSlideRenderer{}
	frame := 0
	for {
		width, height = terminalSize()
		preview := masterViewPreview(deck.Masters, selected)
		renderer.draw(preview, width, height, 0, frame, func(lines []Line) {
			drawMasterNavigator(deck.Masters, selected, width, height)
		})
		frame++
		event := readMasterViewKeyEvent()
		if event.Action == "" {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		count := len(deck.Masters.Layouts) + 1
		switch event.Action {
		case "escape":
			return masterViewResult{}
		case "quit":
			return masterViewResult{Quit: true}
		case "up":
			selected = (selected - 1 + count) % count
		case "down":
			selected = (selected + 1) % count
		case "mouse-click":
			if index, ok := masterNavigatorIndexAtPoint(event.X, event.Y, selected, count, width, height); ok {
				if index == selected {
					if editMasterAt(deck, selected, deckPath, width, height) {
						return masterViewResult{Quit: true}
					}
				} else {
					selected = index
				}
			}
		case "shift-up":
			if selected > 1 {
				deck.Masters.Layouts[selected-1], deck.Masters.Layouts[selected-2] = deck.Masters.Layouts[selected-2], deck.Masters.Layouts[selected-1]
				selected--
				persistMasterDeck(deckPath, deck)
			}
		case "shift-down":
			if selected > 0 && selected < count-1 {
				deck.Masters.Layouts[selected-1], deck.Masters.Layouts[selected] = deck.Masters.Layouts[selected], deck.Masters.Layouts[selected-1]
				selected++
				persistMasterDeck(deckPath, deck)
			}
		case "enter":
			if editMasterAt(deck, selected, deckPath, width, height) {
				return masterViewResult{Quit: true}
			}
		case "new":
			name, err := startupTextInput("New master layout", "Layout name", fmt.Sprintf("Layout %d", len(deck.Masters.Layouts)+1))
			if err == nil && strings.TrimSpace(name) != "" {
				deck.Masters.Layouts = append(deck.Masters.Layouts, MasterLayout{ID: newStableID("layout"), Name: name})
				selected = len(deck.Masters.Layouts)
				persistMasterDeck(deckPath, deck)
			}
		case "clone":
			if selected > 0 {
				clone := cloneMasterLayoutFresh(deck.Masters.Layouts[selected-1])
				deck.Masters.Layouts = append(deck.Masters.Layouts, MasterLayout{})
				copy(deck.Masters.Layouts[selected:], deck.Masters.Layouts[selected-1:])
				deck.Masters.Layouts[selected] = clone
				selected++
				persistMasterDeck(deckPath, deck)
			}
		case "rename":
			if selected > 0 {
				layout := &deck.Masters.Layouts[selected-1]
				if name, err := startupTextInput("Rename master layout", "Layout name", layout.Name); err == nil && strings.TrimSpace(name) != "" {
					layout.Name = name
					persistMasterDeck(deckPath, deck)
				}
			}
		case "delete":
			if selected > 0 && deleteMasterLayout(deck, selected-1, deckPath) {
				selected = min(selected, len(deck.Masters.Layouts))
			}
		case "effect-picker":
			if effect, ok := playEffectPicker(preview, width, height); ok {
				target := masterSlideAt(deck, selected)
				target.EffectSet = true
				target.Effect = effect
				if effect == "none" {
					target.Effect = ""
				}
				persistMasterDeck(deckPath, deck)
			}
		case "background-picker":
			if background, ok := playBackgroundPicker(preview, width, height); ok {
				target := masterSlideAt(deck, selected)
				target.BackgroundSet = true
				target.Background = background
				if background == "none" {
					target.Background = ""
				}
				persistMasterDeck(deckPath, deck)
			}
		case "page-number":
			mode := toggleMasterPageNumber(deck, selected)
			if persistMasterDeck(deckPath, deck) {
				setUINotice("Page number: " + mode)
			}
		case "visual-properties":
			if editMasterVisualProperties(deck, selected, width, height) && persistMasterDeck(deckPath, deck) {
				setUINotice("Visual properties updated")
			}
		}
	}
}

func masterViewPreview(masters MasterDeck, selected int) Slide {
	if selected <= 0 {
		preview := cloneSlide(masters.Base.Slide)
		if preview.PageNumber == pageNumberShow {
			for index := range preview.Elements {
				if preview.Elements[index].Kind == "page-number" {
					preview.Elements[index].Text = "1"
				}
			}
		} else {
			removePageNumberElements(&preview)
		}
		return preview
	}
	if selected-1 >= len(masters.Layouts) {
		return Slide{}
	}
	return masterLayoutPreview(masters, masters.Layouts[selected-1].ID)
}

func masterSlideAt(deck *Deck, selected int) *Slide {
	if selected <= 0 {
		return &deck.Masters.Base.Slide
	}
	return &deck.Masters.Layouts[selected-1].Slide
}

func toggleMasterPageNumber(deck *Deck, selected int) string {
	target := masterSlideAt(deck, selected)
	if selected <= 0 {
		if target.PageNumber == pageNumberShow {
			target.PageNumber = pageNumberHide
			removePageNumberElements(target)
		} else {
			target.PageNumber = pageNumberShow
			ensurePageNumberElement(target, "base")
		}
		return pageNumberModeLabel(target.PageNumber, false)
	}
	target.PageNumber = nextPageNumberOverride(target.PageNumber)
	if target.PageNumber == pageNumberShow {
		ensurePageNumberElement(target, deck.Masters.Layouts[selected-1].ID)
	} else {
		removePageNumberElements(target)
	}
	return pageNumberModeLabel(target.PageNumber, true)
}

func masterNameAt(masters MasterDeck, selected int) string {
	if selected <= 0 {
		return "Base"
	}
	if selected-1 < len(masters.Layouts) {
		return masters.Layouts[selected-1].Name
	}
	return "Master"
}

func editMasterAt(deck *Deck, selected int, deckPath string, width, height int) bool {
	target := masterSlideAt(deck, selected)
	working := masterEditorSlide(deck, selected)
	temporary := Deck{Slides: []Slide{working}}
	state := &EditState{Selected: -1, LastSelected: -1, Cursor: map[int]int{}, MultiSelected: map[int]bool{}}
	persist := func() bool {
		*target = extractEditedMasterSlide(*target, temporary.Slides[0], selected > 0)
		deck.Masters.Normalize()
		return persistMasterDeck(deckPath, deck)
	}
	result := playEditMode(&temporary, 0, width, height, 0, deckPath, state, editModeOptions{
		Master:           true,
		MasterName:       masterNameAt(deck.Masters, selected),
		Persist:          persist,
		TogglePageNumber: func(slide *Slide) string { return toggleMasterEditorPageNumber(deck, selected, slide) },
		EditVisualProperties: func(slide *Slide) bool {
			source := extractEditedMasterSlide(*target, *slide, selected > 0)
			resolve := masterVisualResolver(deck, selected)
			updated, ok := playVisualProperties(source, selected > 0, resolve, width, height)
			if !ok {
				return false
			}
			*target = updated
			*slide = masterEditorSlide(deck, selected)
			return true
		},
	})
	*target = extractEditedMasterSlide(*target, temporary.Slides[0], selected > 0)
	deck.Masters.Normalize()
	persistMasterDeck(deckPath, deck)
	return result == "quit"
}

func editMasterVisualProperties(deck *Deck, selected, width, height int) bool {
	target := masterSlideAt(deck, selected)
	updated, ok := playVisualProperties(cloneSlide(*target), selected > 0, masterVisualResolver(deck, selected), width, height)
	if !ok {
		return false
	}
	*target = updated
	deck.Masters.Normalize()
	return true
}

func masterVisualResolver(deck *Deck, selected int) func(Slide) Slide {
	return func(candidate Slide) Slide {
		preview := cloneDeck(*deck)
		if selected <= 0 {
			preview.Masters.Base.Slide = candidate
		} else if selected-1 < len(preview.Masters.Layouts) {
			preview.Masters.Layouts[selected-1].Slide = candidate
		}
		preview.Masters.Normalize()
		return masterViewPreview(preview.Masters, selected)
	}
}

func masterEditorSlide(deck *Deck, selected int) Slide {
	target := masterSlideAt(deck, selected)
	working := cloneSlide(*target)
	if selected > 0 {
		base := cloneSlide(deck.Masters.Base.Slide)
		layoutID := deck.Masters.Layouts[selected-1].ID
		working = inheritedSlideStyle(deck.Masters, deck.Masters.Layouts[selected-1].ID)
		working.PageNumber = target.PageNumber
		working.Elements = nil
		for _, element := range base.Elements {
			if element.Kind == "page-number" {
				continue
			}
			element.Inherited = true
			working.Elements = append(working.Elements, element)
		}
		for _, element := range cloneSlide(*target).Elements {
			if element.Kind != "page-number" || target.PageNumber == pageNumberShow {
				working.Elements = append(working.Elements, element)
			}
		}
		if deck.effectivePageNumberMode(Slide{LayoutID: layoutID}) == pageNumberShow && target.PageNumber != pageNumberShow {
			working.Elements = append(working.Elements, resolvedPageNumberElement(deck.Masters, layoutID, 0))
		}
	}
	return working
}

func toggleMasterEditorPageNumber(deck *Deck, selected int, working *Slide) string {
	if working == nil {
		return ""
	}
	removePageNumberElements(working)
	if selected <= 0 {
		if working.PageNumber == pageNumberShow {
			working.PageNumber = pageNumberHide
		} else {
			working.PageNumber = pageNumberShow
			ensurePageNumberElement(working, "base")
		}
		return pageNumberModeLabel(working.PageNumber, false)
	}
	working.PageNumber = nextPageNumberOverride(working.PageNumber)
	switch working.PageNumber {
	case pageNumberShow:
		ensurePageNumberElement(working, deck.Masters.Layouts[selected-1].ID)
	case "":
		if deck.Masters.Base.Slide.PageNumber == pageNumberShow {
			number := resolvedPageNumberElement(deck.Masters, "", 0)
			number.Inherited = true
			working.Elements = append(working.Elements, number)
		}
	}
	return pageNumberModeLabel(working.PageNumber, true)
}

func extractEditedMasterSlide(original, working Slide, hasBaseOverlay bool) Slide {
	if !hasBaseOverlay {
		working.Elements = stripRuntimeElementState(working.Elements)
		return working
	}
	out := original
	out.PageNumber = working.PageNumber
	out.Elements = nil
	for _, element := range working.Elements {
		if element.Inherited {
			continue
		}
		element.Inherited = false
		out.Elements = append(out.Elements, element)
	}
	return out
}

func cloneMasterLayoutFresh(source MasterLayout) MasterLayout {
	clone := source
	clone.ID = newStableID("layout")
	clone.Name = source.Name + " Copy"
	clone.Slide = cloneSlide(source.Slide)
	for index := range clone.Slide.Elements {
		element := &clone.Slide.Elements[index]
		element.ID = newStableID(clone.ID + "-element")
		if element.PlaceholderRole != "" {
			element.SlotID = element.ID
		}
	}
	return clone
}

func deleteMasterLayout(deck *Deck, layoutIndex int, deckPath string) bool {
	if layoutIndex < 0 || layoutIndex >= len(deck.Masters.Layouts) {
		return false
	}
	layout := deck.Masters.Layouts[layoutIndex]
	var used []int
	for slideIndex, slide := range deck.Slides {
		if slide.LayoutID == layout.ID {
			used = append(used, slideIndex)
		}
	}
	if len(used) > 0 {
		choice, err := startupMenu("Layout is in use", fmt.Sprintf("%s is used by %d slide(s).", layout.Name, len(used)), []startupMenuItem{
			{Title: "Cancel", Detail: "Keep the layout and its slides unchanged."},
			{Title: "Reassign slides", Detail: "Move content to another available layout."},
			{Title: "Detach slides", Detail: "Convert inherited content into ordinary slide elements."},
		})
		if err != nil || choice == 0 {
			return false
		}
		if choice == 1 {
			targetID := ""
			for _, candidate := range deck.Masters.Layouts {
				if candidate.ID != layout.ID {
					targetID = candidate.ID
					if candidate.ID == "blank" {
						break
					}
				}
			}
			if targetID == "" {
				return false
			}
			for _, slideIndex := range used {
				deck.RebindSlideLayout(slideIndex, targetID)
			}
		} else {
			for _, slideIndex := range used {
				deck.DetachSlide(slideIndex)
			}
		}
	}
	deck.Masters.Layouts = append(deck.Masters.Layouts[:layoutIndex], deck.Masters.Layouts[layoutIndex+1:]...)
	persistMasterDeck(deckPath, deck)
	return true
}

func drawMasterNavigator(masters MasterDeck, selected, width, height int) {
	names := []string{"Base Master"}
	previews := []Slide{cloneSlide(masters.Base.Slide)}
	for _, layout := range masters.Layouts {
		names = append(names, layout.Name)
		previews = append(previews, masterLayoutPreview(masters, layout.ID))
	}
	x, y, w, h := slideNavigatorRect(width, height)
	for row := 0; row < h; row++ {
		termPrintf("\033[0;37;40m\033[%d;%dH%s", y+row+1, x+1, strings.Repeat(" ", w))
	}
	itemH := slideNavigatorItemHeight()
	visible := max(1, h/itemH)
	scroll := max(0, min(selected, len(previews)-visible))
	for slot := 0; slot < visible; slot++ {
		index := scroll + slot
		if index >= len(previews) {
			break
		}
		itemY := y + slot*itemH
		drawSlideNavigatorItem(previews[index], index, selected == index, x, itemY, w, itemH)
		fg, bg := "37", "40"
		if selected == index {
			fg, bg = "30", "47"
		}
		termPrintf("\033[1;%s;%sm\033[%d;%dH%s", fg, bg, itemY+1, x+2, crop(names[index], max(0, w-3)))
	}
	mode := pageNumberModeLabel(masterPageNumberModeAt(masters, selected), selected > 0)
	drawAdaptiveToolbarLine(width, height, "43",
		legacyToolbarSegments(" MASTER VIEW  ↑/↓ select  Enter edit  v properties  # page number  n new  c clone  r rename  d delete  Shift-↑/↓ reorder  e effect  b background  Esc close "),
		[]toolbarSegment{{Long: "MASTER · " + masterNameAt(masters, selected) + " · Page number: " + mode, Short: "MASTER · # " + mode, Required: true}},
	)
}

func masterPageNumberModeAt(masters MasterDeck, selected int) string {
	if selected <= 0 {
		return masters.Base.Slide.PageNumber
	}
	if selected-1 < len(masters.Layouts) {
		return masters.Layouts[selected-1].Slide.PageNumber
	}
	return ""
}

func masterNavigatorIndexAtPoint(x, y, selected, count, width, height int) (int, bool) {
	panelX, panelY, panelW, panelH := slideNavigatorRect(width, height)
	if x < panelX || x >= panelX+panelW || y < panelY || y >= panelY+panelH {
		return 0, false
	}
	itemH := slideNavigatorItemHeight()
	visible := max(1, panelH/itemH)
	scroll := max(0, min(selected, count-visible))
	index := scroll + (y-panelY)/itemH
	return index, index >= 0 && index < count
}

func readMasterViewKeyEvent() KeyEvent {
	var buf [64]byte
	n, _ := os.Stdin.Read(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if event := shiftArrowEvent(b); event.Action != "" {
		return event
	}
	if event := editEscapeEvent(b); event.Action != "" {
		return event
	}
	switch {
	case bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}):
		return KeyEvent{Action: "enter"}
	case bytes.Contains(b, []byte{'n'}):
		return KeyEvent{Action: "new"}
	case bytes.Contains(b, []byte{'c'}):
		return KeyEvent{Action: "clone"}
	case bytes.Contains(b, []byte{'r'}):
		return KeyEvent{Action: "rename"}
	case bytes.Contains(b, []byte{'d'}):
		return KeyEvent{Action: "delete"}
	case bytes.Contains(b, []byte{'e'}):
		return KeyEvent{Action: "effect-picker"}
	case bytes.Contains(b, []byte{'b'}):
		return KeyEvent{Action: "background-picker"}
	case bytes.Contains(b, []byte{'#'}):
		return KeyEvent{Action: "page-number"}
	case bytes.Contains(b, []byte{'v'}):
		return KeyEvent{Action: "visual-properties"}
	case bytes.Contains(b, []byte{'q'}):
		return KeyEvent{Action: "quit"}
	default:
		return KeyEvent{}
	}
}
