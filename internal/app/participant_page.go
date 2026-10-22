package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

var participantTransferJS = embeddedString("web/participant-transfer.js")

// Resolve only the selected slide. Never disclose notes, activity answers or
// unused masters/assets from the remainder of the deck to participants.
func participantSlideMarkdown(deck Deck, index int) ([]byte, error) {
	if index < 0 || index >= len(deck.Slides) {
		return nil, fmt.Errorf("slide out of range")
	}
	slide := cloneSlide(deck.ResolveSlide(index, false))
	slide.Notes, slide.LayoutID, slide.Engagement = "", "", nil
	if slide.PageNumber == pageNumberShow {
		// The resolved original number is already an element. Do not generate
		// another page-one number when this single-page document is reopened.
		slide.PageNumber = pageNumberHide
	}
	slide.EngagementResult = nil
	slide.Extra = nil // Compatible authoring extensions are not participant data.
	if slide.BackgroundMedia != nil {
		slide.BackgroundMedia = cloneSlideBackgroundMedia(slide.BackgroundMedia)
		slide.BackgroundMedia.Extra = nil
		slide.BackgroundMedia.SourceObjectID = ""
		slide.BackgroundMedia.Query = participantElementQuery(Element{Kind: "image", Query: slide.BackgroundMedia.Query})
	}
	slide.TabID = "" // Explicit tab requests render normally, outside the slide show.
	hidden := hiddenSlideObjects(slide)
	// Movies are local-presentation/HTML content, never participant transfers.
	// Treat them like hidden objects so removing one preserves other objects'
	// original flow positions in the legacy Markdown adapter as well.
	for i, element := range slide.Elements {
		if element.Kind == "video" {
			if hidden == nil {
				hidden = map[int]bool{}
			}
			hidden[i] = true
		}
	}
	publicFrames := map[string]sceneRect{}
	if len(hidden) > 0 {
		cols, rows := authoredRenderSize(245, 56)
		scene, err := buildDocumentSlideScene(deck, index, cols, rows)
		if err != nil {
			return nil, err
		}
		for _, object := range scene.Objects {
			publicFrames[object.ID] = object.Bounds
		}
	}
	visible := slide.Elements[:0]
	for elementIndex, element := range slide.Elements {
		if standardElementKind(element.Kind) && !hidden[elementIndex] {
			if len(hidden) > 0 && element.Kind != "connector" {
				id := element.ID
				if id == "" {
					id = fmt.Sprintf("legacy-%d-%d", index, elementIndex)
				}
				if frame, ok := publicFrames[id]; ok {
					_, rows := authoredRenderSize(245, 56)
					element = participantFixedOrigin(element, frame, rows, deck.physicalCanvas())
				}
			}
			visible = append(visible, element)
		}
	}
	slide.Elements = visible // Unknown authored payloads are not public rendering data.
	for i := range slide.Elements {
		e := &slide.Elements[i]
		e.Extra = nil
		e.Query = participantElementQuery(*e)
		query, _ := url.ParseQuery(e.Query)
		if e.Inherited {
			query.Set("participant-inherited", "1")
		}
		if e.Kind == "page-number" {
			query.Set("participant-kind", "page-number")
		}
		e.Query = query.Encode()
		e.Inherited, e.Placeholder, e.MasterSlotID = false, false, ""
		if e.Kind == "image" && e.AssetID == "" {
			e.Kind, e.Text, e.Path = "text", "[IMG]", ""
		}
	}
	return serializeDeck("Presentation.md", Deck{
		Slides:         []Slide{slide},
		Appearance:     participantAppearance(deck.Appearance),
		PageSize:       deck.PageSize.Clone(),
		Assets:         participantMediaAssets(deck.Assets),
		Objects:        participantSlideObjects(deck.Objects, slide),
		Theme:          participantTheme(deck.Theme),
		Typefaces:      cloneTypefaces(deck.Typefaces, false),
		HideActivityQR: deck.HideActivityQR,
	})
}

