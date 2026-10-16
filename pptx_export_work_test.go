package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Deterministically cancel at a conversion checkpoint, not after a flaky sleep.
type pptxCancelAtCheck struct {
	context.Context
	cancel context.CancelFunc
	left   int
}

func (c *pptxCancelAtCheck) Err() error {
	c.left--
	if c.left <= 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestPPTXExportCancellationReturnsNoPartialPackage(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := retroBackgroundDeck(t, "cover", true)
	before := cloneDeck(d)
	for _, checkpoint := range []int{1, 6, 30, 150} {
		base, cancel := context.WithCancel(context.Background())
		ctx := &pptxCancelAtCheck{Context: base, cancel: cancel, left: checkpoint}
		data, _, err := exportPPTXDocumentContext(ctx, d)
		cancel()
		if !errors.Is(err, context.Canceled) || data != nil {
			t.Fatal("cancellation swallowed or partial package returned", checkpoint, err, len(data))
		}
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("cancellation modified authored deck")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := &nativeEditorSession{deck: d}
	request := httptest.NewRequest("POST", "/export-pptx", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	session.handleExportPPTX(recorder, request)
	if recorder.Code == 200 || !strings.Contains(recorder.Body.String(), "context canceled") || strings.Contains(recorder.Body.String(), `"content"`) {
		t.Fatal("endpoint detached cancellation", recorder.Code, recorder.Body.String())
	}
}

func TestPPTXExportAggregateBudgetCannotBecomeFallback(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := retroBackgroundDeck(t, "cover", false)
	d.Slides = append(d.Slides, d.Slides[0])
	before := cloneDeck(d)
	work := newPPTXExportWork(context.Background())
	work.pixelsLeft = 2_473_984 // One padded background fits; the second must fail.
	_, _, err := deckToPPTXWithWork(d, work)
	if !errors.Is(err, errPPTXExportBudget) {
		t.Fatal("aggregate exhaustion was downgraded to omitted art", err)
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("budget failure modified deck")
	}
	// State belongs to an individual export, not a poisoned process-global quota.
	if _, _, err := deckToPPTX(d); err != nil {
		t.Fatal("later export inherited failed quota", err)
	}
}

func TestPPTXExportModernMediaUsesSameRequestBudget(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	id, asset := pptxAnimationFixture(t)
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Assets: map[string]DeckAsset{id: asset}, Slides: []Slide{{Elements: []Element{{ID: "image", Kind: "image", AssetID: id, Path: embeddedAssetPath(id, asset), Query: "image-style=modern&brightness=1.3&left=4&top=4&width=30&height=10"}}}}}
	for _, animated := range []bool{true, false} {
		if !animated {
			asset.Data, asset.MIME, asset.Frames = asset.Frames[0].Data, "image/png", nil
			d.Assets[id] = asset
		}
		before := cloneDeck(d)
		work := newPPTXExportWork(context.Background())
		work.pixelsLeft = 1
		if _, _, err := deckToPPTXWithWork(d, work); !errors.Is(err, errPPTXExportBudget) {
			t.Fatal("modern media budget silently became source fallback", animated, err)
		}
		if !reflect.DeepEqual(before, d) {
			t.Fatal("failed modern conversion changed source")
		}
	}
}

func TestPPTXAnimationCancellationPreservesSource(t *testing.T) {
	_, asset := pptxAnimationFixture(t)
	media := sceneMediaSource(asset)
	before := cloneDeckAssetContent(asset, true)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &pptxCancelAtCheck{Context: base, cancel: cancel, left: 5}
	data, _, err := encodePPTXAnimation(&media, newPPTXExportWork(ctx))
	if !errors.Is(err, context.Canceled) || data != nil {
		t.Fatal("frame conversion ignored cancellation", err)
	}
	if !reflect.DeepEqual(before, asset) {
		t.Fatal("cancelled frame conversion changed source")
	}
}

func TestPPTXExportWriterBoundsAndCancellation(t *testing.T) {
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	writer := &pptxExportWriter{target: &out, work: newPPTXExportWork(ctx), left: 4}
	if n, err := writer.Write([]byte("1234")); n != 4 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := writer.Write([]byte("5")); n != 0 || !errors.Is(err, errPPTXExportBudget) || out.String() != "1234" {
		t.Fatal(n, err, out.String())
	}
	cancel()
	if n, err := writer.Write(nil); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal(n, err)
	}
}

func TestPPTXPackageOutputLimitReturnsNoPartialContent(t *testing.T) {
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{Elements: []Element{{ID: "text", Kind: "text", Text: "Must not return a truncated ZIP", Query: "render=truetype&modern-font=sans"}}}}}
	work := newPPTXExportWork(context.Background())
	work.packageBytes = 64
	data, _, err := exportPPTXDocumentWithWork(d, work)
	if !errors.Is(err, errPPTXExportBudget) || data != nil {
		t.Fatal("output limit returned damaged/partial package", len(data), err)
	}
}

func TestPPTXSwiftTransferCancellation(t *testing.T) {
	binary := os.Getenv("KEYNOPE_SWIFT_EXPORT_WRITER_PROOF")
	if binary == "" {
		t.Skip("headless Swift writer executable not requested")
	}
	var started, cancelled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/export":
			started.Store(true)
			select {
			case <-r.Context().Done():
				cancelled.Store(true)
			case <-time.After(15 * time.Second):
				http.Error(w, "not cancelled", 500)
			}
		case "/started":
			if !started.Load() {
				w.WriteHeader(202)
			}
		case "/cancelled":
			if !cancelled.Load() {
				w.WriteHeader(202)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, t.TempDir(), server.URL)
	output, err := command.CombinedOutput()
	if err != nil || !cancelled.Load() || !strings.Contains(string(output), "PASS: Swift task cancellation") {
		t.Fatalf("Swift transfer/writer cancellation failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}
