package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func plainMetadataComment(namespace, payload string) string {
	return "<!-- keynope-" + namespace + " version=1 base64:" + base64.StdEncoding.EncodeToString([]byte(payload)) + " -->\n"
}

func TestSingletonMetadataEnvelopeBoundaries(t *testing.T) {
	envelope := newSingletonMetadataEnvelope("owned", 32)
	for _, size := range []int{31, 32, 33} {
		text := plainMetadataComment("owned", strings.Repeat("x", size)) + "\nRetain text\n"
		payload, remaining, err := envelope.decode(text)
		if size <= 32 {
			if err != nil || len(payload) != size || remaining != "\nRetain text\n" {
				t.Fatal(size, string(payload), remaining, err)
			}
		} else if err == nil || payload != nil || remaining != text {
			t.Fatal("oversized metadata consumed text", err)
		}
	}
	for _, text := range []string{
		"<!-- keynope-owned -->\n",
		"<!-- keynope-owned version=1 base64:%%% -->\n",
		"<!-- keynope-owned version=1 base64:AAA -->\n",
		"<!-- keynope-owned version=1 base64:AAAA= -->\n",
		"<!-- keynope-owned version=2 base64:e30= -->\n",
		"<!-- keynope-owned version=1 base64:e30=\n",
		plainMetadataComment("owned", "{}") + plainMetadataComment("owned", "{}"),
		plainMetadataComment("owned", "{}") + "<!-- keynope-owned version=2 base64:e30= -->\n",
	} {
		payload, remaining, err := envelope.decode(text)
		if err == nil || payload != nil || remaining != text {
			t.Fatal("invalid singleton accepted/consumed", text, err)
		}
	}
	for _, text := range []string{"<!-- keynope-owned-other version=2 base64:e30= -->", "Ordinary text"} {
		payload, remaining, err := envelope.decode(text)
		if err != nil || payload != nil || remaining != text {
			t.Fatal("unrelated namespace consumed", err)
		}
	}
}

func TestDocumentSingletonMetadataAdmission(t *testing.T) {
	for _, namespace := range []string{"masters", "tabs", "document-extra"} {
		valid := map[string]string{"masters": `{"version":1}`, "tabs": `[{"id":"guide","name":"Guide","page":1}]`, "document-extra": `{"future":{"preserved":true}}`}[namespace]
		decode := func(text string) (string, error) {
			switch namespace {
			case "masters":
				_, remaining, err := decodeMasterDeckMetadata(text)
				return remaining, err
			case "tabs":
				_, remaining, err := decodeDeckTabs(text)
				return remaining, err
			default:
				_, remaining, err := decodeDocumentExtensions(text)
				return remaining, err
			}
		}
		envelope := plainMetadataComment(namespace, valid)
		if remaining, err := decode(envelope + "\n# Content\n"); err != nil || remaining != "\n# Content\n" {
			t.Fatal(namespace, "valid archive", remaining, err)
		}
		for name, text := range map[string]string{
			"duplicate":     envelope + envelope,
			"mixed-version": envelope + strings.Replace(envelope, "version=1", "version=9", 1),
			"future":        strings.Replace(envelope, "version=1", "version=9", 1),
			"malformed":     fmt.Sprintf("<!-- keynope-%s version=1 base64:%% -->\n", namespace),
			"null":          plainMetadataComment(namespace, "null"),
			"wrong-type":    plainMetadataComment(namespace, `"unexpected string"`),
		} {
			t.Run(namespace+"/"+name, func(t *testing.T) {
				if remaining, err := decode(text); err == nil || remaining != text {
					t.Fatal("bad metadata accepted or consumed", remaining, err)
				}
				oldWidth, oldHeight, oldOrder := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
				defer func() {
					authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = oldWidth, oldHeight, oldOrder
				}()
				authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = 132, 41, true
				_, err := parseEditorDeckData("candidate.md", []byte("<!-- keynope width=100 height=80 -->\n"+text+"\n# Candidate\n"))
				if err == nil || authoredTerminalWidth != 132 || authoredTerminalHeight != 41 || !parsedDeckElementOrderChanged {
					t.Fatal("rejected candidate changed runtime state", err)
				}
			})
		}
	}
}

func TestDocumentSingletonMetadataUnknownFieldsRoundTrip(t *testing.T) {
	text := plainMetadataComment("masters", `{"version":1,"future":{"items":[1,2]},"base":{"id":"base","name":"Base","slide":{}}}`) +
		plainMetadataComment("tabs", `[{"id":"guide","name":"Guide","page":1,"future":{"items":[3,4]}}]`) +
		plainMetadataComment("document-extra", ` {"future":{"items":[5,6]}} `) + "\n# Slide\n"
	deck, err := parseDeckData("owned.md", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := serializeDeck("owned.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := parseDeckData("owned.md", encoded)
	if err != nil || opened.Masters.Extra == nil || opened.Tabs[0].Extra == nil || opened.Extra == nil {
		t.Fatal("compatible metadata dropped", err)
	}
	for _, fields := range []*jsonExtensions{opened.Masters.Extra, opened.Tabs[0].Extra, opened.Extra} {
		if _, exists := (*fields)["future"]; !exists {
			t.Fatal("unknown field missing", fields)
		}
	}
}
