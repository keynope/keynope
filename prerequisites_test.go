package main

import (
	"encoding/json"
	"testing"
)

func TestPrerequisitesDefinition(t *testing.T) {
	def, err := normalizeEngagement(EngagementDefinition{Kind: "prerequisites", Named: true, Prerequisites: []PrerequisiteItem{{Title: " Install ", Instructions: " Run the installer. "}}})
	if err != nil {
		t.Fatal(err)
	}
	if def.Prerequisites[0].Title != "Install" || !def.Named {
		t.Fatalf("unexpected definition: %#v", def)
	}
	copy := cloneEngagement(&def)
	copy.Prerequisites[0].Instructions = "Changed"
	if def.Prerequisites[0].Instructions != "Run the installer." {
		t.Fatal("clone shares prerequisites")
	}
	encoded, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	var decoded EngagementDefinition
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Prerequisites[0] != def.Prerequisites[0] {
		t.Fatal("instructions lost in round trip")
	}
	for _, items := range [][]PrerequisiteItem{nil, {{Title: " "}}, make([]PrerequisiteItem, 41)} {
		if _, err = normalizeEngagement(EngagementDefinition{Kind: "prerequisites", Prerequisites: items}); err == nil {
			t.Fatal("invalid prerequisite list accepted")
		}
	}
}

func TestPrerequisitesOptionalGrouping(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		def, err := normalizeEngagement(EngagementDefinition{Kind: "prerequisites", DisableGrouping: disabled, Prerequisites: []PrerequisiteItem{{Title: "Ready"}}})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(cloneEngagement(&def))
		if err != nil {
			t.Fatal(err)
		}
		var decoded EngagementDefinition
		if err = json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.DisableGrouping != disabled {
			t.Fatal("grouping preference lost during serialization")
		}
	}
	var legacy EngagementDefinition
	if err := json.Unmarshal([]byte(`{"kind":"prerequisites","prerequisites":[{"title":"Ready"}]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.DisableGrouping {
		t.Fatal("old decks must retain grouping")
	}
}
