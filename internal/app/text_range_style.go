package app

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/rivo/uniseg"
)

// Browser/DOM offsets are UTF-16. Never split a user-perceived character.
type textStyleRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

var textRangeStyleFields = []string{"text.bold", "text.italic", "text.underline", "paint.color", "text.font", "text.size"}

func textRangeStyleField(key string) bool {
	for _, candidate := range textRangeStyleFields {
		if key == candidate {
			return true
		}
	}
	return false
}

func validateTextDraftRuns(runs []sceneRun) error {
	if len(runs) > 10000 {
		return fmt.Errorf("too many text runs")
	}
	total := 0
	for _, run := range runs {
		if !validSceneRun(run) {
			return fmt.Errorf("invalid text run style")
		}
		total += len(run.Text)
		if total > 100000 {
			return fmt.Errorf("text exceeds editing limit")
		}
	}
	return nil
}

func textRangeBytes(runs []sceneRun, selection *textStyleRange) (int, int, error) {
	if err := validateTextDraftRuns(runs); err != nil {
		return 0, 0, err
	}
	if selection == nil || selection.Start < 0 || selection.End <= selection.Start || selection.End > 200000 {
		return 0, 0, fmt.Errorf("select words to format")
	}
	var text strings.Builder
	for _, run := range runs {
		text.WriteString(run.Text)
	}
	a, b, units := -1, -1, 0
	if selection.Start == 0 {
		a = 0
	}
	clusters := uniseg.NewGraphemes(text.String())
	for clusters.Next() {
		_, end := clusters.Positions()
		units += len(utf16.Encode(clusters.Runes()))
		if units == selection.Start {
			a = end
		}
		if units == selection.End {
			b = end
		}
	}
	if a < 0 || b < 0 {
		return 0, 0, fmt.Errorf("word selection must follow complete Unicode characters")
	}
	return a, b, nil
}

func textRangeField(run sceneRun, key string) ObjectStyleField {
	switch key {
	case "text.bold":
		return ObjectStyleField{Present: true, Value: strconv.FormatBool(run.Bold)}
	case "text.italic":
		return ObjectStyleField{Present: true, Value: strconv.FormatBool(run.Italic)}
	case "text.underline":
		return ObjectStyleField{Present: true, Value: strconv.FormatBool(run.Underline)}
	case "paint.color":
		color, _ := normalizeHexColour(run.Color)
		return ObjectStyleField{Present: run.Color != "", Value: color, Token: run.ColorToken}
	case "text.font":
		if run.FontRole != "" {
			return ObjectStyleField{Present: true, Value: "theme:" + run.FontRole, Fallback: run.FontID}
		}
		return ObjectStyleField{Present: run.FontID != "", Value: run.FontID}
	case "text.size":
		if run.FontSize != 0 {
			return ObjectStyleField{Present: true, Value: strconv.FormatFloat(run.FontSize, 'f', -1, 64)}
		}
	}
	return ObjectStyleField{}
}

func captureTextRangeStyle(runs []sceneRun, selection *textStyleRange) (*ObjectStyleSnapshot, error) {
	a, b, err := textRangeBytes(runs, selection)
	if err != nil {
		return nil, err
	}
	selected, offset := []sceneRun{}, 0
	for _, run := range runs {
		if offset < b && offset+len(run.Text) > a {
			selected = append(selected, run)
		}
		offset += len(run.Text)
	}
	snapshot := &ObjectStyleSnapshot{Version: 1, Context: "text-range", Fields: map[string]ObjectStyleField{}}
	for _, key := range textRangeStyleFields {
		field, uniform := textRangeField(selected[0], key), true
		for _, run := range selected[1:] {
			if textRangeField(run, key) != field {
				uniform = false
				break
			}
		}
		if uniform {
			snapshot.Fields[key] = field
		}
	}
	if err := snapshot.Validate(); err != nil {
		return nil, fmt.Errorf("select a range with some uniform formatting")
	}
	return snapshot, nil
}

