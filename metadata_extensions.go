package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
)

// jsonExtensions retains fields written by a newer compatible Keynope. The
// application owns known fields, so an extension can never override one when
// the value is encoded again.
type jsonExtensions map[string]json.RawMessage

const maxJSONExtensionBytes = 1 << 20

var (
	documentExtensionsRE = regexp.MustCompile(`(?m)^<!--\s*keynope-document-extra\s+version=1\s+base64:([A-Za-z0-9+/=]+)\s*-->\s*`)
	slideExtensionsRE    = regexp.MustCompile(`<!--\s*keynope-slide-extra\s+version=1\s+base64:([A-Za-z0-9+/=]+)\s*-->`)
	elementExtensionsRE  = regexp.MustCompile(`<!--\s*keynope-element-extra\s+version=1\s+base64:([A-Za-z0-9+/=]+)\s*-->`)
)

func encodeExtensionComment(kind string, extra *jsonExtensions) (string, error) {
	if extra == nil || len(*extra) == 0 {
		return "", nil
	}
	data, err := json.Marshal(*extra)
	if err != nil || len(data) > maxJSONExtensionBytes {
		return "", fmt.Errorf("%s extensions are too large or invalid", kind)
	}
	return fmt.Sprintf("<!-- keynope-%s-extra version=1 base64:%s -->", kind, base64.StdEncoding.EncodeToString(data)), nil
}

func decodeExtensionComment(match []string) (*jsonExtensions, error) {
	if len(match) < 2 || len(match[1]) > maxJSONExtensionBytes*2 {
		return nil, fmt.Errorf("invalid extension metadata")
	}
	data, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil || len(data) > maxJSONExtensionBytes {
		return nil, fmt.Errorf("invalid extension metadata")
	}
	var extra jsonExtensions
	if err := json.Unmarshal(data, &extra); err != nil {
		return nil, err
	}
	for key, raw := range extra {
		if !validExtensionMetadataKey(key) || !json.Valid(raw) {
			return nil, fmt.Errorf("invalid extension field")
		}
	}
	return cloneJSONExtensions(&extra), nil
}

func decodeJSONExtensions(data []byte, known ...string) (*jsonExtensions, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, key := range known {
		delete(fields, key)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	copy := jsonExtensions(fields)
	return cloneJSONExtensions(&copy), nil
}

func encodeJSONExtensions(value any, extra *jsonExtensions) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || extra == nil || len(*extra) == 0 {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, raw := range *extra {
		if _, owned := fields[key]; owned || !json.Valid(raw) {
			continue
		}
		fields[key] = append(json.RawMessage(nil), raw...)
	}
	return json.Marshal(fields)
}

func cloneJSONExtensions(extra *jsonExtensions) *jsonExtensions {
	if extra == nil || len(*extra) == 0 {
		return nil
	}
	copy := make(jsonExtensions, len(*extra))
	for key, raw := range *extra {
		copy[key] = append(json.RawMessage(nil), raw...)
	}
	return &copy
}