// Rendering policy is part of the public page, not private authoring data.
// Omitting v3 silently reinterprets concrete fonts/shapes as legacy Retro.
// Legacy profiles expose only their implemented text-scale field, never opaque
// future profile scopes or extension data from the source document.
func participantAppearance(source *DeckAppearance) *DeckAppearance {
	if source == nil {
		return nil
	}
	result := &DeckAppearance{Version: source.Version, Mode: source.Mode, DefaultStyle: source.DefaultStyle}
	if source.Version >= 3 {
		return result
	}
	profiles, err := decodeAppearanceProfiles(source.Extra["profiles"])
	if err != nil {
		return result
	}
	public := map[string]map[string]json.RawMessage{}
	for _, mode := range []string{"retro", "modern"} {
		if raw := profiles[mode]; len(raw) > 0 {
			profile, err := decodeAppearanceProfile(raw)
			if err == nil && len(profile["textScale"]) > 0 {
				public[mode] = map[string]json.RawMessage{"textScale": profile["textScale"]}
			}
		}
	}
	if len(public) > 0 {
		raw, _ := json.Marshal(public)
		result.Extra = map[string]json.RawMessage{"profiles": raw}
	}
	return result
}

// The legacy Markdown adapter needs the visible slide's authored payloads,
// but never the rest of the archive, timelines or compatible authoring extras.
// Own the copies before removing fields so this cannot change editor/history.
func participantSlideObjects(source *DeckObjects, slide Slide) *DeckObjects {
	if source == nil {
		return nil
	}
	result := &DeckObjects{Version: source.Version, Tables: map[string]TableData{}, Charts: map[string]ChartData{}}
	for _, element := range slide.Elements {
		switch element.Kind {
		case "table":
			value, ok := source.Tables[element.ObjectID]
			// Reference validation reports missing payloads.
			if !ok {
				continue
			}
			value = cloneTable(value)
			normalizeTableBorderFragments(&value)
			value.Extra, value.Style.Extra, value.Style.Cell.Extra = nil, nil, nil
			for side, border := range value.Style.Cell.Borders {
				border.Extra = nil
				value.Style.Cell.Borders[side] = border
			}
			for i := range value.Rows {
				value.Rows[i].Extra = nil
			}
			for i := range value.Columns {
				value.Columns[i].Extra = nil
			}
			for r := range value.Cells {
				for c := range value.Cells[r] {
					cell := &value.Cells[r][c]
					cell.Extra = nil
					for _, fragments := range cell.BorderFragments {
						for i := range fragments {
							fragments[i].Extra = nil
						}
					}
					if cell.Style != nil {
						cell.Style.Extra = nil
						for side, border := range cell.Style.Borders {
							border.Extra = nil
							cell.Style.Borders[side] = border
						}
					}
					for i := range cell.Runs {
						cell.Runs[i].Extra = nil
					}
					// Covered cells have no public content or style of their own.
					if cell.Hidden {
						cell.Text, cell.Runs, cell.Style = "", nil, nil
					}
				}
			}
			result.Tables[element.ObjectID] = value
		case "chart":
			value, ok := source.Charts[element.ObjectID]
			if !ok {
				continue
			}
			value = cloneChart(value)
			value.Extra, value.Options.Extra = nil, nil
			for i := range value.Series {
				value.Series[i].Extra = nil
			}
			for i := range value.CategoryColors {
				value.CategoryColors[i].Extra = nil
			}
			result.Charts[element.ObjectID] = value
		}
	}
	if len(result.Tables) == 0 && len(result.Charts) == 0 {
		return nil
	}
	return result
}

