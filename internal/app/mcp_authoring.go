//go:build !js && !keynope_sitegen

package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/image/font/sfnt"
	resources "keynope"
)

// The contributor's guide ships with the engine rather than requiring a
// separate skill installation. Resources are discoverable; the guide tool is
// also available to clients that do not put resources in model context.
func (a *deckAutomation) registerAuthoringResources() {
	for _, entry := range []struct{ uri, path string }{
		{"keynope://authoring/skill", "skills/keynope-slides/SKILL.md"},
		{"keynope://authoring/recipes", "skills/keynope-slides/reference.md"},
	} {
		a.server.AddResource(&mcp.Resource{URI: entry.uri, Name: "Keynope authoring guide", MIMEType: "text/markdown"}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			data, err := resources.Resources.ReadFile(entry.path)
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: entry.uri, MIMEType: "text/markdown", Text: string(data)}}}, err
		})
	}
}

func (a *deckAutomation) commandContext() nativeEditorState {
	s := a.editor
	s.mu.RLock()
	defer s.mu.RUnlock()
	current := s.current
	if s.masterMode {
		current = s.currentMaster
	}
	return nativeEditorState{Version: s.version, Current: current, Selected: s.selected, MasterMode: s.masterMode, NotesSessionID: s.notesSessionID}
}

func (a *deckAutomation) mutationResult(input automationArguments) map[string]any {
	if input.Response == "full" {
		return a.inspect()
	}
	s := a.editor
	s.mu.RLock()
	defer s.mu.RUnlock()
	var slide Slide
	if s.masterMode && s.currentMaster >= 0 && s.currentMaster <= len(s.deck.Masters.Layouts) {
		slide = *masterSlideAt(&s.deck, s.currentMaster)
	} else if s.current >= 0 && s.current < len(s.deck.Slides) {
		slide = s.deck.Slides[s.current]
	}
	ids := selectionTargetIDs(slide, s.selection, s.selected, s.editingGroup)
	return map[string]any{"version": s.version, "path": s.deckPath, "dirty": s.dirtyLocked(), "current_slide": s.current, "current_master": s.currentMaster, "master_mode": s.masterMode, "selection": map[string]any{"targetIds": ids}, "undo": len(s.undo), "redo": len(s.redo)}
}

type automationPartialError struct {
	Err     error
	Applied int
	Result  map[string]any
}

func (e *automationPartialError) Error() string { return e.Err.Error() }
func (e *automationPartialError) Unwrap() error { return e.Err }

type automationObjectStamp struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Slide  int      `json:"slide"`
	Master bool     `json:"master,omitempty"`
	Hash   [32]byte `json:"-"`
}

func (a *deckAutomation) objectStamps() map[string]automationObjectStamp {
	s := a.editor
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]automationObjectStamp)
	visit := func(slide Slide, index int, master bool) {
		metadata := slide
		metadata.Elements = nil
		raw, _ := json.Marshal(metadata)
		out[fmt.Sprintf("%t/%d/@slide", master, index)] = automationObjectStamp{Kind: "slide", Slide: index, Master: master, Hash: sha256.Sum256(raw)}
		for _, e := range slide.Elements {
			// Include referenced structured data but never raster/font payloads.
			var data any
			if s.deck.Objects != nil {
				if e.Kind == "table" {
					data = s.deck.Objects.Tables[e.ObjectID]
				}
				if e.Kind == "chart" {
					data = s.deck.Objects.Charts[e.ObjectID]
				}
			}
			raw, _ := json.Marshal([]any{e, data})
			stamp := automationObjectStamp{e.ID, e.Kind, index, master, sha256.Sum256(raw)}
			out[fmt.Sprintf("%t/%d/%s", master, index, e.ID)] = stamp
		}
	}
	for i, slide := range s.deck.Slides {
		visit(slide, i, false)
	}
	visit(s.deck.Masters.Base.Slide, 0, true)
	for i, layout := range s.deck.Masters.Layouts {
		visit(layout.Slide, i+1, true)
	}
	return out
}

