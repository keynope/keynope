package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

//go:embed web/participant-transfer.js
var participantTransferJS string

// Resolve only the selected slide. Never disclose notes, activity answers or
// unused masters/assets from the remainder of the deck to participants.
func participantSlideMarkdown(deck Deck, index int) ([]byte, error) {
	if index < 0 || index >= len(deck.Slides) {
		return nil, fmt.Errorf("slide out of range")
	}
	slide := cloneSlide(deck.ResolvedSlides()[index])
	slide.Notes, slide.LayoutID, slide.Engagement = "", "", nil
	slide.EngagementResult = nil
	slide.Extra = nil // Compatible authoring extensions are not participant data.
	slide.TabID = ""  // Explicit tab requests render normally, outside the slide show.
	for i := range slide.Elements {
		e := &slide.Elements[i]
		e.Extra = nil
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
		Assets:         deck.Assets,
		HideActivityQR: deck.HideActivityQR,
	})
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
	return exportDeck{Cols: cols, Rows: rows, Pages: []exportPage{page}}, nil
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
