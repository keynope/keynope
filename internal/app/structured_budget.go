package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

type structuredBudgetUsage struct{ Objects, Cells, Points, TextBytes, EncodedBytes int64 }
type structuredBudgetLimits struct{ Objects, Cells, Points, TextBytes, EncodedBytes int64 }

var defaultStructuredBudgetLimits = structuredBudgetLimits{Objects: 10000, Cells: 1000000, Points: 2000000, TextBytes: maxStructuredObjectBytes, EncodedBytes: maxStructuredObjectBytes}

func (usage structuredBudgetUsage) add(other structuredBudgetUsage) structuredBudgetUsage {
	return structuredBudgetUsage{usage.Objects + other.Objects, usage.Cells + other.Cells, usage.Points + other.Points, usage.TextBytes + other.TextBytes, usage.EncodedBytes + other.EncodedBytes}
}
func (usage structuredBudgetUsage) subtract(other structuredBudgetUsage) structuredBudgetUsage {
	return structuredBudgetUsage{usage.Objects - other.Objects, usage.Cells - other.Cells, usage.Points - other.Points, usage.TextBytes - other.TextBytes, usage.EncodedBytes - other.EncodedBytes}
}
func (usage structuredBudgetUsage) validate(limits structuredBudgetLimits) error {
	for _, value := range []struct {
		name        string
		used, limit int64
	}{
		{"objects", usage.Objects, limits.Objects}, {"table cells", usage.Cells, limits.Cells}, {"chart points", usage.Points, limits.Points}, {"text bytes", usage.TextBytes, limits.TextBytes},
		{"metadata bytes", usage.EncodedBytes, limits.EncodedBytes},
	} {
		if value.used > value.limit {
			return editorFailure("payload-limit", "", "", fmt.Errorf("structured data exceeds the combined %s limit (%d > %d)", value.name, value.used, value.limit))
		}
	}
	return nil
}

func tableBudgetUsage(table TableData) structuredBudgetUsage {
	usage := structuredBudgetUsage{EncodedBytes: structuredPayloadBytes(table)}
	for _, row := range table.Cells {
		usage.Cells += int64(len(row)) // Covered slots still occupy authored storage.
		for _, cell := range row {
			usage.TextBytes += int64(len(cell.Text))
			for _, run := range cell.Runs {
				usage.TextBytes += int64(len(run.Text))
			}
		}
	}
	return usage
}
func chartBudgetUsage(chart ChartData) structuredBudgetUsage {
	usage := structuredBudgetUsage{TextBytes: int64(len(chart.Title)) + int64(len(chart.Options.XTitle)) + int64(len(chart.Options.YTitle)), EncodedBytes: structuredPayloadBytes(chart)}
	for _, category := range chart.Categories {
		usage.TextBytes += int64(len(category))
	}
	for _, series := range chart.Series {
		usage.Points += int64(len(series.Values))
		usage.TextBytes += int64(len(series.Name))
	}
	return usage
}

func structuredPayloadBytes(value any) int64 {
	data, err := json.Marshal(value)
	if err != nil {
		return maxStructuredObjectBytes + 1
	}
	return int64(len(data))
}
func structuredRegistryHeaderBytes(objects *DeckObjects) int64 {
	if objects == nil {
		return 0
	}
	header := *objects
	header.Tables, header.Charts, header.Builds = nil, nil, nil
	return structuredPayloadBytes(header)
}

// IDs are validated ASCII identifiers, so their quoted JSON size is len+2.
// Payload bytes are counted independently and only remeasured when changed.
func structuredRegistryOverhead(objects *DeckObjects, header int64) int64 {
	if objects == nil {
		return 0
	}
	overhead := header
	add := func(name string, count int) {
		if count > 0 {
			overhead += int64(len(name) + 6 + count - 1)
		}
	}
	add("tables", len(objects.Tables))
	add("charts", len(objects.Charts))
	add("builds", len(objects.Builds))
	for id := range objects.Tables {
		overhead += int64(len(id) + 3)
	}
	for id := range objects.Charts {
		overhead += int64(len(id) + 3)
	}
	for id := range objects.Builds {
		overhead += int64(len(id) + 3)
	}
	return overhead
}

