package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// A portable in-memory formatting snapshot, never a document or asset bundle.
// Absence is intentional (e.g. no shadow), not an omitted/default field.
type ObjectStyleSnapshot struct {
	Version        int                         `json:"version"`
	Context        string                      `json:"context,omitempty"` // Empty is whole-object; shape-label is a separate subtarget.
	Fields         map[string]ObjectStyleField `json:"fields"`
	ThemeSignature string                      `json:"themeSignature,omitempty"`
}

type ObjectStyleField struct {
	Present  bool   `json:"present"`
	Value    string `json:"value,omitempty"`
	Token    string `json:"token,omitempty"`    // Colour role; Value is its concrete fallback.
	Fallback string `json:"fallback,omitempty"` // Resolved font when "theme" is unavailable.
}

var objectStyleKeys = map[string]string{
	"paint.fill":  "shape-fill",
	"paint.color": "fg", "paint.background": "bg", "paint.opacity": "modern-opacity", "paint.outline": "outline",
	"paint.transparent": "transparent", "paint.gradientStart": "gradient-start", "paint.gradientEnd": "gradient-end",
	"paint.gradientDirection": "gradient-dir", "paint.shadow": "shadow", "paint.shadowColor": "shadow-color",
	"paint.shadowX": "shadow-x", "paint.shadowY": "shadow-y",
	"paint.strokeColor": "shape-stroke", "paint.strokeWidth": "shape-stroke-width",
	"paint.strokePosition": "shape-stroke-position",
	"text.font":            "modern-font", "text.size": "modern-size", "text.width": "modern-width",
	"text.bold": "ttf-weight", "text.italic": "text-italic", "text.underline": "text-underline",
	"text.align": "text-align", "text.vertical": "text-valign", "text.lineHeight": "modern-line-height",
	"text.paragraphBefore": "modern-paragraph-before", "text.paragraphAfter": "modern-paragraph-after",
	"line.width": "connector-width", "line.arrowWidth": "connector-arrow-width", "line.arrows": "connector-arrows",
}

func styleObjectSupported(e Element) bool {
	switch e.Kind {
	case "text", "heading", "bullet", "code", "text-image", "page-number", "shape", "image", "chart", "connector":
		return true
	}
	return false
}

func styleFieldEligible(e Element, field string) bool {
	if _, ok := effectControls[field]; ok {
		return styleObjectSupported(e)
	}
	if field == "paint.fill" {
		return e.Kind == "shape"
	}
	if field == "paint.strokeColor" || field == "paint.strokeWidth" || field == "paint.strokePosition" {
		return e.Kind == "shape"
	}
	if !styleObjectSupported(e) {
		return false
	}
	if e.Kind == "chart" {
		switch field {
		case "paint.color", "paint.opacity", "paint.outline", "paint.shadow", "paint.shadowColor", "paint.shadowX", "paint.shadowY":
			return true
		}
		return false
	}
	if len(field) >= 5 && field[:5] == "text." {
		return e.Kind == "text" || e.Kind == "heading" || e.Kind == "bullet" || e.Kind == "code" || e.Kind == "text-image"
	}
	if len(field) >= 5 && field[:5] == "line." {
		return e.Kind == "connector"
	}
	if field == "paint.opacity" {
		return true
	}
	if field == "paint.color" {
		return e.Kind != "image"
	}
	return e.Kind != "connector"
}

func (snapshot *ObjectStyleSnapshot) Clone() *ObjectStyleSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := &ObjectStyleSnapshot{Version: snapshot.Version, Context: snapshot.Context, Fields: map[string]ObjectStyleField{}, ThemeSignature: snapshot.ThemeSignature}
	for key, field := range snapshot.Fields {
		copy.Fields[key] = field
	}
	return copy
}

