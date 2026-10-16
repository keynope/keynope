package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
)

type fontExportApproval struct {
	Owner        string            `json:"owner"`
	Version      int64             `json:"version"`
	Format       string            `json:"format"`
	Confirmed    bool              `json:"confirmed"`
	Replacements map[string]string `json:"replacements"`
}

type fontExportName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var errFontExportStale = errors.New("The deck changed while capturing artwork; export again.")

func validFontExportFormat(format string) bool {
	return format == "save" || format == "html" || format == "pdf" || format == "pptx" || format == "png"
}

// Inspect actual resolved consumers, not every font in the document library or
// unused masters. Unrestricted decks avoid any extra scene projection work.
func restrictedExportFonts(deck Deck, format string) ([]fontExportName, error) {
	result := []fontExportName{}
	if format == "png" {
		return result, nil
	} // Raster pixels contain no font file.
	potential := map[string]bool{}
	for id, font := range deck.Typefaces {
		if font.referenceOnly() {
			potential[id] = true
		}
	}
	if len(potential) == 0 {
		return result, nil
	}
	used := map[string]bool{}
	for index, slide := range deck.Slides {
		if format != "html" && format != "save" && slide.TabID != "" {
			continue
		}
		scene, err := buildDocumentSlideScene(deck, index, 245, 56)
		if err != nil {
			return nil, err
		}
		for id := range scene.Typefaces {
			if potential[id] {
				used[id] = true
			}
		}
	}
	for id := range used {
		result = append(result, fontExportName{ID: id, Name: deck.Typefaces[id].Name})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (s *nativeEditorSession) handleFontExport(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		format := r.URL.Query().Get("format")
		if !validFontExportFormat(format) {
			http.Error(w, "invalid export format", 400)
			return
		}
		s.mu.Lock()
		s.fontExport = nil // A new export review supersedes any abandoned approval.
		deck, version, owner := cloneDeckForRender(s.deck), s.version, s.notesSessionID
		s.mu.Unlock()
		fonts, err := restrictedExportFonts(deck, format)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			Owner   string           `json:"owner"`
			Version int64            `json:"version"`
			Fonts   []fontExportName `json:"fonts"`
		}{owner, version, fonts})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var decision fontExportApproval
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&decision) != nil || !validFontExportFormat(decision.Format) {
		http.Error(w, "invalid font export choice", 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if decision.Owner != s.notesSessionID || decision.Version != s.version {
		http.Error(w, "The deck changed. Export again to review its fonts.", 409)
		return
	}
	if !decision.Confirmed && len(decision.Replacements) == 0 {
		http.Error(w, "Confirm using installed-font references or choose replacement fonts.", 400)
		return
	}
	for source, target := range decision.Replacements {
		if _, ok := s.deck.Typefaces[source]; !ok || !typefaceIDRE.MatchString(source) || !validTextFont(target) {
			http.Error(w, "invalid replacement font", 400)
			return
		}
		if typefaceIDRE.MatchString(target) {
			face, ok := s.deck.Typefaces[target]
			if !ok {
				face, ok = s.fontLibrary[target]
			}
			if !ok || face.LocalOnly || !editableTypefaceEmbedding(face.Data) {
				http.Error(w, "Choose an editable, embeddable replacement font.", 400)
				return
			}
		}
	}
	s.fontExport = &decision
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// Approval belongs to this exact document/revision/format and is consumed once.
// A replacement changes only the owned export snapshot, never the user's deck.
func (s *nativeEditorSession) deckForFontExport(format string, captures ...*pptxCaptureRequest) (Deck, string, error) {
	s.mu.Lock()
	if len(captures) > 0 && captures[0] != nil && (captures[0].Owner != s.notesSessionID || captures[0].Version != s.version) {
		s.mu.Unlock()
		return Deck{}, "", errFontExportStale
	}
	deck, path := cloneDeckForRender(s.deck), s.deckPath
	approval := s.fontExport
	s.fontExport = nil
	approved := approval != nil && approval.Owner == s.notesSessionID && approval.Version == s.version && approval.Format == format
	if approved {
		deck.Typefaces = mergeTypefaces(deck.Typefaces, s.fontLibrary)
	}
	s.mu.Unlock()
	if approved && len(approval.Replacements) > 0 {
		replaceExportTypefaces(&deck, approval.Replacements)
	}
	if approved && approval.Confirmed {
		normalizeTypefaceReferences(&deck)
		return deck, path, nil
	}
	fonts, err := restrictedExportFonts(deck, format)
	if err != nil {
		return Deck{}, path, err
	}
	if len(fonts) > 0 {
		return Deck{}, path, fmt.Errorf("Review restricted fonts before exporting: %s", fonts[0].Name)
	}
	normalizeTypefaceReferences(&deck)
	return deck, path, nil
}

func replaceExportTypefaces(deck *Deck, replacements map[string]string) {
	replace := func(id string) string {
		if target, ok := replacements[id]; ok {
			return target
		}
		return id
	}
	var query func(string) string
	query = func(encoded string) string {
		q, err := url.ParseQuery(encoded)
		if err != nil {
			return encoded
		}
		changed := false
		for _, key := range []string{"modern-font", "default-font", "label-modern-font", "label-default-font"} {
			if value := q.Get(key); replace(value) != value {
				q.Set(key, replace(value))
				changed = true
			}
		}
		for _, key := range []string{"modern-runs", "label-modern-runs"} {
			if styles := decodeModernRuns(q.Get(key)); styles != nil {
				different := false
				for i := range styles.Runs {
					old := styles.Runs[i].FontID
					styles.Runs[i].FontID = replace(old)
					different = different || old != styles.Runs[i].FontID
				}
				if different {
					if data, err := json.Marshal(styles); err == nil {
						q.Set(key, base64.StdEncoding.EncodeToString(data))
						changed = true
					}
				}
			}
		}
		if raw, err := base64.StdEncoding.DecodeString(q.Get("shape-label")); err == nil && len(raw) > 0 && len(raw) <= 65536 {
			var label shapeLabelData
			if json.Unmarshal(raw, &label) == nil {
				next := query(label.Query)
				if next != label.Query {
					label.Query = next
					if data, err := json.Marshal(label); err == nil {
						q.Set("shape-label", base64.StdEncoding.EncodeToString(data))
						changed = true
					}
				}
			}
		}
		if !changed {
			return encoded
		}
		return q.Encode()
	}
	slide := func(slide *Slide) {
		for i := range slide.Elements {
			slide.Elements[i].Query = query(slide.Elements[i].Query)
		}
	}
	for i := range deck.Slides {
		slide(&deck.Slides[i])
	}
	slide(&deck.Masters.Base.Slide)
	for i := range deck.Masters.Layouts {
		slide(&deck.Masters.Layouts[i].Slide)
	}
	if deck.Theme != nil {
		deck.Theme.Fonts.Heading.FontID = replace(deck.Theme.Fonts.Heading.FontID)
		deck.Theme.Fonts.Body.FontID = replace(deck.Theme.Fonts.Body.FontID)
		deck.Theme.Fonts.Mono.FontID = replace(deck.Theme.Fonts.Mono.FontID)
	}
	if deck.Objects != nil {
		for id, table := range deck.Objects.Tables {
			table.Style.Cell.FontID = replace(table.Style.Cell.FontID)
			for r := range table.Cells {
				for c := range table.Cells[r] {
					cell := &table.Cells[r][c]
					if cell.Style != nil {
						cell.Style.FontID = replace(cell.Style.FontID)
					}
					for i := range cell.Runs {
						cell.Runs[i].FontID = replace(cell.Runs[i].FontID)
					}
				}
			}
			deck.Objects.Tables[id] = table
		}
		for id, chart := range deck.Objects.Charts {
			chart.Options.FontID = replace(chart.Options.FontID)
			chart.Options.DefaultFontID = replace(chart.Options.DefaultFontID)
			deck.Objects.Charts[id] = chart
		}
	}
}
