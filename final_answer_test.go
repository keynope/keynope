package main

import "testing"

func TestFinalAnswerSettings(t *testing.T) {
	definition, err := normalizeEngagement(EngagementDefinition{Kind: "finalanswer", GroupCount: 9, MaxEntries: 99})
	if err != nil {
		t.Fatal(err)
	}
	if definition.GroupCount != 0 || definition.MaxEntries != 1 || !definition.Named || definition.TimerSeconds != 300 {
		t.Fatalf("Final Answer must reuse groups and accept one answer: %+v", definition)
	}
	metadata, err := encodeEngagementMetadata(&definition)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := decodeEngagementMetadata(metadata)
	if err != nil || loaded.Kind != "finalanswer" || loaded.MaxEntries != 1 {
		t.Fatalf("round trip: %+v %v", loaded, err)
	}
	if activityElementText(activityTitleRole, &definition) != "Final Answer" {
		t.Fatal("missing title")
	}
	for _, seconds := range []int{59, 601} {
		if _, err := normalizeEngagement(EngagementDefinition{Kind: "finalanswer", TimerSeconds: seconds}); err == nil {
			t.Fatal("invalid timer accepted")
		}
	}
}