func (snapshot *ObjectStyleSnapshot) Validate() error {
	if snapshot == nil || snapshot.Version != 1 || len(snapshot.Fields) == 0 || len(snapshot.Fields) > len(objectStyleKeys) {
		return fmt.Errorf("unsupported or empty style snapshot")
	}
	if snapshot.Context != "" && snapshot.Context != "shape-label" && snapshot.Context != "table-cells" && snapshot.Context != "text-range" {
		return fmt.Errorf("unsupported style context")
	}
	if snapshot.ThemeSignature != "" {
		data, err := hex.DecodeString(snapshot.ThemeSignature)
		if err != nil || len(data) != 32 {
			return fmt.Errorf("invalid source theme signature")
		}
	}
	if snapshot.Context == "table-cells" {
		return validateTableCellStyleSnapshot(snapshot)
	}
	for key, field := range snapshot.Fields {
		wordRole := snapshot.Context == "text-range" && key == "text.font" && wordFontRole(field.Value) != ""
		if snapshot.Context == "text-range" && (!textRangeStyleField(key) || field.Token != "" && key != "paint.color" || field.Fallback != "" && !wordRole || key == "text.font" && field.Value == "theme") {
			return fmt.Errorf("property %q does not belong to word formatting", key)
		}
		if snapshot.Context == "shape-label" && !labelStyleProperty(key) {
			return fmt.Errorf("property %q does not belong to label formatting", key)
		}
		if _, ok := objectStyleKeys[key]; !ok || len(field.Value) > 100 || len(field.Token) > 30 {
			return fmt.Errorf("unsupported style property %q", key)
		}
		if !field.Present {
			if field.Value != "" || field.Token != "" || field.Fallback != "" {
				return fmt.Errorf("absent style property has a value")
			}
			continue
		}
		if field.Fallback != "" && (key != "text.font" || field.Value != "theme" && !wordRole || !validTextFont(field.Fallback)) {
			return fmt.Errorf("invalid style font fallback")
		}
		if key == "text.font" && (field.Value == "theme" || wordRole) && field.Fallback == "" {
			return fmt.Errorf("theme font requires a concrete fallback")
		}
		if field.Token != "" && ((key != "paint.color" && key != "paint.strokeColor") || !validThemeColorToken(field.Token)) {
			return fmt.Errorf("invalid style colour token")
		}
		v := field.Value
		if _, ok := effectControls[key]; ok {
			if err := validateEffectProperty(key, v); err != nil {
				return err
			}
			continue
		}
		valid := false
		switch key {
		case "paint.strokePosition":
			valid = v == "inside" || v == "center" || v == "outside"
		case "paint.fill":
			valid = v == "none" || v == "solid"
		case "paint.strokeColor":
			_, valid = normalizeHexColour(v)
			valid = valid || v == "none"
		case "paint.color", "paint.background", "paint.gradientStart", "paint.gradientEnd", "paint.shadowColor":
			_, valid = normalizeHexColour(v)
		case "paint.outline":
			valid = v == "1" || v == "dark" || v == "light" || v == "none"
		case "paint.transparent":
			valid = v == "1"
		case "paint.gradientDirection":
			valid = v == "horizontal" || v == "vertical" || v == "diagonal" || v == "none" || extendedGradient(v)
		case "paint.shadow":
			valid = v == "soft" || v == "solid" || v == "none"
		case "text.font":
			valid = validTextFont(v) || v == "theme" || wordRole
		case "text.bold", "text.italic", "text.underline":
			valid = v == "true" || v == "false"
		case "text.align":
			valid = v == "left" || v == "center" || v == "right" || v == "justify"
		case "text.vertical":
			valid = v == "top" || v == "middle" || v == "bottom"
		case "line.arrows":
			valid = v == "none" || v == "start" || v == "end" || v == "both"
		default:
			n, err := strconv.ParseFloat(v, 64)
			valid = err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
			low, high := 0.0, 4.0
			switch key {
			case "paint.strokeWidth":
				high = 64
			case "paint.opacity":
				high = 1
			case "paint.shadowX", "paint.shadowY":
				low, high = -4, 4
				valid = valid && math.Trunc(n) == n
			case "text.size":
				low, high = 1, 1024
			case "text.width":
				low, high = 1, 200
			case "text.lineHeight":
				low = .5
			case "line.width":
				low, high = 1, 8
			case "line.arrowWidth":
				low, high = 1, 20
			}
			valid = valid && n >= low && n <= high
		}
		if !valid {
			return fmt.Errorf("invalid value for style property %q", key)
		}
	}
	return nil
}

