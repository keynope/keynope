package main

import (
	"net/url"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	activityLayoutID     = "activity"
	activityTitleRole    = "activity-title"
	activityQRCodeRole   = "activity-qr"
	activityURLRole      = "activity-url"
	activityTitleSlotID  = "activity-title"
	activityQRCodeSlotID = "activity-qr"
	activityURLSlotID    = "activity-url"
)

func defaultActivityMaster() MasterLayout {
	return MasterLayout{
		ID:   activityLayoutID,
		Name: "Activity",
		Slide: Slide{
			PageNumber: pageNumberHide,
			Elements: []Element{
				{
					Kind: "heading", Level: 2, Text: "Activity",
					ID: activityTitleSlotID, SlotID: activityTitleSlotID,
					PlaceholderRole: activityTitleRole,
					Query:           "top=3&align=center&header=%23ffff55",
				},
				{
					Kind: "code", Text: "QR",
					ID: activityQRCodeSlotID, SlotID: activityQRCodeSlotID,
					PlaceholderRole: activityQRCodeRole,
					Query:           "top=12&align=center&fg=%23000000&bg=%23ffffff",
				},
				{
					Kind: "text", Text: "https://keynope.sh/join/",
					ID: activityURLSlotID, SlotID: activityURLSlotID,
					PlaceholderRole: activityURLRole,
					Query:           "bottom=3&align=center&fg=%2355aaff&link=https%3A%2F%2Fkeynope.sh%2Fjoin%2F",
				},
			},
		},
	}
}

func activityRoleElement(role string) (Element, bool) {
	master := defaultActivityMaster()
	for _, element := range master.Slide.Elements {
		if element.PlaceholderRole == role {
			return element, true
		}
	}
	return Element{}, false
}

// Apply the deck preference to legacy Activity-master elements as well as
// expose it to the presentation HTML. Never mutate the authored master.
func (deck Deck) applyActivityQRSetting(slide Slide) Slide {
	slide.ThemeColors = deck.themeColors(deck.AppearanceMode())
	slide.HideActivityQR = deck.HideActivityQR
	if !deck.HideActivityQR || slide.Engagement == nil {
		return slide
	}
	slide.Elements = append([]Element(nil), slide.Elements...)
	for index, element := range slide.Elements {
		if element.PlaceholderRole != activityQRCodeRole {
			continue
		}
		element.Kind = "text"
		element.Text = "https://keynope.sh/join/"
		if slide.Engagement.Code != "" {
			element.Text += "\nCode: " + slide.Engagement.Code
		}
		element.Query = "align=center&valign=middle&render=truetype&fg=%2355aaff"
		slide.Elements[index] = element
	}
	return slide
}

func protectedActivityElement(element Element) bool {
	switch element.PlaceholderRole {
	case activityTitleRole, activityQRCodeRole, activityURLRole:
		return true
	default:
		return false
	}
}

func activitySlideForDeck(deck Deck, current int, definition *EngagementDefinition) Slide {
	slide := Slide{LayoutID: activityLayoutID, Engagement: cloneEngagement(definition), PageNumber: pageNumberHide}
	layout, ok := deck.Masters.Layout(activityLayoutID)
	if !ok || slideHasAppearance(layout.Slide) || current < 0 || current >= len(deck.Slides) {
		return slide
	}
	appearance := deck.ResolveSlide(current, false)
	slide.Effect, slide.EffectSet = appearance.Effect, appearance.EffectSet
	slide.Background, slide.BackgroundSet = appearance.Background, appearance.BackgroundSet
	slide.FG, slide.FGSet = appearance.FG, appearance.FGSet
	slide.BG, slide.BGSet = appearance.BG, appearance.BGSet
	slide.HeaderFG, slide.HeaderFGSet = appearance.HeaderFG, appearance.HeaderFGSet
	return slide
}

func slideHasAppearance(slide Slide) bool {
	return slide.EffectSet || slide.Effect != "" || slide.BackgroundSet || slide.Background != "" ||
		slide.FGSet || slide.FG != "" || slide.BGSet || slide.BG != "" || slide.HeaderFGSet || slide.HeaderFG != ""
}

