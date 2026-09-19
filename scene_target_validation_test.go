package main

import (
	"reflect"
	"testing"
)

func TestSceneCommandsRejectInvalidTargetsAtomically(t *testing.T) {
	for _, command := range []string{"text", "label", "crop", "style"} {
		for _, invalid := range []string{"duplicate", "missing", "empty", "inherited", "locked", "protected", "malformed"} {
			t.Run(command+"/"+invalid, func(t *testing.T) {
				// Match editor ownership (deep cloned state), including canonical
				// nil/empty collections used by history snapshots.
				deck := cloneDeck(sceneFixture(t))
				id := "body"
				if command == "label" {
					id = "box"
				}
				if command == "crop" {
					id = "image"
				}
				index := -1
				for i, e := range deck.Slides[0].Elements {
					if e.ID == id {
						index = i
					}
				}
				if index < 0 {
					t.Fatal("fixture target missing")
				}
				switch invalid {
				case "duplicate":
					deck.Slides[0].Elements = append(deck.Slides[0].Elements, deck.Slides[0].Elements[index])
				case "missing":
					id = "absent"
				case "empty":
					id = ""
				case "inherited":
					deck.Slides[0].Elements[index].Inherited = true
				case "locked":
					deck.Slides[0].Elements[index].Query += "&object-locked=1"
				case "protected":
					deck.Slides[0].Elements[index].PlaceholderRole = activityTitleRole
				case "malformed":
					deck.Slides[0].Elements[index].Query += "&broken=%xx"
				}
				before := cloneDeck(deck)
				action := nativeEditorAction{Slide: 0, ObjectID: id, TextRuns: []sceneRun{{Text: "Must not commit"}}, Crop: &sceneCrop{Left: .1}}
				var changed bool
				var err error
				switch command {
				case "crop":
					changed, err = applySceneCrop(&deck, action)
				case "style":
					changed, err = setElementStyles(&deck.Slides[0], []string{id}, "modern")
				default:
					changed, err = applySceneText(&deck, action)
				}
				if err == nil || changed {
					t.Fatalf("invalid target accepted: changed=%v err=%v", changed, err)
				}
				if !reflect.DeepEqual(before, deck) {
					t.Fatal("rejected edit mutated deck")
				}
			})
		}
	}
}
