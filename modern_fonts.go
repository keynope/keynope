package main

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
)

// Fonts come from the existing pinned x/image dependency, not a network font
// provider or a user's installed fonts. Both families include real bold/italic.
//
//go:embed assets/fonts/GO-FONT-LICENSE.txt
var modernFontLicense string

var modernFontsCSS = sync.OnceValue(func() string {
	var css strings.Builder
	fmt.Fprintf(&css, "@font-face{font-family:KeynopeC64;font-weight:400;font-style:normal;font-display:block;src:url(data:font/ttf;base64,%s) format('truetype')}\n", strings.TrimSpace(trueTypeFontBase64))
	for _, font := range []struct {
		family, weight, style string
		data                  []byte
	}{
		{"KeynopeModern", "400", "normal", goregular.TTF},
		{"KeynopeModern", "700", "normal", gobold.TTF},
		{"KeynopeModern", "400", "italic", goitalic.TTF},
		{"KeynopeModern", "700", "italic", gobolditalic.TTF},
		{"KeynopeModernMono", "400", "normal", gomono.TTF},
		{"KeynopeModernMono", "700", "normal", gomonobold.TTF},
		{"KeynopeModernMono", "400", "italic", gomonoitalic.TTF},
		{"KeynopeModernMono", "700", "italic", gomonobolditalic.TTF},
	} {
		fmt.Fprintf(&css, "@font-face{font-family:%s;font-weight:%s;font-style:%s;font-display:block;src:url(data:font/ttf;base64,%s) format('truetype')}\n", font.family, font.weight, font.style, base64.StdEncoding.EncodeToString(font.data))
	}
	return css.String()
})

func (s *nativeEditorSession) handleSceneFonts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write([]byte(modernFontsCSS()))
}
