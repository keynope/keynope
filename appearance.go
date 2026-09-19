package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
)

// DeckAppearance is authored document data, not an editor chrome preference.
// Profiles remain opaque until a renderer implements them, allowing compatible
// clients to preserve settings they do not understand.
type DeckAppearance struct {
	Version      int                        `json:"version"`
	Mode         string                     `json:"mode"`
	DefaultStyle string                     `json:"defaultStyle,omitempty"`
	Extra        map[string]json.RawMessage `json:"-"`
}

// documentDiagnostic records a bounded, audience-safe recovery notice. It is
// editor state only: saving a recovered document writes the valid semantic
// model, never the malformed optional envelope or this diagnostic.
type documentDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (a DeckAppearance) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(a.Extra)+2)
	for key, value := range a.Extra {
		fields[key] = value
	}
	fields["version"], _ = json.Marshal(a.Version)
	delete(fields, "mode")
	delete(fields, "defaultStyle")
	if a.Version == 2 {
		fields["defaultStyle"], _ = json.Marshal(a.DefaultStyle)
	} else if a.Version == 1 {
		fields["mode"], _ = json.Marshal(a.Mode)
	}
	return json.Marshal(fields)
}

func (a *DeckAppearance) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var value DeckAppearance
	if err := json.Unmarshal(fields["version"], &value.Version); err != nil {
		return fmt.Errorf("appearance version: %w", err)
	}
	key := "mode"
	target := &value.Mode
	if value.Version == 2 {
		key, target = "defaultStyle", &value.DefaultStyle
		if _, exists := fields["mode"]; exists {
			return fmt.Errorf("version 2 appearance must not contain legacy mode")
		}
	} else if value.Version == 3 {
		if _, exists := fields["mode"]; exists {
			return fmt.Errorf("version 3 appearance must not contain legacy mode")
		}
		if _, exists := fields["defaultStyle"]; exists {
			return fmt.Errorf("version 3 appearance must not contain a style default")
		}
		key, target = "", nil
	} else if _, exists := fields["defaultStyle"]; exists {
		return fmt.Errorf("defaultStyle requires appearance version 2")
	}
	if target != nil {
		if err := json.Unmarshal(fields[key], target); err != nil {
			return fmt.Errorf("appearance %s: %w", key, err)
		}
	}
	delete(fields, "version")
	delete(fields, "mode")
	delete(fields, "defaultStyle")
	if len(fields) > 0 {
		value.Extra = fields
	}
	*a = value
	return nil
}

func (a *DeckAppearance) Clone() *DeckAppearance {
	if a == nil {
		return nil
	}
	copy := *a
	if a.Extra != nil {
		copy.Extra = make(map[string]json.RawMessage, len(a.Extra))
		for key, value := range a.Extra {
			copy.Extra[key] = append(json.RawMessage(nil), value...)
		}
	}
	return &copy
}

func (a *DeckAppearance) Validate() error {
	if a == nil {
		return nil // Legacy documents stay Retro without a rewrite.
	}
	if a.Version != 1 && a.Version != 2 && a.Version != 3 {
		return fmt.Errorf("unsupported appearance version %d; use a newer Keynope", a.Version)
	}
	if a.Version == 3 {
		if a.Mode != "" || a.DefaultStyle != "" {
			return fmt.Errorf("version 3 appearance has no deck style")
		}
		return nil
	}
	style := a.Mode
	if a.Version == 2 {
		style = a.DefaultStyle
		if a.Mode != "" {
			return fmt.Errorf("version 2 appearance must not contain legacy mode")
		}
	} else if a.DefaultStyle != "" {
		return fmt.Errorf("defaultStyle requires appearance version 2")
	}
	if style != "retro" && style != "modern" {
		return fmt.Errorf("unknown presentation appearance %q", style)
	}
	return nil
}

func (d Deck) AppearanceMode() string {
	if d.Appearance == nil || d.Appearance.Version == 3 {
		return "retro"
	}
	if d.Appearance.Version == 2 {
		return d.Appearance.DefaultStyle
	}
	return d.Appearance.Mode
}

// Explicit migration only: reading a legacy deck never rewrites its schema.
// Profiles and unknown compatible properties survive changing the default.
func (d Deck) withDefaultStyle(style string) (*DeckAppearance, error) {
	a := d.Appearance.Clone()
	if a == nil {
		a = &DeckAppearance{}
	}
	a.Version, a.Mode, a.DefaultStyle = 2, "", style
	if _, err := encodeDeckAppearance(a); err != nil {
		return nil, err
	}
	return a, nil
}

// withAppearanceMode exists only for parsing/migration tests for version-1
// documents. Authoring changes defaults through withDefaultStyle; there is no
// runtime/editor deck-mode switch.
func (d Deck) withAppearanceMode(mode string) (*DeckAppearance, error) {
	if mode != "retro" && mode != "modern" {
		return nil, fmt.Errorf("unknown deck appearance %q", mode)
	}
	if d.Appearance == nil && mode == "retro" {
		return nil, nil
	}
	a := d.Appearance.Clone()
	if a == nil {
		a = &DeckAppearance{Version: 1}
	}
	if a.Version == 2 {
		a.DefaultStyle = mode
	} else {
		a.Mode = mode
	}
	if _, err := encodeDeckAppearance(a); err != nil {
		return nil, err
	}
	return a, nil
}

