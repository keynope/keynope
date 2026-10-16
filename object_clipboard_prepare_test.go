package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A deterministic image decoder lets the tests pause the real validation path,
// rather than guess whether a large PNG happened to finish before another edit.
type clipboardDecodeGate struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int64
}

var clipboardTestDecoderOnce sync.Once
var clipboardDecodeGates = struct {
	sync.Mutex
	byID map[string]*clipboardDecodeGate
}{byID: map[string]*clipboardDecodeGate{}}

func blockedClipboardBundle(t *testing.T) (ObjectClipboard, *clipboardDecodeGate) {
	t.Helper()
	clipboardTestDecoderOnce.Do(func() {
		// The test-only signature reports the declared PNG format, so the
		// production MIME preflight still runs before the controlled pixel gate.
		image.RegisterFormat("png", "KNCLPB", func(r io.Reader) (image.Image, error) {
			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}
			clipboardDecodeGates.Lock()
			gate := clipboardDecodeGates.byID[string(data)]
			clipboardDecodeGates.Unlock()
			if gate == nil {
				return nil, fmt.Errorf("test decoder gate is missing")
			}
			gate.calls.Add(1)
			gate.once.Do(func() { close(gate.started) })
			<-gate.release
			return image.NewNRGBA(image.Rect(0, 0, 2, 2)), nil
		}, func(io.Reader) (image.Config, error) {
			return image.Config{ColorModel: color.NRGBAModel, Width: 2, Height: 2}, nil
		})
	})
	data := "KNCLPB" + newStableID("test-image")
	gate := &clipboardDecodeGate{started: make(chan struct{}), release: make(chan struct{})}
	clipboardDecodeGates.Lock()
	clipboardDecodeGates.byID[data] = gate
	clipboardDecodeGates.Unlock()
	t.Cleanup(func() {
		select {
		case <-gate.release:
		default:
			close(gate.release)
		}
		clipboardDecodeGates.Lock()
		delete(clipboardDecodeGates.byID, data)
		clipboardDecodeGates.Unlock()
	})
	return ObjectClipboard{Version: 1, Elements: []Element{{Kind: "image", ID: "source-image", AssetID: "photo"}},
		Assets: map[string]DeckAsset{"photo": {MIME: "image/png", Width: 2, Height: 2, Data: []byte(data)}}}, gate
}

func awaitClipboardDecode(t *testing.T, gate *clipboardDecodeGate) {
	t.Helper()
	select {
	case <-gate.started:
	case <-time.After(3 * time.Second):
		t.Fatal("paste did not reach image validation")
	}
}

func awaitClipboardPaste(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("paste did not finish")
		return nil
	}
}

func TestObjectClipboardPreparationDoesNotHoldDocumentLock(t *testing.T) {
	for _, master := range []bool{false, true} {
		t.Run(fmt.Sprintf("master=%v", master), func(t *testing.T) {
			s := newNativeEditorSession("prepare.md", Deck{Slides: []Slide{{ID: "destination"}}})
			s.masterMode = master
			bundle, gate := blockedClipboardBundle(t)
			done := make(chan error, 1)
			go func() { done <- s.apply(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}) }()
			awaitClipboardDecode(t, gate)
			if !s.mu.TryLock() {
				t.Fatal("pixel validation still holds the document lock")
			}
			s.mu.Unlock()
			// A genuine edit must finish while pixel decoding is still blocked.
			if err := s.apply(nativeEditorAction{Action: "add-element", Kind: "text"}); err != nil {
				t.Fatal(err)
			}
			before, version, undo := cloneDeck(s.deck), s.version, len(s.undo)
			close(gate.release)
			if err := awaitClipboardPaste(t, done); !errors.Is(err, errClipboardPasteStale) {
				t.Fatal("obsolete preparation was not rejected", err)
			}
			if !reflect.DeepEqual(before, s.deck) || s.version != version || len(s.undo) != undo {
				t.Fatal("obsolete paste overwrote a concurrent edit or changed history")
			}
		})
	}
}

