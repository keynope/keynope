package app

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validObjectName(name string) bool {
	if utf8.RuneCountInString(name) > 128 || !utf8.ValidString(name) {
		return false
	}
	for _, char := range name {
		if unicode.IsControl(char) || char == '\u2028' || char == '\u2029' {
			return false
		}
	}
	return true
}

func objectName(element Element) string {
	q, _ := url.ParseQuery(element.Query)
	return objectNameFromValues(q)
}

func objectNameFromValues(q url.Values) string {
	name := q.Get("object-name")
	if !validObjectName(name) {
		return ""
	}
	return strings.TrimSpace(name)
}

func validateObjectNameChange(original, updated Element) error {
	before, _ := url.ParseQuery(original.Query)
	after, _ := url.ParseQuery(updated.Query)
	if before.Get("object-name") != after.Get("object-name") && (!validObjectName(after.Get("object-name")) || len(after["object-name"]) > 1) {
		return fmt.Errorf("object names must be one line and at most 128 characters")
	}
	return nil
}
