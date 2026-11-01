package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQRCodeValidationAndQuietZone(t *testing.T) {
	for _, level := range []string{"", "L", "M", "Q", "H"} {
		svg, n, err := qrCodeSVG(QRCodeOptions{Content: "https://example.com/?a=1&b=2", Correction: level})
		if err != nil || n < 29 || !strings.Contains(svg, fmt.Sprintf(`viewBox="0 0 %d %d"`, n, n)) {
			t.Fatalf("%s: %d %v", level, n, err)
		}
		canonical, _, _, err := sanitizeSVG(context.Background(), []byte(svg))
		if err != nil {
			t.Fatal(err)
		}
		data, err := rasterizeSVG(context.Background(), canonical, n*8, n*8)
		if err != nil {
			t.Fatal(err)
		}
		image, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < n*8; y++ {
			for x := 0; x < n*8; x++ {
				if x >= 32 && y >= 32 && x < n*8-32 && y < n*8-32 {
					continue
				}
				r, g, b, a := image.At(x, y).RGBA()
				if r != 65535 || g != 65535 || b != 65535 || a != 65535 {
					t.Fatalf("quiet zone changed at %d,%d", x, y)
				}
			}
		}
	}
	for _, options := range []QRCodeOptions{{}, {Content: " "}, {Content: string([]byte{255})}, {Content: strings.Repeat("x", 2954)}, {Content: "hi", Correction: "X"}, {Content: "hi", Foreground: "red"}, {Content: "hi", Foreground: "#ffffff", Background: "#000000"}, {Content: "hi", Foreground: "#cccccc"}} {
		if _, _, err := qrCodeSVG(options); err == nil {
			t.Fatalf("accepted invalid options %+v", options)
		}
	}
	if svg, _, err := qrCodeSVG(QRCodeOptions{Content: `<script>alert('hi')</script>`, Foreground: "#102030", Background: "#fff0e0"}); err != nil || strings.Contains(svg, "script") {
		t.Fatal("payload is not inert", err)
	}
}

func TestQRCodeInsertRoundTripAndHistory(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, size := range []*DeckPageSize{nil, {Version: 1, Width: 1080, Height: 1920}} {
		s := newNativeEditorSession(filepath.Join(t.TempDir(), "qr.md"), Deck{PageSize: size, Slides: []Slide{{}}})
		version := s.version
		action := nativeEditorAction{Action: "insert-qr-code", QRCode: &QRCodeOptions{Content: "https://example.com/hello"}, SceneRevision: &version}
		if err := s.apply(action); err != nil {
			t.Fatal(err)
		}
		if len(s.deck.Slides[0].Elements) != 1 || len(s.undo) != 1 {
			t.Fatal("QR insertion must be a single undoable command")
		}
		e := s.deck.Slides[0].Elements[0]
		q, _ := url.ParseQuery(e.Query)
		if q.Get("link") != action.QRCode.Content || s.deck.Assets[e.AssetID].Vector == nil {
			t.Fatal("missing portable vector or clickable link")
		}
		scene, err := buildSlideScene(s.deck, 0, 245, 56)
		if err != nil || len(scene.Objects) != 1 {
			t.Fatal(err, len(scene.Objects))
		}
		b := scene.Objects[0].Bounds
		if math.Abs(b.Width-b.Height) > 1 || b.X < 0 || b.Y < 0 || b.X+b.Width > scene.Width || b.Y+b.Height > scene.Height || math.Abs(b.X+b.Width/2-scene.Width/2) > 2 {
			t.Fatalf("QR must be square and centred: %+v in %fx%f", b, scene.Width, scene.Height)
		}
		data, err := serializeDeck(s.deckPath, s.deck)
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := parseDeckData(s.deckPath, data)
		if err != nil || reopened.Assets[e.AssetID].Vector == nil {
			t.Fatal("QR didn't survive saving", err)
		}
		if err := s.apply(action); err == nil {
			t.Fatal("stale insert was accepted")
		}
		if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || len(s.deck.Slides[0].Elements) != 0 {
			t.Fatal("undo failed", err)
		}
		if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || len(s.deck.Slides[0].Elements) != 1 {
			t.Fatal("redo failed", err)
		}
	}
}

func TestQRCodePreviewAndBrowser(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	s := newNativeEditorSession(filepath.Join(t.TempDir(), "QR.md"), Deck{Slides: []Slide{{}}})
	for _, body := range []string{`{"content":"https://keynope.sh"}`, `{"content":""}`, `{"content":"yes","unknown":1}`} {
		r := httptest.NewRequest("POST", "/api/editor/qr-code", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.handleQRCode(w, r)
		if strings.Contains(body, "https") && w.Code != 200 || !strings.Contains(body, "https") && w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(s.deck.Slides[0].Elements) != 0 || len(s.undo) != 0 {
		t.Fatal("preview changed the deck")
	}
	if os.Getenv("KEYNOPE_QR_BROWSER") != "1" {
		return
	}
	server := newGeometryBrowserServer(t, s)
	runGeometryBrowser(t, "internal/app/testdata/qr_code.cjs", server.URL)
	// The test generates a single image, never an activity or external asset.
	data, err := json.Marshal(s.deck.Slides[0].Elements)
	if err != nil || !bytes.Contains(data, []byte("assetId")) {
		t.Fatal("browser did not insert a portable image", err)
	}
}
