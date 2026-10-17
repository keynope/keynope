package app

import (
	"os"
	"testing"
)

func TestGeometryStepperBrowser(t *testing.T) {
	if os.Getenv("KEYNOPE_GEOMETRY_STEPPER_BROWSER") != "1" {
		t.Skip("set KEYNOPE_GEOMETRY_STEPPER_BROWSER=1 for live Arrange steppers")
	}
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{
		{ID: "box", Kind: "shape", Query: "shape=square&left=20&top=10&width=40&height=20"},
		{ID: "other", Kind: "shape", Query: "shape=circle&left=130&top=10&width=40&height=20"},
	}}}}
	for _, engine := range []string{"chromium", "webkit"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("KEYNOPE_BROWSER_ENGINE", engine)
			s := newNativeEditorSession("Arrange steppers.md", cloneDeck(d))
			server := newGeometryBrowserServer(t, s)
			runGeometryBrowser(t, "tools/test_geometry_steppers.cjs", server.URL)
		})
	}
}
