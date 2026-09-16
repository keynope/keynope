package main

import "testing"

func TestShuffleActivityMetadata(t *testing.T) {
	d, err := normalizeEngagement(EngagementDefinition{Kind: "shuffle", Prompt: "Meet a new group", TimerSeconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Named || d.TimerSeconds != 0 {
		t.Fatal("Shuffle must show names and use explicit presenter controls")
	}
	encoded, err := encodeEngagementMetadata(&d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := decodeEngagementMetadata(encoded)
	if err != nil || loaded.Kind != "shuffle" || loaded.Prompt != d.Prompt {
		t.Fatalf("Shuffle did not roundtrip: %+v %v", loaded, err)
	}
	if activityElementText(activityTitleRole, &d) != "Shuffle" {
		t.Fatal("missing Shuffle title")
	}
}
