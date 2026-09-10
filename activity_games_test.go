package main

import "testing"

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
