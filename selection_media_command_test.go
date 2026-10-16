package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMediaSingleOwnerDispatchMatchesCapabilities(t *testing.T) {
	for _, mode := range []string{"normal", "tab", "base", "layout"} {
		for _, command := range []string{"set-scene-crop", "promote-image-background"} {
			t.Run(mode+"/"+command, func(t *testing.T) {
				s, source, index := objectCommandFixture(t, mode)
				for i := range source.Elements {
					if source.Elements[i].ID == "alpha" || source.Elements[i].ID == "adjustable" {
						source.Elements[i].Query = setQueryValue(source.Elements[i].Query, "group", "media-group")
					}
				}
				revision := s.version
				a := nativeEditorAction{Action: command, Slide: index, SceneMaster: s.masterMode, SceneRevision: &revision, ObjectID: "alpha"}
				if command == "set-scene-crop" {
					a.Crop = &sceneCrop{Left: .1}
				}
				before := cloneDeck(s.deck)
				if command == "set-scene-crop" {
					body, _ := json.Marshal(a)
					w := httptest.NewRecorder()
					s.handleMediaDraft(w, httptest.NewRequest("POST", "/api/editor/media-draft", bytes.NewReader(body)))
					if w.Code != 400 {
						t.Fatal("collapsed-group crop preview accepted", w.Code)
					}
				}
				if err := s.apply(a); err == nil {
					t.Fatal("disabled single-owner command accepted for collapsed group")
				}
				if !reflect.DeepEqual(s.deck, before) || s.version != revision || len(s.undo) != 0 {
					t.Fatal("rejection mutated source/history")
				}
				s.editingGroup = "media-group"
				if err := s.apply(a); err != nil {
					t.Fatal("entered image rejected", err)
				}
				if len(s.undo) != 1 {
					t.Fatal("entered image edit not atomic")
				}
				if err := s.apply(nativeEditorAction{Action: "undo"}); err != nil || !reflect.DeepEqual(s.deck, before) {
					t.Fatal("entered image undo", err)
				}
			})
		}
	}
}
