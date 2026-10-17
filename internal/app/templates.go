package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Templates are ordinary, portable Markdown documents. They are never opened
// as a writable source document; selection creates an in-memory Untitled.
var presentationTemplates = embeddedFiles

type templatePreviewDocument struct {
	Version   int                    `json:"version"`
	Sources   string                 `json:"sources"`
	Templates []presentationTemplate `json:"templates"`
}

var templatePreviewCache struct {
	sync.Once
	data []byte
	err  error
}

func templateSourceHash() (string, error) {
	hash := sha256.New()
	for _, entry := range presentationTemplateCatalog {
		data, err := templateMarkdown(entry.ID)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00", entry.ID, len(data))
		_, _ = hash.Write(data)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// Called only by the offline build generator and tests, never by an editor
// request. The existing parser is process-global; use it before a live session.
func generateTemplatePreviews() ([]byte, error) {
	cols, rows, order := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = cols, rows, order
	}()
	sources, err := templateSourceHash()
	if err != nil {
		return nil, err
	}
	catalog := append([]presentationTemplate(nil), presentationTemplateCatalog...)
	for i := range catalog {
		deck, err := loadPresentationTemplate(catalog[i].ID)
		if err != nil {
			return nil, err
		}
		scene, err := buildSlideScene(deck, 0, 245, 56)
		if err != nil {
			return nil, err
		}
		catalog[i].Scene = &scene
	}
	return json.MarshalIndent(templatePreviewDocument{Version: 1, Sources: sources, Templates: catalog}, "", "  ")
}

func cachedTemplatePreviews() ([]byte, error) {
	templatePreviewCache.Do(func() {
		data, err := presentationTemplates.ReadFile("assets/templates/previews.json")
		var document templatePreviewDocument
		if err == nil {
			err = json.Unmarshal(data, &document)
		}
		if err == nil {
			sources, hashErr := templateSourceHash()
			if hashErr != nil {
				err = hashErr
			} else if document.Version != 1 || document.Sources != sources || len(document.Templates) != len(presentationTemplateCatalog) {
				err = fmt.Errorf("template previews need rebuilding")
			}
		}
		if err == nil {
			templatePreviewCache.data, err = json.Marshal(document.Templates)
		}
		templatePreviewCache.err = err
	})
	return templatePreviewCache.data, templatePreviewCache.err
}

type presentationTemplate struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Scene       *slideScene `json:"scene,omitempty"`
}

var presentationTemplateCatalog = []presentationTemplate{
	{ID: "blank", Name: "Blank", Description: "An empty canvas with clean typography."},
	{ID: "clean-light", Name: "Clean light", Description: "Clear ideas on a crisp, light canvas."},
	{ID: "clean-dark", Name: "Clean dark", Description: "Calm contrast for focused stories."},
	{ID: "editorial", Name: "Editorial", Description: "Warm paper tones and considered typography."},
	{ID: "bold", Name: "Bold colour", Description: "Confident colour for memorable talks."},
	{ID: "workshop", Name: "Workshop", Description: "A practical starting point for learning together."},
	{ID: "c64", Name: "C64-inspired", Description: "Classic computer character, modern editing."},
}

func templateMarkdown(id string) ([]byte, error) {
	for _, entry := range presentationTemplateCatalog {
		if entry.ID == id {
			return presentationTemplates.ReadFile("assets/templates/" + id + ".md")
		}
	}
	return nil, fmt.Errorf("unknown presentation template %q", id)
}

func loadPresentationTemplate(id string) (Deck, error) {
	data, err := templateMarkdown(id)
	if err != nil {
		return Deck{}, err
	}
	return parseDeckData("Untitled.md", data)
}

func (s *nativeEditorSession) handleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if id := r.URL.Query().Get("id"); id != "" {
		data, err := templateMarkdown(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			Content string `json:"content"`
		}{string(data)})
		return
	}
	data, err := cachedTemplatePreviews()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(data)
}
