package app

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestSceneAttachmentTransferMatchesRenderedRoutes(t *testing.T) {
	deck := powerupProofCorpus(t)["diagrams.md"]
	for _, canvas := range [][2]int{{245, 56}, {160, 90}} {
		for _, master := range []bool{false, true} {
			d := cloneDeck(deck)
			var preview Slide
			if master {
				d.Masters.Base.Slide = cloneSlide(d.Slides[0])
				preview = d.masterRenderPreview(0, canvas[0], canvas[1])
			} else {
				preview = d.slideRenderPreview(0, canvas[0], canvas[1])
			}
			if preview.previewGeometry == nil || preview.previewGeometry.scene != preview.ModernScene {
				t.Fatal("missing request-owned geometry transfer")
			}
			pages := exportSlidePages(preview, 0, 1, canvas[0], canvas[1])
			if len(pages) != 1 || !reflect.DeepEqual(pages[0].ShapePorts, preview.previewGeometry.ports) || !reflect.DeepEqual(pages[0].Connectors, preview.previewGeometry.connectors) {
				t.Fatal("page did not use complete attachment projection")
			}
			byID := sceneObjectsByID(*preview.ModernScene)
			for _, route := range pages[0].Connectors {
				object, exists := byID[route.ID]
				if !exists || len(object.Points) != len(route.Points) {
					t.Fatal("attachment route differs from painted connector")
				}
				for i, p := range route.Points {
					if math.Abs(object.Points[i].X-p.X*1920/float64(canvas[0])) > 1e-8 || math.Abs(object.Points[i].Y-p.Y*1080/float64(canvas[1])) > 1e-8 {
						t.Fatal("route position differs from scene")
					}
				}
			}
			// The metadata is a temporary handoff, not another scene schema or
			// authored input held by history/dependency caches.
			if cloneSlide(preview).previewGeometry != nil || workspaceSlideInput(preview).previewGeometry != nil {
				t.Fatal("ephemeral geometry escaped into an authored clone")
			}
			encoded, err := json.Marshal(pages)
			if err != nil || strings.Contains(string(encoded), "previewGeometry") {
				t.Fatal("geometry sidecar exposed on wire")
			}
			safe := standaloneExportSlide(preview, 0)
			if safe.previewGeometry == nil || safe.previewGeometry.scene != safe.ModernScene || preview.previewGeometry.scene != preview.ModernScene {
				t.Fatal("unfiltered export lost transfer or changed original scene association")
			}
		}
	}
}

func TestSceneAttachmentTransferContextFallback(t *testing.T) {
	deck := powerupProofCorpus(t)["diagrams.md"]
	preview := deck.slideRenderPreview(0, 245, 56)
	geometry := *preview.previewGeometry
	geometry.ports = []shapePort{{ID: "transfer-only"}}
	preview.previewGeometry = &geometry
	ports, _ := scenePageAttachments(preview, 245, 56)
	if len(ports) != 1 || ports[0].ID != "transfer-only" {
		t.Fatal("same-context transfer missed")
	}
	for _, changed := range []bool{false, true} {
		copy := preview
		cols := 160
		if changed {
			scene := *preview.ModernScene
			copy.ModernScene = &scene
			cols = 245
		}
		ports, _ := scenePageAttachments(copy, cols, 56)
		for _, p := range ports {
			if p.ID == "transfer-only" {
				t.Fatal("stale context reused")
			}
		}
	}
	if deck.slideRenderPreview(9, 245, 56).ModernScene != nil {
		t.Fatal("invalid slide produced a scene")
	}
}

func TestSceneAttachmentTransferPrivacyDropsRemovedPorts(t *testing.T) {
	deck := powerupProofCorpus(t)["diagrams.md"]
	// An authored shape masquerading as generated activity artwork exercises
	// filtering of ports as well as the usual QR-image/text object filtering.
	deck.Slides[0].Elements[0].PlaceholderRole = activityQRCodeRole
	preview := deck.slideRenderPreview(0, 245, 56)
	privateID := deck.Slides[0].Elements[0].ID
	safe := standaloneExportSlide(preview, 0)
	if safe.previewGeometry != nil {
		t.Fatal("filtered export retained pre-filter attachment geometry")
	}
	page := exportSlidePages(safe, 0, 1, 245, 56)[0]
	for _, p := range page.ShapePorts {
		if p.ID == privateID {
			t.Fatal("removed activity artwork port leaked")
		}
	}
	if len(preview.previewGeometry.ports) == 0 {
		t.Fatal("privacy filtering mutated original transfer")
	}
}
