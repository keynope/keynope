package main

import "testing"

func TestNominateSettings(t *testing.T) {
	for _, named := range []bool{false, true} {
		definition, err := normalizeEngagement(EngagementDefinition{Kind: "nominate", Named: named, TimerSeconds: 60, Options: []string{"ignored"}, GroupCount: 4})
		if err != nil {
			t.Fatal(err)
		}
		if definition.Named != named || definition.TimerSeconds != 60 || len(definition.Options) != 0 || definition.GroupCount != 0 {
			t.Fatalf("unexpected settings: %+v", definition)
		}
		metadata, err := encodeEngagementMetadata(&definition)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := decodeEngagementMetadata(metadata)
		if err != nil || loaded.Kind != "nominate" || loaded.Named != named {
			t.Fatalf("round trip: %+v %v", loaded, err)
		}
		if activityElementText(activityTitleRole, &definition) != "Nominate" {
			t.Fatal("missing title")
		}
	}
}
