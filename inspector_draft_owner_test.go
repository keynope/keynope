package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStampedCommandsCannotCrossDocumentOwners(t *testing.T) {
	for _, master := range []bool{false, true} {
		for _, name := range []string{"select-element", "set-selection-property", "update-elements", "delete-element", "set-object-lock", "start-timer", "undo", "copy-object-style", "confirm-save"} {
			if master && name == "start-timer" { // Timers are not authored-master commands.
				continue
			}
			t.Run(name+map[bool]string{false: "/slide", true: "/master"}[master], func(t *testing.T) {
				source, target := layerScopeSession(master), layerScopeSession(master)
				version := target.version
				action := nativeEditorAction{Action: name, NotesSessionID: source.notesSessionID, SceneRevision: &version, SceneMaster: master, Element: 0, Value: 1}
				switch name {
				case "set-selection-property":
					action.ObjectIDs, action.Property, action.PropertyValue = []string{"a"}, "paint.opacity", json.RawMessage(`0.4`)
				case "update-elements":
					element := cloneSlide(target.deck.Slides[0]).Elements[0]
					element.Text = "Changed description metadata"
					action.ElementIndices, action.ElementsData = []int{0}, []Element{element}
				case "start-timer":
					action.Value = 60
				case "copy-object-style":
					action.ObjectIDs = []string{"a"}
				case "confirm-save":
					action.Path, action.Value = "Owned save.md", int(version)
				}
				before, deck, mutation, deadline := target.state(), cloneDeck(target.deck), target.mutationRevision, target.timerDeadline
				if err := target.apply(action); err == nil || !reflect.DeepEqual(before, target.state()) || !reflect.DeepEqual(deck, target.deck) || target.mutationRevision != mutation || target.timerDeadline != deadline || len(target.undo) != 0 {
					t.Fatal("stamped command crossed document ownership", err)
				}
				// Matching stamps are not mistaken for notes-specific commands.
				action.NotesSessionID = target.notesSessionID
				if err := target.apply(action); err != nil {
					t.Fatal("valid document stamp rejected", err)
				}
			})
		}
	}
}

func TestInspectorDraftFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_INSPECTOR_DRAFT_EVIDENCE")
	if root == "" {
		t.Skip("owned browser fixture not requested")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	palette := []color.NRGBA{{230, 50, 70, 255}, {40, 190, 100, 255}, {40, 100, 230, 255}, {240, 190, 30, 255}}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetNRGBA(x, y, palette[x/16+2*(y/16)])
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	id, asset, source, err := embeddedImageAsset(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	slide := Slide{Elements: []Element{
		{ID: "image", Kind: "image", AssetID: id, Path: source, Query: "image-style=modern&image-fit=contain&left=20&top=10&width=80&height=30&alt-text=Accepted+description"},
		{ID: "shape", Kind: "shape", Query: "shape=square&left=130&top=10&width=60&height=30"},
	}}
	deck := Deck{Appearance: &DeckAppearance{Version: 3}, Masters: defaultMasterDeck(), Slides: []Slide{cloneSlide(slide), cloneSlide(slide)}, Assets: map[string]DeckAsset{id: asset}}
	deck.Masters.Base.Slide = cloneSlide(slide)
	deck.Masters.Layouts[0].Slide = cloneSlide(slide)
	data, err := serializeDeck("Inspector drafts.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixture.md"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
