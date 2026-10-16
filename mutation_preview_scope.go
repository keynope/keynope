package main

// Capture while holding the editor read lock. A preview must retain original
// indices/counts for links and page numbers, but not unrelated authored content.
// Sparse slide/layout shells preserve those positions without cloning their
// elements, private results, media descriptors or structured payloads.
func captureMutationPreviewDeck(source Deck, index int, master bool, draft *Element) (Deck, bool, error) {
	if master && (index < 0 || index > len(source.Masters.Layouts)) || !master && (index < 0 || index >= len(source.Slides)) {
		return Deck{}, false, errInvalidEditorAction
	}
	compatibility := func() (Deck, bool, error) { return cloneDeckForScene(source), false, nil }
	if source.Extra != nil || source.Masters.Extra != nil || source.Masters.Base.Extra != nil || source.Objects != nil && source.Objects.Extra != nil {
		return compatibility()
	}
	if master && index > 0 && source.Masters.Layouts[index-1].Extra != nil {
		return compatibility()
	}
	out := Deck{Theme: source.Theme.Clone(), Appearance: source.Appearance.Clone(), HideActivityQR: source.HideActivityQR, Slides: make([]Slide, len(source.Slides)), Masters: MasterDeck{Version: source.Masters.Version, Base: MasterLayout{ID: source.Masters.Base.ID, Name: source.Masters.Base.Name}, Layouts: make([]MasterLayout, len(source.Masters.Layouts))}}
	out.PageSize = source.PageSize.Clone()
	// Headers are immutable strings. Preserve first-match layout lookup,
	// including compatible duplicate IDs and a nonzero selected master index.
	for i, layout := range source.Masters.Layouts {
		out.Masters.Layouts[i].ID = layout.ID
		out.Masters.Layouts[i].Name = layout.Name
	}
	inputs := []Slide{}
	cloneInput := func(slide Slide) Slide {
		slide.Notes, slide.EngagementResult, slide.ModernScene = "", nil, nil
		return cloneSlide(slide)
	}
	out.Masters.Base.Slide = cloneInput(source.Masters.Base.Slide)
	inputs = append(inputs, out.Masters.Base.Slide)
	layoutID := ""
	if master {
		if index > 0 {
			layoutID = source.Masters.Layouts[index-1].ID
			out.Masters.Layouts[index-1].Slide = cloneInput(source.Masters.Layouts[index-1].Slide)
			inputs = append(inputs, out.Masters.Layouts[index-1].Slide)
		}
	} else {
		out.Slides[index] = cloneInput(source.Slides[index])
		layoutID = source.Slides[index].LayoutID
		inputs = append(inputs, out.Slides[index])
	}
	if layoutID != "" {
		for i, layout := range source.Masters.Layouts {
			if layout.ID == layoutID {
				if layout.Extra != nil {
					return compatibility()
				}
				out.Masters.Layouts[i].Slide = cloneInput(layout.Slide)
				inputs = append(inputs, out.Masters.Layouts[i].Slide)
				break
			}
		}
	}
	assets := map[string]bool{}
	refs := map[workspacePayloadReference]bool{}
	builds := map[string]bool{}
	collect := func(element Element) bool {
		if element.Extra != nil || !standardElementKind(element.Kind) {
			return false
		}
		if element.AssetID != "" {
			assets[element.AssetID] = true
		}
		if element.ObjectID != "" {
			if element.Kind != "table" && element.Kind != "chart" {
				return false
			}
			refs[workspacePayloadReference{element.Kind, element.ObjectID}] = true
		}
		return true
	}
	for _, slide := range inputs {
		if slide.Extra != nil || slide.BackgroundMedia != nil && slide.BackgroundMedia.Extra != nil {
			return compatibility()
		}
		if slide.BackgroundMedia != nil {
			assets[slide.BackgroundMedia.AssetID] = true
		}
		builds[slide.ID] = true
		for _, element := range slide.Elements {
			if !collect(element) {
				return compatibility()
			}
		}
	}
	if draft != nil && !collect(*draft) {
		return compatibility()
	}
	if len(assets) > 0 {
		out.Assets = make(map[string]DeckAsset, len(assets))
		for id := range assets {
			if asset, ok := source.Assets[id]; ok {
				if assetHasOpaqueMetadata(asset) {
					return compatibility()
				}
				out.Assets[id] = cloneDeckAssetContent(asset, false)
			}
		}
	}
	if source.Objects != nil {
		objects := &DeckObjects{Version: source.Objects.Version}
		for ref := range refs {
			if ref.kind == "table" {
				if value, ok := source.Objects.Tables[ref.id]; ok {
					if objects.Tables == nil {
						objects.Tables = map[string]TableData{}
					}
					objects.Tables[ref.id] = cloneTable(value)
				}
			} else if value, ok := source.Objects.Charts[ref.id]; ok {
				if objects.Charts == nil {
					objects.Charts = map[string]ChartData{}
				}
				objects.Charts[ref.id] = cloneChart(value)
			}
		}
		for id := range builds {
			if value, ok := source.Objects.Builds[id]; ok {
				if objects.Builds == nil {
					objects.Builds = map[string]SlideBuild{}
				}
				objects.Builds[id] = cloneBuild(value)
			}
		}
		out.Objects = objects
	}
	if hasOpaqueObjectMetadata(out) {
		return compatibility()
	}
	return out, true, nil
}