// Immutable count summaries only: never authored, serialized or a reference to
// payload data. Ordinary paint/geometry edits do not need this index at all.
type structuredBudgetIndex struct {
	usage                      structuredBudgetUsage
	entries                    map[workspacePayloadReference]structuredBudgetUsage
	headerBytes, overheadBytes int64
}
type structuredBudgetMetrics struct{ Measured []workspacePayloadReference }

func newStructuredBudgetIndex(objects *DeckObjects) *structuredBudgetIndex {
	index := &structuredBudgetIndex{entries: map[workspacePayloadReference]structuredBudgetUsage{}}
	if objects == nil {
		return index
	}
	index.usage.Objects = int64(len(objects.Tables) + len(objects.Charts) + len(objects.Builds))
	index.headerBytes = structuredRegistryHeaderBytes(objects)
	index.overheadBytes = structuredRegistryOverhead(objects, index.headerBytes)
	index.usage.EncodedBytes = index.overheadBytes
	for id, table := range objects.Tables {
		value := tableBudgetUsage(table)
		index.entries[workspacePayloadReference{"table", id}] = value
		index.usage = index.usage.add(value)
	}
	for id, chart := range objects.Charts {
		value := chartBudgetUsage(chart)
		index.entries[workspacePayloadReference{"chart", id}] = value
		index.usage = index.usage.add(value)
	}
	for id, build := range objects.Builds {
		value := structuredBudgetUsage{EncodedBytes: structuredPayloadBytes(build)}
		index.entries[workspacePayloadReference{"build", id}] = value
		index.usage = index.usage.add(value)
	}
	return index
}

func structuredBudgetAction(action nativeEditorAction) bool {
	if editorActionChangesObjectReferences(action.Action) {
		return true
	}
	switch action.Action {
	case "insert-table", "set-table", "insert-chart", "set-chart-data", "set-chart-options", "set-chart-series-style", "paste-objects", "paste-elements", "clone-slide", "duplicate-element", "add-slide", "add-layout", "delete-slide", "delete-layout", "delete-element", "delete-selection", "delete-objects", "set-slide-build":
		return true
	case "set-selection-property", "paste-object-style", "reset-object-style":
		return strings.HasPrefix(action.StyleContext, "table") || strings.HasPrefix(action.Property, "table.") || strings.HasPrefix(action.Property, "cell.") || action.TableWord != nil || action.StyleData != nil && action.StyleData.Context == "table-cells"
	}
	return false
}

