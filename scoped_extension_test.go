package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestScopedExtensionAdmission(t *testing.T) {
	for _, namespace := range []string{"slide-extra", "element-extra"} {
		valid := plainMetadataComment(namespace, `{"future":true}`)
		for name, metadata := range map[string]string{
			"future":        strings.Replace(valid, "version=1", "version=9", 1),
			"malformed":     "<!-- keynope-" + namespace + " version=1 base64:%%% -->\n",
			"duplicate":     valid + valid,
			"null":          plainMetadataComment(namespace, "null"),
			"array":         plainMetadataComment(namespace, "[]"),
			"duplicate-key": plainMetadataComment(namespace, `{"future":1,"future":2}`),
			"escaped-key":   plainMetadataComment(namespace, `{"future":1,"futur\u0065":2}`),
			"trailing":      valid[:len(valid)-1] + " ignored text\n",
		} {
			t.Run(namespace+"/"+name, func(t *testing.T) {
				if _, err := parseDeckData("bad.md", []byte(metadata+"\nText\n")); err == nil {
					t.Fatal("invalid scoped metadata accepted")
				}
			})
		}
		large := "<!-- keynope-" + namespace + " version=1 base64:" + strings.Repeat("A", base64.StdEncoding.EncodedLen(maxJSONExtensionBytes)+4) + " -->\nText"
		if _, err := parseSlideChecked(large, "."); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatal("encoded size guard missing", err)
		}
	}
	if _, err := parseDeckData("orphan.md", []byte(plainMetadataComment("element-extra", `{}`))); err == nil || !strings.Contains(err.Error(), "no following object") {
		t.Fatal("orphan extension silently discarded", err)
	}
	compatible, err := encodeCompatibleElement(Element{Kind: "future-widget", ID: "future", Extra: extensionFixture("future", `1`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSlideChecked(plainMetadataComment("element-extra", `{"future":2}`)+compatible, "."); err == nil || !strings.Contains(err.Error(), "ambiguous element extension") {
		t.Fatal("prefix metadata overwrote complete element metadata", err)
	}
}

func TestScopedExtensionsKeepSeparateOwnersAndLiteralCode(t *testing.T) {
	code := "<!-- keynope-slide-extra version=9 base64:%%% -->\n---\n<!-- keynope-element-extra version=9 base64:%%% -->"
	text := plainMetadataComment("slide-extra", `{"slide":1}`) +
		plainMetadataComment("element-extra", `{"owner":1}`) + "First\n\n" +
		plainMetadataComment("element-extra", `{"owner":2}`) + "- First bullet\n" +
		plainMetadataComment("element-extra", `{"owner":3}`) + "- Separate bullet\n\n" +
		plainMetadataComment("element-extra", `{"owner":4}`) + "```md\n" + code + "\n```\n\n---\n" +
		plainMetadataComment("slide-extra", `{"slide":2}`) + plainMetadataComment("element-extra", `{"owner":5}`) + "Second slide\n"
	deck, err := parseDeckData("scopes.md", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Slides) != 2 || len(deck.Slides[0].Elements) != 4 || len(deck.Slides[1].Elements) != 1 {
		t.Fatal("ownership boundaries changed", deck.Slides)
	}
	requireExtension(t, deck.Slides[0].Extra, "slide", "1")
	requireExtension(t, deck.Slides[1].Extra, "slide", "2")
	for i, want := range []string{"1", "2", "3", "4"} {
		requireExtension(t, deck.Slides[0].Elements[i].Extra, "owner", want)
	}
	if element := deck.Slides[0].Elements[3]; element.Kind != "code" || element.Text != code {
		t.Fatal("metadata/separator consumed inside code", element)
	}
	encoded, err := serializeDeck("scopes.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := parseDeckData("scopes.md", encoded)
	if err != nil || len(opened.Slides) != 2 || opened.Slides[0].Elements[3].Text != code {
		t.Fatal("code/metadata did not survive save/reopen", err)
	}
	for i, want := range []string{"1", "2", "3", "4"} {
		requireExtension(t, opened.Slides[0].Elements[i].Extra, "owner", want)
	}
}
