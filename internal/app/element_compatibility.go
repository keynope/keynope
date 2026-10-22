package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const maxCompatibleElementBytes = 1 << 20
const maxCompatibleElementArchiveBytes = 16 << 20

var compatibleElementRE = regexp.MustCompile(`^<!--\s*keynope-element\s+version=([0-9]+)\s+base64:([A-Za-z0-9+/=]+)\s*-->$`)
var compatibleElementPrefixRE = regexp.MustCompile(`^<!--\s*keynope-element(?:\s|-->|$)`)

func standardElementKind(kind string) bool {
	switch kind {
	case "text", "heading", "bullet", "code", "text-image", "page-number", "image", "video", "shape", "connector", "table", "chart":
		return true
	}
	return false
}

func elementNeedsCompatibilityEnvelope(element Element) bool {
	// Page-number objects are understood by the scene but have no ordinary
	// Markdown spelling. The full envelope preserves their semantic kind too.
	return !standardElementKind(element.Kind) || element.Kind == "page-number" || element.Kind == "video"
}

func encodeCompatibleElement(element Element) (string, error) {
	remaining := maxCompatibleElementBytes
	return encodeCompatibleElementBudget(element, &remaining)
}

func encodeCompatibleElementBudget(element Element, remaining *int) (string, error) {
	if element.Kind == "video" && element.AssetID != "" {
		element.Path = "keynope-asset:" + element.AssetID + ".png"
	}
	data, err := json.Marshal(element)
	if err != nil || len(data) > maxCompatibleElementBytes || strings.TrimSpace(element.Kind) == "" || len(element.Kind) > 128 {
		return "", fmt.Errorf("compatible element metadata is invalid or exceeds 1 MiB")
	}
	if len(data) > *remaining {
		return "", fmt.Errorf("compatible element metadata exceeds 16 MiB per document")
	}
	*remaining -= len(data)
	return "<!-- keynope-element version=1 base64:" + base64.StdEncoding.EncodeToString(data) + " -->", nil
}

func decodeCompatibleElement(text string) (Element, int, error) {
	match := compatibleElementRE.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil || match[1] != "1" {
		return Element{}, 0, fmt.Errorf("invalid or unsupported compatible element metadata version")
	}
	if len(match[2]) > base64.StdEncoding.EncodedLen(maxCompatibleElementBytes) {
		return Element{}, 0, fmt.Errorf("compatible element metadata exceeds 1 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(match[2])
	if err != nil || len(data) > maxCompatibleElementBytes {
		return Element{}, 0, fmt.Errorf("invalid or oversized compatible element metadata")
	}
	var element Element
	if err = json.Unmarshal(data, &element); err != nil || strings.TrimSpace(element.Kind) == "" || len(element.Kind) > 128 {
		return Element{}, 0, fmt.Errorf("compatible element metadata needs a valid element kind")
	}
	return element, len(data), nil
}

// parseSlide has a legacy non-error signature. Validate document envelopes
// before it can consume them, so malformed/future metadata fails explicitly.
func validateCompatibleElementMetadata(text string) error {
	remaining := maxCompatibleElementArchiveBytes
	fence := markdownFenceState{}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence.boundary(trimmed) {
			continue
		}
		if fence.width > 0 || !compatibleElementPrefixRE.MatchString(trimmed) {
			continue
		}
		_, size, err := decodeCompatibleElement(trimmed)
		if err != nil {
			return err
		}
		if size > remaining {
			return fmt.Errorf("compatible element metadata exceeds 16 MiB per document")
		}
		remaining -= size
	}
	return nil
}
