package main

import "testing"

func TestDeducerSettings(t *testing.T) {
	definition, err := normalizeEngagement(EngagementDefinition{Kind: "deducer", GroupCount: 3, MaxEntries: 7, TimerSeconds: 120})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := encodeEngagementMetadata(&definition)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := decodeEngagementMetadata(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GroupCount != 3 || loaded.MaxEntries != 7 || loaded.TimerSeconds != 120 || !loaded.Named {
		t.Fatalf("lost Deducer configuration: %+v", loaded)
	}
	if activityElementText(activityTitleRole, &EngagementDefinition{Kind: "deducer"}) != "Deducer" {
		t.Fatal("missing activity title")
	}
	for _, bad := range []EngagementDefinition{
		{Kind: "deducer", GroupCount: -1}, {Kind: "deducer", GroupCount: 11},
		{Kind: "deducer", MaxEntries: -1}, {Kind: "deducer", MaxEntries: 101},
		{Kind: "deducer", TimerSeconds: 59}, {Kind: "deducer", TimerSeconds: 601},
	} {
		if _, err := normalizeEngagement(bad); err == nil {
			t.Fatalf("accepted invalid settings: %+v", bad)
		}
	}
	defaults, err := normalizeEngagement(EngagementDefinition{Kind: "deducer"})
	if err != nil || defaults.GroupCount != 4 || defaults.MaxEntries != 5 || defaults.TimerSeconds != 300 {
		t.Fatal("incorrect defaults")
	}
}
