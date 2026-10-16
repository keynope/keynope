package main

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestClipboardPreparationConsumesOnlyOwnedDestinationRegistry(t *testing.T) {
	for _, scope := range []string{"normal", "tab", "base", "layout"} {
		t.Run(scope, func(t *testing.T) {
			destination := structuredFixture()
			destination.EnsureDefaultMasters()
			if scope == "tab" {
				destination.Slides[0].TabID = "clipboard-tab"
				syncSlideTabs(&destination)
			}
			s := newNativeEditorSession("destination.md", destination)
			if scope == "base" || scope == "layout" {
				s.masterMode = true
				if scope == "layout" {
					s.currentMaster = 1
				}
			}
			source := structuredFixture()
			bundle, err := makeObjectClipboard(source, source.Slides[0].Elements)
			if err != nil {
				t.Fatal(err)
			}
			originalSource, originalDestination := cloneDeck(source), cloneDeck(s.deck)
			action := nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}
			_, snapshot, err := s.captureObjectClipboardPaste(context.Background(), action)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Objects == s.deck.Objects {
				t.Fatal("capture borrowed live registry")
			}
			if err := bundle.Validate(); err != nil {
				t.Fatal(err)
			}
			elements, _, objects, err := prepareValidatedOwnedClipboardPaste(context.Background(), snapshot, bundle)
			if err != nil {
				t.Fatal(err)
			}
			if objects != snapshot.Objects {
				t.Fatal("owned destination registry was copied a second time")
			}
			// The consumed snapshot is disposable. Mutating either retained or
			// inserted payloads must not affect the live destination or source.
			objects.Tables["table-1"].Cells[0][0].Text = "Retained destination edit"
			for _, element := range elements {
				if element.Kind == "table" {
					objects.Tables[element.ObjectID].Cells[0][0].Text = "Inserted edit"
				}
				if element.Kind == "chart" {
					objects.Charts[element.ObjectID].Series[0].Values[0] = -999
				}
			}
			if !reflect.DeepEqual(source, originalSource) || !reflect.DeepEqual(s.deck, originalDestination) {
				t.Fatal("consuming the captured registry mutated source/live destination")
			}
			if !reflect.DeepEqual(bundle.Objects.Tables["table-1"], source.Objects.Tables["table-1"]) || !reflect.DeepEqual(bundle.Objects.Charts["chart-1"], source.Objects.Charts["chart-1"]) {
				t.Fatal("prepared payload aliases source clipboard")
			}
		})
	}
}

func TestDirectClipboardPreparationStillOwnsDestinationRegistry(t *testing.T) {
	destination := structuredFixture()
	before := cloneDeck(destination)
	bundle := ObjectClipboard{Version: 1, Elements: []Element{{ID: "pasted-text", Kind: "text", Text: "Hello"}}}
	_, _, objects, err := prepareObjectClipboardPasteContext(context.Background(), destination, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if objects == destination.Objects {
		t.Fatal("direct helper consumed caller-owned registry")
	}
	objects.Tables["table-1"].Cells[1][0].Runs[0].Text = "Independent edit"
	objects.Charts["chart-1"].Series[0].Values[0] = -77
	objects.Builds["slide-1"].Steps[0].TargetIDs[0] = "another-target"
	if !reflect.DeepEqual(destination, before) {
		t.Fatal("direct helper aliases caller-owned payloads")
	}
}

func BenchmarkClipboardCapturedRegistryPreparation(b *testing.B) {
	for _, consume := range []bool{false, true} {
		b.Run(fmt.Sprintf("consume-owned=%t", consume), func(b *testing.B) {
			s := newNativeEditorSession("benchmark.md", structuredReplacementFixture(b, 24))
			bundle := ObjectClipboard{Version: 1, Elements: []Element{{ID: "text", Kind: "text", Text: "Pasted"}}}
			action := nativeEditorAction{Action: "paste-objects", Clipboard: &bundle}
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, snapshot, err := s.captureObjectClipboardPaste(ctx, action)
				if err != nil {
					b.Fatal(err)
				}
				if consume {
					if err := bundle.validateContext(ctx); err != nil {
						b.Fatal(err)
					}
					_, _, _, err = prepareValidatedOwnedClipboardPaste(ctx, snapshot, bundle)
				} else {
					// Previous session behavior: the already-owned capture was
					// passed through the defensive direct-caller helper again.
					_, _, _, err = prepareObjectClipboardPasteContext(ctx, snapshot, bundle)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