// Edit one profile property without discarding fields introduced by a newer
// compatible writer. A nil scale clears the override, not the other profile.
func (d Deck) withAppearanceTextScale(mode string, scale *float64) (*DeckAppearance, error) {
	if mode != "retro" && mode != "modern" {
		return nil, fmt.Errorf("unknown appearance profile")
	}
	if scale != nil && (math.IsNaN(*scale) || math.IsInf(*scale, 0) || *scale < .05 || *scale > 2) {
		return nil, fmt.Errorf("text scale must be between 5%% and 200%%")
	}
	a := d.Appearance.Clone()
	if a == nil {
		a = &DeckAppearance{Version: 1, Mode: "retro"}
	}
	if a.Extra == nil {
		a.Extra = map[string]json.RawMessage{}
	}
	profiles := map[string]json.RawMessage{}
	if raw := a.Extra["profiles"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &profiles); err != nil || profiles == nil {
			return nil, fmt.Errorf("invalid appearance profiles")
		}
	}
	profile := map[string]json.RawMessage{}
	if raw := profiles[mode]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &profile); err != nil || profile == nil {
			return nil, fmt.Errorf("invalid %s appearance profile", mode)
		}
	}
	if scale == nil {
		delete(profile, "textScale")
	} else {
		profile["textScale"], _ = json.Marshal(*scale)
	}
	if len(profile) == 0 {
		delete(profiles, mode)
	} else {
		profiles[mode], _ = json.Marshal(profile)
	}
	if len(profiles) == 0 {
		delete(a.Extra, "profiles")
	} else {
		a.Extra["profiles"], _ = json.Marshal(profiles)
	}
	if len(a.Extra) == 0 {
		a.Extra = nil
	}
	if d.Appearance == nil && a.Extra == nil {
		return nil, nil
	}
	if _, err := encodeDeckAppearance(a); err != nil {
		return nil, err
	}
	return a, nil
}

func (d Deck) appearanceTextScale(mode string) float64 {
	fallback := 1.0
	if mode == "modern" {
		fallback = .5
	}
	if d.Appearance == nil {
		return fallback
	}
	var profiles map[string]json.RawMessage
	if json.Unmarshal(d.Appearance.Extra["profiles"], &profiles) != nil {
		return fallback
	}
	var profile struct {
		TextScale *float64 `json:"textScale"`
	}
	if json.Unmarshal(profiles[mode], &profile) != nil || profile.TextScale == nil {
		return fallback
	}
	if *profile.TextScale < .05 || *profile.TextScale > 2 {
		return fallback
	}
	return *profile.TextScale
}

var appearanceMetadataRE = regexp.MustCompile(`(?m)^<!--\s*keynope-appearance\s+base64:([^\r\n]*?)\s*-->[\t ]*\r?\n?`)
var appearanceEnvelopeRE = regexp.MustCompile(`(?m)^<!--\s*keynope-appearance\b[^\r\n]*(?:\r?\n|$)`)

func decodeDeckAppearance(text string) (*DeckAppearance, string, error) {
	matches := appearanceMetadataRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil, text, nil
	}
	if len(matches) != 1 {
		return nil, text, fmt.Errorf("duplicate deck appearance metadata")
	}
	if len(matches[0][1]) > 64<<10 {
		return nil, text, fmt.Errorf("deck appearance metadata is too large")
	}
	data, err := base64.StdEncoding.DecodeString(matches[0][1])
	if err != nil {
		return nil, text, fmt.Errorf("decode deck appearance: %w", err)
	}
	var appearance DeckAppearance
	if err := json.Unmarshal(data, &appearance); err != nil {
		return nil, text, fmt.Errorf("parse deck appearance: %w", err)
	}
	if err := appearance.Validate(); err != nil {
		return nil, text, err
	}
	return &appearance, appearanceMetadataRE.ReplaceAllString(text, ""), nil
}

// Optional appearance metadata must never make the authored slides
// inaccessible. Keep the strict decoder for mutations/tests, but recover on
// document open and surface the reason to the editor.
func recoverDeckAppearance(text string) (*DeckAppearance, string, []documentDiagnostic) {
	appearance, remaining, err := decodeDeckAppearance(text)
	if err == nil && (appearance != nil || !appearanceEnvelopeRE.MatchString(text)) {
		return appearance, remaining, nil
	}
	message := "Ignored malformed appearance metadata and opened the deck with the Retro default."
	if err != nil {
		detail := []rune(err.Error())
		if len(detail) > 240 {
			detail = append(detail[:239], '…')
		}
		message = "Ignored invalid appearance metadata (" + string(detail) + ") and opened the deck with the Retro default."
	}
	return nil, appearanceEnvelopeRE.ReplaceAllString(text, ""), []documentDiagnostic{{Code: "appearance-recovered", Message: message}}
}

func encodeDeckAppearance(a *DeckAppearance) (string, error) {
	if a == nil {
		return "", nil
	}
	if err := a.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	if len(encoded) > 64<<10 {
		return "", fmt.Errorf("deck appearance metadata is too large")
	}
	return "<!-- keynope-appearance base64:" + encoded + " -->", nil
}
