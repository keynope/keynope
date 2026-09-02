package main

import (
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
					Kind: "heading", Level: 1, Text: "Activity",
					ID: activityTitleSlotID, SlotID: activityTitleSlotID,
					PlaceholderRole: activityTitleRole,
					Query:           "top=3&align=center&header=%23ffff55",
				},
				{
					Kind: "code", Text: "QR",
					ID: activityQRCodeSlotID, SlotID: activityQRCodeSlotID,
					PlaceholderRole: activityQRCodeRole,
					Query:           "align=center&valign=middle&fg=%23000000&bg=%23ffffff",
				},
				{
					Kind: "text", Text: "https://keynope.sh/join/",
					ID: activityURLSlotID, SlotID: activityURLSlotID,
					PlaceholderRole: activityURLRole,
					Query:           "bottom=3&align=center&fg=%2355aaff",
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

func protectedActivityElement(element Element) bool {
	switch element.PlaceholderRole {
	case activityTitleRole, activityQRCodeRole, activityURLRole:
		return true
	default:
		return false
	}
}

func activityElementText(role string, definition *EngagementDefinition) string {
	if definition == nil {
		return ""
	}
	switch role {
	case activityTitleRole:
		if definition.Prompt != "" {
			return definition.Prompt
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

// activityQRCodeText maps two vertical QR modules to one Unicode block cell.
// Terminal cells are roughly twice as tall as they are wide, so ▀/▄/█ keeps
// the rendered modules close to square while retaining the QR quiet zone.
func activityQRCodeText(value string) string {
	if value == "" {
		return ""
	}
	code, err := qrcode.New(value, qrcode.Medium)
	if err != nil {
		return "[QR]"
	}
	bitmap := code.Bitmap()
	if len(bitmap)%2 != 0 {
		bitmap = append(bitmap, make([]bool, len(bitmap[0])))
	}
	var out strings.Builder
	for row := 0; row < len(bitmap); row += 2 {
		if row > 0 {
			out.WriteByte('\n')
		}
		for col := range bitmap[row] {
			top, bottom := bitmap[row][col], bitmap[row+1][col]
			switch {
			case top && bottom:
				out.WriteRune('█')
			case top:
				out.WriteRune('▀')
			case bottom:
				out.WriteRune('▄')
			default:
				out.WriteByte(' ')
			}
		}
	}
	return out.String()
}