// Keep only the supported appearance tokens. Table cells can inherit body
// typography, so dropping the theme would silently alter their public layout.
func participantTheme(source *DeckTheme) *DeckTheme {
	result := source.Clone()
	if result == nil {
		return nil
	}
	result.Name, result.Extra = "", nil
	result.Colors.Extra, result.Fonts.Extra, result.Defaults.Extra = nil, nil, nil
	result.Fonts.Heading.Extra, result.Fonts.Body.Extra, result.Fonts.Mono.Extra = nil, nil, nil
	return result
}

func restoreParticipantLayers(slide *Slide) {
	for i := range slide.Elements {
		e := &slide.Elements[i]
		query, _ := url.ParseQuery(e.Query)
		e.Inherited = query.Get("participant-inherited") == "1"
		if query.Get("participant-kind") == "page-number" {
			e.Kind = "page-number"
		}
		query.Del("participant-inherited")
		query.Del("participant-kind")
		e.Query = query.Encode()
	}
}

// Current page documents need the same theme/font/physical-canvas projection
// as the editor. Raw legacy pages retain their existing multipage adapter.
func participantMarkdownExportPages(deck Deck, slideIndex, slideCount, cols, rows int) []exportPage {
	slide := deck.Slides[0]
	if deck.usesConcreteAppearance() {
		slide = deck.slideRenderPreview(0, cols, rows)
	}
	pages := exportSlidePages(slide, slideIndex, slideCount, cols, rows)
	for i := range pages {
		pages[i] = participantExportPage(pages[i])
	}
	return pages
}

// Keep standalone HTML playback intact: this extra projection belongs only to
// participant broadcasts (including participant slide tabs and activity pages).
// Own the modified envelopes; the shared editor scene must retain its videos.
func participantExportPage(page exportPage) exportPage {
	page = audienceExportPage(page)
	if page.Scene == nil {
		return page
	}
	for _, object := range page.Scene.Objects {
		if object.Kind == "video" || object.Video != nil {
			scene := *page.Scene
			scene.Objects = make([]sceneObject, 0, len(page.Scene.Objects))
			for _, candidate := range page.Scene.Objects {
				if candidate.Kind != "video" && candidate.Video == nil {
					scene.Objects = append(scene.Objects, candidate)
				}
			}
			page.Scene = &scene
			break
		}
	}
	return page
}

func participantRenderedDeck(deck Deck, index int) (exportDeck, error) {
	cols, rows := authoredRenderSize(245, 56)
	scene, err := buildDocumentSlideScene(deck, index, cols, rows)
	if err != nil {
		return exportDeck{}, err
	}
	// The typed projection excludes notes, definitions and private results.
	// All treatments use the same projection, including headerless decks.
	page := exportPage{Scene: &scene, Slide: index, PageCount: 1, SlideCount: len(deck.Slides), Lines: []exportLine{}, BG: scene.Background, HideChromePageNumber: true, HideActivityQR: deck.HideActivityQR}
	resolved := deck.ResolveSlide(index, false)
	page.Effect, page.Background = resolved.Effect, resolved.Background
	page.FG = ansiCSSColour(slideFG(resolved))
	page.BackgroundLines = staticBackgroundExportLines(resolved.Background, cols, rows, page.FG, slideBG(resolved))
	return exportDeck{Cols: cols, Rows: rows, Pages: []exportPage{participantExportPage(page)}}, nil
}

func (s *nativeEditorSession) handleParticipantPage(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.URL.Query().Get("slide"))
	if r.Method != http.MethodGet || err != nil {
		http.Error(w, "invalid slide", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	deck, version := cloneDeck(s.deck), s.version
	s.mu.RUnlock()
	if r.URL.Query().Get("format") == "rendered" {
		rendered, err := participantRenderedDeck(deck, index)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"rendered": rendered, "version": version, "slide": index, "slideCount": len(deck.Slides)})
		return
	}
	data, err := participantSlideMarkdown(deck, index)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"markdown": string(data), "version": version, "slide": index, "slideCount": len(deck.Slides)})
}
