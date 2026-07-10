package main

import (
	"strings"
	"testing"
)

func TestPresenterPhosphorAppliesToSlidesAndFallback(t *testing.T) {
	javascript := exportHTMLSuffix()
	if strings.Contains(javascript, "drawPresenterTestCardPhosphor") {
		t.Fatal("test-card-only phosphor function remains")
	}
	if !strings.Contains(javascript, "function drawPresenterPhosphor(w, h, intensity = 1)") {
		t.Fatal("shared presenter phosphor function is missing")
	}
	call := "drawPresenterPhosphor(presenterCanvas.width, presenterCanvas.height);"
	if count := strings.Count(javascript, call); count != 2 {
		t.Fatalf("presenter slide phosphor call count = %d, want 2", count)
	}
	if !strings.Contains(javascript, "drawPresenterPhosphor(w, h, 1.3);") {
		t.Fatal("timer test card no longer uses intensified presenter phosphor")
	}
}

func TestExternalPresenterNavigationConsumesHandledKeys(t *testing.T) {
	javascript := exportHTMLSuffix()
	expected := "else if (e.key === 'End') pageIndex = deck.pages.length - 1;\n  else return;\n  e.preventDefault();\n  e.stopPropagation();\n  frame = 0;"
	if !strings.Contains(javascript, expected) {
		t.Fatal("external presenter navigation does not suppress WebKit's native key action")
	}
}

func TestPresenterTestCardHasIsolatedAnalogSignalArtifacts(t *testing.T) {
	javascript := exportHTMLSuffix()
	for _, function := range []string{
		"function drawPresenterTestCardSyncDistortion(w, h)",
		"function drawPresenterTestCardHumBar(w, h)",
	} {
		if !strings.Contains(javascript, function) {
			t.Fatalf("missing presenter test-card artifact function %q", function)
		}
	}
	for _, call := range []string{
		"drawPresenterTestCardSyncDistortion(w, h);",
		"drawPresenterTestCardHumBar(w, h);",
	} {
		if count := strings.Count(javascript, call); count != 1 {
			t.Fatalf("test-card artifact call %q count = %d, want 1", call, count)
		}
	}
	testCardStart := strings.Index(javascript, "function drawPresenterTestCard()")
	testCardEnd := strings.Index(javascript, "function drawPresenterTestCardSyncDistortion")
	if testCardStart < 0 || testCardEnd <= testCardStart {
		t.Fatal("could not isolate presenter test-card renderer")
	}
	testCard := javascript[testCardStart:testCardEnd]
	if !strings.Contains(testCard, "drawPresenterTestCardSyncDistortion(w, h);") || !strings.Contains(testCard, "drawPresenterTestCardHumBar(w, h);") {
		t.Fatal("analog signal artifacts are not scoped to the test-card renderer")
	}
	if !strings.Contains(javascript, "const sideInset = Math.max(1, Math.round(w * 0.03));") {
		t.Fatal("test-card sync distortion no longer protects the side gutters")
	}
	if !strings.Contains(javascript, "rgba(0,0,0,0.16)") {
		t.Fatal("test-card hum bar does not use the reduced opacity")
	}
}

func TestPresenterTestCardUsesReferenceCalibrationLayout(t *testing.T) {
	javascript := exportHTMLSuffix()
	for _, marker := range []string{
		"presenterContext.ellipse(w * 0.5, h * 0.496, w * 0.342, h * 0.435",
		"const colourBars = [",
		"const greys = ['#111111', '#303030', '#515151'",
		"let wedgeX = w * 0.188;",
		"const checkerWidth = 1 / 18;",
		"rect('#00a05a', 0.073, 0.095, 0.057, 0.402);",
		"rect('#554de1', 0.871, 0.500, 0.057, 0.397);",
	} {
		if !strings.Contains(javascript, marker) {
			t.Fatalf("presenter test card is missing reference-layout marker %q", marker)
		}
	}
}