func TestObjectClipboardCancellationDuringPreparation(t *testing.T) {
	s := newNativeEditorSession("cancel.md", Deck{Slides: []Slide{{ID: "destination"}}})
	bundle, gate := blockedClipboardBundle(t)
	before, version := cloneDeck(s.deck), s.version
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- s.prepareAndApplyObjectClipboard(ctx, nativeEditorAction{Action: "paste-objects", Clipboard: &bundle})
	}()
	awaitClipboardDecode(t, gate)
	cancel()
	close(gate.release)
	if err := awaitClipboardPaste(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled preparation was committed", err)
	}
	if !reflect.DeepEqual(before, s.deck) || s.version != version || len(s.undo) != 0 || len(s.redo) != 0 {
		t.Fatal("cancelled paste changed document/history")
	}
	id := clipboardAssetHash(bundle.Assets["photo"])
	embeddedStillAssets.RLock()
	_, registered := embeddedStillAssets.assets[id]
	embeddedStillAssets.RUnlock()
	if registered {
		t.Fatal("cancelled paste registered its asset")
	}
}

func TestObjectClipboardCommitRechecksCancellationAndTarget(t *testing.T) {
	for _, change := range []string{"cancelled", "revision", "scope", "slide", "identity", "owner"} {
		t.Run(change, func(t *testing.T) {
			s := newNativeEditorSession("prepare.md", Deck{Slides: []Slide{{ID: "first"}, {ID: "second"}}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			bundle := ObjectClipboard{Version: 1, Elements: []Element{{ID: "text", Kind: "text", Text: "Pasted"}}}
			action := nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}
			draft, snapshot, err := s.captureObjectClipboardPaste(ctx, action)
			if err != nil {
				t.Fatal(err)
			}
			draft.elements, draft.assets, draft.objects, err = prepareObjectClipboardPaste(snapshot, bundle)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "cancelled":
				cancel()
			case "revision":
				s.version++
			case "scope":
				s.masterMode = true
			case "slide":
				s.current = 1
			case "identity":
				s.deck.Slides[0].ID = "replacement"
			case "owner":
				draft.owner = nil
			}
			before, version, selected := cloneDeck(s.deck), s.version, s.selected
			action.preparedClipboard, action.Clipboard = draft, nil
			if err := s.apply(action); err == nil {
				t.Fatal("accepted obsolete or cancelled private draft")
			}
			if !reflect.DeepEqual(before, s.deck) || s.version != version || s.selected != selected || len(s.undo) != 0 {
				t.Fatal("rejected commit changed document/view/history")
			}
		})
	}
}

func TestObjectClipboardPreparationSnapshotOwnsDependencies(t *testing.T) {
	s := newNativeEditorSession("prepare.md", structuredFixture())
	s.deck.Theme = builtinDeckTheme("clean-light")
	s.deck.Assets = map[string]DeckAsset{"photo": clipboardTestAsset(t)}
	bundle := ObjectClipboard{Version: 1, Elements: []Element{{ID: "text", Kind: "text", Text: "Pasted"}}}
	_, snapshot, err := s.captureObjectClipboardPaste(context.Background(), nativeEditorAction{Action: "paste-objects", Clipboard: &bundle})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Assets["photo"].Data[0] ^= 0xff
	value := snapshot.Objects.Tables["table-1"]
	value.Cells[0][0].Text = "Changed snapshot"
	snapshot.Objects.Tables["table-1"] = value
	if reflect.DeepEqual(snapshot.Assets, s.deck.Assets) || s.deck.Objects.Tables["table-1"].Cells[0][0].Text == "Changed snapshot" {
		t.Fatal("preparation snapshot aliases live dependencies")
	}
	if len(snapshot.Slides) != 0 || len(snapshot.Masters.Layouts) != 0 {
		t.Fatal("preparation copied unrelated document content")
	}
	snapshot.Theme.Colors.Accent[0] = "#123456"
	if s.deck.Theme.Colors.Accent[0] == "#123456" {
		t.Fatal("preparation theme aliases live document")
	}
}

