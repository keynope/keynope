package main

import (
	"strings"
	"testing"
)

func TestAutomaticDotVotingConfigurationRoundTrip(t *testing.T) {
	definition := EngagementDefinition{ID: "vote", Kind: "dots", ImportPrevious: true, DotBudget: 3}
	normalized, err := normalizeEngagement(definition)
	if err != nil {
		t.Fatal(err)
	}
	if !normalized.ImportPrevious || len(normalized.Options) != 0 {
		t.Fatal("automatic voting should not need authored options")
	}
	deck := Deck{Slides: []Slide{{Engagement: &definition}}}
	data, err := serializeDeck("test.md", deck)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("test.md", data)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Slides[0].Engagement.ImportPrevious {
		t.Fatal("import preference lost on save/reload")
	}
	definition.ImportPrevious = false
	if _, err := normalizeEngagement(definition); err == nil {
		t.Fatal("manual voting still requires options")
	}
}

func TestImportedDotVotingKeepsWorkshopContributions(t *testing.T) {
	text := strings.Repeat("é", 200)
	definition := EngagementDefinition{ID: "vote", Kind: "dots", ImportPrevious: true, Options: []string{text, "Second idea"}}
	normalized, err := normalizeEngagement(definition)
	if err != nil || normalized.Options[0] != text {
		t.Fatalf("imported text was truncated: %v", err)
	}
	definition.Options = make([]string, 500)
	for i := range definition.Options {
		definition.Options[i] = "Workshop contribution"
	}
	if _, err := normalizeEngagement(definition); err != nil {
		t.Fatal(err)
	}
	definition.Options = append(definition.Options, "One too many")
	if _, err := normalizeEngagement(definition); err == nil {
		t.Fatal("oversized import accepted")
	}
}
