package pptx

// Read-only reference to an effective scheme within one Read result.
type ThemeColourReference struct {
	Slot       string
	SourcePart string
}

func themeReferenceSource(reference, part string) string {
	if reference != "" {
		return part
	}
	return ""
}

// Only unmodified, recognised scheme colours are directly representable as a
// live theme slot. Tint/shade/etc. remain concrete until a matching transform
// model exists; in particular, equal resulting RGB is not proof of inheritance.
func resolvedThemeColourSlot(paint *node, resolver colourResolver) string {
	colour := firstColourChild(paint)
	if colour == nil || colour.Name != "schemeClr" || len(colour.Children) != 0 {
		return ""
	}
	key := attr(colour, "val")
	if mapped := resolver.mapping[key]; mapped != "" {
		key = mapped
	}
	switch key {
	case "dk1", "lt1", "dk2", "lt2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6", "hlink", "folHlink":
		if _, _, _, valid := colourRGB(resolver.colours[key]); valid {
			return key
		}
	}
	return ""
}

func latinThemeRole(face string, resolver colourResolver) string {
	switch face {
	case "+mj-lt":
		if resolver.majorFont != "" {
			return "major"
		}
	case "+mn-lt":
		if resolver.minorFont != "" {
			return "minor"
		}
	}
	return ""
}
