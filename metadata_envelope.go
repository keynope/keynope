package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
)

// A word boundary also matches '-' and '.', which may belong to a different
// comment namespace. Keep legacy '=' assignments within the known owner so
// malformed actual metadata is still rejected or intentionally recovered.
func metadataNamespaceEnvelopeRE(namespace string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^<!--\s*` + regexp.QuoteMeta(namespace) + `(?:\s|=|-->|$)[^\r\n]*(?:\r?\n|$)`)
}

// Versioned base64 archives have one owner within their document/slide scope.
// Match the exact namespace (not a hyphenated extension namespace), and bound
// matches before decoding so duplicates cannot silently discard metadata.
// Callers validate JSON or decompress the returned bounded payload separately.
type singletonMetadataEnvelope struct {
	name         string
	limit        int
	prefix, full *regexp.Regexp
}

func newSingletonMetadataEnvelope(name string, limit int) singletonMetadataEnvelope {
	namespace := regexp.QuoteMeta("keynope-" + name)
	label := name
	if name == "masters" {
		label = "master deck"
	}
	return singletonMetadataEnvelope{
		name: label, limit: limit,
		prefix: regexp.MustCompile(`(?m)^<!--\s*` + namespace + `(?:\s|-->|$)`),
		full:   regexp.MustCompile(`(?m)^<!--\s*` + namespace + `\s+version=([0-9]+)\s+base64:([A-Za-z0-9+/=]+)\s*-->[\t ]*\r?\n?`),
	}
}

func (envelope singletonMetadataEnvelope) decode(text string) ([]byte, string, error) {
	markers := findDocumentMetadataIndices(text, envelope.prefix, 2)
	if len(markers) == 0 {
		return nil, text, nil
	}
	if len(markers) != 1 {
		return nil, text, fmt.Errorf("duplicate %s metadata", envelope.name)
	}
	matches := findDocumentMetadataIndices(text, envelope.full, 2)
	if len(matches) != 1 || matches[0][0] != markers[0][0] {
		return nil, text, fmt.Errorf("malformed %s metadata", envelope.name)
	}
	match := matches[0]
	if text[match[2]:match[3]] != "1" {
		return nil, text, fmt.Errorf("unsupported %s version", envelope.name)
	}
	encoded := text[match[4]:match[5]]
	if len(encoded) > base64.StdEncoding.EncodedLen(envelope.limit) {
		return nil, text, fmt.Errorf("%s metadata exceeds %d bytes", envelope.name, envelope.limit)
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(payload) > envelope.limit {
		return nil, text, fmt.Errorf("invalid or oversized %s metadata", envelope.name)
	}
	return payload, text[:match[0]] + text[match[1]:], nil
}

const maxMasterMetadataBytes = 16 << 20
const maxTabMetadataBytes = 16 << 20

var masterMetadataEnvelope = newSingletonMetadataEnvelope("masters", maxMasterMetadataBytes)
var tabMetadataEnvelope = newSingletonMetadataEnvelope("tabs", maxTabMetadataBytes)
var documentExtraEnvelope = newSingletonMetadataEnvelope("document-extra", maxJSONExtensionBytes)
var slideExtraEnvelope = newSingletonMetadataEnvelope("slide-extra", maxJSONExtensionBytes)
var elementExtraEnvelope = newSingletonMetadataEnvelope("element-extra", maxJSONExtensionBytes)
var engagementMetadataEnvelope = newSingletonMetadataEnvelope("engagement", maxEngagementMetadataBytes)
var engagementResultsEnvelope = newSingletonMetadataEnvelope("engagement-results", maxEngagementResultBytes*3/2)
var backgroundMetadataEnvelope = newSingletonMetadataEnvelope("background-media", maxJSONExtensionBytes)

func (envelope singletonMetadataEnvelope) decodeComment(line string) ([]byte, error) {
	payload, remaining, err := envelope.decode(line)
	if err != nil {
		return nil, err
	}
	if payload == nil || len(bytes.TrimSpace([]byte(remaining))) != 0 {
		return nil, fmt.Errorf("%s metadata needs one envelope", envelope.name)
	}
	return payload, nil
}

func decodeScopedExtensions(line string, envelope singletonMetadataEnvelope) (*jsonExtensions, error) {
	payload, err := envelope.decodeComment(line)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(bytes.TrimSpace(payload), []byte("{")) {
		return nil, fmt.Errorf("%s metadata needs one JSON object envelope", envelope.name)
	}
	return decodeExtensionPayload(payload)
}

func decodeDocumentExtensions(text string) (*jsonExtensions, string, error) {
	payload, remaining, err := documentExtraEnvelope.decode(text)
	if err != nil || payload == nil {
		return nil, text, err
	}
	// Reuse field validation and ambiguity handling; absence differs from null.
	if !bytes.HasPrefix(bytes.TrimSpace(payload), []byte("{")) {
		return nil, text, fmt.Errorf("document extensions need a JSON object")
	}
	extra, err := decodeExtensionPayload(payload)
	if err != nil {
		return nil, text, err
	}
	return extra, remaining, nil
}
