package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
)

// Results belong to the presenter's document, never to participant slide exports.
// Connection handles, signing keys, room codes and private chat are not archived.
type EngagementResult struct {
	Version    int                        `json:"version"`
	ActivityID string                     `json:"activityId"`
	Kind       string                     `json:"kind"`
	Definition *EngagementDefinition      `json:"definition,omitempty"`
	State      map[string]json.RawMessage `json:"state"`
}

const maxEngagementResultBytes = 1 << 20

var engagementResultRE = regexp.MustCompile(`<!--\s*keynope-engagement-results\s+version=1\s+base64:([A-Za-z0-9+/=]+)\s*-->`)

var engagementResultFields = map[string]bool{
	"phase": true, "counts": true, "ideas": true, "assignments": true,
	"respondents": true, "attributions": true, "groups": true, "participants": true,
	"questionIndex": true, "questionRevealed": true, "memberNames": true,
	"cardAssignments": true, "pairAssignments": true, "roleAssignments": true,
	"finishedAt": true, "startedAt": true, "stoppedAt": true, "game": true,
	"responseByIdentity": true, "entryResponses": true, "questionVotes": true,
	"stormResponses": true, "responseSequence": true,
	"resumeState": true,
}

func cloneEngagementResult(result *EngagementResult) *EngagementResult {
	if result == nil {
		return nil
	}
	copy := *result
	copy.Definition = cloneEngagement(result.Definition)
	copy.State = make(map[string]json.RawMessage, len(result.State))
	for key, value := range result.State {
		copy.State[key] = append(json.RawMessage(nil), value...)
	}
	return &copy
}

func validateEngagementResult(result *EngagementResult) error {
	if result == nil {
		return nil
	}
	if result.Version != 1 || result.ActivityID == "" || result.Kind == "" || result.Kind == "onboarding" {
		return fmt.Errorf("invalid activity result")
	}
	if result.Definition != nil && (result.Definition.ID != result.ActivityID || result.Definition.Kind != result.Kind || result.Definition.Code != "") {
		return fmt.Errorf("activity result definition does not match")
	}
	var phase int
	if json.Unmarshal(result.State["phase"], &phase) != nil || phase < 3 || phase > 4 {
		return fmt.Errorf("activity results must be in reveal or discuss state")
	}
	for key := range result.State {
		if !engagementResultFields[key] {
			return fmt.Errorf("unsupported activity result field: %s", key)
		}
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > maxEngagementResultBytes {
		return fmt.Errorf("activity result is too large or invalid")
	}
	return nil
}

func encodeEngagementResult(result *EngagementResult) (string, error) {
	if result == nil {
		return "", nil
	}
	if err := validateEngagementResult(result); err != nil {
		return "", err
	}
	data, _ := json.Marshal(result)
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return "<!-- keynope-engagement-results version=1 base64:" + base64.StdEncoding.EncodeToString(buf.Bytes()) + " -->", nil
}

func decodeEngagementResult(comment string) (*EngagementResult, error) {
	match := engagementResultRE.FindStringSubmatch(comment)
	if match == nil || len(match[1]) > maxEngagementResultBytes*2 {
		return nil, fmt.Errorf("invalid activity results metadata")
	}
	data, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		return nil, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err = io.ReadAll(io.LimitReader(reader, maxEngagementResultBytes+1))
	if err != nil || len(data) > maxEngagementResultBytes {
		return nil, fmt.Errorf("activity result exceeds size limit")
	}
	var result EngagementResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, validateEngagementResult(&result)
}

// Runtime result updates do not navigate, rebuild the canvas, or create undo
// steps. Carry them into editing history so undoing text cannot erase a workshop.
func (s *nativeEditorSession) applyEngagementResult(action nativeEditorAction) error {
	if err := validateEngagementResult(action.EngagementResult); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.deck.Slides {
		slide := &s.deck.Slides[i]
		if slide.Engagement == nil || slide.Engagement.ID != action.Name {
			continue
		}
		result := action.EngagementResult
		if result != nil && (result.ActivityID != action.Name || result.Kind != slide.Engagement.Kind) {
			return errInvalidEditorAction
		}
		if reflect.DeepEqual(slide.EngagementResult, result) {
			return nil
		}
		slide.EngagementResult = cloneEngagementResult(result)
		for _, history := range [][]Deck{s.undo, s.redo} {
			for j := range history {
				for k := range history[j].Slides {
					old := &history[j].Slides[k]
					if old.Engagement != nil && old.Engagement.ID == action.Name && old.Engagement.Kind == slide.Engagement.Kind {
						old.EngagementResult = cloneEngagementResult(result)
					}
				}
			}
		}
		s.version++
		return nil
	}
	return errInvalidEditorAction
}
