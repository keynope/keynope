package main

type visualProperty int

const (
	visualForeground visualProperty = iota
	visualTerminalBackground
	visualHeader
	visualBackgroundPattern
	visualEffect
)

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
