package main

import (
	"net/url"
	"reflect"
	"testing"
)

func TestSceneTextUsesProvidedTypographyValues(t *testing.T) {
	for _, themed := range []bool{false, true} {
		deck := Deck{}
		if themed {
			var err error
			deck.Appearance, err = deck.withTheme("studio-v1")
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, style := range []string{"retro", "modern"} {
			for _, kind := range []string{"heading", "text", "bullet", "code"} {
				for _, query := range []string{"", "ttf-size=193&ttf-width=75", "modern-size=42&modern-width=120&modern-line-height=2", "ttf-size=bad&ttf-width=NaN&broken=%ZZ", "ttf-weight=bold&text-align=justify&text-valign=middle"} {
					e := Element{Kind: kind, Level: 2, Text: "Hello [color=#ff0055]world[/color]", Query: query}
					want := deck.sceneTextForStyle(e, style)
					q, _ := url.ParseQuery(query)
					before := q.Encode()
					// The parsed-value path must not silently read a stale query.
					e.Query = "ttf-size=1&ttf-width=1"
					got := deck.sceneTextForStyleFromValues(e, style, q)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("theme=%t %s %s %s: parsed projection differs", themed, style, kind, query)
					}
					if q.Encode() != before {
						t.Fatal("projection mutated query values")
					}
				}
			}
		}
	}
}