// Compare registry keys, not unrelated cell text or point arrays. Existing
// payload replacements are identified by the command's stable owner IDs;
// copy-on-write and paste are naturally discovered as new payload keys.
func updateStructuredBudget(index *structuredBudgetIndex, objects *DeckObjects, touched map[workspacePayloadReference]bool, limits structuredBudgetLimits, metrics *structuredBudgetMetrics) (*structuredBudgetIndex, error) {
	if index == nil {
		index = newStructuredBudgetIndex(nil)
	}
	usage := index.usage
	usage.EncodedBytes -= index.overheadBytes
	usage.Objects = 0
	changes := map[workspacePayloadReference]structuredBudgetUsage{}
	removed := map[workspacePayloadReference]bool{}
	measure := func(key workspacePayloadReference, read func() structuredBudgetUsage) {
		old, exists := index.entries[key]
		if exists && !touched[key] {
			return
		}
		value := read()
		if metrics != nil {
			metrics.Measured = append(metrics.Measured, key)
		}
		if !exists || value != old {
			changes[key] = value
			usage = usage.subtract(old).add(value)
		}
	}
	if objects != nil {
		usage.Objects = int64(len(objects.Tables) + len(objects.Charts) + len(objects.Builds))
		for id, table := range objects.Tables {
			measure(workspacePayloadReference{"table", id}, func() structuredBudgetUsage { return tableBudgetUsage(table) })
		}
		for id, chart := range objects.Charts {
			measure(workspacePayloadReference{"chart", id}, func() structuredBudgetUsage { return chartBudgetUsage(chart) })
		}
		for id, build := range objects.Builds {
			measure(workspacePayloadReference{"build", id}, func() structuredBudgetUsage {
				return structuredBudgetUsage{EncodedBytes: structuredPayloadBytes(build)}
			})
		}
	}
	for key, value := range index.entries {
		exists := false
		if objects != nil {
			if key.kind == "table" {
				_, exists = objects.Tables[key.id]
			} else if key.kind == "chart" {
				_, exists = objects.Charts[key.id]
			} else {
				_, exists = objects.Builds[key.id]
			}
		}
		if !exists {
			removed[key] = true
			usage = usage.subtract(value)
		}
	}
	header := index.headerBytes
	if objects != nil && header == 0 {
		header = structuredRegistryHeaderBytes(objects)
	}
	overhead := structuredRegistryOverhead(objects, header)
	usage.EncodedBytes += overhead
	if err := usage.validate(limits); err != nil {
		return nil, err
	}
	if len(changes) == 0 && len(removed) == 0 && usage == index.usage {
		return index, nil
	}
	entries := index.entries
	if len(changes) > 0 || len(removed) > 0 {
		entries = make(map[workspacePayloadReference]structuredBudgetUsage, len(index.entries)+len(changes))
		for key, value := range index.entries {
			if !removed[key] {
				entries[key] = value
			}
		}
		for key, value := range changes {
			entries[key] = value
		}
	}
	return &structuredBudgetIndex{usage: usage, entries: entries, headerBytes: header, overheadBytes: overhead}, nil
}

func (s *nativeEditorSession) acceptStructuredBudget(before Deck, action nativeEditorAction, master bool, index int) error {
	if action.Action == "undo" || action.Action == "redo" {
		s.structuredBudget = newStructuredBudgetIndex(s.deck.Objects)
		return nil
	}
	if !structuredBudgetAction(action) {
		return nil
	}
	base := s.structuredBudget
	if base == nil {
		base = newStructuredBudgetIndex(before.Objects)
	}
	touched := map[workspacePayloadReference]bool{}
	// Structural reconciliation can remove inherited targets from timelines on
	// multiple slides. These commands already validate reference scopes; account
	// for their resulting build sizes without inspecting unrelated data arrays.
	if editorActionChangesObjectReferences(action.Action) && s.deck.Objects != nil {
		for id := range s.deck.Objects.Builds {
			touched[workspacePayloadReference{"build", id}] = true
		}
	}
	owners := map[string]bool{action.ObjectID: true}
	for _, id := range action.ObjectIDs {
		owners[id] = true
	}
	var source *Slide
	if master {
		source = masterSlideAt(&s.deck, index)
	} else if index >= 0 && index < len(s.deck.Slides) {
		source = &s.deck.Slides[index]
	}
	if source != nil {
		if action.Action == "set-slide-build" && source.ID != "" {
			touched[workspacePayloadReference{"build", source.ID}] = true
		}
		for _, element := range source.Elements {
			if owners[element.ID] && (element.Kind == "table" || element.Kind == "chart") {
				touched[workspacePayloadReference{element.Kind, element.ObjectID}] = true
			}
		}
	}
	limits := s.structuredLimits
	if limits.Objects == 0 {
		limits = defaultStructuredBudgetLimits
	}
	next, err := updateStructuredBudget(base, s.deck.Objects, touched, limits, nil)
	if err != nil {
		return err
	}
	s.structuredBudget = next
	return nil
}
