//go:build js && wasm

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall/js"
)

type wasmResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType,omitempty"`
	Body        string `json:"body"`
}

type wasmWorkspace struct {
	Cols  int          `json:"cols"`
	Rows  int          `json:"rows"`
	Pages []exportPage `json:"pages"`
}

var wasmCallbacks []js.Func

func wasmJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		data, _ = json.Marshal(wasmResponse{Status: http.StatusInternalServerError, Body: err.Error()})
	}
	return string(data)
}

func wasmEditorInit(_ js.Value, args []js.Value) any {
	if len(args) == 0 {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "missing Markdown document"})
	}
	source := args[0].String()
	path := "Untitled.md"
	if len(args) > 1 && strings.TrimSpace(args[1].String()) != "" {
		path = args[1].String()
	}
	untitled := true
	if len(args) > 2 {
		untitled = args[2].Bool()
	}
	deck, err := parseDeckData(path, []byte(source))
	if err != nil {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: err.Error()})
	}
	ensureDefaultAuthoredSize()
	if len(deck.Slides) == 0 {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "deck has no slides"})
	}
	activeNativeEditor = newNativeEditorSession(path, deck, untitled, parsedDeckElementOrderChanged)
	return wasmJSON(wasmResponse{Status: http.StatusOK, ContentType: "application/json", Body: wasmJSON(activeNativeEditor.state())})
}

func wasmEditorHandler(path string) http.HandlerFunc {
	if activeNativeEditor == nil {
		return nil
	}
	switch path {
	case "/api/editor/state":
		return activeNativeEditor.handleState
	case "/api/editor/action":
		return activeNativeEditor.handleAction
	case "/api/editor/preview":
		return activeNativeEditor.handlePreview
	case "/api/editor/connector-preview":
		return activeNativeEditor.handleConnectorPreview
	case "/api/editor/fit-text":
		return activeNativeEditor.handleFitText
	case "/api/editor/normalize-text-kind":
		return activeNativeEditor.handleNormalizeTextKind
	case "/api/editor/emojis":
		return activeNativeEditor.handleEmojiCatalog
	case "/api/editor/activity-qr":
		return activeNativeEditor.handleActivityQR
	case "/api/editor/fonts/default":
		return activeNativeEditor.handleDefaultFont
	case "/api/editor/fonts/library":
		return activeNativeEditor.handleFontLibrary
	case "/api/editor/workspace":
		return activeNativeEditor.handleWorkspace
	case "/api/editor/upload":
		return activeNativeEditor.handleUpload
	case "/api/editor/document":
		return activeNativeEditor.handleDocument
	case "/api/editor/participant-page":
		return activeNativeEditor.handleParticipantPage
	case "/api/editor/export-document":
		return activeNativeEditor.handleExportDocument
	default:
		return nil
	}
}

func wasmEditorRequest(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "invalid editor request"})
	}
	method, target := args[0].String(), args[1].String()
	body := ""
	if len(args) > 2 {
		body = args[2].String()
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "invalid editor URL"})
	}
	handler := wasmEditorHandler(parsed.Path)
	if handler == nil {
		status := http.StatusNotFound
		message := "editor is not initialized"
		if activeNativeEditor != nil {
			message = "editor endpoint not found"
		}
		return wasmJSON(wasmResponse{Status: status, Body: message})
	}
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	result := recorder.Result()
	contentType := result.Header.Get("Content-Type")
	return wasmJSON(wasmResponse{Status: result.StatusCode, ContentType: contentType, Body: recorder.Body.String()})
}

func wasmEditorUpload(_ js.Value, args []js.Value) any {
	if activeNativeEditor == nil || len(args) == 0 {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "editor is not initialized"})
	}
	data := make([]byte, args[0].Get("byteLength").Int())
	js.CopyBytesToGo(data, args[0])
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "image")
	if err == nil {
		_, err = part.Write(data)
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: err.Error()})
	}
	request := httptest.NewRequest(http.MethodPost, "/api/editor/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	activeNativeEditor.handleUpload(recorder, request)
	return wasmJSON(wasmResponse{Status: recorder.Code, ContentType: recorder.Header().Get("Content-Type"), Body: recorder.Body.String()})
}