func activityElementText(role string, definition *EngagementDefinition) string {
	if definition == nil {
		return ""
	}
	switch role {
	case activityTitleRole:
		names := map[string]string{
			"shuffle":     "Shuffle",
			"pressure":    "Pressure Cooker",
			"deducer":     "Deducer",
			"finalanswer": "Final Answer",
			"nominate":    "Nominate",
			"chosen":      "The Chosen",
			"onboarding":  "Onboarding",
			"pulse":       "Pulse", "storm": "Storm", "sort": "Sort", "dual": "Dual Response",
			"quiz": "Quiz", "truefalse": "Fact or Fiction", "match": "Mix & Match",
			"questions": "Questions", "wall": "Feedback Wall", "draw": "Draw Yourself",
			"introduction": "Introduction",
			"pair":         "Pair Share", "expertise": "Expertise Map", "cards": "Playing Cards", "impostor": "Impostor",
		}
		if name := names[definition.Kind]; name != "" {
			return name
		}
		return "Activity"
	case activityQRCodeRole:
		return activityQRCodeText(activityJoinURL(definition))
	case activityURLRole:
		return activityJoinURL(definition)
	default:
		return ""
	}
}

func resolveActivityElement(element Element, definition *EngagementDefinition) Element {
	element.Text = activityElementText(element.PlaceholderRole, definition)
	switch element.PlaceholderRole {
	case activityTitleRole:
		element.Kind = "heading"
		element.Level = 2
	case activityQRCodeRole:
		element.Kind = "code"
		values, _ := url.ParseQuery(element.Query)
		values.Set("qr", "1")
		element.Query = encodeQueryStable(values)
	case activityURLRole:
		element.Kind = "text"
		values, _ := url.ParseQuery(element.Query)
		values.Set("link", activityJoinURL(definition))
		element.Query = encodeQueryStable(values)
	}
	return element
}

// activityQRCodeText renders each QR module as a two-column full block (or
// space). A terminal cell is roughly twice as tall as it is wide, so this makes
// every module optically square while doubling both axes compared with the
// compact half-block representation. Keep one module of the encoder's quiet
// zone; the QR code block itself supplies no additional padding.
func activityQRCodeText(value string) string {
	if value == "" {
		return ""
	}
	code, err := qrcode.New(value, qrcode.Medium)
	if err != nil {
		return "[QR]"
	}
	bitmap := cropQRCodeQuietZone(code.Bitmap(), 1)
	var out strings.Builder
	for row := range bitmap {
		if row > 0 {
			out.WriteByte('\n')
		}
		for col := range bitmap[row] {
			if bitmap[row][col] {
				out.WriteString("██")
			} else {
				out.WriteString("  ")
			}
		}
	}
	return out.String()
}

func cropQRCodeQuietZone(bitmap [][]bool, quiet int) [][]bool {
	if len(bitmap) == 0 || len(bitmap[0]) == 0 {
		return bitmap
	}
	minRow, minCol := len(bitmap), len(bitmap[0])
	maxRow, maxCol := -1, -1
	for row := range bitmap {
		for col, dark := range bitmap[row] {
			if !dark {
				continue
			}
			minRow, maxRow = min(minRow, row), max(maxRow, row)
			minCol, maxCol = min(minCol, col), max(maxCol, col)
		}
	}
	if maxRow < 0 || maxCol < 0 {
		return bitmap
	}
	minRow, minCol = max(0, minRow-quiet), max(0, minCol-quiet)
	maxRow, maxCol = min(len(bitmap)-1, maxRow+quiet), min(len(bitmap[0])-1, maxCol+quiet)
	cropped := make([][]bool, 0, maxRow-minRow+1)
	for row := minRow; row <= maxRow; row++ {
		cropped = append(cropped, append([]bool(nil), bitmap[row][minCol:maxCol+1]...))
	}
	return cropped
}