func applyTextRangeStyle(runs []sceneRun, selection *textStyleRange, snapshot *ObjectStyleSnapshot, baseBold, reset bool) ([]sceneRun, bool, error) {
	a, b, err := textRangeBytes(runs, selection)
	if err != nil {
		return nil, false, err
	}
	fields := map[string]ObjectStyleField{}
	if reset {
		for _, key := range textRangeStyleFields {
			fields[key] = ObjectStyleField{}
		}
		fields["text.bold"] = ObjectStyleField{Present: true, Value: strconv.FormatBool(baseBold)}
	} else {
		if err := snapshot.Validate(); err != nil {
			return nil, false, err
		}
		if snapshot.Context != "text-range" {
			return nil, false, fmt.Errorf("copy a word style first")
		}
		fields = snapshot.Fields
	}
	return rewriteTextRangeRuns(runs, a, b, func(styled sceneRun) sceneRun {
		for key, field := range fields {
			switch key {
			case "text.bold":
				styled.Bold = field.Present && field.Value == "true" || !field.Present && baseBold
				styled.BoldOff = !styled.Bold && field.Present && !reset && (baseBold || styled.BoldOff)
			case "text.italic":
				styled.Italic = field.Present && field.Value == "true"
			case "text.underline":
				styled.Underline = field.Present && field.Value == "true"
			case "paint.color":
				styled.Color, _ = normalizeHexColour(field.Value)
				styled.ColorToken = field.Token
			case "text.font":
				styled.FontID = field.Value
				styled.FontRole = wordFontRole(field.Value)
				if styled.FontRole != "" {
					styled.FontID = field.Fallback
				}
			case "text.size":
				styled.FontSize = 0
				if field.Present {
					styled.FontSize, _ = strconv.ParseFloat(field.Value, 64)
				}
			}
		}
		return styled
	})
}

// Both fixed and incremental word properties share the same validated range,
// split/compaction and no-op behavior. Never apply a uniform size to mixed runs
// merely because the first word supplied the inspector's displayed value.
func rewriteTextRangeRuns(runs []sceneRun, a, b int, transform func(sceneRun) sceneRun) ([]sceneRun, bool, error) {
	draft, offset, changed := []sceneRun{}, 0, false
	appendRun := func(run sceneRun) {
		if run.Text == "" {
			return
		}
		if len(draft) > 0 && sameSceneRunStyle(draft[len(draft)-1], run) {
			draft[len(draft)-1].Text += run.Text
		} else {
			draft = append(draft, run)
		}
	}
	for _, run := range runs {
		start, end := max(0, a-offset), min(len(run.Text), b-offset)
		if end <= start {
			appendRun(run)
		} else {
			prefix, styled, suffix := run, run, run
			prefix.Text, styled.Text, suffix.Text = run.Text[:start], run.Text[start:end], run.Text[end:]
			styled = transform(styled)
			changed = changed || !sameSceneRunStyle(styled, run)
			appendRun(prefix)
			appendRun(styled)
			appendRun(suffix)
		}
		offset += len(run.Text)
	}
	if !changed {
		return append([]sceneRun(nil), runs...), false, nil
	}
	return draft, !reflect.DeepEqual(draft, runs), nil
}

func validWordBaseSize(size float64) bool {
	return size >= 1 && size <= 1024 && !math.IsNaN(size) && !math.IsInf(size, 0)
}

func applyTextRangeSizeDelta(runs []sceneRun, selection *textStyleRange, baseSize, delta float64) ([]sceneRun, bool, error) {
	a, b, err := textRangeBytes(runs, selection)
	if err != nil {
		return nil, false, err
	}
	if !validWordBaseSize(baseSize) || math.IsNaN(delta) || math.IsInf(delta, 0) || math.Abs(delta) > 1024 {
		return nil, false, fmt.Errorf("invalid word font size increment")
	}
	return rewriteTextRangeRuns(runs, a, b, func(run sceneRun) sceneRun {
		size := run.FontSize
		if size == 0 {
			size = baseSize
		}
		next := math.Max(1, math.Min(1024, size+delta))
		if next != size {
			run.FontSize = next
		}
		return run
	})
}

