package app

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestPartialCropCommandsHistoryAndRejection(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	for _, mode := range []string{"normal", "tab", "base", "layout"} {
		for _, field := range []string{"mask", "crop", "both"} {
			t.Run(mode+"/"+field, func(t *testing.T) {
				d := sceneFixture(t)
				d.Slides[0].Engagement, d.Slides[0].EngagementResult = nil, nil
				s := newNativeEditorSession("Partial crop.md", d)
				if mode == "tab" {
					s.deck.Slides[0].TabID = "crop-tab"
					syncSlideTabs(&s.deck)
				}
				index := 0
				if mode == "base" || mode == "layout" {
					s.masterMode = true
					if mode == "layout" {
						index = 1
					}
					s.currentMaster = index
					*masterSlideAt(&s.deck, index) = cloneSlide(s.deck.Slides[0])
					masterSlideAt(&s.deck, index).ID = "partial-crop-master"
				}
				before := cloneDeck(s.deck)
				revision := s.version
				mask := "rounded"
				a := nativeEditorAction{Action: "set-scene-crop", Slide: index, SceneMaster: s.masterMode, SceneRevision: &revision, ObjectID: "image"}
				if field != "crop" {
					a.ModernMask = &mask
				}
				if field != "mask" {
					a.Crop = &sceneCrop{Left: .2}
				}
				if err := s.apply(a); err != nil {
					t.Fatal(err)
				}
				after := cloneDeck(s.deck)
				if reflect.DeepEqual(before, after) {
					t.Fatal("partial command did not edit")
				}
				revision = s.version
				if err := s.apply(a); err != nil || s.version != revision {
					t.Fatal("partial no-op", err)
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(before, s.deck) {
					t.Fatal("partial undo", err)
				}
				if err := s.apply(nativeEditorAction{Action: "redo"}); err != nil || !reflect.DeepEqual(after, s.deck) {
					t.Fatal("partial redo", err)
				}
				revision = s.version
				invalidMask := "sphere"
				for _, bad := range []nativeEditorAction{{}, {Crop: &sceneCrop{Left: math.NaN()}, ModernMask: &mask}, {ModernMask: &invalidMask}, {ObjectID: "absent", ModernMask: &mask}, {ObjectID: "body", ModernMask: &mask}} {
					bad.Action, bad.Slide, bad.SceneMaster, bad.SceneRevision = "set-scene-crop", index, s.masterMode, &revision
					if bad.ObjectID == "" {
						bad.ObjectID = "image"
					}
					assertEditorFailureUnchanged(t, s, bad)
				}
			})
		}
	}
}

func cropFieldOwnershipFixture(t *testing.T) Deck {
	t.Helper()
	d := strokeInheritanceFixture()
	sources := sceneFixture(t)
	d.Assets = sources.Assets
	var assetID string
	for _, e := range sources.Slides[0].Elements {
		if e.Kind == "image" {
			assetID = e.AssetID
			break
		}
	}
	master := &d.Masters.Layouts[0].Slide.Elements[0]
	master.Kind, master.AssetID = "image", assetID
	master.Query = "image-style=modern&align=center&width=100&height=30&modern-crop=0.1,0.1,0.1,0.1&modern-mask=ellipse&brightness=1.4"
	child := &d.Slides[0].Elements[0]
	child.Kind, child.AssetID, child.Text, child.Query = "image", assetID, "", "top=10&master-clear=shadow"
	return d
}

func TestCropFieldsRemainIndependentlyInherited(t *testing.T) {
	for _, field := range []string{"mask", "crop"} {
		t.Run(field, func(t *testing.T) {
			d := cropFieldOwnershipFixture(t)
			before := cloneDeck(d)
			mask := "rounded"
			a := nativeEditorAction{Slide: 0, ObjectID: "instance"}
			if field == "mask" {
				a.ModernMask = &mask
			} else {
				a.Crop = &sceneCrop{Left: .2}
			}
			changed, err := applySceneCrop(&d, a)
			if err != nil || !changed {
				t.Fatal("partial crop command", changed, err)
			}
			if !reflect.DeepEqual(d.Masters, before.Masters) || !reflect.DeepEqual(d.Assets, before.Assets) {
				t.Fatal("partial crop changed source")
			}
			q := mustQuery(d.Slides[0].Elements[0].Query)
			untouched := "modern-crop"
			if field == "crop" {
				untouched = "modern-mask"
			}
			if q.Has(untouched) {
				t.Fatal("materialized untouched inherited field", q)
			}
			for _, key := range strings.Split(q.Get("master-clear"), ",") {
				if key == untouched {
					t.Fatal("suppressed untouched inherited field", q)
				}
			}
			data, err := serializeDeck("Partial crop.md", d)
			if err != nil {
				t.Fatal(err)
			}
			d, err = parseDeckData("Partial crop.md", data)
			if err != nil {
				t.Fatal(err)
			}
			if field == "mask" {
				if removeImageQueryKeys(d.Slides[0].Elements[0].Query, "modern-mask") != mustQuery(before.Slides[0].Elements[0].Query).Encode() {
					t.Fatal("mask-only changed natural frame", q)
				}
				d.Masters.Layouts[0].Slide.Elements[0].Query = setQueryValue(d.Masters.Layouts[0].Slide.Elements[0].Query, "modern-crop", "0.2,0,0,0")
			} else {
				d.Masters.Layouts[0].Slide.Elements[0].Query = setQueryValue(d.Masters.Layouts[0].Slide.Elements[0].Query, "modern-mask", "rounded")
			}
			found := false
			for _, e := range d.ResolveSlide(0, false).Elements {
				if e.ID == "instance" {
					found = true
					rq := mustQuery(e.Query)
					if field == "mask" && rq.Get("modern-crop") != "0.2,0,0,0" || field == "crop" && rq.Get("modern-mask") != "rounded" {
						t.Fatal("untouched field stopped following master", rq)
					}
				}
			}
			if !found {
				t.Fatal("reopened inherited image missing")
			}
		})
	}
}
