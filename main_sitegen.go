//go:build keynope_sitegen && !js

package main

// Internal HTML generation for web builds and the hosted renderer. This entry
// point is not included in the distributed Mac app or exposed as a public CLI.
import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--template-previews" {
		data, err := generateTemplatePreviews()
		if err != nil {
			panic(err)
		}
		data = append(data, '\n')
		previous, _ := os.ReadFile(os.Args[2])
		if !bytes.Equal(previous, data) {
			if err := os.WriteFile(os.Args[2], data, 0644); err != nil {
				panic(err)
			}
		}
		fmt.Println("Generated portable template previews")
		return
	}
	if len(os.Args) != 3 {
		panic("site generator requires a source deck and output directory")
	}
	deck, err := parseDeck(os.Args[1])
	if err != nil {
		panic(err)
	}
	ensureDefaultAuthoredSize()
	html, err := exportHTMLDocument(os.Args[1], deck.ResolvedSlides(), authoredTerminalWidth, authoredTerminalHeight, preservedExportHead{}, false)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(os.Args[2], "Welcome.html"), []byte(html), 0644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(os.Args[2], "licenses.txt"), []byte(bundledLicenseText()), 0644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(os.Args[2], "modern-fonts.css"), []byte(modernFontsCSS()), 0644); err != nil {
		panic(err)
	}
	fmt.Println("Generated editor shell and bundled licenses")
}
