package app

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const maxAuthoredCanvasAxis = 4096
const maxAuthoredCanvasCells = 1 << 20

type scalarMetadataSetting struct {
	name         string
	prefix, full *regexp.Regexp
}

func scalarSetting(name, namespace string, full *regexp.Regexp) scalarMetadataSetting {
	return scalarMetadataSetting{name, regexp.MustCompile(`<!--\s*` + regexp.QuoteMeta(namespace) + `(?:=|\s|-->|$)`), full}
}

// Scalar settings have one value per owning scope. Never take the first value
// and then remove the rest. Scoped callers also track ownership across lines.
func (setting scalarMetadataSetting) read(text string) ([]string, string, error) {
	markers := findDocumentMetadataIndices(text, setting.prefix, 2)
	if len(markers) == 0 {
		return nil, text, nil
	}
	if len(markers) > 1 {
		return nil, text, fmt.Errorf("duplicate %s metadata", setting.name)
	}
	matches := findDocumentMetadataIndices(text, setting.full, 2)
	if len(matches) != 1 || matches[0][0] != markers[0][0] {
		return nil, text, fmt.Errorf("malformed %s metadata", setting.name)
	}
	indices := matches[0]
	if len(strings.TrimSpace(text[indices[0]:indices[1]])) > 256 {
		return nil, text, fmt.Errorf("%s metadata exceeds 256 bytes", setting.name)
	}
	match := make([]string, len(indices)/2)
	for i := range match {
		if indices[2*i] >= 0 {
			match[i] = text[indices[2*i]:indices[2*i+1]]
		}
	}
	return match, text[:indices[0]] + text[indices[1]:], nil
}

var canvasMetadata = scalarSetting("canvas", "keynope", keynopeMetaRE)
var activityQRMetadata = scalarSetting("activity QR", "keynope-activity-qr", activityQRSettingRE)
var slideIdentityMetadata = scalarSetting("slide identity", "keynope-slide-id", slideIdentityRE)
var slideTabMetadata = scalarSetting("slide tab", "keynope-tab", slideTabRE)
var slideLayoutMetadata = scalarSetting("slide layout", "layout", layoutRE)
var slidePageNumberMetadata = scalarSetting("page number", "page-number", pageNumberRE)
var slideEffectMetadata = scalarSetting("slide effect", "effect", effectRE)
var slideBackgroundMetadata = scalarSetting("slide background", "background", backgroundRE)

func validateAuthoredCanvas(width, height int) error {
	if width < 1 || height < 1 || width > maxAuthoredCanvasAxis || height > maxAuthoredCanvasAxis || int64(width)*int64(height) > maxAuthoredCanvasCells {
		return fmt.Errorf("canvas must have positive dimensions up to %d per axis and %d cells total", maxAuthoredCanvasAxis, maxAuthoredCanvasCells)
	}
	return nil
}

func decodeCanvasMetadata(text string) (int, int, string, error) {
	match, remaining, err := canvasMetadata.read(text)
	if err != nil || match == nil {
		return 0, 0, text, err
	}
	width, widthErr := strconv.Atoi(match[1])
	height, heightErr := strconv.Atoi(match[2])
	if widthErr != nil || heightErr != nil {
		return 0, 0, text, fmt.Errorf("canvas dimensions are not representable integers")
	}
	if err := validateAuthoredCanvas(width, height); err != nil {
		return 0, 0, text, err
	}
	return width, height, remaining, nil
}
