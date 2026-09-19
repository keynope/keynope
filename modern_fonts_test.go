package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestModernFontsAreCompleteAndOffline(t *testing.T) {
	css := modernFontsCSS()
	if got := strings.Count(css, "@font-face{"); got != 9 {
		t.Fatalf("font bundle has %d faces, want C64 plus eight Modern faces", got)
	}
	for _, signature := range []string{
		"font-family:KeynopeC64;font-weight:400;font-style:normal",
		"font-family:KeynopeModern;font-weight:400;font-style:normal",
		"font-family:KeynopeModern;font-weight:700;font-style:normal",
		"font-family:KeynopeModern;font-weight:400;font-style:italic",
		"font-family:KeynopeModern;font-weight:700;font-style:italic",
		"font-family:KeynopeModernMono;font-weight:400;font-style:normal",
		"font-family:KeynopeModernMono;font-weight:700;font-style:normal",
		"font-family:KeynopeModernMono;font-weight:400;font-style:italic",
		"font-family:KeynopeModernMono;font-weight:700;font-style:italic",
	} {
		if !strings.Contains(css, signature) {
			t.Fatalf("font bundle omitted %q", signature)
		}
	}
	if strings.Contains(css, "http://") || strings.Contains(css, "https://") {
		t.Fatal("font bundle depends on a network URL")
	}
	faces := regexp.MustCompile(`src:url\(data:font/ttf;base64,([^\)]+)\)`).FindAllStringSubmatch(css, -1)
	if len(faces) != 9 {
		t.Fatalf("found %d embedded font payloads, want 9", len(faces))
	}
	for index, match := range faces {
		payload, err := base64.StdEncoding.DecodeString(match[1])
		if err != nil || len(payload) < 1024 {
			t.Fatalf("font face %d has invalid embedded data: %v (%d bytes)", index, err, len(payload))
		}
	}
}

func TestSceneFontEndpointIsRetryableAndSelfContained(t *testing.T) {
	session := &nativeEditorSession{}
	request := httptest.NewRequest(http.MethodGet, "/api/editor/scene-fonts", nil)
	response := httptest.NewRecorder()
	session.handleSceneFonts(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("font endpoint: %d %q", response.Code, response.Header())
	}
	if response.Body.String() != modernFontsCSS() {
		t.Fatal("font endpoint did not return the pinned embedded bundle")
	}
	if !strings.Contains(response.Header().Get("Cache-Control"), "max-age=") {
		t.Fatal("font endpoint should be reusable without repeated decoding")
	}

	request = httptest.NewRequest(http.MethodPost, "/api/editor/scene-fonts", nil)
	response = httptest.NewRecorder()
	session.handleSceneFonts(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST font endpoint status = %d", response.Code)
	}
}

func TestBrowserFontReadinessRequiresC64AndSurfacesRecovery(t *testing.T) {
	scene, err := os.ReadFile("web/scene.js")
	if err != nil {
		t.Fatal(err)
	}
	trueType, err := os.ReadFile("web/truetype.js")
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{"scene": string(scene), "truetype": string(trueType)} {
		if !strings.Contains(source, "keynope-font-readiness") || !strings.Contains(source, "reportFontReadiness(false") {
			t.Fatalf("%s renderer does not expose a recoverable font failure", name)
		}
	}
	if !strings.Contains(string(scene), "['KeynopeC64','normal','400']") || !strings.Contains(string(scene), "fonts=null") {
		t.Fatal("scene readiness neither requires the C64 face nor permits a retry")
	}
	if !strings.Contains(string(trueType), "if(!results[0])throw new Error") {
		t.Fatal("legacy C64 canvas path still accepts a silent fallback")
	}
}