func TestPreparedClipboardRetainsDestinationThemeRoles(t *testing.T) {
	source := chartThemeFixture("column")
	table, err := newTableData(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	table.Cells[0][0].Runs = []ObjectTextRun{{Text: "Linked", Color: "#112233", ColorToken: "accent-2", FontRole: "body", FontID: "sans"}}
	source.Objects.Tables = map[string]TableData{"source-table": table}
	source.Slides[0].Elements = append(source.Slides[0].Elements, Element{ID: "table", Kind: "table", ObjectID: "source-table", Query: "width=80&height=15&top=30"})
	bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
	if err != nil {
		t.Fatal(err)
	}
	for _, themed := range []bool{false, true} {
		for _, master := range []bool{false, true} {
			t.Run(fmt.Sprintf("theme=%t/master=%t", themed, master), func(t *testing.T) {
				destination := Deck{Slides: []Slide{{ID: "target"}}}
				if themed {
					destination.Theme = builtinDeckTheme("clean-dark")
					destination.Theme.Colors.Accent[1] = "#abcdef"
					destination.Theme.Fonts.Body.FontID = "mono"
				}
				s := newNativeEditorSession("Destination.md", destination)
				s.masterMode = master
				s.currentMaster = 0
				before := cloneDeck(s.deck)
				if err := s.prepareAndApplyObjectClipboard(context.Background(), nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}); err != nil {
					t.Fatal(err)
				}
				target := s.deck.Slides[0]
				if master {
					target = s.deck.Masters.Base.Slide
				}
				var pastedChart ChartData
				var pastedTable TableData
				for _, e := range target.Elements {
					if e.Kind == "chart" {
						pastedChart = s.deck.Objects.Charts[e.ObjectID]
					}
					if e.Kind == "table" {
						pastedTable = s.deck.Objects.Tables[e.ObjectID]
					}
				}
				series := pastedChart.Series[0]
				run := pastedTable.Cells[0][0].Runs[0]
				if themed {
					if series.ColorToken != "accent-2" || run.ColorToken != "accent-2" || run.FontRole != "body" {
						t.Fatal("themed paste flattened linked roles", series, run)
					}
					if color := s.deck.chartStyleColor(ChartColorStyle{Color: series.Color, ColorToken: series.ColorToken}, ""); color != "#abcdef" {
						t.Fatal("chart did not adopt destination role", color)
					}
					if s.deck.wordFontID(run.FontRole, run.FontID) != "mono" {
						t.Fatal("font did not adopt destination role")
					}
				} else if series.ColorToken != "" || run.ColorToken != "" || run.FontRole != "" || series.Color != source.Theme.Colors.Accent[1] || run.Color != source.Theme.Colors.Accent[1] {
					t.Fatal("legacy paste lost source fallbacks", series, run)
				}
				if !reflect.DeepEqual(s.deck.Theme, before.Theme) || len(s.undo) != 1 {
					t.Fatal("paste changed destination theme/history")
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(s.deck, before) {
					t.Fatal("undo failed to restore destination")
				}
				if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil {
					t.Fatal(err)
				}
				serialized, err := serializeDeck("Destination.md", s.deck)
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := parseDeckData("Destination.md", serialized)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(loaded.Objects, s.deck.Objects) {
					t.Fatal("saved clipboard payloads lost destination-linked metadata")
				}
			})
		}
	}
}

func TestObjectClipboardEndpointRejectsStaleRevisionAndCancellation(t *testing.T) {
	s := newNativeEditorSession("prepare.md", Deck{Slides: []Slide{{ID: "destination"}}})
	bundle := ObjectClipboard{Version: 1, Elements: []Element{{ID: "text", Kind: "text", Text: "Pasted"}}}
	for _, cancelled := range []bool{false, true} {
		version := s.version - 1
		body, _ := json.Marshal(nativeEditorAction{Action: "paste-objects", Clipboard: &bundle, SceneRevision: &version})
		r := httptest.NewRequest(http.MethodPost, "/api/editor/paste-clipboard", strings.NewReader(string(body)))
		want := http.StatusConflict
		if cancelled {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			r = r.WithContext(ctx)
			want = http.StatusRequestTimeout
		}
		w := httptest.NewRecorder()
		s.handlePasteObjectClipboard(w, r)
		if w.Code != want || len(s.deck.Slides[0].Elements) != 0 || len(s.undo) != 0 {
			t.Fatal("endpoint accepted an obsolete/cancelled request", w.Code, w.Body.String())
		}
	}
}
