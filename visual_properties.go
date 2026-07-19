package main

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"
)

type visualProperty int

const (
	visualForeground visualProperty = iota
	visualTerminalBackground
	visualHeader
	visualBackgroundPattern
	visualEffect
)

var visualProperties = []struct {
	Property visualProperty
	Name     string
}{
	{visualForeground, "Foreground color"},
	{visualTerminalBackground, "Terminal background color"},
	{visualHeader, "Header color"},
	{visualBackgroundPattern, "Background pattern"},
	{visualEffect, "Effect"},
}

func playVisualProperties(source Slide, allowInherit bool, resolve func(Slide) Slide, width, height int) (Slide, bool) {
	if resolve == nil {
		resolve = func(slide Slide) Slide { return slide }
	}
	original := cloneSlide(source)
	working := cloneSlide(source)
	selected := 0
	committed := false
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalSize()
			preview := resolve(working)
			renderer.draw(preview, width, height, 0, frame, func(lines []Line) {
				drawVisualPropertiesPanel(working, allowInherit, selected, width, height)
			})
		},
		Read: readEffectPickerKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "escape":
				if reflect.DeepEqual(original, working) {
					return overlayDecision{Disposition: overlayCancel}
				}
				save, decided := playVisualSaveConfirmation(resolve(working), width, height)
				if !decided {
					return overlayDecision{Disposition: overlayContinue}
				}
				if save {
					committed = true
					return overlayDecision{Disposition: overlayCommit}
				}
				return overlayDecision{Disposition: overlayCancel}
			case "up", "left":
				selected = (selected - 1 + len(visualProperties) + 1) % (len(visualProperties) + 1)
			case "down", "right":
				selected = (selected + 1) % (len(visualProperties) + 1)
			case "enter":
				if selected == len(visualProperties) {
					committed = true
					return overlayDecision{Disposition: overlayCommit}
				}
				property := visualProperties[selected].Property
				if updated, ok := editVisualProperty(working, property, allowInherit, resolve, width, height); ok {
					working = updated
				}
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	if decision.Disposition != overlayCommit || !committed || reflect.DeepEqual(original, working) {
		return original, false
	}
	return working, true
}

func playVisualSaveConfirmation(preview Slide, width, height int) (bool, bool) {
	selected := 0
	result := false
	decided := false
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalSize()
			renderer.draw(preview, width, height, 0, frame, func(lines []Line) {
				drawSimpleChoicePanel("Save visual changes? [Y/n]", []string{"Yes", "No"}, selected, width, height, "enter/y save  n discard  esc return")
			})
		},
		Read: readVisualConfirmationKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape", "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "yes":
				result, decided = true, true
				return overlayDecision{Disposition: overlayCommit}
			case "no":
				result, decided = false, true
				return overlayDecision{Disposition: overlayCommit}
			case "up", "left":
				selected = (selected - 1 + 2) % 2
			case "down", "right":
				selected = (selected + 1) % 2
			case "enter":
				result, decided = selected == 0, true
				return overlayDecision{Disposition: overlayCommit}
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	if decision.Disposition != overlayCommit {
		return false, false
	}
	return result, decided
}

func readVisualConfirmationKeyEvent() KeyEvent {
	var buf [32]byte
	n, _ := os.Stdin.Read(buf[:])
	if n == 0 {
		return KeyEvent{}
	}
	b := buf[:n]
	if event := parseMouseEvent(b); event.Action != "" {
		return event
	}
	if event := editEscapeEvent(b); event.Action != "" {
		return event
	}
	if bytes.Contains(b, []byte{'\r'}) || bytes.Contains(b, []byte{'\n'}) {
		return KeyEvent{Action: "enter"}
	}
	if bytes.ContainsAny(b, "yY") {
		return KeyEvent{Action: "yes"}
	}
	if bytes.ContainsAny(b, "nN") {
		return KeyEvent{Action: "no"}
	}
	if bytes.Contains(b, []byte{'q'}) {
		return KeyEvent{Action: "quit"}
	}
	return KeyEvent{}
}

