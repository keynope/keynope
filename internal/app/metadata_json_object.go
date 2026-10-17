package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Inspect one authored object scope, leaving nested values opaque. In
// particular, unknown extension objects must not be interpreted recursively.
// Known field aliases follow encoding/json's case-insensitive field matching;
// registry IDs remain case sensitive because they are map keys, not fields.
func decodeMetadataJSONObject(data []byte, known ...string) (map[string]json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, fmt.Errorf("metadata needs a JSON object")
	}
	fields := map[string]json.RawMessage{}
	knownFold := make(map[string]string, len(known))
	for _, key := range known {
		knownFold[strings.ToLower(key)] = key
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("invalid metadata property")
		}
		canonical := key
		if value, ok := knownFold[strings.ToLower(key)]; ok {
			canonical = value
		} else if strings.IndexFunc(key, func(r rune) bool { return r > 127 }) >= 0 {
			for _, candidate := range known {
				if strings.EqualFold(key, candidate) {
					canonical = candidate
					break
				}
			}
		}
		if _, exists := fields[canonical]; exists {
			return nil, fmt.Errorf("ambiguous metadata property %q", canonical)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[canonical] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return nil, fmt.Errorf("unterminated metadata object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after metadata object")
	}
	return fields, nil
}
