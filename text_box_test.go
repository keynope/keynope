package main

import (
	"net/url"
	"testing"
)

func TestTextBoxWrapsWithoutChangingFontSize(t *testing.T) {
	for _, kind := range []string{"text", "heading", "bullet", "code"} {
		t.Run(kind, func(t *testing.T) {
			e := Element{Kind: kind, Level: 2, Text: "alpha beta gamma delta epsilon\nsecond line", Query: "render=text-image&source=bitmap&scale=1.00&text-size=5&text-box=1&width=100&height=200"}
			wide := renderElementRows(e, 245)
			q, _ := url.ParseQuery(e.Query)
			q.Set("width", "28")
			e.Query = q.Encode()
			narrow := renderElementRows(e, 245)
			if len(narrow) <= len(wide) {
				t.Fatalf("narrow box should wrap: wide=%d narrow=%d", len(wide), len(narrow))
			}
			if maxLineDisplayWidth(narrow) > 28 {
				t.Fatal("content escaped box width")
			}
			q.Set("height", "3")
			e.Query = q.Encode()
			if got := len(renderElementRows(e, 245)); got != 3 {
				t.Fatalf("height should clip to 3, got %d", got)
			}
			if q.Get("scale") != "1.00" || q.Get("text-size") != "5" || e.Text != "alpha beta gamma delta epsilon\nsecond line" {
				t.Fatal("box sizing changed text or font size")
			}
		})
	}
}
