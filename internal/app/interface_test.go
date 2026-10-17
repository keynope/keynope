package app

import (
	"os"
	"testing"
)

func TestInterfaceBrowser(t *testing.T) {
	if os.Getenv("KEYNOPE_INTERFACE_BROWSER") != "1" {
		t.Skip("set KEYNOPE_INTERFACE_BROWSER=1 for editor appearance and keyboard checks")
	}
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{BG: "#ffffff", BGSet: true, Elements: []Element{
		{ID: "heading", Kind: "heading", Level: 1, Text: "A story worth sharing", Query: "render=truetype&modern-font=sans&ttf-size=97&top=5&left=15&width=200&height=12&header=%23155e75&fg=%23155e75"},
		{ID: "box", Kind: "shape", Query: "shape=square&left=20&top=25&width=40&height=20&fg=%230055aa"},
	}}, {BG: "#102a43", BGSet: true, Elements: []Element{
		{ID: "next-heading", Kind: "text", Text: "Next slide", Query: "render=truetype&modern-font=sans&ttf-size=97&top=8&left=15&width=200&height=12&fg=%23ffffff"},
	}}}}
	for _, engine := range []string{"chromium", "webkit"} {
		t.Run(engine, func(t *testing.T) {
			t.Setenv("KEYNOPE_BROWSER_ENGINE", engine)
			s := newNativeEditorSession("Interface checks.md", cloneDeck(d))
			server := newGeometryBrowserServer(t, s)
			runGeometryBrowser(t, "tools/test_interface.cjs", server.URL)
			runGeometryBrowser(t, "tools/test_activity_interface.cjs", server.URL)
		})
	}
}