func editVisualProperty(source Slide, property visualProperty, allowInherit bool, resolve func(Slide) Slide, width, height int) (Slide, bool) {
	mode, ok := playVisualPropertyMode(source, property, allowInherit, resolve(source), width, height)
	if !ok {
		return source, false
	}
	switch mode {
	case "inherit":
		setVisualProperty(&source, property, false, "")
		return source, true
	case "none":
		setVisualProperty(&source, property, true, "")
		return source, true
	}
	preview := resolve(source)
	switch property {
	case visualEffect:
		value, picked := playEffectPicker(preview, width, height)
		if !picked {
			return source, false
		}
		if value == "none" {
			value = ""
		}
		setVisualProperty(&source, property, true, value)
	case visualBackgroundPattern:
		value, picked := playBackgroundPicker(preview, width, height)
		if !picked {
			return source, false
		}
		if value == "none" {
			value = ""
		}
		setVisualProperty(&source, property, true, value)
	default:
		current := visualPropertyResolvedColor(preview, property)
		value, picked := playSlideColorPicker(preview, current, width, height)
		if !picked {
			return source, false
		}
		setVisualProperty(&source, property, true, value)
	}
	return source, true
}

func playVisualPropertyMode(source Slide, property visualProperty, allowInherit bool, preview Slide, width, height int) (string, bool) {
	choices := []string{"None", "Choose value"}
	values := []string{"none", "choose"}
	if allowInherit {
		choices = append([]string{"Inherit"}, choices...)
		values = append([]string{"inherit"}, values...)
	}
	selected := 0
	current := visualPropertyMode(source, property, allowInherit)
	for index, value := range values {
		if value == current {
			selected = index
			break
		}
	}
	result := ""
	renderer := &liveSlideRenderer{}
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) {
			width, height = terminalSize()
			renderer.draw(preview, width, height, 0, frame, func(lines []Line) {
				drawSimpleChoicePanel(visualPropertyName(property), choices, selected, width, height, "arrows select  enter confirm  esc cancel")
			})
		},
		Read: readEffectPickerKeyEvent,
		Handle: func(event KeyEvent) overlayDecision {
			switch event.Action {
			case "escape", "quit":
				return overlayDecision{Disposition: overlayCancel}
			case "up", "left":
				selected = (selected - 1 + len(choices)) % len(choices)
			case "down", "right":
				selected = (selected + 1) % len(choices)
			case "enter":
				result = values[selected]
				return overlayDecision{Disposition: overlayCommit}
			}
			return overlayDecision{Disposition: overlayContinue}
		},
	})
	return result, decision.Disposition == overlayCommit
}

func drawVisualPropertiesPanel(source Slide, allowInherit bool, selected, width, height int) {
	panelW := min(max(58, width/3), max(32, width-4))
	panelH := min(height-2, len(visualProperties)+7)
	left := max(0, (width-panelW)/2)
	top := max(0, (height-panelH)/2)
	for row := 0; row < panelH; row++ {
		termPrintf("\033[0;37;40m\033[%d;%dH%s", top+row+1, left+1, strings.Repeat(" ", panelW))
	}
	termPrintf("\033[1;37;40m\033[%d;%dH%s", top+2, left+3, crop("Visual properties", max(0, panelW-5)))
	for index, property := range visualProperties {
		fg, bg, prefix := "37", "40", "  "
		if selected == index {
			fg, bg, prefix = "30", "47", "> "
		}
		label := fmt.Sprintf("%-28s %s", property.Name, visualPropertyLabel(source, property.Property, allowInherit))
		termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, bg, top+4+index, left+3, padRight(crop(prefix+label, max(0, panelW-6)), max(0, panelW-6)))
	}
	separatorRow := top + 4 + len(visualProperties)
	termPrintf("\033[0;90;40m\033[%d;%dH%s", separatorRow, left+3, strings.Repeat("-", max(0, panelW-6)))
	fg, bg, prefix := "37", "40", "  "
	if selected == len(visualProperties) {
		fg, bg, prefix = "30", "47", "> "
	}
	termPrintf("\033[0;%s;%sm\033[%d;%dH%s", fg, bg, separatorRow+1, left+3, padRight(crop(prefix+"Close menu", max(0, panelW-6)), max(0, panelW-6)))
	termPrintf("\033[0;90;40m\033[%d;%dH%s", top+panelH-1, left+3, crop("arrows select  enter open  esc cancel", max(0, panelW-5)))
}

