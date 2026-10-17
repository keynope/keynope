package app

import (
	"strings"
	"testing"
)

func TestLegacyMetadataDecodersOwnExactNamespaces(t *testing.T) {
	objects, err := encodeDeckObjects(structuredFixture().Objects)
	if err != nil {
		t.Fatal(err)
	}
	theme, err := encodeDeckTheme(builtinDeckTheme("clean-light"))
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-future", ".future", "_future", "Future"} {
		for _, namespace := range []string{"objects", "theme", "fonts"} {
			t.Run(namespace+suffix, func(t *testing.T) {
				foreign := "<!-- keynope-" + namespace + suffix + " version=9 base64:e30= -->\n"
				switch namespace {
				case "objects":
					value, remaining, err := decodeDeckObjects(foreign)
					if err != nil || value != nil || remaining != foreign {
						t.Fatal("unrelated object namespace consumed", err)
					}
					value, remaining, err = decodeDeckObjects(objects + "\n" + foreign)
					if err != nil || value == nil || remaining != foreign {
						t.Fatal("foreign namespace interfered with valid object archive", remaining, err)
					}
				case "theme":
					value, remaining, err := decodeDeckTheme(foreign)
					if err != nil || value != nil || remaining != foreign {
						t.Fatal("unrelated theme namespace consumed", err)
					}
					value, remaining, err = decodeDeckTheme(theme + "\n" + foreign)
					if err != nil || value == nil || remaining != foreign {
						t.Fatal("foreign namespace interfered with valid theme archive", remaining, err)
					}
				case "fonts":
					if firstDocumentMetadata(foreign, keynopeFontsEnvelopeRE) != nil || rewriteDocumentMetadata(foreign, keynopeFontsEnvelopeRE, nil) != foreign {
						t.Fatal("unrelated font namespace discarded as retired glyph data")
					}
				}
			})
		}
		t.Run("truetype-text"+suffix, func(t *testing.T) {
			foreign := "<!-- truetype-text" + suffix + " version=9 base64:RVhURU5TSU9O -->\n"
			remaining, recovered := recoverTrueTypeEnvelopes(foreign)
			if recovered || remaining != foreign {
				t.Fatal("unrelated text namespace recovered into authored content", remaining)
			}
		})
	}
	// Narrow matching must not weaken rejection of a malformed/future actual
	// archive, nor remove the intentional legacy text recovery behavior.
	for _, input := range []string{"<!-- keynope-objects -->", "<!-- keynope-objects=wrong -->", strings.Replace(objects, "version=1", "version=9", 1), objects + "\n" + objects} {
		if _, remaining, err := decodeDeckObjects(input); err == nil || remaining != input {
			t.Fatal("invalid owned object archive accepted/consumed")
		}
	}
	for _, input := range []string{"<!-- keynope-theme -->", "<!-- keynope-theme=wrong -->", strings.Replace(theme, "version=1", "version=9", 1), theme + "\n" + theme} {
		if _, remaining, err := decodeDeckTheme(input); err == nil || remaining != input {
			t.Fatal("invalid owned theme archive accepted/consumed")
		}
	}
	if firstDocumentMetadata("<!-- keynope-fonts version=1 base64:e30= -->", keynopeFontsEnvelopeRE) == nil {
		t.Fatal("actual retired font archive stopped being recognized")
	}
	if value, recovered := recoverTrueTypeEnvelopes("<!-- truetype-text base64:VGV4dA== -->\n"); !recovered || !strings.Contains(value, "truetype-text=base64:VGV4dA== kind=text") {
		t.Fatal("actual legacy text recovery changed", value)
	}
}

func TestForeignMetadataNamespaceDoesNotDecodeTextOrRetireFonts(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, suffix := range []string{"-future", ".future"} {
		text := "<!-- keynope width=245 height=56 -->\n"
		var comments []string
		for _, namespace := range []string{"keynope-objects", "keynope-theme", "keynope-fonts", "truetype-text"} {
			comment := "<!-- " + namespace + suffix + " version=9 base64:RVhURU5TSU9O -->"
			comments = append(comments, comment)
			text += comment + "\n"
		}
		text += "\n# Original\n"
		deck, err := parseEditorDeckData("Namespaces.md", []byte(text))
		if err != nil {
			t.Fatal(suffix, err)
		}
		// Unknown comment syntax keeps the existing literal-content policy.
		// This fix does not invent a new opaque envelope format or discard it.
		if len(deck.Slides) != 1 || len(deck.Slides[0].Elements) != 2 || deck.Slides[0].Elements[1].Text != "Original" {
			t.Fatal("foreign namespace changed ordinary content", deck.Slides)
		}
		for _, comment := range comments {
			if !strings.Contains(deck.Slides[0].Elements[0].Text, comment) {
				t.Fatal("unknown literal markup was rewritten/discarded", comment)
			}
		}
		for _, element := range deck.Slides[0].Elements {
			if element.Text == "EXTENSION" {
				t.Fatal("foreign encoded text was decoded as legacy content")
			}
		}
		for _, diagnostic := range deck.Diagnostics {
			if diagnostic.Code == "retired-font-data" || diagnostic.Code == "text-payload-recovered" {
				t.Fatal("foreign namespace triggered migration", diagnostic)
			}
		}
		saved, err := serializeDeck("Namespaces.md", deck)
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := parseEditorDeckData("Namespaces.md", saved)
		if err != nil {
			t.Fatal(err)
		}
		if len(reopened.Slides) != 1 || len(reopened.Slides[0].Elements) != 2 {
			t.Fatal("literal foreign comments changed structure after save/reopen")
		}
		for i, element := range deck.Slides[0].Elements {
			if reopened.Slides[0].Elements[i].Text != element.Text {
				t.Fatal("literal foreign comment text changed after save/reopen")
			}
		}
	}
}