func currentWASMWorkspace() (wasmWorkspace, error) {
	if activeNativeEditor == nil {
		return wasmWorkspace{}, fmt.Errorf("editor is not initialized")
	}
	cols, rows := authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
	activeNativeEditor.mu.RLock()
	deck := cloneDeck(activeNativeEditor.deck)
	masterMode, currentMaster := activeNativeEditor.masterMode, activeNativeEditor.currentMaster
	activeNativeEditor.mu.RUnlock()
	workspace := wasmWorkspace{Cols: cols, Rows: rows}
	if masterMode {
		if currentMaster < 0 || currentMaster > len(deck.Masters.Layouts) {
			return wasmWorkspace{}, errInvalidEditorAction
		}
		workspace.Pages = exportSlidePages(masterViewPreview(deck.Masters, currentMaster), currentMaster, len(deck.Masters.Layouts)+1, cols, rows)
		return workspace, nil
	}
	resolved := deck.ResolvedSlides()
	for slideIndex, slide := range resolved {
		workspace.Pages = append(workspace.Pages, exportSlidePages(slide, slideIndex, len(resolved), cols, rows)...)
	}
	return workspace, nil
}

func wasmEditorWorkspace(_ js.Value, _ []js.Value) any {
	workspace, err := currentWASMWorkspace()
	if err != nil {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: err.Error()})
	}
	return wasmJSON(wasmResponse{Status: http.StatusOK, ContentType: "application/json", Body: wasmJSON(workspace)})
}

func registerWASMFunction(name string, fn func(js.Value, []js.Value) any) {
	callback := js.FuncOf(fn)
	wasmCallbacks = append(wasmCallbacks, callback)
	js.Global().Set(name, callback)
}

func main() {
	registerWASMFunction("keynopeWasmRenderParticipantPage", wasmRenderParticipantPage)
	registerWASMFunction("keynopeWasmInit", wasmEditorInit)
	registerWASMFunction("keynopeWasmRequest", wasmEditorRequest)
	registerWASMFunction("keynopeWasmUpload", wasmEditorUpload)
	registerWASMFunction("keynopeWasmWorkspace", wasmEditorWorkspace)
	js.Global().Set("keynopeWasmReady", true)
	select {}
}

func wasmRenderParticipantPage(_ js.Value, args []js.Value) any {
	if len(args) < 4 || len(args[0].String()) > 16<<20 {
		return wasmJSON(wasmResponse{Status: 400, Body: "invalid page document"})
	}
	deck, err := parseDeckData("Presentation.md", []byte(args[0].String()))
	if err != nil || len(deck.Slides) != 1 {
		return wasmJSON(wasmResponse{Status: 400, Body: "invalid page Markdown"})
	}
	ensureDefaultAuthoredSize()
	cols, rows := authoredRenderSize(245, 56)
	if cols > 1024 || rows > 512 {
		return wasmJSON(wasmResponse{Status: 400, Body: "Page dimensions too large"})
	}
	pageIndex, slideIndex, slideCount := args[1].Int(), args[2].Int(), args[3].Int()
	restoreParticipantLayers(&deck.Slides[0])
	pages := exportSlidePages(deck.Slides[0], slideIndex, slideCount, cols, rows)
	if pageIndex < 0 || pageIndex >= len(pages) {
		return wasmJSON(wasmResponse{Status: 400, Body: "Page out of range"})
	}
	payload, err := json.Marshal(exportDeck{Cols: cols, Rows: rows, Pages: []exportPage{pages[pageIndex]}})
	if err != nil {
		return wasmJSON(wasmResponse{Status: 500, Body: err.Error()})
	}
	html := exportHTMLPrefix(preservedExportHead{}, false) + "<script id=\"keynope-data\" type=\"application/json\">" + string(payload) + "</script>" + exportHTMLSuffix()
	return wasmJSON(wasmResponse{Status: 200, Body: html})
}
