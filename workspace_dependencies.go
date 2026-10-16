package main

import "reflect"

type workspacePayloadReference struct{ kind, id string }
type workspaceConsumers map[int]bool

// Index only bounded retained entries. Eviction/reordering rebuilds this small
// reverse map from their accepted references; it never owns media or payloads.
type workspaceConsumerIndex struct {
	assets   map[string]workspaceConsumers
	payloads map[workspacePayloadReference]workspaceConsumers
	layouts  map[string]workspaceConsumers
	builds   map[string]workspaceConsumers
	base     workspaceConsumers
	unknown  workspaceConsumers
}

type workspaceReferences struct {
	assets   map[string]bool
	payloads map[workspacePayloadReference]bool
	layout   string
	build    string
	unknown  bool
}

func workspaceSlideInput(slide Slide) Slide {
	// None of these appear in editor page JSON. Keep authored activity
	// definitions, annotation inputs and unknown extensions intact.
	slide.Notes, slide.EngagementResult, slide.ModernScene = "", nil, nil
	slide.previewGeometry = nil
	slide.ThemeColors, slide.ShapeStrokeDefaults = nil, nil
	return slide
}

func workspacePageReferences(source, resolved Slide) workspaceReferences {
	r := workspaceReferences{assets: map[string]bool{}, payloads: map[workspacePayloadReference]bool{}, layout: source.LayoutID, build: resolved.ID}
	for _, e := range resolved.Elements {
		if e.AssetID != "" {
			r.assets[e.AssetID] = true
		}
		if e.ObjectID != "" {
			r.payloads[workspacePayloadReference{e.Kind, e.ObjectID}] = true
			if e.Kind != "table" && e.Kind != "chart" {
				r.unknown = true
			}
		}
		switch e.Kind {
		case "heading", "text", "bullet", "code", "text-image", "page-number", "image", "shape", "connector", "table", "chart":
		default:
			r.unknown = true
		}
	}
	if resolved.BackgroundMedia != nil {
		r.assets[resolved.BackgroundMedia.AssetID] = true
	}
	return r
}

func newWorkspaceConsumerIndex() workspaceConsumerIndex {
	return workspaceConsumerIndex{assets: map[string]workspaceConsumers{}, payloads: map[workspacePayloadReference]workspaceConsumers{}, layouts: map[string]workspaceConsumers{}, builds: map[string]workspaceConsumers{}, base: workspaceConsumers{}, unknown: workspaceConsumers{}}
}

func addWorkspaceConsumer[K comparable](index map[K]workspaceConsumers, key K, page int) {
	if index[key] == nil {
		index[key] = workspaceConsumers{}
	}
	index[key][page] = true
}

func (index workspaceConsumerIndex) add(page int, r workspaceReferences) {
	for id := range r.assets {
		addWorkspaceConsumer(index.assets, id, page)
	}
	for id := range r.payloads {
		addWorkspaceConsumer(index.payloads, id, page)
	}
	if r.layout != "" {
		addWorkspaceConsumer(index.layouts, r.layout, page)
	}
	addWorkspaceConsumer(index.builds, r.build, page)
	// The base default style may apply even to a detached slide. Comparison of
	// actual resolved inputs below proves whether a candidate really consumes it.
	index.base[page] = true
	if r.unknown {
		index.unknown[page] = true
	}
}

func workspaceChangedKeys[T any](before, after map[string]T) []string {
	return workspaceChangedKeysEqual(before, after, func(a, b T) bool { return reflect.DeepEqual(a, b) })
}

// Structured payloads use complete typed authored-value comparisons shared
// with immutable history. No pointer/revision shortcut may hide an in-place
// mutation; callers still own independent input snapshots.
func workspaceChangedKeysEqual[T any](before, after map[string]T, equal func(T, T) bool) []string {
	var changed []string
	for id, old := range before {
		next, exists := after[id]
		if !exists || !equal(old, next) {
			changed = append(changed, id)
		}
	}
	for id := range after {
		if _, existed := before[id]; !existed {
			changed = append(changed, id)
		}
	}
	return changed
}

// Compare each registry source once, then find its cached consumers. Ordinary
// unchanged pages no longer resolve masters or scan payloads on every edit.
func (index workspaceConsumerIndex) changedSources(before, after Deck) (data, masters workspaceConsumers, unknownMaster bool) {
	data, masters = workspaceConsumers{}, workspaceConsumers{}
	mark := func(dst, consumers workspaceConsumers) {
		for page := range consumers {
			dst[page] = true
		}
	}
	assets := workspaceChangedKeys(before.Assets, after.Assets)
	for _, id := range assets {
		mark(data, index.assets[id])
	}
	var a, b DeckObjects
	if before.Objects != nil {
		a = *before.Objects
	}
	if after.Objects != nil {
		b = *after.Objects
	}
	tables := workspaceChangedKeysEqual(a.Tables, b.Tables, historyTableEqual)
	charts := workspaceChangedKeysEqual(a.Charts, b.Charts, historyChartEqual)
	builds := workspaceChangedKeysEqual(a.Builds, b.Builds, historyBuildEqual)
	for _, id := range tables {
		mark(data, index.payloads[workspacePayloadReference{"table", id}])
	}
	for _, id := range charts {
		mark(data, index.payloads[workspacePayloadReference{"chart", id}])
	}
	for _, id := range builds {
		mark(data, index.builds[id])
	}
	if len(assets)+len(tables)+len(charts)+len(builds) > 0 {
		mark(data, index.unknown)
	}
	x, y := before.Masters, after.Masters
	unknownMaster = x.Version != y.Version || !reflect.DeepEqual(x.Extra, y.Extra) || !reflect.DeepEqual(x.Base.Extra, y.Base.Extra)
	if !reflect.DeepEqual(workspaceSlideInput(x.Base.Slide), workspaceSlideInput(y.Base.Slide)) {
		mark(masters, index.base)
	}
	layouts := func(m MasterDeck) map[string]MasterLayout {
		out := map[string]MasterLayout{}
		for _, layout := range m.Layouts {
			if _, duplicate := out[layout.ID]; duplicate || layout.ID == "" {
				unknownMaster = true
			}
			layout.Name = ""
			layout.Slide = workspaceSlideInput(layout.Slide)
			out[layout.ID] = layout
		}
		return out
	}
	old, next := layouts(x), layouts(y)
	for _, id := range workspaceChangedKeys(old, next) {
		mark(masters, index.layouts[id])
		if !reflect.DeepEqual(old[id].Extra, next[id].Extra) {
			unknownMaster = true
		}
	}
	return data, masters, unknownMaster
}
