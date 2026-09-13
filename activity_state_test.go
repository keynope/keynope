package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActivityStateNeedsOnlyChannelCode(t *testing.T) {
	var original EngagementDefinition
	if err := json.Unmarshal([]byte(`{"id":"test-state","code":"Test1234","kind":"onboarding","stateKey":"retired-secret"}`), &original); err != nil {
		t.Fatal(err)
	}
	a, err := normalizeEngagement(original)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeEngagementMetadata(&a)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeEngagementMetadata(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Code != original.Code {
		t.Fatal("channel lost during save/open")
	}
	payload, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "stateKey") || strings.Contains(string(payload), "retired-secret") {
		t.Fatal("retired presenter credential persisted")
	}
}
