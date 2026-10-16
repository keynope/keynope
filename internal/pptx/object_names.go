package pptx

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func objectDisplayName(object Object) string {
	name := strings.TrimSpace(object.Name)
	if name == "" || !validObjectDisplayName(name) {
		return object.ID
	}
	return name
}

func validObjectDisplayName(name string) bool {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 {
		return false
	}
	for _, char := range name {
		if unicode.IsControl(char) || char == '\u2028' || char == '\u2029' {
			return false
		}
	}
	return true
}
