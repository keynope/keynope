package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"keynope/internal/pptx"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestShippedTemplateNativePPTXThemes(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, entry := range presentationTemplateCatalog {
		t.Run(entry.ID, func(t *testing.T) {
			d, err := loadPresentationTemplate(entry.ID)
			if err != nil {
				t.Fatal(err)
			}
			before := cloneDeck(d)
			presentation, _, err := deckToPPTX(d)
			if err != nil {
				t.Fatal(err)
			}
			if presentation.Theme == nil || presentation.Theme.Name != d.Theme.Name || !strings.EqualFold(presentation.Theme.Light1, d.Theme.Colors.Background) || !strings.EqualFold(presentation.Theme.Dark1, d.Theme.Colors.Text) || !reflect.DeepEqual(presentation.Theme.Accent[:], d.Theme.Colors.Accent) {
				t.Fatal("authored palette replaced by fixed export defaults", presentation.Theme)
			}
			for _, face := range []string{presentation.Theme.HeadingFont, presentation.Theme.BodyFont} {
				if strings.HasPrefix(face, "Keynope C64") {
					found := false
					for _, font := range presentation.EmbeddedFonts {
						if font.Family == face && len(font.Data) > 0 {
							found = true
						}
					}
					if !found {
						t.Fatal("theme refers to unembedded C64 face", face)
					}
				}
			}
			var themed, plain bytes.Buffer
			if _, err := pptx.Write(context.Background(), &themed, presentation); err != nil {
				t.Fatal(err)
			}
			if root := os.Getenv("KEYNOPE_NATIVE_THEME_EVIDENCE"); root != "" {
				if err := os.MkdirAll(root, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, entry.ID+".pptx"), themed.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			archive, err := zip.NewReader(bytes.NewReader(themed.Bytes()), int64(themed.Len()))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, file := range archive.File {
				if file.Name != "ppt/theme/theme1.xml" {
					continue
				}
				r, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(r)
				r.Close()
				if err != nil {
					t.Fatal(err)
				}
				var header struct {
					Name string `xml:"name,attr"`
				}
				if err := xml.Unmarshal(data, &header); err != nil {
					t.Fatal(err)
				}
				if header.Name != d.Theme.Name {
					t.Fatal("package palette was not serialized")
				}
				found = true
			}
			if !found {
				t.Fatal("missing native theme part")
			}
			presentation.Theme = nil
			if _, err := pptx.Write(context.Background(), &plain, presentation); err != nil {
				t.Fatal(err)
			}
			with, _, err := pptx.Read(context.Background(), bytes.NewReader(themed.Bytes()), int64(themed.Len()), pptx.Options{})
			if err != nil {
				t.Fatal(err)
			}
			without, _, err := pptx.Read(context.Background(), bytes.NewReader(plain.Bytes()), int64(plain.Len()), pptx.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(with.Slides, without.Slides) || !reflect.DeepEqual(before, d) {
				t.Fatal("native palette changed concrete object fallbacks or source deck")
			}
		})
	}
}

func TestThemeFontRolesEmbedUnusedC64WidthVariants(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	theme := builtinDeckTheme("c64")
	theme.Fonts.Heading.Width = 80
	theme.Fonts.Body.Width = 120
	d := Deck{Theme: theme, Slides: []Slide{{ID: "shapes-only", Elements: []Element{{ID: "square", Kind: "shape", Text: "[shape:square]", Query: "shape=square&width=20&height=10"}}}}}
	p, _, err := deckToPPTX(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, face := range []string{p.Theme.HeadingFont, p.Theme.BodyFont} {
		found := false
		for _, font := range p.EmbeddedFonts {
			if font.Family == face && len(font.Data) > 0 {
				found = true
			}
		}
		if !found {
			t.Fatal("unused theme font missing from package", face)
		}
	}
	if p.Theme.HeadingFont == p.Theme.BodyFont {
		t.Fatal("theme font widths collapsed")
	}
}