func visualPropertyName(property visualProperty) string {
	for _, candidate := range visualProperties {
		if candidate.Property == property {
			return candidate.Name
		}
	}
	return "Visual property"
}

func visualPropertyMode(slide Slide, property visualProperty, allowInherit bool) string {
	set, value := visualPropertyValue(slide, property)
	if !set && allowInherit {
		return "inherit"
	}
	if !set || value == "" {
		return "none"
	}
	return "choose"
}

func visualPropertyLabel(slide Slide, property visualProperty, allowInherit bool) string {
	set, value := visualPropertyValue(slide, property)
	if !set && allowInherit {
		return "Inherit"
	}
	if !set || value == "" {
		return "None"
	}
	switch property {
	case visualForeground, visualTerminalBackground, visualHeader:
		return ansiCodeName(value, property == visualTerminalBackground)
	default:
		return value
	}
}

func visualPropertyValue(slide Slide, property visualProperty) (bool, string) {
	switch property {
	case visualForeground:
		return slide.FGSet || slide.FG != "", slide.FG
	case visualTerminalBackground:
		return slide.BGSet || slide.BG != "", slide.BG
	case visualHeader:
		return slide.HeaderFGSet || slide.HeaderFG != "", slide.HeaderFG
	case visualBackgroundPattern:
		return slide.BackgroundSet || slide.Background != "", slide.Background
	case visualEffect:
		return slide.EffectSet || slide.Effect != "", slide.Effect
	default:
		return false, ""
	}
}

func setVisualProperty(slide *Slide, property visualProperty, set bool, value string) {
	if slide == nil {
		return
	}
	switch property {
	case visualForeground:
		slide.FGSet, slide.FG = set, visualColorCode(value, false)
	case visualTerminalBackground:
		slide.BGSet, slide.BG = set, visualColorCode(value, true)
	case visualHeader:
		slide.HeaderFGSet, slide.HeaderFG = set, visualColorCode(value, false)
	case visualBackgroundPattern:
		slide.BackgroundSet, slide.Background = set, value
	case visualEffect:
		slide.EffectSet, slide.Effect = set, value
	}
	if !set {
		switch property {
		case visualForeground:
			slide.FG = ""
		case visualTerminalBackground:
			slide.BG = ""
		case visualHeader:
			slide.HeaderFG = ""
		case visualBackgroundPattern:
			slide.Background = ""
		case visualEffect:
			slide.Effect = ""
		}
	}
}

func visualColorCode(value string, background bool) string {
	if value == "" {
		return ""
	}
	if background {
		if code, ok := ansiBG(value); ok {
			return code
		}
	} else if code, ok := ansiFG(value); ok {
		return code
	}
	return value
}

func visualPropertyResolvedColor(slide Slide, property visualProperty) string {
	code := slide.FG
	switch property {
	case visualTerminalBackground:
		code = slide.BG
	case visualHeader:
		code = slideHeaderFG(slide)
	}
	if code == "" {
		if property == visualTerminalBackground {
			code = slideBG(slide)
		} else {
			code = slideFG(slide)
		}
	}
	return ansiCSSColour(code)
}
