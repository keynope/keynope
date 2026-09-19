package main

import (
	"encoding/json"
	"testing"
)

func TestChosenDefinition(t *testing.T) {
	for _, count := range []int{0, 1, 5, 1000} {
		definition, err := normalizeEngagement(EngagementDefinition{Kind: "chosen", ChosenCount: count, TimerSeconds: 120})
		if err != nil {
			t.Fatal(err)
		}
		if !definition.Named || definition.ChosenCount != max(1, count) || definition.TimerSeconds != 120 {
			t.Fatalf("incorrect chosen settings: %#v", definition)
		}
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		var decoded EngagementDefinition
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.ChosenCount != definition.ChosenCount || cloneEngagement(&definition).ChosenCount != definition.ChosenCount {
			t.Fatal("chosen count lost in serialization or cloning")
		}
		metadata, err := encodeEngagementMetadata(&definition)
		if err != nil {
			t.Fatal(err)
		}
		fromMD, err := decodeEngagementMetadata(metadata)
		if err != nil || fromMD.ChosenCount != definition.ChosenCount || !fromMD.Named {
			t.Fatal("chosen settings lost in Markdown round trip")
		}
	}
	for _, count := range []int{-1, 1001} {
		if _, err := normalizeEngagement(EngagementDefinition{Kind: "chosen", ChosenCount: count}); err == nil {
			t.Fatalf("accepted invalid count %d", count)
		}
	}
	other, err := normalizeEngagement(EngagementDefinition{Kind: "storm", ChosenCount: 7})
	if err != nil || other.ChosenCount != 0 {
		t.Fatal("chosen count leaked to another activity")
	}
	if activityElementText(activityTitleRole, &EngagementDefinition{Kind: "chosen"}) != "The Chosen" {
		t.Fatal("missing activity title")
	}
}

func TestPressureCookerDefinition(t *testing.T) {
	for _, seconds := range []int{0, 1, 120, 5999} {
		definition, err := normalizeEngagement(EngagementDefinition{Kind: "pressure", TimerSeconds: seconds})
		if err != nil {
			t.Fatal(err)
		}
		want := seconds
		if want == 0 {
			want = 300
		}
		if definition.TimerSeconds != want {
			t.Fatalf("timer = %d, want %d", definition.TimerSeconds, want)
		}
		metadata, err := encodeEngagementMetadata(&definition)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeEngagementMetadata(metadata)
		if err != nil || decoded.TimerSeconds != want {
			t.Fatal("timer lost in deck metadata")
		}
	}
	for _, seconds := range []int{-1, 6000} {
		if _, err := normalizeEngagement(EngagementDefinition{Kind: "pressure", TimerSeconds: seconds}); err == nil {
			t.Fatal("invalid pressure timer accepted")
		}
	}
	if activityElementText(activityTitleRole, &EngagementDefinition{Kind: "pressure"}) != "Pressure Cooker" {
		t.Fatal("missing title")
	}
}

func TestWorkshopDefinitions(t *testing.T) {
	for _, kind := range []string{"dots", "finishpair", "ball", "gallery", "hunt", "teach", "fame", "agreements", "three"} {
		definition, err := normalizeEngagement(EngagementDefinition{Kind: kind, Options: []string{"*First", "Second"}})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if definition.Kind != kind {
			t.Fatalf("kind changed: %s", definition.Kind)
		}
		if kind == "dots" && definition.DotBudget != 3 {
			t.Fatal("missing default dot budget")
		}
		if kind == "teach" && (definition.JoinSeconds != 180 || definition.DiscussionSeconds != 60) {
			t.Fatal("missing teach-back timers")
		}
		if kind == "finishpair" && !definition.Named {
			t.Fatal("pairing requires names")
		}
	}
	for _, definition := range []EngagementDefinition{{Kind: "dots", Options: []string{"A", "B"}, DotBudget: 21}, {Kind: "dots", Options: []string{"A"}}, {Kind: "hunt", Options: []string{"A", "B"}}} {
		if _, err := normalizeEngagement(definition); err == nil {
			t.Fatalf("accepted invalid definition %#v", definition)
		}
	}
}
