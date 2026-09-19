package main

import (
	"net/url"
	"reflect"
	"testing"
)

func TestScenePaintReusesParsedEffects(t *testing.T) {
	for _, query := range []string{
		"",
		"gradient-start=%23ff0055&gradient-end=%23ffffaa&gradient-dir=diagonal&shadow=soft&shadow-color=%2355aaff&shadow-x=-2&shadow-y=3&outline=light&modern-opacity=0.4",
		"gradient-start=invalid&gradient-end=%23ffffff&shadow=solid&shadow-color=invalid&shadow-x=100&shadow-y=-100",
		"gradient-start=%23000000&gradient-end=%23ffffff&shadow=solid&broken=%XX",
	} {
		t.Run(query, func(t *testing.T) {
			e := Element{Kind: "text", Query: query}
			q, err := url.ParseQuery(query)
			got := scenePaintFromValues(e, Slide{}, 2, 3, q, err)
			if !reflect.DeepEqual(got, scenePaintFor(e, Slide{}, 2, 3)) {
				t.Fatal("parsed paint differs from standalone paint")
			}
			gradient, hasGradient := parseElementTextGradient(query)
			if hasGradient != (got.GradientStart != "") || hasGradient && got.GradientDirection != gradient.direction {
				t.Fatal("gradient parsing semantics changed")
			}
			shadow, hasShadow := parseElementTextShadow(query)
			if hasShadow != (got.ShadowColor != "") || hasShadow && (got.ShadowX != float64(shadow.x)*2 || got.ShadowY != float64(shadow.y)*3) {
				t.Fatal("shadow parsing semantics changed")
			}
			if err != nil && (got.ShadowColor != "" || got.GradientStart != "") {
				t.Fatal("malformed query enabled effects")
			}
		})
	}
}
