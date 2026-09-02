package main

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const engagementVersion = 1

var engagementMetaRE = regexp.MustCompile(`<!--\s*keynope-engagement\s+version=1\s+base64:([A-Za-z0-9+/=]+)\s*-->`)

// Eight symbols from a 64-character alphabet carry 48 bits of entropy. The
// live channel creation is atomic, so an occupied code is rejected rather
// than ever joining two unrelated activities together.
const activityCodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

var activityCodeRE = regexp.MustCompile(`^[A-Za-z0-9_-]{8}$`)

// EngagementDefinition is authored deck data. Live answers deliberately do not
// belong here: saving a deck must never persist names, votes, or workshop input.
type EngagementDefinition struct {
	ID      string   `json:"id,omitempty"`
	Code    string   `json:"code,omitempty"`
	Kind    string   `json:"kind"`
	Prompt  string   `json:"prompt,omitempty"`
	Options []string `json:"options,omitempty"`
	Zones   []string `json:"zones,omitempty"`
	Cards   []string `json:"cards,omitempty"`
}

func randomActivityString(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("activity identifier length must be positive")
	}
	result := make([]byte, 0, length)
	limit := 256 - 256%len(activityCodeAlphabet)
	for len(result) < length {
		var raw [1]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", fmt.Errorf("generate activity identifier: %w", err)
		}
		if int(raw[0]) >= limit {
			continue
		}
		result = append(result, activityCodeAlphabet[int(raw[0])%len(activityCodeAlphabet)])
	}
	return string(result), nil
}

func ensureActivityIdentity(definition *EngagementDefinition) error {
	if definition == nil {
		return nil
	}
	if definition.ID == "" {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return fmt.Errorf("generate activity id: %w", err)
		}
		definition.ID = strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	}
	if definition.Code == "" {
		code, err := randomActivityString(8)
		if err != nil {
			return err
		}
		definition.Code = code
	}
	if !activityCodeRE.MatchString(definition.Code) {
		return fmt.Errorf("activity code must contain eight URL-safe characters")
	}
	return nil
}

func activityJoinURL(definition *EngagementDefinition) string {
	if definition == nil || definition.Code == "" {
		return ""
	}
	return "https://keynope.sh/join/" + definition.Code
}

// EngagementRuntimeState is transient presenter state shared between Keynope's
// controller and presentation surfaces. It is never serialized into Markdown.
type EngagementRuntimeState struct {
	Definition   EngagementDefinition `json:"definition"`
	Slide        int                  `json:"slide"`
	Phase        int                  `json:"phase"`
	Counts       []int                `json:"counts,omitempty"`
	Ideas        []string             `json:"ideas,omitempty"`
	Assignments  []int                `json:"assignments,omitempty"`
	Respondents  []string             `json:"respondents,omitempty"`
	SessionCode  string               `json:"sessionCode,omitempty"`
	JoinURL      string               `json:"joinUrl,omitempty"`
	Participants int                  `json:"participants,omitempty"`
}

func cloneEngagementRuntime(runtime *EngagementRuntimeState) *EngagementRuntimeState {
	if runtime == nil {
		return nil
	}
	copyRuntime := *runtime
	copyRuntime.Definition = *cloneEngagement(&runtime.Definition)
	copyRuntime.Counts = append([]int(nil), runtime.Counts...)
	copyRuntime.Ideas = append([]string(nil), runtime.Ideas...)
	copyRuntime.Assignments = append([]int(nil), runtime.Assignments...)
	copyRuntime.Respondents = append([]string(nil), runtime.Respondents...)
	return &copyRuntime
}

func cloneEngagement(definition *EngagementDefinition) *EngagementDefinition {
	if definition == nil {
		return nil
	}
	copyDefinition := *definition
	copyDefinition.Options = append([]string(nil), definition.Options...)
	copyDefinition.Zones = append([]string(nil), definition.Zones...)
	copyDefinition.Cards = append([]string(nil), definition.Cards...)
	return &copyDefinition
}

func normalizeEngagement(definition EngagementDefinition) (EngagementDefinition, error) {
	if err := ensureActivityIdentity(&definition); err != nil {
		return EngagementDefinition{}, err
	}
	definition.Kind = strings.ToLower(strings.TrimSpace(definition.Kind))
	definition.Prompt = strings.TrimSpace(definition.Prompt)
	definition.Options = cleanEngagementItems(definition.Options)
	definition.Zones = cleanEngagementItems(definition.Zones)
	definition.Cards = cleanEngagementItems(definition.Cards)
	if len(definition.Prompt) > 500 {
		return EngagementDefinition{}, fmt.Errorf("engagement prompt is too long")
	}
	switch definition.Kind {
	case "pulse":
		if len(definition.Options) == 0 {
			definition.Options = []string{"0", "1", "2", "3", "4", "5"}
		}
		if len(definition.Options) < 2 || len(definition.Options) > 12 {
			return EngagementDefinition{}, fmt.Errorf("pulse needs between 2 and 12 choices")
		}
		definition.Zones, definition.Cards = nil, nil
	case "storm":
		definition.Options, definition.Zones, definition.Cards = nil, nil, nil
	case "sort":
		if len(definition.Zones) < 2 || len(definition.Zones) > 8 {
			return EngagementDefinition{}, fmt.Errorf("sort needs between 2 and 8 destinations")
		}
		if len(definition.Cards) == 0 || len(definition.Cards) > 40 {
			return EngagementDefinition{}, fmt.Errorf("sort needs between 1 and 40 cards")
		}
		definition.Options = nil
	default:
		return EngagementDefinition{}, fmt.Errorf("unknown engagement type %q", definition.Kind)
	}
	return definition, nil
}

func cleanEngagementItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if len(item) > 160 {
			item = item[:160]
		}
		out = append(out, item)
	}
	return out
}

func encodeEngagementMetadata(definition *EngagementDefinition) (string, error) {
	if definition == nil {
		return "", nil
	}
	normalized, err := normalizeEngagement(*definition)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("<!-- keynope-engagement version=%d base64:%s -->", engagementVersion, base64.StdEncoding.EncodeToString(payload)), nil
}

func decodeEngagementMetadata(comment string) (*EngagementDefinition, error) {
	match := engagementMetaRE.FindStringSubmatch(strings.TrimSpace(comment))
	if match == nil {
		return nil, nil
	}
	payload, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		return nil, fmt.Errorf("decode engagement: %w", err)
	}
	var definition EngagementDefinition
	if err := json.Unmarshal(payload, &definition); err != nil {
		return nil, fmt.Errorf("decode engagement: %w", err)
	}
	normalized, err := normalizeEngagement(definition)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}
