package main

import (
	"os"
	"strings"
	"testing"
)

func TestInterfaceUsesSystemFontWithoutRemovingSlideFonts(t *testing.T) {
	css := trueTypeCSS()
	if !strings.Contains(css, "font-family:var(--keynope-ui-font)!important") ||
		!strings.Contains(css, `--keynope-ui-font:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif`) {
		t.Fatal("editor controls must use the system UI font")
	}
	if !strings.Contains(css, "@font-face{font-family:KeynopeC64;") ||
		!strings.Contains(modernFontsCSS(), "font-family:KeynopeC64;") {
		t.Fatal("C64 must remain available for slide content")
	}
	for _, path := range []string{"web/ui-font.css", "web/workspace.css"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "KeynopeC64") {
			t.Fatalf("%s still applies the retro font to UI", path)
		}
	}
	data, err := os.ReadFile("web/ui-font.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "font-family:Menlo,Monaco,monospace") {
		t.Fatal("block artwork must retain its grid font")
	}
}
