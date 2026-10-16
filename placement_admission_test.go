package main

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestPlacementAdmissionRejectsAmbiguousOwners(t *testing.T) {
	for _, body := range []string{
		"top=1 top=2", "top=1&top=2", "top=1 Top=2",
		"top=1&%74op=2", "fg=red color=blue", "group=a group=b",
		"top=1 link=https%ZZ", "top=1 future=%ZZ",
	} {
		t.Run(body, func(t *testing.T) {
			if _, _, err := textPlacementCommentChecked("<!-- " + body + " -->"); err == nil {
				t.Fatal("ambiguous or malformed placement accepted")
			}
			if _, err := parseDeckData("placement.md", []byte("<!-- "+body+" -->\nText\n")); err == nil {
				t.Fatal("document parser bypassed checked placement admission")
			}
		})
	}
	for _, line := range []string{"<!-- top=1 --> junk", "<!-- top=1 --><!-- top=2 -->"} {
		if _, _, err := textPlacementCommentChecked(line); err == nil {
			t.Fatal("partial placement comment accepted", line)
		}
	}
}

func TestPlacementAdmissionDecodesOnceAndPreservesFutureValues(t *testing.T) {
	link := "https://example.com/a%2Fb?q=a+b&percent=%25"
	link, _ = normalizeLinkValue(link, 0)
	q := url.Values{
		"top": {"7"}, "fg": {"#ff0055"}, "link": {link},
		"future":          {"first + %2F", "second"},
		"connector-route": {`[{"x":1,"y":2},{"x":3,"y":2},{"x":3,"y":4},{"x":5,"y":4}]`},
	}
	for _, body := range []string{placementCommentText(q.Encode()), q.Encode()} {
		got, ok, err := textPlacementCommentChecked("<!-- " + body + " -->")
		if err != nil || !ok {
			t.Fatal("valid placement rejected", err, body)
		}
		parsed, err := url.ParseQuery(got)
		if err != nil || !reflect.DeepEqual(parsed, q) {
			t.Fatalf("placement changed: got %#v want %#v (%v)", parsed, q, err)
		}
	}
	// A future-only ordinary Markdown comment remains opaque, not a placement.
	if _, ok, err := textPlacementCommentChecked("<!-- future=a future=b -->"); ok || err != nil {
		t.Fatal("claimed an opaque comment", err)
	}
}

func TestPlacementAdmissionLiteralCodeAndPayloadRoundTrip(t *testing.T) {
	code := "<!-- top=1 top=2 -->"
	d, err := parseDeckData("code.md", []byte("```\n"+code+"\n```\n"))
	if err != nil || len(d.Slides) != 1 || len(d.Slides[0].Elements) != 1 || !strings.Contains(d.Slides[0].Elements[0].Text, code) {
		t.Fatal("code was interpreted as metadata", err)
	}
	// Exercise real encoded label/run payloads via the production serializer.
	for _, q := range []string{
		"top=3&label-modern-font=c64&label-ttf-size=97&label-fg=%23ff0055",
		"top=4&shape-label=eyJ0ZXh0IjoiSGVsbG8ifQ%3D%3D",
		"top=4&shape-label=eyJ0ZXh0IjoiPj4%2BIn0%3D",
	} {
		body := placementCommentText(q)
		got, ok, err := textPlacementCommentChecked("<!-- " + body + " -->")
		want, _ := url.ParseQuery(q)
		if err != nil || !ok || got != want.Encode() {
			t.Fatal("payload round trip changed", body, got, q, err)
		}
	}
}