func (a *deckAutomation) call(ctx context.Context, name string, input automationArguments) (map[string]any, []byte, error) {
	if input.Response != "" && input.Response != "compact" && input.Response != "full" {
		return nil, nil, errors.New("response must be compact or full")
	}
	switch name {
	case "deck_authoring_guide":
		path := "skills/keynope-slides/SKILL.md"
		if input.Topic == "recipes" {
			path = "skills/keynope-slides/reference.md"
		}
		data, err := resources.Resources.ReadFile(path)
		text := string(data)
		if input.Topic != "all" && input.Topic != "recipes" {
			heading := map[string]string{"": "1. Workflow", "workflow": "1. Workflow", "typography": "4. Typography", "layouts": "3. Layouts", "artwork": "5. Artwork"}[input.Topic]
			if heading == "" {
				return nil, nil, errors.New("guide topic: workflow, typography, layouts, artwork, recipes or all")
			}
			if start := strings.Index(text, "## "+heading); start >= 0 {
				text = text[start:]
				if end := strings.Index(text[3:], "\n## "); end >= 0 {
					text = text[:end+3]
				}
			}
		}
		return map[string]any{"guide": text, "credit": "Based on Josh Gonggryp's keynope-slides contribution (PR #16)."}, nil, err
	case "deck_objects":
		return a.listObjects(input)
	case "slide_measure":
		return a.measureSlide(input)
	case "text_measure":
		return a.measureText(input)
	case "table_measure":
		return a.measureTable(input)
	case "diagram_layers":
		return automationDiagram(input)
	case "image_pixelart":
		return a.pixelart(ctx, input)
	}
	mutating := name == "editor_command" || name == "editor_batch" || name == "deck_new" || name == "deck_open" || name == "asset_import" || name == "qr_code_create" || name == "video_import" || name == "font_import" || name == "font_use_installed" || name == "font_export_confirm"
	var before map[string]automationObjectStamp
	if mutating {
		before = a.objectStamps()
	}
	var result map[string]any
	var image []byte
	var err error
	if name == "animation_create" {
		if err = a.checkVersion(input.ExpectedVersion); err == nil {
			result, image, err = a.createAnimation(ctx, input)
		}
	} else {
		result, image, err = a.callCore(ctx, name, input)
	}
	if !mutating {
		return result, image, err
	}
	var partial *automationPartialError
	if err != nil && !errors.As(err, &partial) {
		return result, image, err
	}
	if result == nil {
		result = a.mutationResult(input)
	}
	created, changed, removed := []automationObjectStamp{}, []automationObjectStamp{}, []automationObjectStamp{}
	after := a.objectStamps()
	keys := make([]string, 0, len(after))
	for key := range after {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	affected := map[int]bool{}
	masterChanged := false
	for _, key := range keys {
		next := after[key]
		old, exists := before[key]
		if next.Kind == "slide" {
			if !exists || old.Hash != next.Hash {
				if next.Master {
					masterChanged = true
				} else {
					affected[next.Slide] = true
				}
			}
			continue
		}
		if !exists {
			created = append(created, next)
		} else if old.Hash != next.Hash {
			changed = append(changed, next)
		} else {
			continue
		}
		if !next.Master {
			affected[next.Slide] = true
		} else {
			masterChanged = true
		}
	}
	keys = keys[:0]
	for key := range before {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, exists := after[key]; !exists {
			if before[key].Kind != "slide" {
				removed = append(removed, before[key])
			}
			if !before[key].Master {
				affected[before[key].Slide] = true
			} else {
				masterChanged = true
			}
		}
	}
	if masterChanged {
		a.editor.mu.RLock()
		for i := range a.editor.deck.Slides {
			affected[i] = true
		}
		a.editor.mu.RUnlock()
	}
	slides := make([]int, 0, len(affected))
	for slide := range affected {
		slides = append(slides, slide)
	}
	sort.Ints(slides)
	result["created"], result["changed"], result["removed"], result["affected_slides"] = created, changed, removed, slides
	if name == "editor_batch" {
		result["applied_count"] = len(input.Commands)
	}
	if partial != nil {
		result["applied_count"] = partial.Applied
		result["error"] = partial.Error()
		partial.Result = result
	}
	return result, image, err
}

func (a *deckAutomation) listObjects(input automationArguments) (map[string]any, []byte, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	if input.Offset < 0 || limit < 1 || limit > 200 {
		return nil, nil, errors.New("offset must be non-negative; limit 1–200")
	}
	s := a.editor
	s.mu.RLock()
	defer s.mu.RUnlock()
	wanted := map[int]bool{}
	for _, i := range input.Slides {
		if i < 0 || i >= len(s.deck.Slides) {
			return nil, nil, errors.New("slide out of range")
		}
		wanted[i] = true
	}
	items := []map[string]any{}
	visit := func(slide Slide, index int, master bool) {
		for n, e := range slide.Elements {
			text := []rune(e.Text)
			if len(text) > 120 {
				text = text[:120]
			}
			query := e.Query
			if !input.IncludeData {
				// Rich runs and labels can dwarf the actual geometry/style data.
				query = removeImageQueryKeys(query, "modern-runs", "shape-label")
			}
			item := map[string]any{"id": e.ID, "kind": e.Kind, "index": n, "slide": index, "master": master, "text": string(text), "query": query}
			if input.IncludeData && s.deck.Objects != nil {
				if e.Kind == "table" {
					item["table"] = s.deck.Objects.Tables[e.ObjectID]
				}
				if e.Kind == "chart" {
					item["chart"] = s.deck.Objects.Charts[e.ObjectID]
				}
			}
			items = append(items, item)
		}
	}
	for i, slide := range s.deck.Slides {
		if len(wanted) == 0 || wanted[i] {
			visit(slide, i, false)
		}
	}
	if input.IncludeMasters {
		visit(s.deck.Masters.Base.Slide, 0, true)
		for i, layout := range s.deck.Masters.Layouts {
			visit(layout.Slide, i+1, true)
		}
	}
	start := min(input.Offset, len(items))
	end := min(start+limit, len(items))
	return map[string]any{"version": s.version, "objects": items[start:end], "total": len(items), "more": end < len(items), "next_offset": end}, nil, nil
}

func automationGridRect(r sceneRect, width, height float64) sceneRect {
	return sceneRect{r.X * float64(authoredTerminalWidth) / width, r.Y * float64(authoredTerminalHeight) / height, r.Width * float64(authoredTerminalWidth) / width, r.Height * float64(authoredTerminalHeight) / height}
}

func (a *deckAutomation) measureSlide(input automationArguments) (map[string]any, []byte, error) {
	deck, _, version := a.snapshot()
	authored, slide, err := automationSlide(deck, input)
	if err != nil {
		return nil, nil, err
	}
	scene := slide.ModernScene
	if scene == nil {
		return nil, nil, errors.New("slide could not be projected")
	}
	local := map[string]bool{}
	for _, e := range authored.Elements {
		local[e.ID] = true
	}
	items := []map[string]any{}
	outside := []string{}
	overlap := [][2]string{}
	var panel *sceneObject
	if input.PanelID != "" {
		for i := range scene.Objects {
			if scene.Objects[i].ID == input.PanelID {
				panel = &scene.Objects[i]
				break
			}
		}
		if panel == nil {
			return nil, nil, errors.New("panel_id not found")
		}
	}
	panelOutside, behind := []string{}, []string{}
	for i, o := range scene.Objects {
		item := map[string]any{"id": o.ID, "kind": o.Kind, "master": !local[o.ID], "layer": i, "pixels": o.Bounds, "grid": automationGridRect(o.Bounds, scene.Width, scene.Height), "rotation": o.Rotation, "paint": map[string]any{"colour": o.Paint.Color, "opacity": o.Paint.Opacity, "stroke": o.Paint.Stroke, "stroke_width": o.Paint.StrokeWidth}}
		if o.Text != nil {
			plain := []rune(sceneTextPlain(o.Text))
			if len(plain) > 120 {
				plain = plain[:120]
			}
			item["text"] = string(plain)
			item["required_height_pixels"] = sceneTextWrappedHeight(o.Text, o.Bounds.Width)
			item["text_overflow"] = sceneTextWrappedHeight(o.Text, o.Bounds.Width) > o.Bounds.Height+.5
		}
		items = append(items, item)
		b := o.Bounds
		if b.X < -.5 || b.Y < -.5 || b.X+b.Width > scene.Width+.5 || b.Y+b.Height > scene.Height+.5 {
			outside = append(outside, o.ID)
		}
		for j := 0; j < i; j++ {
			p := scene.Objects[j].Bounds
			if b.X < p.X+p.Width && b.X+b.Width > p.X && b.Y < p.Y+p.Height && b.Y+b.Height > p.Y {
				if len(overlap) < 200 {
					overlap = append(overlap, [2]string{scene.Objects[j].ID, o.ID})
				}
			}
		}
		if panel != nil && o.ID != panel.ID && local[o.ID] && b.Y+b.Height > panel.Bounds.Y+.5 {
			p := panel.Bounds
			if b.X < p.X-.5 || b.Y < p.Y-.5 || b.Y+b.Height > p.Y+p.Height+.5 || b.X+b.Width > p.X+p.Width+.5 {
				panelOutside = append(panelOutside, o.ID)
			}
			for j := i + 1; j < len(scene.Objects); j++ {
				if scene.Objects[j].ID == panel.ID {
					behind = append(behind, o.ID)
				}
			}
		}
	}
	return map[string]any{"version": version, "objects": items, "off_slide": outside, "overlapping_bounds": overlap, "panel_outside": panelOutside, "behind_panel": behind, "note": "Bounds overlap is intentional for panels, labels and artwork. Rotated objects are reported with unrotated layout bounds; verify final screenshots."}, nil, nil
}

func (a *deckAutomation) measureText(input automationArguments) (map[string]any, []byte, error) {
	if input.Element == nil || input.BoxWidth <= 0 || input.BoxWidth > float64(authoredTerminalWidth) || !finite(input.BoxWidth) || input.BoxHeight < 0 || !finite(input.BoxHeight) {
		return nil, nil, errors.New("provide a text element and finite box_width in grid cells (0,245]; box_height is optional")
	}
	e := *input.Element
	if e.Kind == "" {
		e.Kind = "text"
	}
	if e.Kind != "text" && e.Kind != "heading" && e.Kind != "code" && e.Kind != "bullet" {
		return nil, nil, errors.New("element must be text, heading, code or bullet")
	}
	if len(e.Text) > 256<<10 {
		return nil, nil, errors.New("text exceeds 256 KiB")
	}
	deck, _, version := a.snapshot()
	// Resolve inheritance without rendering unrelated artwork just to measure
	// one proposed text box.
	var resolved Slide
	if input.Master {
		if input.Slide < 0 || input.Slide > len(deck.Masters.Layouts) {
			return nil, nil, errors.New("master index out of range")
		}
		resolved = masterViewPreview(deck.Masters, input.Slide)
	} else {
		if input.Slide < 0 || input.Slide >= len(deck.Slides) {
			return nil, nil, errors.New("slide index out of range")
		}
		resolved = deck.ResolveSlide(input.Slide, false)
	}
	q, err := url.ParseQuery(e.Query)
	if err != nil {
		return nil, nil, err
	}
	if input.TextRuns != nil {
		if len(input.TextRuns) > 10000 {
			return nil, nil, errors.New("too many text runs")
		}
		var plain strings.Builder
		for _, run := range input.TextRuns {
			plain.WriteString(run.Text)
		}
		if plain.Len() > 100000 {
			return nil, nil, errors.New("text runs exceed editing limit")
		}
		e.Text = plain.String()
		encoded, err := encodeModernRunsMetadata(e.Text, q.Get("ttf-weight"), input.TextRuns, true)
		if err != nil {
			return nil, nil, err
		}
		q.Set("modern-runs", encoded)
		e.Query = q.Encode()
	}
	// Resolve the same inherited typography as a real object on this slide.
	resolved.Elements = []Element{e}
	resolved = deck.withThemeTypography(withTrueTypeDefaults(resolved))
	e = resolved.Elements[0]
	q, _ = url.ParseQuery(e.Query)
	style := "modern"
	if !deck.usesConcreteAppearance() {
		style = deck.elementStyle(resolved, e)
	}
	text := deck.sceneTextForStyleFromValues(e, style, q)
	width, height := sceneTextNaturalSize(text)
	canvas := deck.physicalCanvas()
	wrapped := sceneTextWrappedHeight(text, input.BoxWidth*canvas.Width/float64(authoredTerminalWidth))
	gridHeight := wrapped * float64(authoredTerminalHeight) / canvas.Height
	return map[string]any{"version": version, "natural_width_pixels": width, "natural_height_pixels": height, "wrapped_height_pixels": wrapped, "height": gridHeight, "width": input.BoxWidth, "fits": input.BoxHeight == 0 || gridHeight <= input.BoxHeight+1e-6, "font_id": text.FontID, "font_size": text.Size, "line_height": text.LineHeight, "width_scale": text.WidthScale, "missing_glyphs": automationMissingGlyphs(text), "measurement_note": "Font metrics exclude system fallback shaping. Missing glyphs may render differently on other devices; use a font containing them or portable ASCII."}, nil, nil
}

func automationMissingGlyphs(text *sceneText) []map[string]string {
	warnings := []map[string]string{}
	seen := map[string]bool{}
	paragraphs := text.Paragraphs
	if len(paragraphs) == 0 {
		paragraphs = []sceneParagraph{{Runs: text.Runs}}
	}
	var buffer sfnt.Buffer
	for _, paragraph := range paragraphs {
		for _, run := range paragraph.Runs {
			fontID := text.FontID
			if run.FontID != "" {
				fontID = run.FontID
			}
			if text.FontRoles[run.FontRole] != "" {
				fontID = text.FontRoles[run.FontRole]
			}
			face := chartMetricFonts()[fontID]
			if owned, ok := text.TypefaceMetrics[fontID]; ok && owned.metric != nil {
				face = owned.metric
			}
			for _, token := range splitEmojiText(run.Text) {
				if token.assetKey != "" {
					continue
				}
				for _, ch := range token.text {
					if unicode.IsControl(ch) || unicode.IsSpace(ch) {
						continue
					}
					missing := face == nil
					if face != nil {
						glyph, err := face.GlyphIndex(&buffer, ch)
						missing = err != nil || glyph == 0
					}
					key := fontID + ":" + string(ch)
					if missing && !seen[key] {
						seen[key] = true
						warnings = append(warnings, map[string]string{"font_id": fontID, "character": string(ch), "codepoint": fmt.Sprintf("U+%04X", ch)})
						if len(warnings) >= 64 {
							return warnings
						}
					}
				}
			}
		}
	}
	return warnings
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func (a *deckAutomation) measureTable(input automationArguments) (map[string]any, []byte, error) {
	if !finite(input.BoxWidth) || input.BoxWidth <= 0 || input.BoxWidth > float64(authoredTerminalWidth) {
		return nil, nil, errors.New("box_width must be in (0,245] grid cells")
	}
	deck, _, version := a.snapshot()
	table := input.Table
	if table == nil {
		authored, _, err := automationSlide(deck, input)
		if err != nil {
			return nil, nil, err
		}
		for _, e := range authored.Elements {
			if e.ID == input.ObjectID && e.Kind == "table" && deck.Objects != nil {
				data := deck.Objects.Tables[e.ObjectID]
				table = &data
				break
			}
		}
	}
	if table == nil {
		return nil, nil, errors.New("provide table or table object_id")
	}
	canvas := deck.physicalCanvas()
	projected, err := deck.projectTable(*table, sceneRect{Width: input.BoxWidth * canvas.Width / float64(authoredTerminalWidth), Height: canvas.Height}, "")
	if err != nil {
		return nil, nil, err
	}
	heights := make([]float64, len(table.Rows))
	cells := []map[string]any{}
	for _, cell := range projected.Cells {
		needed := sceneTextWrappedHeight(cell.Text, cell.ContentBounds.Width) + 2*cell.ContentBounds.Y
		if cell.RowSpan == 1 {
			heights[cell.Row] = math.Max(heights[cell.Row], needed)
		}
		cells = append(cells, map[string]any{"row": cell.Row, "column": cell.Column, "height_pixels": needed})
	}
	for _, cell := range projected.Cells {
		if cell.RowSpan > 1 {
			needed := sceneTextWrappedHeight(cell.Text, cell.ContentBounds.Width) + 2*cell.ContentBounds.Y
			have := 0.
			for r := cell.Row; r < cell.Row+cell.RowSpan; r++ {
				have += heights[r]
			}
			if needed > have {
				for r := cell.Row; r < cell.Row+cell.RowSpan; r++ {
					heights[r] += (needed - have) / float64(cell.RowSpan)
				}
			}
		}
	}
	total := 0.
	for i := range heights {
		heights[i] = math.Max(1, heights[i]) * float64(authoredTerminalHeight) / canvas.Height
		total += heights[i]
	}
	return map[string]any{"version": version, "row_heights": heights, "height": total, "cells": cells, "units": "row_heights and height are grid cells; cell heights are pixels"}, nil, nil
}

func (a *deckAutomation) commandSchema(name string) map[string]any {
	schema := automationTypeSchema(reflect.TypeFor[nativeEditorAction]())
	fields := map[string][]string{
		"set-layout": {"kind"}, "move-object-layer": {"objectId", "kind", "name", "value"},
		"add-element": {"kind", "name", "elementData", "textRuns"}, "set-scene-text": {"objectId", "textRuns"},
		"set-selection-property": {"objectIds", "property", "propertyValue"}, "set-selection-geometry": {"objectIds", "geometry", "geometryResize"},
		"select-slide": {"slide"}, "update-slide": {"slideData"}, "set-table": {"objectId", "tableData"}, "set-slide-build": {"slide", "buildData"},
		"insert-qr-code": {"qrCode"},
	}
	keys, ok := fields[name]
	if !ok {
		return map[string]any{"command": name, "note": "Use deck_schema(topic=action) for the full native schema; common commands have focused examples.", "commands": strings.Fields(automationCommands)}
	}
	selected := map[string]any{}
	raw, _ := json.Marshal(schema)
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	props, _ := object["properties"].(map[string]any)
	for _, key := range append([]string{"action"}, keys...) {
		selected[key] = props[key]
	}
	example := map[string]any{"action": name}
	if name == "insert-qr-code" {
		example["qrCode"] = QRCodeOptions{Content: "https://example.com", Correction: "M"}
	}
	if name == "set-layout" {
		example["kind"] = "blank"
	}
	if name == "move-object-layer" {
		example["objectId"] = "<id>"
		example["kind"] = "front"
	}
	return map[string]any{"command": name, "fields": selected, "example": example, "note": "Select the target slide first. Layout kind is its ID, not its display name. Layer kind is front/back/forward/backward, or before/after with name=target object ID."}
}