// The browser owns uncommitted table topology, just as it owns uncommitted text
// runs. Transmit only its bounded anchor/header context, never a multi-MiB table
// on every caret selection. Final set-table still validates the whole payload.
type tableWordContext struct {
	Row      int      `json:"row"`
	Column   int      `json:"column"`
	Rows     int      `json:"rows"`
	Columns  int      `json:"columns"`
	Header   bool     `json:"header"`
	BaseSize *float64 `json:"baseSize,omitempty"` // Current production-projected draft cell size; never persisted here.
}

func (cell *tableWordContext) Validate() error {
	if cell == nil || cell.Rows < 1 || cell.Rows > 100 || cell.Columns < 1 || cell.Columns > 100 || cell.Row < 0 || cell.Row >= cell.Rows || cell.Column < 0 || cell.Column >= cell.Columns || cell.Header && cell.Row != 0 {
		return fmt.Errorf("select one valid editable table cell")
	}
	if cell.BaseSize != nil && !validWordBaseSize(*cell.BaseSize) {
		return fmt.Errorf("invalid inherited cell font size")
	}
	return nil
}

func textRangeStyleTarget(deck Deck, action nativeEditorAction, group string) (Element, error) {
	var authored, resolved Slide
	if action.SceneMaster {
		if action.Slide < 0 || action.Slide > len(deck.Masters.Layouts) {
			return Element{}, errInvalidEditorAction
		}
		authored, resolved = *masterSlideAt(&deck, action.Slide), masterViewPreview(deck.Masters, action.Slide)
	} else {
		if action.Slide < 0 || action.Slide >= len(deck.Slides) {
			return Element{}, errInvalidEditorAction
		}
		authored, resolved = deck.Slides[action.Slide], deck.ResolveSlide(action.Slide, false)
	}
	return textRangeStyleOwner(deck, authored, resolved, action, group, nil)
}

func textRangeStyleOwner(deck Deck, authored, resolved Slide, action nativeEditorAction, group string, tables map[string]wordTableInfo) (Element, error) {
	indices, err := editableObjectIndices(authored, []string{action.ObjectID})
	if err != nil {
		return Element{}, err
	}
	owner := authored.Elements[indices[0]]
	if owner.Inherited || protectedActivityElement(owner) || objectLocked(owner) || elementGroup(owner) != "" && elementGroup(owner) != group {
		return Element{}, fmt.Errorf("text range is not editable; unlock or enter its group first")
	}
	for _, candidate := range resolved.Elements {
		if candidate.ID == owner.ID {
			owner = candidate
			break
		}
	}
	if objectLocked(owner) || protectedActivityElement(owner) {
		return Element{}, fmt.Errorf("resolved text is locked or protected")
	}
	if owner.Kind == "table" {
		if err := action.TableWord.Validate(); err != nil {
			return Element{}, err
		}
		if action.TableData != nil || action.CellRange != nil {
			return Element{}, fmt.Errorf("word styling needs a cell anchor, not a table/cell-range payload")
		}
		info, ok := tables[owner.ObjectID]
		if tables == nil && deck.Objects != nil {
			if data, exists := deck.Objects.Tables[owner.ObjectID]; exists {
				info, ok = wordTableInfo{data.Version, len(data.Rows), len(data.Columns)}, true
			}
		}
		if !ok || info.version != structuredObjectsVersion || info.rows == 0 || info.columns == 0 {
			return Element{}, fmt.Errorf("table payload is missing")
		}
		// Reset clears only word overrides. Header emphasis remains inherited
		// from the local table draft, not from a previously committed topology.
		label := Element{Kind: "text"}
		if action.TableWord.Header {
			label.Query = "ttf-weight=bold"
		}
		return label, nil
	}
	if action.TableWord != nil {
		return Element{}, fmt.Errorf("table word context belongs to a table")
	}
	if owner.Kind == "shape" {
		label, _, err := sceneEditableShapeLabel(owner)
		return label, err
	}
	if owner.Kind != "text" && owner.Kind != "heading" && owner.Kind != "bullet" && owner.Kind != "code" {
		return Element{}, fmt.Errorf("select editable text")
	}
	return owner, nil
}