// No scene rendering, media conversion, notes, activity data or content enters
// this projection. Uniform text properties come from the shared resolver.
func captureObjectStyle(deck Deck, authored, resolved Slide, id string) (*ObjectStyleSnapshot, error) {
	var source, effective Element
	found := false
	for _, e := range authored.Elements {
		if e.ID == id {
			if found {
				return nil, fmt.Errorf("ambiguous style source")
			}
			source, found = e, true
		}
	}
	if !found || !styleObjectSupported(source) {
		return nil, fmt.Errorf("select one supported style source")
	}
	effective = source
	for _, e := range resolved.Elements {
		if e.ID == id {
			effective = e
			break
		}
	}
	q, err := url.ParseQuery(effective.Query)
	if err != nil {
		return nil, err
	}
	// Fixed defaults are concrete formatting, not a missing live-theme choice.
	// Resolve them before classifying font provenance, as the text renderer does.
	q = concreteTextDefaults(q)
	snapshot := &ObjectStyleSnapshot{Version: 1, Fields: map[string]ObjectStyleField{}, ThemeSignature: objectStyleThemeSignature(deck)}
	for property, key := range objectStyleKeys {
		if !styleFieldEligible(source, property) {
			continue
		}
		if len(property) >= 5 && property[:5] == "text." {
			continue
		}
		field := ObjectStyleField{Present: q.Has(key), Value: q.Get(key)}
		if property == "paint.fill" {
			paint := deck.applyThemeObjectPaint(effective, resolved, q, scenePaintFor(effective, resolved, 1, 1))
			field = ObjectStyleField{Present: true, Value: "solid"}
			if paint.NoFill {
				field.Value = "none"
			}
		} else if property == "paint.color" {
			paint := deck.applyThemeObjectPaint(effective, resolved, q, scenePaintFor(effective, resolved, 1, 1))
			field = ObjectStyleField{Present: true, Value: paint.Color}
			if !q.Has("fg") && !q.Has("header") {
				field.Token = q.Get("theme-fg")
			}
		} else if property == "paint.strokeColor" {
			paint := deck.applyThemeObjectPaint(effective, resolved, q, scenePaintFor(effective, resolved, 1, 1))
			color := paint.Stroke
			if color == "" {
				color = "none"
			}
			field = ObjectStyleField{Present: true, Value: color}
			if strings.HasPrefix(q.Get(key), "theme:") {
				field.Token = strings.TrimPrefix(q.Get(key), "theme:")
			}
		} else if property == "paint.strokeWidth" {
			// Copy the configured resolved width, including Off/zero and weak
			// detached defaults. An absent authored width is not an absent style.
			value, _, _, _ := selectionPaintValue(deck, resolved, source, effective, q, q, property)
			field = ObjectStyleField{Present: true, Value: fmt.Sprint(value)}
		} else if property == "paint.opacity" && !field.Present {
			field = ObjectStyleField{Present: true, Value: "1"}
		}
		if field.Present && (property == "paint.gradientStart" || property == "paint.gradientEnd" || property == "paint.shadowColor" || property == "paint.background") {
			if fg, ok := ansiFG(q.Get(key)); ok {
				field.Value = sceneColor(fg)
			}
		}
		snapshot.Fields[property] = field
	}
	caps := buildSelectionCapabilities(deck, authored, resolved, []string{id}, "", 0)
	for property := range objectStyleKeys {
		if len(property) < 5 || property[:5] != "text." {
			continue
		}
		state, exists := caps.Properties[property]
		if !exists || state.State != "value" {
			continue
		} // Mixed rich emphasis isn't flattened.
		value := fmt.Sprint(state.Value)
		if property == "text.font" && deck.Theme != nil {
			// A master's explicit font is still a concrete family when copied.
			// Only a genuinely theme-resolved font carries the theme reference.
			if q.Get("modern-font") == "" || q.Get("modern-font") == "theme" {
				value = "theme"
			}
		}
		field := ObjectStyleField{Present: true, Value: value}
		if property == "text.font" && value == "theme" {
			field.Fallback = fmt.Sprint(state.Value)
		}
		snapshot.Fields[property] = field
	}
	return snapshot, snapshot.Validate()
}

func objectStyleThemeSignature(deck Deck) string {
	data, _ := json.Marshal(deck.resolvedDeckTheme())
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func applyObjectStyle(deck *Deck, action nativeEditorAction, editingGroup string) (bool, error) {
	if action.StyleContext == "table-cells" {
		return applyTableCellStyleCommand(deck, action, editingGroup)
	}
	if action.Slide < 0 || action.Slide >= len(deck.Slides) {
		return false, errInvalidEditorAction
	}
	reset := action.Action == "reset-object-style"
	if !reset {
		if err := action.StyleData.Validate(); err != nil {
			return false, err
		}
	}
	if action.StyleContext == "shape-label" {
		return applyShapeLabelStyleTransfer(deck, action, editingGroup)
	}
	if action.StyleContext != "" || !reset && action.StyleData.Context != "" {
		return false, fmt.Errorf("style context does not match the target")
	}
	slide := &deck.Slides[action.Slide]
	selected, err := typographySelection(*slide, action.ObjectIDs, editingGroup)
	if err != nil {
		return false, err
	}
	updates := map[int]string{}
	emphasis := reset
	if action.StyleData != nil {
		for _, property := range []string{"text.bold", "text.italic", "text.underline"} {
			_, present := action.StyleData.Fields[property]
			emphasis = emphasis || present
		}
	}
	for index, e := range slide.Elements {
		if !selected[e.ID] {
			continue
		}
		if !styleObjectSupported(e) {
			return false, fmt.Errorf("selected object does not support formatting")
		}
		q, _ := url.ParseQuery(e.Query)
		previous := q.Encode()
		fields := map[string]ObjectStyleField{}
		if reset {
			for key := range objectStyleKeys {
				fields[key] = ObjectStyleField{}
			}
		} else {
			fields = action.StyleData.Fields
		}
		for property, field := range fields {
			if !styleFieldEligible(e, property) {
				continue
			}
			if e.MasterSlotID != "" {
				removeMasterStyleClears(q, property)
			}
			key := objectStyleKeys[property]
			if property == "text.bold" || property == "text.italic" || property == "text.underline" {
				continue
			}
			q.Del(key)
			switch property {
			case "paint.strokeColor":
				if field.Present {
					value := field.Value
					if field.Token != "" && deck.themeTokenColor(field.Token) != "" && (deck.Theme != nil || action.StyleData.ThemeSignature == objectStyleThemeSignature(*deck)) {
						value = "theme:" + field.Token
					}
					q.Set(key, value)
				}
			case "paint.color":
				q.Del("header")
				q.Del("theme-fg")
				if field.Present {
					if field.Token != "" && deck.themeTokenColor(field.Token) != "" && (deck.Theme != nil || action.StyleData.ThemeSignature == objectStyleThemeSignature(*deck)) {
						q.Set("theme-fg", field.Token)
					} else {
						q.Set("fg", field.Value)
					}
				}
				if e.MasterSlotID != "" {
					clears := []string{}
					if q.Has("theme-fg") {
						clears = []string{"fg", "header"}
					} else if q.Has("fg") {
						clears = []string{"header", "theme-fg"}
					}
					setMasterColorClears(q, clears)
				}
			case "text.size":
				q.Del("ttf-size")
				if field.Present {
					q.Set(key, field.Value)
				}
			case "text.width":
				q.Del("ttf-width")
				if field.Present {
					q.Set(key, field.Value)
				}
			case "text.align":
				if q.Get("align") == "justify" {
					q.Del("align")
				}
				if field.Present {
					q.Set(key, field.Value)
				}
			case "text.font":
				if field.Present {
					value := field.Value
					if value == "theme" && deck.Theme == nil {
						value = field.Fallback
					}
					q.Set(key, value)
				}
			default:
				if field.Present {
					q.Set(key, field.Value)
				}
			}
		}
		if e.Kind == "shape" && e.MasterSlotID != "" {
			_, outline := fields["paint.outline"]
			_, stroke := fields["paint.strokeColor"]
			if outline || stroke {
				setMasterStrokeClears(q)
			}
		}
		if emphasis && e.MasterSlotID != "" && !reset && styleFieldEligible(e, "text.bold") {
			for property, key := range map[string]string{"text.bold": "text-bold", "text.italic": "text-italic", "text.underline": "text-underline"} {
				if field, ok := fields[property]; ok {
					q.Del(key)
					if field.Present {
						q.Set(key, emphasisFlag(field.Value == "true"))
					} else if property == "text.bold" {
						q.Del("ttf-weight")
					}
				}
			}
		} else if emphasis && styleFieldEligible(e, "text.bold") {
			for property, key := range map[string]string{"text.bold": "text-bold", "text.italic": "text-italic", "text.underline": "text-underline"} {
				if _, ok := fields[property]; ok {
					q.Del(key)
				}
			}
			if reset {
				for _, key := range []string{"text-bold", "text-italic", "text-underline"} {
					q.Del(key)
				}
			}
			source := e
			runs := exportedRichRuns(source)
			if runs == nil {
				plain := source
				if plain.Kind == "bullet" {
					plain.Kind = "text"
				}
				runs = sceneTextFor(plain).Runs
			}
			runs = append([]sceneRun{}, runs...) // Never mutate a cached text projection.
			weight := mustQuery(source.Query).Get("ttf-weight")
			for property, field := range fields {
				if property != "text.bold" && property != "text.italic" && property != "text.underline" {
					continue
				}
				on := field.Present && field.Value == "true"
				if property == "text.bold" {
					q.Del("ttf-weight")
					weight = ""
					if on {
						q.Set("ttf-weight", "bold")
						weight = "bold"
					}
				}
				for i := range runs {
					switch property {
					case "text.bold":
						runs[i].Bold = on
						runs[i].BoldOff = false
					case "text.italic":
						runs[i].Italic = on
					case "text.underline":
						runs[i].Underline = on
					}
				}
			}
			if reset {
				for i := range runs {
					runs[i].Color = ""
				}
			}
			encoded, err := encodeTextEmphasisRuns(e.Text, weight, e.Kind, runs)
			if reset {
				encoded, err = encodeModernRuns(e.Text, weight, e.Kind, runs)
			}
			if err != nil {
				return false, err
			}
			q.Del("modern-runs")
			if encoded != "" {
				q.Set("modern-runs", encoded)
			}
			if e.MasterSlotID != "" {
				var clears []string
				if encoded == "" && !reset {
					clears = []string{"modern-runs"}
				}
				setMasterQueryClears(q, []string{"modern-runs"}, clears)
			}
		}
		if next := q.Encode(); next != previous {
			updates[index] = next
		}
	}
	for index, query := range updates {
		slide.Elements[index].Query = query
	}
	return len(updates) > 0, nil
}

// Absence in a semantic style snapshot means restore inheritance. A previous
// master-clear is itself an override, so leaving it behind would make Reset
// or an absent pasted field silently keep suppressing the master property.
func removeMasterStyleClears(q url.Values, property string) {
	raw := q.Get("master-clear")
	if raw == "" {
		return
	}
	keys := []string{objectStyleKeys[property]}
	switch property {
	case "text.bold":
		keys = []string{"ttf-weight", "text-bold"}
	case "paint.color":
		keys = []string{"fg", "header", "theme-fg"}
	case "text.size":
		keys = []string{"modern-size", "ttf-size"}
	case "text.width":
		keys = []string{"modern-width", "ttf-width"}
	}
	possible := false
	for _, key := range keys {
		if key != "" && strings.Contains(raw, key) {
			possible = true
			break
		}
	}
	if !possible {
		return
	}
	remove := map[string]bool{}
	for _, key := range keys {
		if key != "" {
			remove[key] = true
		}
	}
	remaining := map[string]bool{}
	for _, key := range strings.Split(raw, ",") {
		key = strings.TrimSpace(key)
		if key != "" && !remove[key] {
			remaining[key] = true
		}
	}
	values := make([]string, 0, len(remaining))
	for key := range remaining {
		values = append(values, key)
	}
	sort.Strings(values)
	if len(values) == 0 {
		q.Del("master-clear")
	} else {
		q.Set("master-clear", strings.Join(values, ","))
	}
}

func (s *nativeEditorSession) copyObjectStyle(action nativeEditorAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if action.SceneRevision == nil || *action.SceneRevision != s.version || action.SceneMaster != s.masterMode {
		return fmt.Errorf("selection changed; copy the style again")
	}
	var authored, resolved Slide
	if s.masterMode {
		if action.Slide != s.currentMaster {
			return errInvalidEditorAction
		}
		authored = *masterSlideAt(&s.deck, s.currentMaster)
		resolved = masterViewPreview(s.deck.Masters, s.currentMaster)
	} else {
		if action.Slide != s.current || action.Slide < 0 || action.Slide >= len(s.deck.Slides) {
			return errInvalidEditorAction
		}
		authored, resolved = s.deck.Slides[s.current], s.deck.ResolveSlide(s.current, false)
	}
	if action.StyleContext != "table-cells" && len(action.ObjectIDs) != 1 {
		return fmt.Errorf("copy style requires one object")
	}
	var style *ObjectStyleSnapshot
	var err error
	if action.StyleContext == "table-cells" {
		_, _, table, targetErr := tableCellStyleTarget(s.deck, action)
		if targetErr != nil {
			return targetErr
		}
		action.TableData = nil
		capabilities, targetErr := tableCellStyleCapabilities(s.deck, action, s.editingGroup, s.version)
		if targetErr != nil {
			return targetErr
		}
		if !capabilities.Commands["style.copy"].Enabled {
			return fmt.Errorf("%s", capabilities.Commands["style.copy"].Reason)
		}
		style, err = captureTableCellStyle(s.deck, table, action.CellRange)
	} else if action.StyleContext == "shape-label" {
		style, err = captureShapeLabelStyle(s.deck, authored, resolved, action.ObjectIDs[0], s.editingGroup)
	} else if action.StyleContext == "" {
		style, err = captureObjectStyle(s.deck, authored, resolved, action.ObjectIDs[0])
	} else {
		err = fmt.Errorf("unsupported style context")
	}
	if err == nil {
		s.styleClipboard = style
	}
	return err
}
